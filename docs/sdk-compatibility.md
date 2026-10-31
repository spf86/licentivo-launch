# Launch, SDK and Licentivo compatibility

This guide describes the **Licentivo account-exchange integration in the current source tree**, reviewed on 2026-10-31. Use it with the [application integration guide](software-sdk-integration.md) and [purchase/account boundary](account-purchase-boundary.md). Merchant-owned licensing services use the [custom HTTP integration](external-licensing.md) and their own runtime SDK instead.

## Current source baseline

| Component | Version or contract | Requirement |
| --- | --- | --- |
| Launch server | `0.3.6-beta` | Provides desktop account sessions, short-lived identity assertions, purchase snapshots and durable session-revocation notifications. The server version is defined in `cmd/launch/main.go`. |
| Launch application packages | `0.1.8` | All nine language wrappers use account identity exchange. Install only the Launch package for your language. The source version is recorded in [sdk/VERSION](../sdk/VERSION). |
| Embedded official Licentivo SDK | `0.11.8` | Owns identity exchange with Licentivo, device credentials, signed permissions, feature execution, offline rules and device logout. See [sdk/UPSTREAM_VERSION](../sdk/UPSTREAM_VERSION) and the [reviewed file manifest](../sdk/managed-sdk-manifest.json). |
| Licentivo backend | Account contract `licentivo-launch-account-v1`, including session revocation | Must implement the endpoint set below. This repository does not record a minimum semantic server version; confirm the deployed backend's supported contract rather than inferring it from an SDK version. |
| Licentivo device-information backend revision | Matching server and database schema **51** | The [2026-10-30 source-validation record](sdk-validation-20261030.md) requires this backend revision before using the reviewed clients' `device_info` reporting. This is a Licentivo database revision, not a Launch database version or the minimum version of every account endpoint. |

### Release status and evidence

The public [Launch SDK v0.1.8 release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.8) exists and describes embedded SDK 0.11.8 and device-information support. A published version number does not prove that an uncommitted local change is present in that package.

Before installing or upgrading, check the selected release's source revision, artifact checksums and per-language [publication/installation evidence](https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json). Check the version **inside** each report: the registry reports currently retained under `sdk/release/docs/` identify 0.1.5 with upstream 0.11.5 and cannot certify 0.1.8. Do not relabel historical 9/9 installation results as results for a newer package.

The 2026-10-30 record covers local source/runtime tests on the listed platforms. It does not certify public package installation or a real merchant payment-to-activation workflow. A source-only fix needs a reviewed local build or a release proven to include it. The installation commands in the [package guide](integration-packages.md) target 0.1.8; their presence is not an installation result.

## Required backend endpoints

Launch calls these Licentivo management endpoints with its server-held management key:

| Endpoint | Required behavior |
| --- | --- |
| `GET /api/v1/products`, `GET /api/v1/policies`, `GET /api/v1/policies/{id}` | Discover products, policy versions and runtime settings. |
| `GET /api/v1/launch/connections`, `POST /api/v1/launch/connections`, `PUT /api/v1/launch/connections/{id}` | Register and recover merchant-origin, public signing-key and environment trust connections. |
| `PUT /api/v1/launch/connections/{id}/plans/{plan}` | Register the immutable Launch plan version and Licentivo policy mapping. |
| `POST /api/v1/launch/connections/{id}/events`, `GET /api/v1/launch/connections/{id}/events/{event}`, `POST /api/v1/launch/connections/{id}/events/{event}/retry` | Apply/recover absolute, versioned purchase snapshots using the original event ID. Only `applied` confirms synchronization. |
| `POST /api/v1/launch/connections/{id}/session-revocations`, `GET /api/v1/launch/connections/{id}/session-revocations/{event}` | Apply and recover per-session/account revocations, including the `revoked_before` receipt used to fence old logins. |

The embedded official SDK calls `POST /api/v1/launch/exchange` on Licentivo, then uses its runtime, device and logout APIs. Launch's device page also requires the corresponding device-management API and binding-generation checks. See the [Licentivo contract](licentivo-contract.md) for responsibilities and configuration.

Product/policy reads alone do not prove support for purchase synchronization, identity exchange or session revocation. An older key-only licensing backend is incompatible with this account flow.

## Application flow and the removed credential endpoint

1. `Connect/connect` prepares public merchant, product, environment and signing-key parameters. It does not log in or grant access.
2. `Login` establishes a desktop session using `POST /api/v1/auth/login` on Launch. `Restore` confirms a remembered account online.
3. The wrapper requests `POST /api/v1/software/products/{id}/identity`. Launch returns an assertion valid for **at most 120 seconds**, bounded by the account session expiry and scoped to issuer, audience, session, product, environment and purchase context.
4. The wrapper passes `identity_token`, `launch_connection_id` and `environment` to the embedded official SDK in memory. Its `license_key` is empty in this account flow. The official SDK exchanges the assertion with Licentivo and owns the resulting device credential and signed permission.
5. Every protected business action executes through `RunFeature` or the language's equivalent official SDK guard. A login, paid order, purchase date, successful identity response or `applied` receipt is not runtime permission.

`POST /api/v1/software/products/{id}/credential` is removed and returns **HTTP 410**, code `LICENSE_CREDENTIAL_REMOVED`. Do not retry it as pending authorization or fall back to a cached long-term license key. Upgrade the application to the matching account-exchange wrapper and sign in again. Keep each merchant/account/product/environment's state isolated.

Identity assertions are never stored in remembered account records, public configuration, prompts or logs. Remembered records contain the protected Launch account session and public routing context. `Restore` requires an online account check and a fresh assertion; applications needing offline startup must use the official SDK's own recovery contract. Only that SDK can accept a still-valid signed offline grant.

The unpublished F03/floating source update is recorded in `sdk/managed-sdk-manifest.json` as `licentivo-sdk-v0.11.8+workspace-f03-floating-2026-10-31`. It requires the matching Licentivo server changes: floating plan mappings and purchase snapshots, separate instance/account sessions, bounded seat grants, retained-credential reopening and durable metering settlement. Older published SDK builds reject Launch floating grants during signature validation. A package version of `0.11.8` alone does not identify this source update.

With this update, account-mode task journals use the verified license, product, device and licensing origin, so obtaining a fresh identity assertion during `Restore` preserves pending work. Retain the cache path and operation ID. Resolve an unknown business result from the application's durable outcome before retrying settlement. Upgrade legacy journals while the original source credential/cache remains available; a job ID alone cannot establish another journal's account ownership.

Ordinary `Close`/`Dispose`/`Drop` preserves intentionally remembered login and device binding. Explicit `Logout` marks the local account signed out, delegates device logout to the official SDK and revokes the Launch session. Retain protected pending account-exit tasks until confirmation, and leave the official SDK cache and `.logout.json` recovery journal under SDK control. `ReleaseDevice` is a compatibility entry point for the same explicit logout flow.

## Upgrade and acceptance

1. Record the deployed Launch build, application package, embedded SDK, Licentivo backend revision and artifact/source checksums. Verify the required backend endpoints and, for the reviewed device-information build, the matching schema 51 deployment.
2. Back up the merchant instance. Test the server and matching application package together in sandbox before rolling out account-exchange clients. A server upgrade cannot update an installed client that still calls `/credential`.
3. Recheck the merchant public origin, product/policy mapping, environment, registered identity keys and pinned product keys in **程序接入 → 接入检查与代码**. Use the generated connection function; normal applications do not need JSON configuration files.
4. Migrate confirmed legacy license bindings through `entitlement.migrated` and obtain a confirmed receipt before synchronizing the current purchase snapshot. Preserve uncertain legacy results for manual review; do not replay old license-issuance commands or copy old keys into the new account flow.
5. Run [acceptance](acceptance.md) for login/identity exchange, protected operations, process restart, explicit logout/retry, account switching, product/environment isolation, purchase synchronization, renewals and refunds. Record local fixtures, public package installations and external merchant acceptance separately.

Keep future version changes consistent across `cmd/launch/main.go`, `sdk/VERSION`, `sdk/UPSTREAM_VERSION`, language package manifests, the reviewed SDK manifest, this matrix, installation instructions and generated AI prompts. Historical verification records retain their original versions and dates.
