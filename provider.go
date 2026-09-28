// Package featbit connects the FeatBit server-side SDK to OpenFeature.
package featbit

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	sdk "github.com/featbit/featbit-go-sdk"
	"github.com/featbit/featbit-go-sdk/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
)

type evaluationClient interface {
	IsInitialized() bool
	Variation(string, interfaces.FBUser, string) (string, interfaces.EvalDetail, error)
	DoubleVariation(string, interfaces.FBUser, float64) (float64, interfaces.EvalDetail, error)
	JsonVariation(string, interfaces.FBUser, any) (any, interfaces.EvalDetail, error)
}

// Provider evaluates flags using an application-owned FeatBit client.
// Create providers with NewProvider. A provider can be shared by concurrent callers.
type Provider struct {
	client evaluationClient
}

var (
	_ openfeature.FeatureProvider = (*Provider)(nil)
	_ openfeature.StateHandler    = (*Provider)(nil)
	_ evaluationClient            = (*sdk.FBClient)(nil)
)

// NewProvider wraps client without taking ownership of it. A nil client returns
// an error. Initialize the client before registering the provider with OpenFeature,
// and close it after the application has stopped evaluating flags.
func NewProvider(client *sdk.FBClient) (*Provider, error) {
	if client == nil {
		return nil, errors.New("featbit: client must not be nil")
	}
	return &Provider{client: client}, nil
}

// Metadata identifies this provider.
func (p *Provider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: "featbit"}
}

// Hooks returns no provider-specific hooks.
func (p *Provider) Hooks() []openfeature.Hook { return nil }

// Init checks readiness without opening connections or waiting for initialization.
func (p *Provider) Init(openfeature.EvaluationContext) error {
	if !p.ready() {
		return &openfeature.ProviderInitError{
			ErrorCode: openfeature.ProviderNotReadyCode,
			Message:   "FeatBit client is not ready",
		}
	}
	return nil
}

// Shutdown is a no-op: the application owns the client and must call its Close
// method once, after draining in-flight evaluations and shutting down OpenFeature.
func (p *Provider) Shutdown() {}

func (p *Provider) ready() bool {
	return p != nil && p.client != nil && p.client.IsInitialized()
}

// BooleanEvaluation resolves a boolean flag.
func (p *Provider) BooleanEvaluation(ctx context.Context, flag string, defaultValue bool, flatCtx openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	// The SDK's BoolVariation silently accepts numeric flags as false. Read the
	// raw variation once and validate it before converting to a boolean.
	raw := resolve(p, ctx, strconv.FormatBool(defaultValue), flatCtx, func(user interfaces.FBUser) (string, interfaces.EvalDetail, error) {
		return p.client.Variation(flag, user, strconv.FormatBool(defaultValue))
	})
	result := openfeature.BoolResolutionDetail{Value: defaultValue, ProviderResolutionDetail: raw.ProviderResolutionDetail}
	if raw.Reason == openfeature.ErrorReason {
		return result
	}
	switch raw.Value {
	case "true":
		result.Value = true
	case "false":
		result.Value = false
	default:
		result.ProviderResolutionDetail = typeMismatch()
	}
	return result
}

// StringEvaluation resolves a string flag.
func (p *Provider) StringEvaluation(ctx context.Context, flag string, defaultValue string, flatCtx openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	return resolve(p, ctx, defaultValue, flatCtx, func(user interfaces.FBUser) (string, interfaces.EvalDetail, error) {
		return p.client.Variation(flag, user, defaultValue)
	})
}

// FloatEvaluation resolves a finite floating-point flag.
func (p *Provider) FloatEvaluation(ctx context.Context, flag string, defaultValue float64, flatCtx openfeature.FlattenedContext) openfeature.FloatResolutionDetail {
	result := resolve(p, ctx, defaultValue, flatCtx, func(user interfaces.FBUser) (float64, interfaces.EvalDetail, error) {
		return p.client.DoubleVariation(flag, user, defaultValue)
	})
	if result.Reason != openfeature.ErrorReason && (math.IsNaN(result.Value) || math.IsInf(result.Value, 0)) {
		result.Value = defaultValue
		result.ProviderResolutionDetail = typeMismatch()
	}
	return result
}

// IntEvaluation resolves an exact int64 value, including on 32-bit systems.
// Fractional and out-of-range values return the supplied default and TYPE_MISMATCH.
func (p *Provider) IntEvaluation(ctx context.Context, flag string, defaultValue int64, flatCtx openfeature.FlattenedContext) openfeature.IntResolutionDetail {
	// IntVariation converts through float64 and int, losing precision and range.
	// Variation preserves the original numeric text and records one evaluation.
	raw := resolve(p, ctx, strconv.FormatInt(defaultValue, 10), flatCtx, func(user interfaces.FBUser) (string, interfaces.EvalDetail, error) {
		return p.client.Variation(flag, user, strconv.FormatInt(defaultValue, 10))
	})
	result := openfeature.IntResolutionDetail{Value: defaultValue, ProviderResolutionDetail: raw.ProviderResolutionDetail}
	if raw.Reason == openfeature.ErrorReason {
		return result
	}
	value, ok := parseInt64(raw.Value)
	if !ok {
		result.ProviderResolutionDetail = typeMismatch()
		return result
	}
	result.Value = value
	return result
}

// ObjectEvaluation resolves a JSON object or array into standard encoding/json
// types: map[string]any, []any, float64, string, bool and nil. On error, it returns
// defaultValue unchanged, including when defaultValue is nil or a typed struct.
func (p *Provider) ObjectEvaluation(ctx context.Context, flag string, defaultValue any, flatCtx openfeature.FlattenedContext) openfeature.InterfaceResolutionDetail {
	result := resolve(p, ctx, defaultValue, flatCtx, func(user interfaces.FBUser) (any, interfaces.EvalDetail, error) {
		// FeatBit uses the default's concrete type to decode JSON. RawMessage
		// handles both objects and arrays without reflecting on a nil default.
		return p.client.JsonVariation(flag, user, json.RawMessage(nil))
	})
	if result.Reason == openfeature.ErrorReason {
		return result
	}
	raw, ok := result.Value.(json.RawMessage)
	if !ok {
		return openfeature.InterfaceResolutionDetail{Value: defaultValue, ProviderResolutionDetail: typeMismatch()}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return openfeature.InterfaceResolutionDetail{
			Value:                    defaultValue,
			ProviderResolutionDetail: errorDetails(openfeature.NewParseErrorResolutionError("flag contains invalid JSON")),
		}
	}
	switch value.(type) {
	case map[string]any, []any:
		result.Value = value
		return result
	default:
		return openfeature.InterfaceResolutionDetail{Value: defaultValue, ProviderResolutionDetail: typeMismatch()}
	}
}

func resolve[T any](p *Provider, ctx context.Context, defaultValue T, flatCtx openfeature.FlattenedContext, evaluate func(interfaces.FBUser) (T, interfaces.EvalDetail, error)) openfeature.GenericResolutionDetail[T] {
	result := openfeature.GenericResolutionDetail[T]{Value: defaultValue}
	if ctx == nil || ctx.Err() != nil {
		result.ProviderResolutionDetail = errorDetails(openfeature.NewGeneralResolutionError("evaluation context is canceled or unavailable"))
		return result
	}
	if !p.ready() {
		result.ProviderResolutionDetail = errorDetails(openfeature.NewProviderNotReadyResolutionError("FeatBit client is not ready"))
		return result
	}
	user, contextErr := evaluationUser(flatCtx)
	if contextErr != (openfeature.ResolutionError{}) {
		result.ProviderResolutionDetail = errorDetails(contextErr)
		return result
	}
	value, detail, err := evaluate(user)
	result.ProviderResolutionDetail = resolutionDetails(detail, err)
	if result.Reason != openfeature.ErrorReason {
		result.Value = value
	}
	return result
}

func typeMismatch() openfeature.ProviderResolutionDetail {
	return errorDetails(openfeature.NewTypeMismatchResolutionError("flag value does not match the requested type"))
}
