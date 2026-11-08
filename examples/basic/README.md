# Checkout shipping promotion example

This example shows a flag changing a real business decision: whether a checkout gets free shipping. The default flow charges `$5.99`; when the flag is enabled and the cart subtotal is at least `$50`, the quote applies free shipping. It only creates a quote; it does not place an order or charge a payment method.

Create a boolean flag in your FeatBit environment named `checkout-free-shipping`. From the repository root:

```sh
export FEATBIT_ENV_SECRET='<your-environment-secret>'
export FEATBIT_STREAMING_URL='ws://localhost:5100'
export FEATBIT_EVENT_URL='http://localhost:5100'
export FEATBIT_FLAG_KEY='checkout-free-shipping'
go run ./examples/basic
```

Use your deployment's SDK URLs. The example waits for FeatBit to initialize and exits if startup fails.

Open http://127.0.0.1:1234/, enter a demo customer ID and subtotal, then request a quote. In FeatBit, enable the flag for that customer and save it. A subtotal of at least `$50` should then produce a `$0.00` shipping charge; disable the flag or use a subtotal below `$50` to get the standard `$5.99` charge. The page shows the shipping decision, total, evaluation reason, and any fallback error.

```sh
curl -X POST http://localhost:1234/checkout/quote \
  -H 'Content-Type: application/json' \
  -d '{"customerId":"demo-customer-123","subtotalCents":6500}'
# {"flagKey":"checkout-free-shipping","promoApplied":true,"subtotalCents":6500,"shippingCents":0,"totalCents":6500,"reason":"TARGETING_MATCH"}
```

`FEATBIT_FLAG_KEY` defaults to `checkout-free-shipping`; `LISTEN_ADDR` defaults to `127.0.0.1:1234`. On evaluation errors, the quote uses standard shipping and includes an `errorCode`. The form's customer ID is for demonstration only. In a real service, take the stable customer ID from authenticated request context rather than trusting an ID supplied by the caller.

Press Ctrl+C to stop accepting requests, finish active requests, shut down OpenFeature, and close the FeatBit client.
