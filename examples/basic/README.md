# HTTP server example

Choose a boolean flag in your FeatBit environment. From the repository root:

```sh
export FEATBIT_ENV_SECRET='<your-environment-secret>'
export FEATBIT_STREAMING_URL='ws://localhost:5100'
export FEATBIT_EVENT_URL='http://localhost:5100'
export FEATBIT_FLAG_KEY='my-feature'
export SERVICE_TARGETING_KEY='orders-api'
go run ./examples/basic
```

Use your deployment's SDK URLs. The example waits up to 10 seconds for FeatBit to initialize and exits if startup fails.

Open http://127.0.0.1:8080/ for a live view that evaluates the flag every second and records changes. Toggle the flag in the FeatBit UI and save. To see `true` and `false`, configure the enabled rule to serve `true` for this service and the disabled variation to return `false`.

The page shows the evaluated value, reason, and any fallback error. `DISABLED` means the flag is off; an enabled flag may still evaluate to `false` depending on its targeting rules.

```sh
curl http://localhost:8080/feature
# {"flagKey":"my-feature","enabled":true,"reason":"DEFAULT"}
```

`FEATBIT_FLAG_KEY` defaults to `my-feature`. `LISTEN_ADDR` defaults to `127.0.0.1:8080`. Evaluation errors return `false` with an `errorCode`. For per-user targeting, obtain a stable user ID from your authentication middleware and pass it to `NewEvaluationContext`.

Press Ctrl+C to stop accepting requests, finish active requests, shut down OpenFeature, and close the FeatBit client.
