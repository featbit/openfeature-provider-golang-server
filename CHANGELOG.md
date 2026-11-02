# Changelog

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
