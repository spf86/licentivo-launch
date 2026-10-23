# Waffo and Paddle setup

Launch supports one-time purchases and manual renewals through Waffo Payment API and Paddle Billing. It verifies provider results on the server before granting access. Full refunds are supported; partial refunds and automatic recurring charges are not implemented.

Automated adapter and MySQL integration tests exercise both providers. Real sandbox checkout, webhook delivery, and refunds still require merchant credentials and deployment-specific acceptance.

## Waffo Payment API

This adapter targets Waffo's standard Payment API, not Waffo Pancake.

1. Obtain a merchant account and credentials for the intended environment from [Waffo](https://waffo.com/).
2. Open **System settings > Payments**, choose **Sandbox**, and select **Waffo**.
3. Enter the merchant ID, API key, merchant RSA private key, and the environment's Waffo public key. Keys may use PEM or Base64-encoded DER. Keep the private key on the server.
4. Choose **Verify and save**. Launch checks the merchant configuration and displays this account's webhook URL.
5. Configure payment and refund notifications for that URL in Waffo. The checkout request also supplies its payment notification URL.
6. Publish a sandbox plan, make a test payment, and request/approve a full refund in Launch.

The adapter uses `api-sandbox.waffo.com/api/v1` for sandbox and `api.waffo.com/api/v1` for live. It signs raw JSON using RSA/SHA-256, verifies signed responses when present, and requires signed callbacks. Payment and refund callbacks use `PAYMENT_NOTIFICATION` and `REFUND_NOTIFICATION`. Waffo receives a signed raw success acknowledgement after the notification is stored durably.

Launch queries the original transaction or refund before changing the ledger. A redirect to the store does not establish payment success. Only a matching `PAY_SUCCESS` payment or `ORDER_FULLY_REFUNDED` refund completes its corresponding workflow. `REFUND_IN_PROGRESS` remains pending; `ORDER_REFUND_FAILED` fails the attempt. A partially refunded result requires review and cannot remove coverage as a full refund.

See the [official Waffo Go SDK](https://github.com/waffo-com/waffo-go) and [signature verification](https://waffo.com/docs/ja/developer-docs/webhook/signature-verification).

## Paddle Billing

1. Create a [Paddle sandbox account](https://sandbox-vendors.paddle.com/). Create an active product representing the software being sold.
2. Create a sandbox API key with `transaction.read`, `transaction.write`, `adjustment.read`, `adjustment.write`, and `product.read` permissions.
3. Create a client-side token for Paddle.js. This public token is separate from the secret API key.
4. Create a notification destination and obtain its endpoint secret. Subscribe to `transaction.completed`, `transaction.paid`, `transaction.updated`, `transaction.canceled`, `adjustment.created`, and `adjustment.updated`.
5. In **System settings > Payments > Sandbox > Paddle**, enter the API key, client-side token, seller ID (`slr_...`), product ID (`pro_...`), and webhook secret. Choose **Verify and save**.
6. Copy the displayed webhook URL to the notification destination. Use the displayed `/pay/paddle/{account-id}` page as the default payment link and configure the checkout domain in Paddle. Complete domain approval before live checkout.
7. Publish a sandbox plan and complete checkout. Approve a full-refund request in Launch and verify its final result in both systems.

Launch checks the API root to bind the API key to the seller, environment, and required permissions, then verifies that the product is active. Prices are one-time, tax-inclusive amounts taken from the published Launch plan. Configure the product's tax category correctly in Paddle. Current Launch currencies are USD, EUR, GBP, CAD, and AUD.

The dedicated payment page loads official Paddle.js and opens the existing transaction using `_ptxn`. It exposes the public client token only. The server retains the secret API key and webhook secret. Restrictive checkout-specific CSP allows Paddle's official scripts and frames.

`paid` alone does not grant access: Launch requires a completed transaction with captured payments covering the exact order amount and no credit balance. Signatures are checked over the original request bytes with timestamp tolerance. Duplicate events cannot add duplicate paid coverage.

Paddle may return `pending_approval` for refunds. Launch keeps the order in the refund workflow and preserves its current coverage until approval is confirmed. `approved` completes the refund; `rejected` or `reversed` marks the attempt failed and restores the paid order. Provider confirmation and arrival of funds in the customer's bank account are separate events.

Paddle does not offer client-supplied idempotency keys for these API calls. Launch records an outbound attempt before creating a transaction or adjustment. After an ambiguous timeout or restart, it searches by the stored operation metadata or refund reason instead of issuing another create request. If recovery cannot find the result, the operation stays unresolved for review; it does not silently risk a duplicate charge or refund.

Official references: [create transaction](https://developer.paddle.com/api-reference/transactions/create-transaction/), [API root](https://developer.paddle.com/api-reference/api-root/get-api-root/), [Paddle.js checkout](https://developer.paddle.com/build/transactions/pass-transaction-checkout/), [refund adjustments](https://developer.paddle.com/api-reference/adjustments/create-adjustment/), and [webhook signatures](https://developer.paddle.com/webhooks/about/signature-verification/).

## Acceptance before live sales

Verify each provider independently: checkout success and cancellation, invalid signatures, repeated and delayed callbacks, exact amount/currency binding, missing-callback reconciliation, full refund, approval delay/rejection, restart recovery, and sandbox/live isolation. Verify the resulting software access and refunded coverage in the customer account. Use a public HTTPS URL reachable by the provider.

Do not enter live credentials into the sandbox form. Blank secret fields retain existing credentials only for the same provider, environment, and merchant. The settings API never returns stored secrets.
