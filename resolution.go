package featbit

import (
	"context"
	"encoding/json"
	"errors"

	sdk "github.com/featbit/featbit-go-sdk"
	"github.com/featbit/featbit-go-sdk/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
)

// resolve keeps validation, SDK errors, conversion errors and defaults consistent
// across evaluation types. The client is accessed only after readiness checks.
func resolve[T, R any](p *Provider, ctx context.Context, defaultValue T, flatCtx openfeature.FlattenedContext,
	evaluate func(evaluationClient, interfaces.FBUser) (R, interfaces.EvalDetail, error),
	convert func(R) (T, openfeature.ResolutionError),
) openfeature.GenericResolutionDetail[T] {
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
	raw, detail, err := evaluate(p.client, user)
	result.ProviderResolutionDetail = resolutionDetails(detail, err)
	if result.Reason == openfeature.ErrorReason {
		return result
	}
	value, conversionErr := convert(raw)
	if conversionErr != (openfeature.ResolutionError{}) {
		result.ProviderResolutionDetail = errorDetails(conversionErr)
		return result
	}
	result.Value = value
	return result
}

func typeMismatch() openfeature.ResolutionError {
	return openfeature.NewTypeMismatchResolutionError("flag value does not match the requested type")
}

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
		return errorDetails(typeMismatch())
	case sdk.ReasonUserNotSpecified:
		return errorDetails(openfeature.NewInvalidContextResolutionError("FeatBit user is invalid"))
	case sdk.ReasonError:
		return errorDetails(openfeature.NewGeneralResolutionError("FeatBit evaluation failed"))
	}
	if err != nil {
		if _, ok := errors.AsType[*json.SyntaxError](err); ok {
			return errorDetails(openfeature.NewParseErrorResolutionError("flag value is not valid JSON"))
		}
		if _, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			return errorDetails(typeMismatch())
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
