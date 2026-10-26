# Licentivo account and purchase integration

This guide describes the current Licentivo provider. Merchants using another licensing system can choose the [custom HTTP provider](external-licensing.md); that mode does not call Licentivo or require its API key.

## Administrator configuration

1. Create products and policies in Licentivo.
2. Configure a read/write management key in Launch's **System settings → Licentivo**. The Licentivo origin is fixed to `https://www.licentivo.com`.
3. Choose the policy for each Launch sales-plan version. Launch reads the product, policy version and runtime display settings.
4. Launch registers separate sandbox/live account-trust connections under its one Licentivo management connection, including its identity-signing public key and merchant origin.

The management key remains encrypted on the merchant server. Client installation files contain only public routing information and pinned public product keys.

## Server-side API sequence

All paths below are on Licentivo, called by Launch with its management credential:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/v1/products` | Product discovery |
| `GET /api/v1/policies`, `GET /api/v1/policies/{id}` | Policy discovery and version checks |
| `GET /api/v1/launch/connections` | Find origin/environment trust connections |
| `POST /api/v1/launch/connections`, `PUT /api/v1/launch/connections/{id}` | Register/update public identity keys and routing |
| `PUT /api/v1/launch/connections/{id}/plans/{plan}` | Register the immutable sales-plan version and Licentivo policy mapping |
| `POST /api/v1/launch/connections/{id}/events` | Submit an absolute, versioned purchase snapshot |
| `GET /api/v1/launch/connections/{id}/events/{event}` | Recover/query the original result |
| `POST /api/v1/launch/connections/{id}/events/{event}/retry` | Retry the same event after review |
| `POST /api/v1/launch/connections/{id}/session-revocations` | Notify account/session revocation |
| `GET /api/v1/launch/connections/{id}/session-revocations/{event}` | Verify the revocation receipt |

Launch records verified payments, immutable paid coverage and the outgoing task in one local transaction. A worker sends complete absolute purchase dates and a stable event ID. Only an `applied` receipt confirms synchronization; HTTP receipt alone does not.

Refunds and confirmed reversals remove the corresponding order's coverage. The next full snapshot may preserve an active base purchase, shorten its term, restore a lower plan or terminate all coverage. Retries never add another number of days. Legacy issued-license bindings use explicit migration events rather than automatic replay of old management commands.

## Customer application flow

1. The application signs into Launch using a desktop account session.
2. `POST /api/v1/software/products/{id}/identity` on Launch returns an identity assertion lasting at most 120 seconds, bound to the real session, product, environment, issuer and audience.
3. The Launch package passes it in memory to the official SDK, which exchanges it with Licentivo at `POST /api/v1/launch/exchange`.
4. Licentivo issues the device credential and signed permission. The SDK checks each protected action, device policy, runtime state and offline rule.

A Launch login, paid order or synchronized purchase is not runtime permission. The old `/credential` endpoint returns HTTP 410. Custom-provider delivery is a separate, owner-scoped API and does not restore that endpoint.

Device listing, release quotas, binding generations and logout recovery remain Licentivo responsibilities. Session/account revocations use durable notifications with per-environment receipts; offline revocation remains bounded by already-issued SDK grants.

See [application integration](software-sdk-integration.md), [the purchase/account boundary](account-purchase-boundary.md), [manual integration](manual-integration.md) and [acceptance](acceptance.md). The implementation is in `internal/adapters/licentivo/launch_bridge.go`, `internal/app/launch_trust.go`, `internal/app/purchase_sync.go` and `internal/app/identity.go`.
