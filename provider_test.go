package featbit

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/featbit/featbit-go-sdk"
	"github.com/featbit/featbit-go-sdk/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
)

type stubClient struct {
	initialized bool
	text        string
	number      float64
	object      any
	detail      interfaces.EvalDetail
	err         error
	calls       int
	jsonDefault any
}

func (s *stubClient) IsInitialized() bool { return s.initialized }
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
	ready := &Provider{client: &stubClient{initialized: true}}
	if err := ready.Init(openfeature.EvaluationContext{}); err != nil {
		t.Fatal(err)
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
				p := &Provider{client: client}
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
		p := &Provider{client: client}
		r := p.FloatEvaluation(t.Context(), "flag", 2.5, openfeature.FlattenedContext{openfeature.TargetingKey: "user"})
		if r.Value != 2.5 || r.ResolutionDetail().ErrorCode != openfeature.TypeMismatchCode {
			t.Fatalf("non-finite result: %+v", r)
		}
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
			p := &Provider{client: client}
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
