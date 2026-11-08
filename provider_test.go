package featbit

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/featbit/featbit-go-sdk/v2"
	"github.com/featbit/featbit-go-sdk/v2/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

type stubClient struct {
	initialized bool
	status      interfaces.DataUpdateStatusProvider
	text        string
	number      float64
	object      any
	detail      interfaces.EvalDetail
	err         error
	calls       int
	jsonDefault any
	closeCalls  int
}

func (s *stubClient) Close() error {
	s.closeCalls++
	return nil
}

func (s *stubClient) IsInitialized() bool { return s.initialized }
func (s *stubClient) GetDataUpdateStatusProvider() interfaces.DataUpdateStatusProvider {
	return s.status
}
func (s *stubClient) Variation(string, interfaces.FBUser, string) (string, interfaces.EvalDetail, error) {
	s.calls++
	return s.text, s.detail, s.err
}
func (s *stubClient) DoubleVariation(string, interfaces.FBUser, float64) (float64, interfaces.EvalDetail, error) {
	s.calls++
	return s.number, s.detail, s.err
}
func (s *stubClient) JsonVariation(_ string, _ interfaces.FBUser, fallback any) (any, interfaces.EvalDetail, error) {
	s.calls++
	s.jsonDefault = fallback
	return s.object, s.detail, s.err
}

type stubStatusProvider struct {
	mu      sync.Mutex
	state   interfaces.State
	changed chan struct{}
}

func newStubStatusProvider(state interfaces.StateType) *stubStatusProvider {
	return &stubStatusProvider{state: interfaces.State{StateType: state}, changed: make(chan struct{})}
}

func (s *stubStatusProvider) GetCurrentState() interfaces.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *stubStatusProvider) WaitFor(target interfaces.StateType, timeout time.Duration) bool {
	var timer *time.Timer
	var timeoutC <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutC = timer.C
		defer timer.Stop()
	}
	for {
		s.mu.Lock()
		state := s.state.StateType
		changed := s.changed
		s.mu.Unlock()
		if state == target {
			return true
		}
		if state == interfaces.OFF {
			return false
		}
		select {
		case <-changed:
		case <-timeoutC:
			return false
		}
	}
}

func (s *stubStatusProvider) WaitForOKState(timeout time.Duration) bool {
	return s.WaitFor(interfaces.OK, timeout)
}

func (s *stubStatusProvider) Close() error { return nil }

func (s *stubStatusProvider) setState(state interfaces.StateType) {
	s.mu.Lock()
	s.state = interfaces.State{StateType: state}
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

func TestProviderInitialization(t *testing.T) {
	if p, err := NewProvider(nil); err == nil || p != nil {
		t.Fatalf("nil client: provider=%v, err=%v", p, err)
	}
	p, err := NewProvider(&sdk.FBClient{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*Provider{nil, {}, p} {
		var initErr *openfeature.ProviderInitError
		if err := p.Init(openfeature.EvaluationContext{}); !errors.As(err, &initErr) || initErr.ErrorCode != openfeature.ProviderNotReadyCode {
			t.Fatalf("unready provider: %v", err)
		}
		p.Shutdown()
		p.Shutdown()
	}
	ready := newProvider(&stubClient{
		initialized: true,
		status:      newStubStatusProvider(interfaces.OK),
	}, time.Second)
	if err := ready.Init(openfeature.EvaluationContext{}); err != nil {
		t.Fatal(err)
	}
	ready.Shutdown()
}

func TestProviderShutdownClosesClient(t *testing.T) {
	owned := &stubClient{}
	p := newProvider(owned, time.Second)
	p.Shutdown()
	p.Shutdown()
	if owned.closeCalls != 1 {
		t.Fatalf("owned client closed %d times; want 1", owned.closeCalls)
	}

	shared := &stubClient{}
	p = newProvider(shared, time.Second)
	WithoutClosingClient()(p)
	p.Shutdown()
	if shared.closeCalls != 0 {
		t.Fatalf("shared client closed %d times; want 0", shared.closeCalls)
	}
}

func TestProviderStatusMapping(t *testing.T) {
	status := newStubStatusProvider(interfaces.INITIALIZING)
	p := newProvider(&stubClient{status: status}, 10*time.Millisecond)
	err := p.Init(openfeature.EvaluationContext{})
	var initErr *openfeature.ProviderInitError
	if !errors.As(err, &initErr) || initErr.ErrorCode != openfeature.ProviderNotReadyCode {
		t.Fatalf("initializing client: got %v, want PROVIDER_NOT_READY", err)
	}

	for _, tc := range []struct {
		state interfaces.StateType
		event openfeature.EventType
		code  openfeature.ErrorCode
	}{
		{interfaces.OK, openfeature.ProviderReady, ""},
		{interfaces.INTERRUPTED, openfeature.ProviderStale, ""},
		{interfaces.OK, openfeature.ProviderReady, ""},
		{interfaces.OFF, openfeature.ProviderError, openfeature.ProviderFatalCode},
	} {
		status.setState(tc.state)
		select {
		case event := <-p.EventChannel():
			if event.EventType != tc.event || event.ProviderName != "featbit" || event.ErrorCode != tc.code {
				t.Fatalf("event=%+v, want type %s and code %s", event, tc.event, tc.code)
			}
			if tc.state == interfaces.INTERRUPTED && event.Message != "FeatBit data source is interrupted" {
				t.Fatalf("stale event exposed an unexpected message: %q", event.Message)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s event", tc.event)
		}
	}

	p.Shutdown()
	if _, ok := <-p.EventChannel(); ok {
		t.Fatal("event channel remained open after shutdown")
	}
}

func TestProviderInitializationFatalStatus(t *testing.T) {
	p := newProvider(&stubClient{status: newStubStatusProvider(interfaces.OFF)}, time.Second)
	err := p.Init(openfeature.EvaluationContext{})
	var initErr *openfeature.ProviderInitError
	if !errors.As(err, &initErr) || initErr.ErrorCode != openfeature.ProviderFatalCode {
		t.Fatalf("stopped client: got %v, want PROVIDER_FATAL", err)
	}
	p.Shutdown()
}

func TestOpenFeatureProviderStatusTracksFeatBit(t *testing.T) {
	status := newStubStatusProvider(interfaces.OK)
	p := newProvider(&stubClient{status: status}, time.Second)
	api := isolated.NewAPI()
	t.Cleanup(func() {
		if err := api.Shutdown(t.Context()); err != nil {
			t.Error(err)
		}
	})
	if err := api.SetProviderAndWait(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	client := api.NewClient()
	requireProviderState(t, client, openfeature.ReadyState)

	for _, tc := range []struct {
		sdkState         interfaces.StateType
		openFeatureState openfeature.State
	}{
		{interfaces.INTERRUPTED, openfeature.StaleState},
		{interfaces.OK, openfeature.ReadyState},
		{interfaces.OFF, openfeature.FatalState},
	} {
		status.setState(tc.sdkState)
		requireProviderState(t, client, tc.openFeatureState)
	}
}

func requireProviderState(t *testing.T, client *openfeature.Client, want openfeature.State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for client.State() != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := client.State(); got != want {
		t.Fatalf("provider state=%s, want %s", got, want)
	}
}

func TestEvaluationFailuresPreserveDefaults(t *testing.T) {
	fallbackObject := map[string]any{"nested": []any{"unchanged"}}
	type result struct {
		value any
		openfeature.ProviderResolutionDetail
	}
	evaluations := []struct {
		name     string
		fallback any
		call     func(*Provider, context.Context, openfeature.FlattenedContext) result
	}{
		{"boolean", true, func(p *Provider, ctx context.Context, ec openfeature.FlattenedContext) result {
			r := p.BooleanEvaluation(ctx, "flag", true, ec)
			return result{r.Value, r.ProviderResolutionDetail}
		}},
		{"string", "fallback", func(p *Provider, ctx context.Context, ec openfeature.FlattenedContext) result {
			r := p.StringEvaluation(ctx, "flag", "fallback", ec)
			return result{r.Value, r.ProviderResolutionDetail}
		}},
		{"integer", int64(math.MaxInt64), func(p *Provider, ctx context.Context, ec openfeature.FlattenedContext) result {
			r := p.IntEvaluation(ctx, "flag", math.MaxInt64, ec)
			return result{r.Value, r.ProviderResolutionDetail}
		}},
		{"float", 123.5, func(p *Provider, ctx context.Context, ec openfeature.FlattenedContext) result {
			r := p.FloatEvaluation(ctx, "flag", 123.5, ec)
			return result{r.Value, r.ProviderResolutionDetail}
		}},
		{"object", fallbackObject, func(p *Provider, ctx context.Context, ec openfeature.FlattenedContext) result {
			r := p.ObjectEvaluation(ctx, "flag", fallbackObject, ec)
			return result{r.Value, r.ProviderResolutionDetail}
		}},
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, evaluation := range evaluations {
		for _, failure := range []struct {
			name string
			code openfeature.ErrorCode
		}{
			{"nil provider", openfeature.ProviderNotReadyCode},
			{"not initialized", openfeature.ProviderNotReadyCode},
			{"canceled", openfeature.GeneralCode},
			{"nil context", openfeature.GeneralCode},
			{"no targeting key", openfeature.TargetingKeyMissingCode},
			{"invalid attribute", openfeature.InvalidContextCode},
			{"SDK error", openfeature.GeneralCode},
			{"detail error without SDK error", openfeature.FlagNotFoundCode},
		} {
			t.Run(evaluation.name+"/"+failure.name, func(t *testing.T) {
				client := &stubClient{initialized: true, text: "unexpected", number: 99, object: json.RawMessage(`{"unexpected":true}`)}
				p := newProvider(client, providerInitTimeout)
				ctx := context.Background()
				ec := openfeature.FlattenedContext{openfeature.TargetingKey: "user"}
				wantCalls := 0
				switch failure.name {
				case "nil provider":
					p = nil
				case "not initialized":
					client.initialized = false
				case "canceled":
					ctx = canceled
				case "nil context":
					ctx = nil
				case "no targeting key":
					ec = nil
				case "invalid attribute":
					ec["bad"] = []string{"secret"}
				case "SDK error":
					client.err = errors.New("private credential")
					wantCalls = 1
				case "detail error without SDK error":
					client.detail.Reason = sdk.ReasonFlagNotFound
					wantCalls = 1
				}
				r := evaluation.call(p, ctx, ec)
				if !reflect.DeepEqual(r.value, evaluation.fallback) {
					t.Fatalf("value=%#v, want default %#v", r.value, evaluation.fallback)
				}
				details := r.ResolutionDetail()
				if details.ErrorCode != failure.code || details.Reason != openfeature.ErrorReason || r.Error() == nil {
					t.Fatalf("details=%+v, want %s", details, failure.code)
				}
				if strings.Contains(details.ErrorMessage, "secret") || strings.Contains(details.ErrorMessage, "credential") {
					t.Fatal("error exposed private data")
				}
				if client.calls != wantCalls {
					t.Fatalf("SDK calls=%d, want %d", client.calls, wantCalls)
				}
			})
		}
	}
}

func TestInvalidFloatResults(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		client := &stubClient{initialized: true, number: value}
		p := newProvider(client, providerInitTimeout)
		r := p.FloatEvaluation(t.Context(), "flag", 2.5, openfeature.FlattenedContext{openfeature.TargetingKey: "user"})
		if r.Value != 2.5 || r.ResolutionDetail().ErrorCode != openfeature.TypeMismatchCode {
			t.Fatalf("non-finite result: %+v", r)
		}
	}
}

func TestSuccessfulEvaluationsCallSDKOnce(t *testing.T) {
	ec := openfeature.FlattenedContext{openfeature.TargetingKey: "user"}
	t.Run("boolean", func(t *testing.T) {
		checkSingleEvaluation(t, &stubClient{text: "true"}, func(p *Provider) openfeature.BoolResolutionDetail {
			return p.BooleanEvaluation(t.Context(), "flag", false, ec)
		}, true)
	})
	t.Run("string", func(t *testing.T) {
		checkSingleEvaluation(t, &stubClient{text: "resolved"}, func(p *Provider) openfeature.StringResolutionDetail {
			return p.StringEvaluation(t.Context(), "flag", "fallback", ec)
		}, "resolved")
	})
	t.Run("integer", func(t *testing.T) {
		checkSingleEvaluation(t, &stubClient{text: "9007199254740993.0"}, func(p *Provider) openfeature.IntResolutionDetail {
			return p.IntEvaluation(t.Context(), "flag", -1, ec)
		}, int64(9007199254740993))
	})
	t.Run("float", func(t *testing.T) {
		checkSingleEvaluation(t, &stubClient{number: 2.5}, func(p *Provider) openfeature.FloatResolutionDetail {
			return p.FloatEvaluation(t.Context(), "flag", -1, ec)
		}, 2.5)
	})
	t.Run("object", func(t *testing.T) {
		checkSingleEvaluation(t, &stubClient{object: json.RawMessage(`{"enabled":true}`)}, func(p *Provider) openfeature.InterfaceResolutionDetail {
			return p.ObjectEvaluation(t.Context(), "flag", nil, ec)
		}, any(map[string]any{"enabled": true}))
	})
}

func checkSingleEvaluation[T any](t *testing.T, client *stubClient, evaluate func(*Provider) openfeature.GenericResolutionDetail[T], want T) {
	t.Helper()
	client.initialized = true
	client.detail.Reason = sdk.ReasonTargetMatch
	result := evaluate(newProvider(client, providerInitTimeout))
	if result.Error() != nil || result.Reason != openfeature.TargetingMatchReason || !reflect.DeepEqual(result.Value, want) {
		t.Fatalf("result=%+v, want value=%#v and TARGETING_MATCH", result, want)
	}
	if client.calls != 1 {
		t.Fatalf("SDK calls=%d; each evaluation must record analytics only once", client.calls)
	}
}

func TestBooleanEvaluationRejectsCoercion(t *testing.T) {
	for _, raw := range []string{"1", "0", "TRUE", "False", "null", ""} {
		t.Run(raw, func(t *testing.T) {
			client := &stubClient{initialized: true, text: raw}
			p := newProvider(client, providerInitTimeout)
			result := p.BooleanEvaluation(t.Context(), "flag", true, openfeature.FlattenedContext{openfeature.TargetingKey: "user"})
			if !result.Value || result.ResolutionDetail().ErrorCode != openfeature.TypeMismatchCode || client.calls != 1 {
				t.Fatalf("result=%+v, SDK calls=%d; want default and TYPE_MISMATCH", result, client.calls)
			}
		})
	}
}

func TestJSONConversion(t *testing.T) {
	type fallback struct{ Name string }
	defaultValue := &fallback{Name: "unchanged"}
	ec := openfeature.FlattenedContext{openfeature.TargetingKey: "user"}
	for _, tc := range []struct {
		name  string
		value any
		code  openfeature.ErrorCode
	}{
		{"unexpected SDK type", "not RawMessage", openfeature.TypeMismatchCode},
		{"invalid JSON", json.RawMessage(`{"broken"`), openfeature.ParseErrorCode},
		{"number overflow", json.RawMessage(`{"number":1e999}`), openfeature.ParseErrorCode},
		{"scalar", json.RawMessage(`true`), openfeature.TypeMismatchCode},
		{"null", json.RawMessage(`null`), openfeature.TypeMismatchCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &stubClient{initialized: true, object: tc.value}
			p := newProvider(client, providerInitTimeout)
			r := p.ObjectEvaluation(t.Context(), "flag", defaultValue, ec)
			if r.Value != defaultValue || defaultValue.Name != "unchanged" || r.ResolutionDetail().ErrorCode != tc.code {
				t.Fatalf("result=%+v, want original default and %s", r, tc.code)
			}
			if _, ok := client.jsonDefault.(json.RawMessage); !ok {
				t.Fatal("SDK requires a nonnil concrete JSON decoding type")
			}
		})
	}
}
