// Package featbit connects the FeatBit server-side SDK to OpenFeature.
package featbit

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	sdk "github.com/featbit/featbit-go-sdk/v2"
	"github.com/featbit/featbit-go-sdk/v2/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
)

const (
	providerInitTimeout        = 15 * time.Second
	providerStatusPollInterval = 100 * time.Millisecond
)

type evaluationClient interface {
	IsInitialized() bool
	GetDataUpdateStatusProvider() interfaces.DataUpdateStatusProvider
	Variation(string, interfaces.FBUser, string) (string, interfaces.EvalDetail, error)
	DoubleVariation(string, interfaces.FBUser, float64) (float64, interfaces.EvalDetail, error)
	JsonVariation(string, interfaces.FBUser, any) (any, interfaces.EvalDetail, error)
	Close() error
}

// Option configures a Provider.
type Option func(*Provider)

// WithoutClosingClient keeps the FeatBit client open when the provider shuts
// down. Use it when the client is shared with other providers or application
// code; the application must then close the client itself.
func WithoutClosingClient() Option {
	return func(p *Provider) { p.closeClient = false }
}

// Provider evaluates flags using a FeatBit client.
// Create providers with NewProvider. A provider can be shared by concurrent callers.
type Provider struct {
	client       evaluationClient
	closeClient  bool
	initTimeout  time.Duration
	events       chan openfeature.Event
	shutdown     chan struct{}
	lifecycleMu  sync.Mutex
	shutdownOnce sync.Once
	monitorOnce  sync.Once
	monitorWG    sync.WaitGroup
}

var (
	_ openfeature.FeatureProvider = (*Provider)(nil)
	_ openfeature.StateHandler    = (*Provider)(nil)
	_ openfeature.EventHandler    = (*Provider)(nil)
	_ evaluationClient            = (*sdk.FBClient)(nil)
)

// NewProvider wraps client. A nil client returns an error. Provider
// initialization waits up to 15 seconds for the client to be ready. Provider
// shutdown closes the client unless WithoutClosingClient is given.
func NewProvider(client *sdk.FBClient, opts ...Option) (*Provider, error) {
	if client == nil {
		return nil, errors.New("featbit: client must not be nil")
	}
	p := newProvider(client, providerInitTimeout)
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p, nil
}

func newProvider(client evaluationClient, initTimeout time.Duration) *Provider {
	return &Provider{
		client:      client,
		closeClient: true,
		initTimeout: initTimeout,
		events:      make(chan openfeature.Event, 8),
		shutdown:    make(chan struct{}),
	}
}

// Metadata identifies this provider.
func (p *Provider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: "featbit"}
}

// Hooks returns no provider-specific hooks.
func (p *Provider) Hooks() []openfeature.Hook { return nil }

// Init waits for the FeatBit data source to become operational.
func (p *Provider) Init(openfeature.EvaluationContext) error {
	if p == nil || p.client == nil {
		return &openfeature.ProviderInitError{
			ErrorCode: openfeature.ProviderNotReadyCode,
			Message:   "FeatBit client is not ready",
		}
	}
	select {
	case <-p.shutdown:
		return &openfeature.ProviderInitError{
			ErrorCode: openfeature.ProviderFatalCode,
			Message:   "FeatBit provider is shut down",
		}
	default:
	}
	status := p.client.GetDataUpdateStatusProvider()
	if status == nil {
		if p.ready() {
			return nil
		}
		return &openfeature.ProviderInitError{
			ErrorCode: openfeature.ProviderNotReadyCode,
			Message:   "FeatBit client is not ready",
		}
	}

	ready := status.WaitForOKState(p.initTimeout)
	select {
	case <-p.shutdown:
		return &openfeature.ProviderInitError{
			ErrorCode: openfeature.ProviderFatalCode,
			Message:   "FeatBit provider is shut down",
		}
	default:
	}
	if ready {
		p.startStatusMonitor(status, interfaces.OK)
		return nil
	}

	state := status.GetCurrentState().StateType
	p.startStatusMonitor(status, state)
	if state == interfaces.OFF {
		return &openfeature.ProviderInitError{
			ErrorCode: openfeature.ProviderFatalCode,
			Message:   "FeatBit data source stopped permanently",
		}
	}
	return &openfeature.ProviderInitError{
		ErrorCode: openfeature.ProviderNotReadyCode,
		Message:   "FeatBit client is not ready",
	}
}

// Shutdown stops status monitoring and closes the FeatBit client, unless the
// provider was created with WithoutClosingClient. It is safe to call repeatedly.
func (p *Provider) Shutdown() {
	if p == nil || p.shutdown == nil {
		return
	}
	p.shutdownOnce.Do(func() {
		p.lifecycleMu.Lock()
		close(p.shutdown)
		p.lifecycleMu.Unlock()
		p.monitorWG.Wait()
		if p.closeClient && p.client != nil {
			_ = p.client.Close()
		}
		close(p.events)
	})
}

// EventChannel reports FeatBit data-source status changes to OpenFeature.
func (p *Provider) EventChannel() <-chan openfeature.Event {
	if p == nil {
		return nil
	}
	return p.events
}

func (p *Provider) startStatusMonitor(status interfaces.DataUpdateStatusProvider, state interfaces.StateType) {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	select {
	case <-p.shutdown:
		return
	default:
	}
	p.monitorOnce.Do(func() {
		if state == interfaces.OFF {
			return
		}
		p.monitorWG.Add(1)
		go func() {
			defer p.monitorWG.Done()
			ticker := time.NewTicker(providerStatusPollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-p.shutdown:
					return
				case <-ticker.C:
				}
				current := status.GetCurrentState().StateType
				if current == state {
					continue
				}
				if event, ok := providerStatusEvent(p.Metadata().Name, current); ok {
					p.emit(event)
				}
				state = current
				if current == interfaces.OFF {
					return
				}
			}
		}()
	})
}

func providerStatusEvent(providerName string, state interfaces.StateType) (openfeature.Event, bool) {
	event := openfeature.Event{ProviderName: providerName}
	switch state {
	case interfaces.OK:
		event.EventType = openfeature.ProviderReady
	case interfaces.INTERRUPTED:
		event.EventType = openfeature.ProviderStale
		event.Message = "FeatBit data source is interrupted"
	case interfaces.OFF:
		event.EventType = openfeature.ProviderError
		event.ErrorCode = openfeature.ProviderFatalCode
		event.Message = "FeatBit data source stopped permanently"
	default:
		return openfeature.Event{}, false
	}
	return event, true
}

func (p *Provider) emit(event openfeature.Event) {
	select {
	case p.events <- event:
	case <-p.shutdown:
	}
}

func (p *Provider) ready() bool {
	return p != nil && p.client != nil && p.client.IsInitialized()
}

// BooleanEvaluation resolves a boolean flag.
func (p *Provider) BooleanEvaluation(ctx context.Context, flag string, defaultValue bool, flatCtx openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	// The SDK's BoolVariation silently accepts numeric flags as false. Read the
	// raw variation once and validate it before converting to a boolean.
	fallback := strconv.FormatBool(defaultValue)
	return resolve(p, ctx, defaultValue, flatCtx, func(client evaluationClient, user interfaces.FBUser) (string, interfaces.EvalDetail, error) {
		return client.Variation(flag, user, fallback)
	}, booleanValue)
}

// StringEvaluation resolves a string flag.
func (p *Provider) StringEvaluation(ctx context.Context, flag string, defaultValue string, flatCtx openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	return resolve(p, ctx, defaultValue, flatCtx, func(client evaluationClient, user interfaces.FBUser) (string, interfaces.EvalDetail, error) {
		return client.Variation(flag, user, defaultValue)
	}, stringValue)
}

// FloatEvaluation resolves a finite floating-point flag.
func (p *Provider) FloatEvaluation(ctx context.Context, flag string, defaultValue float64, flatCtx openfeature.FlattenedContext) openfeature.FloatResolutionDetail {
	return resolve(p, ctx, defaultValue, flatCtx, func(client evaluationClient, user interfaces.FBUser) (float64, interfaces.EvalDetail, error) {
		return client.DoubleVariation(flag, user, defaultValue)
	}, floatValue)
}

// IntEvaluation resolves an exact int64 value, including on 32-bit systems.
// Fractional and out-of-range values return the supplied default and TYPE_MISMATCH.
func (p *Provider) IntEvaluation(ctx context.Context, flag string, defaultValue int64, flatCtx openfeature.FlattenedContext) openfeature.IntResolutionDetail {
	// IntVariation converts through float64 and int, losing precision and range.
	// Variation preserves the original numeric text and records one evaluation.
	fallback := strconv.FormatInt(defaultValue, 10)
	return resolve(p, ctx, defaultValue, flatCtx, func(client evaluationClient, user interfaces.FBUser) (string, interfaces.EvalDetail, error) {
		return client.Variation(flag, user, fallback)
	}, integerValue)
}

// ObjectEvaluation resolves a JSON object or array into standard encoding/json
// types: map[string]any, []any, float64, string, bool and nil. On error, it returns
// defaultValue unchanged, including when defaultValue is nil or a typed struct.
func (p *Provider) ObjectEvaluation(ctx context.Context, flag string, defaultValue any, flatCtx openfeature.FlattenedContext) openfeature.InterfaceResolutionDetail {
	return resolve(p, ctx, defaultValue, flatCtx, func(client evaluationClient, user interfaces.FBUser) (any, interfaces.EvalDetail, error) {
		// FeatBit uses the default's concrete type to decode JSON. RawMessage
		// handles both objects and arrays without reflecting on a nil default.
		return client.JsonVariation(flag, user, json.RawMessage(nil))
	}, objectValue)
}
