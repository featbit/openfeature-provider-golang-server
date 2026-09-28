package featbit_test

import (
	"context"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/featbit/featbit-go-sdk"
	"github.com/featbit/featbit-go-sdk/factories"
	"github.com/featbit/featbit-go-sdk/interfaces"
	featbit "github.com/featbit/openfeature-provider-golang-server"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

func newOfflineClient(t *testing.T, transform func(string) string, storageFactory ...interfaces.DataStorageFactory) *sdk.FBClient {
	t.Helper()
	data, err := os.ReadFile("testdata/flags.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := string(data)
	if transform != nil {
		fixture = transform(fixture)
	}
	config := sdk.FBConfig{
		Offline: true, StartWait: time.Second, LogLevel: sdk.ERROR,
	}
	if len(storageFactory) != 0 {
		config.DataStorageFactory = storageFactory[0]
	}
	client, err := sdk.MakeCustomFBClient("", "", "", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	if ok, err := client.InitializeFromExternalJson(fixture); err != nil || !ok {
		t.Fatalf("initialize fixture: ok=%v, err=%v", ok, err)
	}
	return client
}

func registerOfflineProvider(t *testing.T, sdkClient *sdk.FBClient) (*openfeature.Client, *openfeature.EvaluationAPI, *featbit.Provider) {
	t.Helper()
	provider, err := featbit.NewProvider(sdkClient)
	if err != nil {
		t.Fatal(err)
	}
	api := isolated.NewAPI()
	t.Cleanup(func() {
		if err := api.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := api.SetProviderAndWait(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	return api.NewClient(), api, provider
}

func requireOfflineDetails(t *testing.T, details openfeature.EvaluationDetails, err error, reason openfeature.Reason) {
	t.Helper()
	if err != nil || details.ErrorCode != "" {
		t.Fatalf("evaluation failed: details=%+v, err=%v", details, err)
	}
	if details.Reason != reason {
		t.Errorf("reason=%q, want %q", details.Reason, reason)
	}
	if details.Variant != "" {
		t.Errorf("variant=%q; SDK does not expose variation identifiers", details.Variant)
	}
}

func TestOfflineEvaluations(t *testing.T) {
	client, _, _ := registerOfflineProvider(t, newOfflineClient(t, nil))
	ec := openfeature.NewEvaluationContext("workload", nil)

	t.Run("boolean", func(t *testing.T) {
		d, err := client.BooleanValueDetails(t.Context(), "boolean", false, ec)
		requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DefaultReason)
		if !d.Value {
			t.Fatal("want true")
		}
	})
	t.Run("string", func(t *testing.T) {
		d, err := client.StringValueDetails(t.Context(), "string", "fallback", ec)
		requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DefaultReason)
		if d.Value != "basic" {
			t.Fatalf("value=%q, want basic", d.Value)
		}
	})
	t.Run("integer", func(t *testing.T) {
		d, err := client.IntValueDetails(t.Context(), "integer", -1, ec)
		requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DefaultReason)
		if d.Value != 42 {
			t.Fatalf("value=%d, want 42", d.Value)
		}
	})
	t.Run("float", func(t *testing.T) {
		d, err := client.FloatValueDetails(t.Context(), "float", -1, ec)
		requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DefaultReason)
		if d.Value != 2.5 {
			t.Fatalf("value=%v, want 2.5", d.Value)
		}
	})
	t.Run("object", func(t *testing.T) {
		fallback := map[string]any{"fallback": []any{"unchanged"}}
		d, err := client.ObjectValueDetails(t.Context(), "object", fallback, ec)
		requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DefaultReason)
		want := map[string]any{
			"limits":  map[string]any{"requests": float64(12)},
			"regions": []any{"eu", "us"}, "enabled": true, "optional": nil,
		}
		if !reflect.DeepEqual(d.Value, want) {
			t.Fatalf("value=%#v, want %#v", d.Value, want)
		}
		if !reflect.DeepEqual(fallback, map[string]any{"fallback": []any{"unchanged"}}) {
			t.Fatal("object default was mutated")
		}
	})
	t.Run("array with nil default", func(t *testing.T) {
		d, err := client.ObjectValueDetails(t.Context(), "array", nil, ec)
		requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DefaultReason)
		want := []any{"eu", map[string]any{"weight": float64(2)}, nil}
		if !reflect.DeepEqual(d.Value, want) {
			t.Fatalf("value=%#v, want %#v", d.Value, want)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		d, err := client.BooleanValueDetails(t.Context(), "disabled", true, ec)
		requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DisabledReason)
		if d.Value {
			t.Fatal("want disabled flag's false variation")
		}
	})
}

func TestOfflineDefaultsAndErrors(t *testing.T) {
	client, _, _ := registerOfflineProvider(t, newOfflineClient(t, nil))
	ec := openfeature.NewEvaluationContext("workload", nil)
	fallback := map[string]any{"nested": []any{"fallback"}}
	type result struct {
		value   any
		details openfeature.EvaluationDetails
		err     error
	}
	cases := []struct {
		name     string
		fallback any
		evaluate func(string) result
	}{
		{"boolean", true, func(key string) result {
			d, err := client.BooleanValueDetails(t.Context(), key, true, ec)
			return result{d.Value, d.EvaluationDetails, err}
		}},
		{"string", "fallback", func(key string) result {
			d, err := client.StringValueDetails(t.Context(), key, "fallback", ec)
			return result{d.Value, d.EvaluationDetails, err}
		}},
		{"integer", int64(-17), func(key string) result {
			d, err := client.IntValueDetails(t.Context(), key, -17, ec)
			return result{d.Value, d.EvaluationDetails, err}
		}},
		{"float", -1.25, func(key string) result {
			d, err := client.FloatValueDetails(t.Context(), key, -1.25, ec)
			return result{d.Value, d.EvaluationDetails, err}
		}},
		{"object", fallback, func(key string) result {
			d, err := client.ObjectValueDetails(t.Context(), key, fallback, ec)
			return result{d.Value, d.EvaluationDetails, err}
		}},
	}
	for _, tc := range cases {
		for _, failure := range []struct {
			key  string
			code openfeature.ErrorCode
		}{
			{"missing", openfeature.FlagNotFoundCode},
			{"unknown-type", openfeature.TypeMismatchCode},
		} {
			t.Run(tc.name+"/"+failure.key, func(t *testing.T) {
				r := tc.evaluate(failure.key)
				if r.err == nil || r.details.ErrorCode != failure.code || r.details.Reason != openfeature.ErrorReason {
					t.Fatalf("details=%+v, err=%v; want %s", r.details, r.err, failure.code)
				}
				if !reflect.DeepEqual(r.value, tc.fallback) {
					t.Fatalf("value=%#v, want default %#v", r.value, tc.fallback)
				}
			})
		}
	}
	d, err := client.ObjectValueDetails(t.Context(), "invalid-json", fallback, ec)
	if err == nil || d.Reason != openfeature.ErrorReason || !reflect.DeepEqual(d.Value, fallback) {
		t.Fatalf("invalid JSON must return default and an error: %+v, %v", d, err)
	}
	if !reflect.DeepEqual(fallback, map[string]any{"nested": []any{"fallback"}}) {
		t.Fatal("object default was mutated")
	}
	for _, key := range []string{"json-null", "json-scalar"} {
		t.Run(key, func(t *testing.T) {
			d, err := client.ObjectValueDetails(t.Context(), key, fallback, ec)
			if err == nil || d.ErrorCode != openfeature.TypeMismatchCode || !reflect.DeepEqual(d.Value, fallback) {
				t.Fatalf("details=%+v, err=%v; want default and TYPE_MISMATCH", d, err)
			}
		})
	}
	t.Run("numeric flag as boolean", func(t *testing.T) {
		d, err := client.BooleanValueDetails(t.Context(), "integer", true, ec)
		if err == nil || d.ErrorCode != openfeature.TypeMismatchCode || !d.Value {
			t.Fatalf("details=%+v, err=%v; want true default and TYPE_MISMATCH", d, err)
		}
	})
}

func TestOfflineIntegerPrecisionAndBoundaries(t *testing.T) {
	client, _, _ := registerOfflineProvider(t, newOfflineClient(t, nil))
	ec := openfeature.NewEvaluationContext("workload", nil)
	for _, tc := range []struct {
		key  string
		want int64
	}{
		{"integer-precision", 9007199254740993},
		{"integer-max", math.MaxInt64},
		{"integer-min", math.MinInt64},
		{"integer-decimal", 42},
		{"integer-exponent", 42},
	} {
		t.Run(tc.key, func(t *testing.T) {
			d, err := client.IntValueDetails(t.Context(), tc.key, -17, ec)
			requireOfflineDetails(t, d.EvaluationDetails, err, openfeature.DefaultReason)
			if d.Value != tc.want {
				t.Fatalf("value=%d, want %d", d.Value, tc.want)
			}
		})
	}
	for _, key := range []string{"float", "integer-overflow", "integer-underflow"} {
		t.Run(key, func(t *testing.T) {
			d, err := client.IntValueDetails(t.Context(), key, math.MaxInt64, ec)
			if err == nil || d.ErrorCode != openfeature.TypeMismatchCode || d.Value != math.MaxInt64 {
				t.Fatalf("details=%+v, err=%v; want exact default and TYPE_MISMATCH", d, err)
			}
		})
	}
}

func TestOfflineTargetingAndContextIsolation(t *testing.T) {
	client, _, _ := registerOfflineProvider(t, newOfflineClient(t, nil))
	attrs := map[string]any{"tenant": "paid", "seats": int64(3), "enabled": true, "ratio": 1.5, "userName": "service-a"}
	paid := openfeature.NewEvaluationContext("paid-workload", attrs)
	defaultName := openfeature.NewEvaluationContext("service-a", map[string]any{"tenant": "paid", "seats": 3, "enabled": true, "ratio": 1.5})
	basic := openfeature.NewEvaluationContext("basic-workload", nil)
	targeted := openfeature.NewEvaluationContext("targeted-workload", nil)
	for _, tc := range []struct {
		name   string
		ctx    openfeature.EvaluationContext
		value  string
		reason openfeature.Reason
	}{
		{"scalar rule", paid, "premium", openfeature.TargetingMatchReason},
		{"name defaults to targeting key", defaultName, "premium", openfeature.TargetingMatchReason},
		{"specific workload", targeted, "targeted", openfeature.TargetingMatchReason},
		{"next request", basic, "basic", openfeature.DefaultReason},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := client.StringValueDetails(t.Context(), "string", "fallback", tc.ctx)
			requireOfflineDetails(t, d.EvaluationDetails, err, tc.reason)
			if d.Value != tc.value {
				t.Fatalf("value=%q, want %q", d.Value, tc.value)
			}
		})
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for _, tc := range []struct {
				ctx  openfeature.EvaluationContext
				want string
			}{{paid, "premium"}, {basic, "basic"}} {
				value, err := client.StringValue(t.Context(), "string", "fallback", tc.ctx)
				if err != nil || value != tc.want {
					t.Errorf("concurrent evaluation=%q, err=%v; want %q", value, err, tc.want)
				}
			}
		})
	}
	wg.Wait()
	if !reflect.DeepEqual(paid.Attributes(), attrs) {
		t.Fatal("request attributes were mutated")
	}
	for _, tc := range []struct {
		name string
		ctx  openfeature.EvaluationContext
		code openfeature.ErrorCode
	}{
		{"missing identity", openfeature.EvaluationContext{}, openfeature.TargetingKeyMissingCode},
		{"nested attribute", openfeature.NewEvaluationContext("workload", map[string]any{"tenant": map[string]any{"id": "paid"}}), openfeature.InvalidContextCode},
		{"null attribute", openfeature.NewEvaluationContext("workload", map[string]any{"tenant": nil}), openfeature.InvalidContextCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := client.StringValueDetails(t.Context(), "string", "fallback", tc.ctx)
			if err == nil || d.ErrorCode != tc.code || d.Value != "fallback" {
				t.Fatalf("details=%+v, err=%v; want default and %s", d, err, tc.code)
			}
		})
	}
}

func TestOfflineProviderIsolationAndClientOwnership(t *testing.T) {
	storage := &closeTrackingStorage{}
	first := newOfflineClient(t, nil, storage)
	second := newOfflineClient(t, func(fixture string) string {
		return strings.ReplaceAll(fixture, `"value": "basic"`, `"value": "other-environment"`)
	})
	clientA, apiA, providerA := registerOfflineProvider(t, first)
	clientB, _, _ := registerOfflineProvider(t, second)
	ec := openfeature.NewEvaluationContext("same-workload", nil)
	for _, tc := range []struct {
		client *openfeature.Client
		want   string
	}{{clientA, "basic"}, {clientB, "other-environment"}} {
		value, err := tc.client.StringValue(t.Context(), "string", "fallback", ec)
		if err != nil || value != tc.want {
			t.Fatalf("environment value=%q, err=%v; want %q", value, err, tc.want)
		}
	}
	if err := apiA.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	providerA.Shutdown()
	providerA.Shutdown()
	if got := storage.closeCalls.Load(); got != 0 {
		t.Fatalf("provider closed application-owned SDK storage %d times", got)
	}
	user, err := interfaces.NewUserBuilder("same-workload").Build()
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := first.Variation("string", user, "fallback")
	if err != nil || value != "basic" {
		t.Fatalf("provider shutdown closed application-owned client: value=%q, err=%v", value, err)
	}
	value, err = clientB.StringValue(t.Context(), "string", "fallback", ec)
	if err != nil || value != "other-environment" {
		t.Fatalf("shutting down one provider affected another: value=%q, err=%v", value, err)
	}
}

type closeTrackingStorage struct {
	interfaces.DataStorage
	closeCalls atomic.Int32
}

func (s *closeTrackingStorage) CreateDataStorage(ctx interfaces.Context) (interfaces.DataStorage, error) {
	storage, err := factories.NewInMemoryStorageBuilder().CreateDataStorage(ctx)
	if err != nil {
		return nil, err
	}
	s.DataStorage = storage
	return s, nil
}

func (s *closeTrackingStorage) Close() error {
	s.closeCalls.Add(1)
	return s.DataStorage.Close()
}
