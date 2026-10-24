# Application integration acceptance

> Account-mode probes now obtain short-lived identity assertions. Negative Launch purchase status is purchase evidence only, not proof of Licentivo runtime denial. Runtime proof requires the official SDK probe; no production deployment or SDK publication is implied. See [account boundary](account-purchase-boundary.md).

There are two separate evidence levels: controlled local automated checks and external merchant-sandbox probes. A test double, successful catalog read, generated configuration or passing health check never establishes real payment, license issuance or activation. External service configuration was explicitly deferred for this development iteration.

## Local automated checks

Run from the repository root:

```powershell
go test ./...
go vet ./...
node --check internal/httpapi/web/app.js
node --check internal/httpapi/web/integration.js
node --check internal/httpapi/web/integration-prompts.js
node --test scripts/integration-prompts.test.mjs scripts/integration-state.test.mjs
python scripts/test_acceptance.py
./scripts/test-mysql.ps1
```

The MySQL helper creates a new isolated server and databases under `.cache`; it must not be pointed at a customer database. A plain `go test ./...` skips database scenarios unless the isolated DSNs are provided. SDK/package tests and builds are described in [integration-packages.md](integration-packages.md). Preserve the test output and tool versions with the release record.

The local matrix includes authentication, conditional email verification, product/environment ownership, pending/expired/suspended status, payment event replay, licensing reconciliation, renewals/refunds, account switching, protected session persistence and offline fallback decisions. Lifecycle tests use controlled runtimes; actual cryptographic runtime verification remains the official SDK's responsibility and is separately exercised before release.

For **运营工具 → AI 快速接入**, check all nine language choices, platform/project/feature selection, an unconfigured draft, and a prompt generated from validated public configuration. Check copy and download against the visible prompt, and ensure changing the product or environment cannot retain a previous validated configuration. Prompts must exclude sessions, passwords, runtime license/device credentials and management keys; generating or copying a prompt must not call an AI service. The source-bundle Node test command above exercises the pure generator. A generated prompt, or code subsequently produced by an AI tool, still needs the language tests and the external acceptance steps below.

## Prepare external sandbox acceptance

1. Deploy a dedicated Launch test instance with a reachable HTTPS origin and provider webhooks. Keep live sales disabled.
2. Configure **one** payment provider's sandbox credentials and a Licentivo read/write connection. Payment sandbox records are isolated, but they still use the configured Licentivo service and can create real test licenses/device records. Use a dedicated test product/policy and test customers.
3. Publish a sandbox plan and use **程序接入** to export `launch-config.json` and validated `product.json` into `.local/acceptance/`.
4. Install only the reviewed Launch Python package locally: `python -m pip install ./sdk/python`, or install its wheel. Its private SDK implementation is included.
5. Use an unmetered test feature such as `export`. `--activate` starts the official SDK, synchronously calls `refresh_online()`, requires an active online status, and invokes its protected-operation API. A cached offline grant alone cannot pass this probe; this probe does not exercise metering.

Credentials are prompted without echo. Automation can supply `LAUNCH_ACCEPTANCE_EMAIL` and `LAUNCH_ACCEPTANCE_PASSWORD` in its private process environment. Do not put passwords in command arguments, reports, screenshots or committed files. The tool revokes its temporary desktop session after each authenticated probe, but does not deactivate devices on normal exit.

```powershell
python scripts/acceptance.py preflight --config .local/acceptance/launch-config.json --report .local/acceptance/01-preflight.json
python scripts/acceptance.py verify --config .local/acceptance/launch-config.json --expect not_purchased --report .local/acceptance/02-no-purchase.json
```

The tool refuses live environment configuration, non-loopback HTTP, credential-bearing redirects, mismatched products and public configuration containing runtime secrets. It rejects activation/order assertions outside `verify` and checkout creation options outside `checkout` before making requests. Existing reports are never overwritten. Reports contain allowlisted evidence, not raw responses, access tokens or license keys. A missing server or SDK creates a failed probe, not a synthetic success.

Local merchant probes use `scope: local_sandbox_probe` and `licensing_scope: not_called`. `verify --activate` contacts the official external licensing service even when the merchant runs on localhost; that combination is recorded as `local_merchant_external_licensing_probe` with `licensing_scope: official_external_service`.

## First payment and activation

Create a test checkout in the merchant website, or create one with a stable operation UUID:

```powershell
$operation = [guid]::NewGuid().ToString()
python scripts/acceptance.py checkout --config .local/acceptance/launch-config.json --provider stripe --plan-version-id PLAN_VERSION_UUID --operation-id $operation --report .local/acceptance/03-checkout.json
```

Replace the provider and plan version with your published sandbox plan. Retain and reuse the same operation UUID when retrying an uncertain checkout creation. The tool prints the order ID. Complete that owned order through the merchant account center using the provider's official sandbox checkout. Provider UI interactions are deliberately not simulated by the probe.

After the provider confirms payment and the webhook is received:

```powershell
python scripts/acceptance.py verify --config .local/acceptance/launch-config.json --order-id ORDER_UUID --expect-order paid --expect active --activate --product-config .local/acceptance/product.json --feature export --report .local/acceptance/04-activation.json
```

This checks the authenticated account, owned sandbox order, entitlement, credential/product correspondence and an actual official SDK protected operation. Pending authorization is tried at most five times within a bounded window (default 30 seconds, maximum 120), respecting `Retry-After`. Unexpected authorization failure is recorded without exposing the SDK's raw error text.

## Lifecycle acceptance matrix

For each released language/OS pair, save a dated record with source revision, Launch/client/SDK versions, provider environment, anonymized customer label, order IDs, expected result, observed result and relevant report filename. Mark unexecuted rows **not run**. Do not mark the whole matrix passed from the first-payment probe.

| Scenario | Procedure | Required evidence |
| --- | --- | --- |
| Registration / verification | Register without purchase; repeat with verification enabled and disabled | Disabled verification does not block; enabled verification produces `EMAIL_VERIFICATION_REQUIRED` until completion |
| No purchase | Run `verify --expect not_purchased` | Credential denied; no protected operation runs |
| First purchase | Complete sandbox payment, then run the activation probe | Provider confirms payment; one paid entitlement, one synchronized license, SDK operation succeeds |
| Restart | Close the chosen language example with remembered login, launch a new process and restore | No password prompt; same account/product cache and valid signed grant |
| Offline startup | After a successful remembered activation, disable networking for that test process/machine and restart | Signed cached grant permits only policy-allowed operations; no-offline/floating policy refuses startup |
| Offline expiry / clock rollback | Use a short-lived test policy and a disposable test machine | SDK rejects expired/rolled-back state; feature callback does not execute |
| Renewal / upgrade | Buy a renewal or quoted upgrade through the sandbox storefront | Correct order/coverage; new validity/features visible after sync and SDK refresh |
| Refund | Request/approve a full sandbox refund; wait for provider confirmation | Provider refund and local coverage agree; `verify --expect suspended` or `--expect expired` as appropriate; other paid coverage remains usable |
| Logout / account switch | Logout A, sign in as B without a purchase; restart and inspect behavior | A's saved session/cache removed; B cannot use A's license, devices or features |
| Product / environment isolation | Use another product and separately switch sandbox/live configurations | Credentials and caches cannot cross identity scopes; the external probe itself refuses live |
| Device full / release | Activate on the policy's maximum distinct test devices; attempt one more, then explicitly release one | SDK rejects the excess device and permits replacement after confirmed release; ordinary close does not consume release quota |
| Recovery | Restart Launch during pending issuance, replay a signed sandbox webhook | Single order coverage/license, eventual reconciliation; no duplicate checkout or charge |

Refund/suspension effects on a previously issued offline grant remain bounded by the official SDK's signed offline policy. Immediate revocation while disconnected is not claimed. A long Launch outage at contract expiration can delay upstream suspension; see [the contract limitations](licentivo-contract.md).

## Completion and retained evidence

Every probe report deliberately sets `acceptance_complete: false`: it records only the checks actually executed in that invocation. Review the matrix and provider records before accepting a release. Configure external services later and resume the commands above; do not replace missing results with a passed flag or enable live sales merely because local tests pass.
