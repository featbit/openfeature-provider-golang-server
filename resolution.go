package featbit

import (
	"encoding/json"
	"errors"

	sdk "github.com/featbit/featbit-go-sdk"
	"github.com/featbit/featbit-go-sdk/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
)

func errorDetails(err openfeature.ResolutionError) openfeature.ProviderResolutionDetail {
	return openfeature.ProviderResolutionDetail{
		Reason:          openfeature.ErrorReason,
		ResolutionError: err,
	}
}

func resolutionDetails(detail interfaces.EvalDetail, err error) openfeature.ProviderResolutionDetail {
	// Some SDK fallbacks are signaled in the detail even when err is nil.
	switch detail.Reason {
	case sdk.ReasonClientNotReady:
		return errorDetails(openfeature.NewProviderNotReadyResolutionError("FeatBit client is not ready"))
	case sdk.ReasonFlagNotFound:
		return errorDetails(openfeature.NewFlagNotFoundResolutionError("flag was not found"))
	case sdk.ReasonWrongType:
		return errorDetails(openfeature.NewTypeMismatchResolutionError("flag value does not match the requested type"))
	case sdk.ReasonUserNotSpecified:
		return errorDetails(openfeature.NewInvalidContextResolutionError("FeatBit user is invalid"))
	case sdk.ReasonError:
		return errorDetails(openfeature.NewGeneralResolutionError("FeatBit evaluation failed"))
	}
	if err != nil {
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) {
			return errorDetails(openfeature.NewParseErrorResolutionError("flag value is not valid JSON"))
		}
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) {
			return errorDetails(openfeature.NewTypeMismatchResolutionError("flag value does not match the requested type"))
		}
		return errorDetails(openfeature.NewGeneralResolutionError("FeatBit evaluation failed"))
	}

	reason := openfeature.UnknownReason
	switch detail.Reason {
	case sdk.ReasonFlagOff:
		reason = openfeature.DisabledReason
	case sdk.ReasonTargetMatch, sdk.ReasonRuleMatch:
		reason = openfeature.TargetingMatchReason
	case sdk.ReasonFallthrough:
		reason = openfeature.DefaultReason
	}
	// EvalDetail.Variation contains the value, not a variation identifier.
	return openfeature.ProviderResolutionDetail{Reason: reason}
}
