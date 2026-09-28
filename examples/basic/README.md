# HTTP server example

Create a boolean flag named `my-feature` in your FeatBit environment. From the repository root:

```sh
export FEATBIT_ENV_SECRET='<your-environment-secret>'
export FEATBIT_STREAMING_URL='ws://localhost:5100'
export FEATBIT_EVENT_URL='http://localhost:5100'
export SERVICE_TARGETING_KEY='orders-api'
go run ./examples/basic
```

Use your deployment's SDK URLs. The example waits up to 10 seconds for FeatBit to initialize and exits if startup fails.

```sh
curl http://localhost:8080/feature
# {"enabled":true}
```

The response reflects `my-feature` for the configured service identity; evaluation errors return `false`. For per-user targeting, obtain a stable user ID from your authentication middleware and pass it to `NewEvaluationContext`.

Press Ctrl+C to stop accepting requests, finish active requests, shut down OpenFeature, and close the FeatBit client.
