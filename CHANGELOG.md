# Changelog

## v2.1.2 - 2026-11-10

### Changes

- Wait for FeatBit SDK readiness during provider initialization and map runtime data-source status changes to OpenFeature provider status events.
- Provider shutdown now closes the FeatBit client once, matching the Python provider, so `api.Shutdown` releases every registered provider and its client. Use `WithoutClosingClient()` for clients shared with other code. **Behavior change:** remove separate `fbClient.Close()` calls unless using that option.
- Replace the flag polling demo with an interactive checkout shipping quote controlled by a FeatBit flag.

### Compatibility

- Require Go 1.26 or later, FeatBit Go SDK v2.1.1, and OpenFeature Go SDK v1.19.0.
- Continue using the `github.com/featbit/openfeature-provider-golang-server/v2` module and import path.

## v2.1.1 - 2026-11-04

### Changes

- Upgrade FeatBit Go SDK from v2.1.0 to v2.1.1.
- Inherit the SDK's removal of `sendToExperiment` from evaluation event payloads and SDK-side experiment eligibility calculations and configuration fields.
- Flag evaluation, percentage rollouts, and `insightsEnabled` controls remain supported; the provider's public API is unchanged.

### Compatibility

- Require Go 1.26 or later, FeatBit Go SDK v2.1.1, and OpenFeature Go SDK v1.19.0.
- Continue using the `github.com/featbit/openfeature-provider-golang-server/v2` module and import path.

## v2.1.0 - 2026-11-02

First public release of the FeatBit server-side OpenFeature provider.

### Features

- Support boolean, string, integer, float, and JSON evaluations.
- Support request-scoped targeting, typed evaluation errors, and application-owned client lifecycle.
- Preserve exact `int64` values and support JSON objects and arrays with nil defaults.
- Include an HTTP server example and offline integration tests.

### Compatibility

- Publish as `github.com/featbit/openfeature-provider-golang-server/v2`; applications must use the `/v2` provider import path.
- Upgrade to FeatBit Go SDK v2.1.0 (`github.com/featbit/featbit-go-sdk/v2`). `NewProvider` accepts an application-owned v2 SDK client.
- Require Go 1.26 or later and OpenFeature Go SDK v1.19.0.
- Set the quick-start example's SDK log level to `WARN`, matching the HTTP server example.

### Known limitations

- Original FeatBit flag types are not strictly enforced for every evaluation. Some cross-type reads can succeed instead of returning `TYPE_MISMATCH`, including boolean/JSON flags read as strings and JSON scalar `true` read as a boolean. Applications should select the evaluation method matching the configured flag type.
