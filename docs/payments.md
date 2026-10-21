# Stripe and PayPal integration baseline

Stripe hosted Checkout, PayPal Orders/Capture, payment queries, refunds, and notification verification adapters are implemented. HTTP contract and local-ledger tests have passed. Real provider sandbox acceptance still requires merchant credentials.

## Shared ledger

The merchant owns the payment accounts. Each payment account binds `provider`, `environment`, and `merchant_ref`. Transactions and events are deduplicated within the payment account rather than by a global event identifier alone.

`sandbox` and `live` distinguish payment environments. They do not switch the Licentivo service; a plan's license mapping is shared across both modes.

The server creates price and entitlement snapshots from published plan versions. Amounts are integers in currency minor units. Historical baseline tests use USD. Validate supported currencies and decimal places in each adapter; do not assume every currency has two decimal places. PayPal decimal strings must convert exactly to/from integer minor units.

Verification checks signatures, receiving account, environment, order relationship, amount, currency, and actual captured/paid state. A transaction deduplicates the event, records the payment, updates the order, appends paid coverage, and creates a stable licensing task before acknowledging the event. Unknown/out-of-order results are queried; late payment on a closed order requires reconciliation.

## Stripe

The initial integration uses hosted Checkout with `mode=payment`. Customer-side price/quantity changes must not alter the committed snapshot. The server saves a stable operation identifier; the return page only queries the order and cannot establish payment success.

Signatures are verified over the original body using the endpoint signing secret. `checkout.session.completed` still requires trusted payment-state verification. Delayed success and active queries enter the same ledger path; concurrency or replay must not grant access again. Pin the deployed API version and accepted payment methods. See [fulfillment](https://docs.stripe.com/checkout/fulfillment) and [signature verification](https://docs.stripe.com/events/manage-webhook-endpoints#signature-errors).

## PayPal

The adapter uses Orders v2 with CAPTURE intent. Approval alone is insufficient: the server verifies the final capture, amount, currency, and payee. See [Orders v2](https://developer.paypal.com/api/orders/v2).

Creation, capture, and refund use distinct stable `PayPal-Request-Id` values; retries of one operation reuse its value. Provider retention periods depend on the API. Query unknown outcomes before creating another operation. See [idempotency](https://developer.paypal.com/api/rest/reference/idempotency/).

Webhook verification binds the app, environment, and webhook ID. Use the official verification API; never fetch an arbitrary certificate URL from an untrusted notification. Test timeout and failure handling. See the [Webhook API](https://developer.paypal.com/api/webhooks/v1).

## Refunds and acceptance

Refund requests, submissions, and successful refunds are separate facts. Lock the original payment and enforce its refundable balance. Query an unknown refund outcome instead of submitting another refund. Only a trusted successful refund triggers entitlement recalculation; an upgrade refund must retain legitimate base-plan coverage.

Accept both providers independently: creation, captured payment, incorrect amount, invalid signature, duplicate/out-of-order/missing events, payment queries, create-timeout recovery, full refund, refund-timeout recovery, and sandbox/live isolation. Missing credentials do not block local development, but that provider's external acceptance remains incomplete.
