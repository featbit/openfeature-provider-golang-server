package featbit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/featbit/featbit-go-sdk"
	"github.com/featbit/featbit-go-sdk/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
)

func TestResolutionDetailsSuccess(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   openfeature.Reason
	}{
		{"disabled", sdk.ReasonFlagOff, openfeature.DisabledReason},
		{"target", sdk.ReasonTargetMatch, openfeature.TargetingMatchReason},
		{"rule", sdk.ReasonRuleMatch, openfeature.TargetingMatchReason},
		{"fallthrough", sdk.ReasonFallthrough, openfeature.DefaultReason},
		{"empty", "", openfeature.UnknownReason},
		{"unknown", "new upstream reason", openfeature.UnknownReason},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := resolutionDetails(interfaces.EvalDetail{Reason: tt.reason, Variation: "evaluated-value"}, nil)
			if detail.Reason != tt.want {
				t.Errorf("reason = %q, want %q", detail.Reason, tt.want)
			}
			if detail.Error() != nil {
				t.Errorf("successful evaluation returned error: %v", detail.Error())
			}
			if detail.Variant != "" {
				t.Errorf("value was exposed as a variation identifier: %q", detail.Variant)
			}
		})
	}
}

func TestResolutionDetailsErrors(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   openfeature.ErrorCode
	}{
		{"not ready", sdk.ReasonClientNotReady, openfeature.ProviderNotReadyCode},
		{"not found", sdk.ReasonFlagNotFound, openfeature.FlagNotFoundCode},
		{"wrong type", sdk.ReasonWrongType, openfeature.TypeMismatchCode},
		{"invalid user", sdk.ReasonUserNotSpecified, openfeature.InvalidContextCode},
		{"evaluation error", sdk.ReasonError, openfeature.GeneralCode},
	}
	for _, tt := range tests {
		for _, withError := range []bool{false, true} {
			name := tt.name + "/detail only"
			var sdkError error
			if withError {
				name = tt.name + "/detail and error"
				sdkError = errors.New("secret upstream context")
			}
			t.Run(name, func(t *testing.T) {
				detail := resolutionDetails(interfaces.EvalDetail{Reason: tt.reason}, sdkError)
				if got := detail.ResolutionDetail().ErrorCode; got != tt.want {
					t.Errorf("code = %q, want %q", got, tt.want)
				}
				if detail.Reason != openfeature.ErrorReason || detail.Error() == nil {
					t.Errorf("missing error resolution: %+v", detail)
				}
				if strings.Contains(detail.ResolutionError.Error(), "secret") || detail.ResolutionError.Unwrap() != nil {
					t.Error("upstream error leaked through resolution details")
				}
			})
		}
	}
}

func TestResolutionDetailsSDKErrorOverridesSuccess(t *testing.T) {
	for _, reason := range []string{"", "unknown", sdk.ReasonFlagOff, sdk.ReasonTargetMatch, sdk.ReasonRuleMatch, sdk.ReasonFallthrough} {
		t.Run(reason, func(t *testing.T) {
			detail := resolutionDetails(interfaces.EvalDetail{Reason: reason}, errors.New("secret upstream context"))
			if got := detail.ResolutionDetail().ErrorCode; got != openfeature.GeneralCode {
				t.Errorf("code = %q, want GENERAL", got)
			}
			if detail.Reason != openfeature.ErrorReason {
				t.Errorf("reason = %q, want ERROR", detail.Reason)
			}
			if strings.Contains(detail.ResolutionError.Error(), "secret") || detail.ResolutionError.Unwrap() != nil {
				t.Error("upstream error leaked through resolution details")
			}
		})
	}
}

func TestResolutionDetailsJSONErrors(t *testing.T) {
	var parsed any
	syntaxError := json.Unmarshal([]byte(`{"secret":`), &parsed)
	var number int
	typeError := json.Unmarshal([]byte(`"secret"`), &number)
	tests := []struct {
		name string
		err  error
		want openfeature.ErrorCode
	}{
		{"syntax", syntaxError, openfeature.ParseErrorCode},
		{"wrapped syntax", fmt.Errorf("secret: %w", syntaxError), openfeature.ParseErrorCode},
		{"type", typeError, openfeature.TypeMismatchCode},
		{"wrapped type", fmt.Errorf("secret: %w", typeError), openfeature.TypeMismatchCode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := resolutionDetails(interfaces.EvalDetail{Reason: sdk.ReasonTargetMatch}, tt.err)
			if got := detail.ResolutionDetail().ErrorCode; got != tt.want {
				t.Errorf("code = %q, want %q", got, tt.want)
			}
			if detail.Reason != openfeature.ErrorReason {
				t.Errorf("reason = %q, want ERROR", detail.Reason)
			}
			if strings.Contains(detail.ResolutionError.Error(), "secret") || detail.ResolutionError.Unwrap() != nil {
				t.Error("JSON error leaked through resolution details")
			}
		})
	}
}
