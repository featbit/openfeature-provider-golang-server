# FeatBit OpenFeature Provider for Go

## Scope

- Build a server-side OpenFeature provider for REST APIs, gRPC services, background workers, and infrastructure products.
- Keep public APIs independent of HTTP frameworks, transports, dependency injection containers, and deployment platforms.
- Keep the adapter small; delegate flag evaluation, streaming, caching, and analytics to FeatBit.
- Initial scope: five evaluation types, context conversion, resolution details, and lifecycle support.
- Add optional provider events and tracking only when their FeatBit mappings are defined and tested.

## Version Baseline

| Component | Version / path |
| --- | --- |
| Go | Minimum `1.26.0`; use the latest patch in CI |
| FeatBit SDK | [`github.com/featbit/featbit-go-sdk`](https://pkg.go.dev/github.com/featbit/featbit-go-sdk@v1.1.3) `v1.1.3` |
| OpenFeature SDK | [`github.com/open-feature/go-sdk`](https://github.com/open-feature/go-sdk/blob/v1.19.0/go.mod) `v1.19.0` |
| Provider module | `github.com/featbit/openfeature-provider-golang-server` |
| Provider package | `featbit` at the repository root |
| First release | `v0.1.0` |

- Import OpenFeature from `github.com/open-feature/go-sdk/openfeature`.
- Pin dependency versions in `go.mod` and commit `go.sum`; avoid branch dependencies and local `replace` directives in releases.
- Review dependency upgrades explicitly and update the documented Go minimum when required.

## Architecture

- Use one Go module and one public provider package; keep helpers unexported.
- Start with `provider.go`, `context.go`, `resolution.go`, adjacent `*_test.go` files, and `examples/basic/`.
- Expose `NewProvider(client *featbit.FBClient)`; reject nil clients and document constructor errors.
- Accept an application-owned client. Applications configure FeatBit endpoints, credentials, and startup timeout through the FeatBit SDK.
- Reuse one client per FeatBit environment; never construct a client during evaluation.
- Implement `openfeature.FeatureProvider` and `openfeature.StateHandler`, with compile-time interface assertions.
- `Init` must verify client readiness and return an initialization error when unavailable.
- `Shutdown` must be idempotent and release adapter resources; the application remains responsible for `FBClient.Close()`.
- Return stable provider metadata named `featbit`; return an empty hook list unless provider hooks are needed.
- Use a small private client interface for test doubles; avoid a general abstraction framework.
- Support concurrent evaluations without mutating caller contexts or package-level SDK configuration.

## Server Integration

- Initialize and register providers at application startup; inject OpenFeature clients into handlers and services without implicit global registration.
- Accept standard `context.Context`; keep request and tenant attributes in per-evaluation contexts, never in shared mutable state.
- Use a stable user, tenant, service, or workload identifier as the targeting key; infrastructure workloads do not require an end-user identity.
- Keep evaluation local through the FeatBit SDK; add no network calls to the request path.
- Let applications choose startup failure and readiness policies, including whether to serve defaults when initialization fails.
- Keep signal handling, listeners, and process termination in the application.
- Drain in-flight requests before provider shutdown and client closure; close each application-owned FeatBit client once.
- Isolate multiple FeatBit environments with separate clients and providers, registered through OpenFeature domains or isolated API instances.

## Evaluation Contract

| OpenFeature evaluation | FeatBit method |
| --- | --- |
| Boolean | `Variation` with boolean validation |
| String | `Variation` |
| Integer | `Variation` with exact `int64` parsing |
| Float | `DoubleVariation` |
| Object | `JsonVariation` |

- Map the flattened context's nonempty string `targetingKey` to the FeatBit user key.
- Return `TARGETING_KEY_MISSING` for an absent or empty key and `INVALID_CONTEXT` for an invalid type.
- Map optional string `userName` to the FeatBit user name; default it to the targeting key.
- Convert scalar custom attributes to deterministic strings; reject unsupported nested or null values with `INVALID_CONTEXT`.
- Reserve identity fields so custom attributes cannot overwrite the user key or name.
- Preserve the caller's default value on every evaluation error and populate the OpenFeature resolution error.
- Map FeatBit not-ready, missing-flag, and wrong-type results to `PROVIDER_NOT_READY`, `FLAG_NOT_FOUND`, and `TYPE_MISMATCH`; use `GENERAL` for unclassified failures.
- Inspect both the SDK error and evaluation detail; a fallback value alone does not indicate success.
- Map known success reasons explicitly; use `UNKNOWN` when the SDK provides insufficient detail.
- Populate `Variant` only from an actual variation identifier, never from the evaluated value.
- Validate raw boolean and integer variations; avoid the SDK's numeric-to-boolean coercion and float-to-int truncation.
- Decode JSON through a nonnil `json.RawMessage` default, support objects and arrays, and preserve caller defaults on errors.
- Keep secrets and evaluation-context contents out of error messages and logs.

## Validation

- Use table-driven tests for all five types, defaults, context conversion, reason/error mappings, and integer boundaries.
- Cover nil clients, uninitialized clients, repeated shutdown, concurrent evaluations, and isolation between requests, tenants, and providers.
- Include an offline FeatBit fixture test to verify the real SDK adapter without credentials or a running server.
- Run `gofmt`, `go vet ./...`, and `go test -race ./...` for code changes; keep `go mod tidy` clean.
- Run CI on the minimum supported Go minor and the latest stable Go release, using their latest patches.
- Include a standard-library `net/http` example showing startup registration, request-scoped context, fallback behavior, and graceful shutdown; compile examples in CI.

## Releases and Versioning

- Maintain source and releases in `featbit/openfeature-provider-golang-server` on GitHub.
- Develop through focused pull requests into `main`; require passing CI before merging.
- Version the provider independently of both upstream SDKs using Semantic Versioning.
- During `v0.x`, use minor releases for breaking changes and features, and patch releases for compatible fixes.
- Publish `v1.0.0` once the public API, context mapping, lifecycle, and error behavior are stable and tested.
- After `v1.0.0`, use patch/minor/major releases for fixes/compatible features/breaking changes; add `/v2` to the module and imports for major version 2.
- Record user-visible changes and dependency compatibility in `CHANGELOG.md` and GitHub release notes.
- Release a tested commit with an immutable root tag such as `v0.1.0` and a matching GitHub Release; never move a published tag.
- Distribute through Go Modules: `go get github.com/featbit/openfeature-provider-golang-server@v0.1.0`.
- Follow the [Go module publishing workflow](https://go.dev/doc/modules/publishing); verify proxy resolution and documentation on pkg.go.dev after tagging.
- Include a license, package documentation, and a working quick-start example before the first release.
- Submit the released provider to the OpenFeature ecosystem listing; keep this repository as the canonical distribution source.

## References

- [FeatBit Go SDK](https://github.com/featbit/featbit-go-sdk): wrapped client and evaluation behavior.
- [OpenFeature Go SDK](https://github.com/open-feature/go-sdk): provider interfaces and lifecycle contracts; use the pinned version as the API authority.
