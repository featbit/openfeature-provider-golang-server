# FeatBit OpenFeature Provider for Go

Use [FeatBit](https://www.featbit.co/) feature flags in Go server applications with [OpenFeature](https://openfeature.dev/).

## Installation

Requires Go 1.26 or later.

```sh
go get github.com/featbit/openfeature-provider-golang-server
```

## Quick start

Obtain your [environment secret and SDK URLs](https://docs.featbit.co/sdk/faq), then set `FEATBIT_ENV_SECRET`, `FEATBIT_STREAMING_URL`, and `FEATBIT_EVENT_URL`.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	featbitsdk "github.com/featbit/featbit-go-sdk"
	featbitprovider "github.com/featbit/openfeature-provider-golang-server"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() (err error) {
	fbClient, err := featbitsdk.MakeCustomFBClient(
		os.Getenv("FEATBIT_ENV_SECRET"),
		os.Getenv("FEATBIT_STREAMING_URL"),
		os.Getenv("FEATBIT_EVENT_URL"),
		featbitsdk.FBConfig{StartWait: 10 * time.Second},
	)
	if fbClient != nil {
		defer func() { err = errors.Join(err, fbClient.Close()) }()
	}
	if err != nil {
		return err
	}

	provider, err := featbitprovider.NewProvider(fbClient)
	if err != nil {
		return err
	}
	api := isolated.NewAPI()
	defer func() { err = errors.Join(err, api.Shutdown(context.Background())) }()
	if err = api.SetProviderAndWait(context.Background(), provider); err != nil {
		return err
	}

	client := api.NewClient()
	evaluationContext := openfeature.NewEvaluationContext("user-123", map[string]any{
		"userName": "Alice",
		"plan":     "premium",
	})
	enabled, evaluationErr := client.BooleanValue(context.Background(), "my-feature", false, evaluationContext)
	if evaluationErr != nil {
		log.Print("Flag evaluation failed; using the default value")
	}
	fmt.Println("Feature enabled:", enabled)
	return nil
}
```

Use `BooleanValue`, `StringValue`, `IntValue`, `FloatValue`, and `ObjectValue` for the five supported value types. Their `*ValueDetails` methods also return evaluation details.

## Evaluation context

- Supply a nonempty targeting key: a stable user, tenant, service, or workload ID.
- An omitted or empty `userName` defaults to the targeting key.
- Custom attributes accept strings, booleans, and numbers, converted to strings for targeting. Nested values and `nil` are rejected. Identity aliases `key`, `keyid`, and `name` are reserved (case-insensitive).
- `ObjectValue` accepts JSON object and array flags. Results use `map[string]any` and `[]any`, with numbers decoded as `float64`. Evaluation errors return the supplied default unchanged, including a `nil` default.

## Server applications

Create one FeatBit client and provider per environment at startup, then share the OpenFeature client with handlers and services. Supply request and tenant attributes on each evaluation. Use separate isolated API instances or OpenFeature domains for different environments.

`NewProvider` returns an error for a nil client. Registration fails if the FeatBit client is not ready. The quick start exits on startup failure; choose a readiness policy appropriate for your application.

Drain requests before shutting down OpenFeature, then call `fbClient.Close()`. OpenFeature shutdown does not close the application-owned FeatBit client.

See the [HTTP server example](examples/basic/) for configuration, request-scoped evaluation, defaults, and graceful shutdown.
