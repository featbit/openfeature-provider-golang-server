package featbit

import (
	"strconv"
	"strings"

	"github.com/featbit/featbit-go-sdk/interfaces"
	"github.com/open-feature/go-sdk/openfeature"
)

func evaluationUser(flatCtx openfeature.FlattenedContext) (interfaces.FBUser, openfeature.ResolutionError) {
	rawKey, exists := flatCtx[openfeature.TargetingKey]
	if !exists {
		return interfaces.FBUser{}, openfeature.NewTargetingKeyMissingResolutionError("targetingKey is required")
	}
	key, ok := rawKey.(string)
	if !ok {
		return interfaces.FBUser{}, openfeature.NewInvalidContextResolutionError("targetingKey must be a string")
	}
	if key == "" {
		return interfaces.FBUser{}, openfeature.NewTargetingKeyMissingResolutionError("targetingKey must not be empty")
	}

	name := key
	if rawName, exists := flatCtx["userName"]; exists {
		value, ok := rawName.(string)
		if !ok {
			return interfaces.FBUser{}, openfeature.NewInvalidContextResolutionError("userName must be a string")
		}
		if value != "" {
			name = value
		}
	}

	builder := interfaces.NewUserBuilder(key).UserName(name)
	for attribute, value := range flatCtx {
		if attribute == openfeature.TargetingKey || attribute == "userName" {
			continue
		}
		// FeatBit resolves these aliases before custom attributes, ignoring case.
		switch strings.ToLower(attribute) {
		case "key", "keyid", "name":
			return interfaces.FBUser{}, openfeature.NewInvalidContextResolutionError("custom attributes must not use reserved identity names")
		}
		text, ok := scalarString(value)
		if !ok {
			return interfaces.FBUser{}, openfeature.NewInvalidContextResolutionError("custom attributes must be strings, booleans, or finite numbers")
		}
		builder.Custom(attribute, text)
	}
	user, err := builder.Build()
	if err != nil {
		return interfaces.FBUser{}, openfeature.NewInvalidContextResolutionError("evaluation context cannot form a valid FeatBit user")
	}
	return user, openfeature.ResolutionError{}
}

func scalarString(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case int:
		return strconv.FormatInt(int64(value), 10), true
	case int8:
		return strconv.FormatInt(int64(value), 10), true
	case int16:
		return strconv.FormatInt(int64(value), 10), true
	case int32:
		return strconv.FormatInt(int64(value), 10), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint:
		return strconv.FormatUint(uint64(value), 10), true
	case uint8:
		return strconv.FormatUint(uint64(value), 10), true
	case uint16:
		return strconv.FormatUint(uint64(value), 10), true
	case uint32:
		return strconv.FormatUint(uint64(value), 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	case float32:
		if isFinite(float64(value)) {
			return strconv.FormatFloat(float64(value), 'g', -1, 32), true
		}
	case float64:
		if isFinite(value) {
			return strconv.FormatFloat(value, 'g', -1, 64), true
		}
	}
	return "", false
}
