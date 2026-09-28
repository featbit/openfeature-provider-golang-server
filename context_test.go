package featbit

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
)

func TestEvaluationUserIdentity(t *testing.T) {
	tests := []struct {
		name    string
		context openfeature.FlattenedContext
		want    string
	}{
		{"default name", openfeature.FlattenedContext{"targetingKey": "service-1"}, "service-1"},
		{"explicit name", openfeature.FlattenedContext{"targetingKey": "service-1", "userName": "Worker"}, "Worker"},
		{"empty name", openfeature.FlattenedContext{"targetingKey": "service-1", "userName": ""}, "service-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := evaluationUser(tt.context)
			if err != (openfeature.ResolutionError{}) {
				t.Fatalf("evaluationUser() error = %v", err)
			}
			if user.GetKey() != "service-1" || user.GetUserName() != tt.want || !user.IsValid() {
				t.Fatalf("invalid identity: key = %q, name = %q", user.GetKey(), user.GetUserName())
			}
			if len(user.CustomAttributes()) != 0 {
				t.Fatalf("identity fields leaked to custom attributes: %v", user.CustomAttributes())
			}
		})
	}
}

func TestEvaluationUserScalarAttributes(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "paid", "paid"},
		{"empty string", "", ""},
		{"true", true, "true"},
		{"false", false, "false"},
		{"int", int(-42), "-42"},
		{"int8", int8(-128), "-128"},
		{"int16", int16(-32768), "-32768"},
		{"int32", int32(math.MinInt32), "-2147483648"},
		{"int64", int64(math.MinInt64), "-9223372036854775808"},
		{"uint", uint(42), "42"},
		{"uint8", uint8(255), "255"},
		{"uint16", uint16(65535), "65535"},
		{"uint32", uint32(math.MaxUint32), "4294967295"},
		{"uint64", uint64(math.MaxUint64), "18446744073709551615"},
		{"float32", float32(1.2), "1.2"},
		{"float64", float64(1.25), "1.25"},
		{"negative float", -1.25, "-1.25"},
		{"zero", float64(0), "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := evaluationUser(openfeature.FlattenedContext{"targetingKey": "tenant-1", "plan": tt.value})
			if err != (openfeature.ResolutionError{}) {
				t.Fatalf("evaluationUser() error = %v", err)
			}
			if got := user.CustomAttributes()["plan"]; got != tt.want {
				t.Errorf("attribute = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluationUserInvalidContext(t *testing.T) {
	tests := []struct {
		name    string
		context openfeature.FlattenedContext
		code    openfeature.ErrorCode
	}{
		{"nil", nil, openfeature.TargetingKeyMissingCode},
		{"missing key", openfeature.FlattenedContext{}, openfeature.TargetingKeyMissingCode},
		{"empty key", openfeature.FlattenedContext{"targetingKey": ""}, openfeature.TargetingKeyMissingCode},
		{"numeric key", openfeature.FlattenedContext{"targetingKey": 1}, openfeature.InvalidContextCode},
		{"null key", openfeature.FlattenedContext{"targetingKey": nil}, openfeature.InvalidContextCode},
		{"numeric name", openfeature.FlattenedContext{"targetingKey": "service-1", "userName": 1}, openfeature.InvalidContextCode},
		{"null name", openfeature.FlattenedContext{"targetingKey": "service-1", "userName": nil}, openfeature.InvalidContextCode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := evaluationUser(tt.context)
			if user.IsValid() {
				t.Error("invalid context produced a valid user")
			}
			if got := errorDetails(err).ResolutionDetail().ErrorCode; got != tt.code {
				t.Errorf("code = %q, want %q", got, tt.code)
			}
		})
	}
}

func TestEvaluationUserRejectsUnsupportedAttributes(t *testing.T) {
	pointer := "private-context-value"
	tests := []struct {
		name  string
		value any
	}{
		{"null", nil},
		{"map", map[string]any{"private-context-value": true}},
		{"slice", []string{"private-context-value"}},
		{"array", [1]string{"private-context-value"}},
		{"pointer", &pointer},
		{"struct", struct{ Value string }{"private-context-value"}},
		{"nan", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
		{"float32 nan", float32(math.NaN())},
		{"float32 infinity", float32(math.Inf(1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := evaluationUser(openfeature.FlattenedContext{
				"targetingKey": "private-context-value", "private-attribute-name": tt.value,
			})
			if got := errorDetails(err).ResolutionDetail().ErrorCode; got != openfeature.InvalidContextCode {
				t.Fatalf("code = %q, want INVALID_CONTEXT", got)
			}
			if strings.Contains(err.Error(), "private-") {
				t.Errorf("error exposed context contents: %v", err)
			}
		})
	}
}

func TestEvaluationUserRejectsIdentityAliases(t *testing.T) {
	for _, attribute := range []string{"key", "Key", "KEY", "keyid", "keyId", "KEYID", "name", "Name", "NAME"} {
		t.Run(attribute, func(t *testing.T) {
			_, err := evaluationUser(openfeature.FlattenedContext{"targetingKey": "tenant-1", attribute: "tenant-2"})
			if got := errorDetails(err).ResolutionDetail().ErrorCode; got != openfeature.InvalidContextCode {
				t.Errorf("code = %q, want INVALID_CONTEXT", got)
			}
		})
	}
}

func TestEvaluationUserIsolation(t *testing.T) {
	context := openfeature.FlattenedContext{"targetingKey": "tenant-1", "userName": "worker", "plan": "free"}
	original := openfeature.FlattenedContext{"targetingKey": "tenant-1", "userName": "worker", "plan": "free"}
	first, err := evaluationUser(context)
	if err != (openfeature.ResolutionError{}) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(context, original) {
		t.Fatalf("caller context was modified: %v", context)
	}
	context["targetingKey"] = "tenant-2"
	context["plan"] = "paid"
	second, err := evaluationUser(context)
	if err != (openfeature.ResolutionError{}) {
		t.Fatal(err)
	}
	if first.GetKey() != "tenant-1" || first.Get("plan") != "free" {
		t.Fatal("first user changed after a separate evaluation")
	}
	if second.GetKey() != "tenant-2" || second.Get("plan") != "paid" {
		t.Fatal("second user inherited a previous evaluation context")
	}
}
