# Historical verification record

Original baseline: 2026-10-15, Windows, Go 1.26.6, isolated MySQL 8.0.32. This record preserves historical development checks and subsequent notes. It does not certify production readiness or replace a fresh acceptance run.

| Check | Recorded result |
| --- | --- |
| `go test ./...` | Passed; MySQL integration tests explicitly skipped without test configuration |
| `scripts/test-mysql.ps1` | Passed with isolated data directory, random credentials, and temporary port |
| First/repeated GORM AutoMigrate | Passed; core tables created from Go models |
| Database constraints | Rejected negative amounts; enforced idempotency, transaction/event deduplication, one current license binding, historical replacement, task versions, and payment-environment relationships |
| Licentivo settings | One connection, no licensing environment selector, versioned shared mapping; purchase/renewal/upgrade/refund/synchronization doubles passed |
| DeepL | Ten locales, UI/brand/product/plan translations, key encryption, queue deduplication/resume, and fallback verified with doubles |
| Contract arithmetic | Month-end anchors, leap years, UTC conversion, calendar months versus 30 days, and expired renewal passed |
| Upgrade amounts | Integer rounding, overflow prevention, and cross-currency/expired quote rejection passed |
| Secret encryption | AES-256-GCM nonce/context handling and wrong-key/tamper/truncation rejection passed |
| Licentivo event signatures | Raw bytes, timestamp window, explicit legacy headers, and conflicting-header rejection passed |
| `go vet ./...` | Passed |
| Windows amd64 build | Passed |
| Linux amd64, CGO disabled | Cross-build passed; Linux runtime not exercised |
| HTTP smoke check | Root, CSS, JavaScript, health, and system status returned 200 in a test preview |
| Desktop connection settings | No duplicate Licentivo environment/connection fields; separate payment environments; no overflow at width 1280 |
| Unimplemented payment route at M0 | Returned 404 instead of acknowledging an unimplemented event; this is a historical state, not the current adapter inventory |
| OpenAPI subset | JSON parsing passed for the three system endpoints |

An initial MySQL run was blocked by sandbox TCP permissions. The same isolated script later passed in an approved host execution environment. Disposable previews used test-only data.

Docker/Compose was not run because Docker was unavailable. A real Licentivo catalog/policy read and plan mapping were subsequently verified; management operation/recovery tests used automation. First real license issuance remains part of test-payment acceptance. Real DeepL, Stripe/PayPal sandbox payments, Resend delivery, complete language/OS SDK flows, full accessibility, and production deployment remain unaccepted. Local tests must not be presented as a verified external commercial workflow.

## Public source preparation, 2026-10-21

`go test ./...`, `go vet ./...`, and `node --check internal/httpapi/web/app.js` passed. This run did not enable MySQL integration tests or real provider calls. All repository Markdown and configuration documentation was checked for remaining Chinese text, and relative Markdown links were checked for existing local targets. Publication files were checked to exclude private development configuration, local database files, uploads, backups, binaries, caches, and screenshots. The source includes the notices for all server dependencies listed in `go.mod`.

## Initial-account and release verification, 2026-10-21

The isolated MySQL test suite passed with a separate bootstrap test database. It covers concurrent first starts creating one administrator, direct initial login without a setup token, blocked administration/desktop access before a password change, rejection of an unchanged/short replacement, old-session revocation, old-password rejection, and preservation of the changed password on a later start. The CLI initialization test verifies an independent generated master key and refusal to overwrite an existing configuration.

The release builder targets Windows amd64/arm64 and Linux amd64/arm64 with CGO disabled. It packages only the executable, English installation text, a public configuration example, version/source information, and license notices, and writes SHA-256 checksums. Windows amd64 can be smoke-tested on the development host; Linux and ARM64 execution is not claimed as natively verified there.

## Payment, email, and interface verification, 2026-10-22

`go test ./...` passed with isolated MySQL databases for schema, application, restore, and administrator-bootstrap tests. `go vet ./...` passed. The separate `TestNewPaymentProvidersAndRegistrationWithoutMail` integration test also passed using production Waffo/Paddle adapters against controlled HTTP fixtures and a fresh isolated MySQL schema.

The new tests cover signed Waffo callbacks and acknowledgements, raw Paddle signatures and timestamp limits, exact amount/currency and account binding, captured payment state, duplicate events, checkout/refund recovery across service restarts, one outbound create attempt, pending refund approval, confirmed full refunds, rejected refunds retaining paid access, and removal of refunded coverage. Adapter tests cover malformed results and untrusted redirect/pagination destinations.

Email tests cover registration and purchase without delivery, no false verified flag, no undeliverable outbox accumulation, generic SMTP credentials, default verification with email enabled, purchase blocking before verification, successful verification links, independent preferences surviving restart, password recovery with required verification disabled, and payment/software notifications. The original and isolated MySQL servers were left running.

No Waffo/Paddle sandbox account or real email credentials were supplied. These automated results do not establish real checkout acceptance, inbox delivery, settlement, or arrival of refunded funds. Complete the external acceptance steps in [Waffo and Paddle setup](waffo-paddle.md) and [email setup](resend-email.md) before live sales.

The final browser review covered the selected desktop settings design, mobile settings and product editor, storefront/product/document/download pages, admin list pages, and customer-account tabs. It fixed missing empty-state helpers, incorrect settings shortcuts, a false unsaved-change prompt after reverting a form, and mobile footer overflow. Payment-channel switching and reverted-mail-setting navigation passed; the final browser error log was empty. See [interface design QA](../design-qa.md) for comparison conditions, evidence, and review limits.

## Application integration and nine-language scope, 2026-10-22

The integration work now follows the official SDK 0.11.0 source inventory: Go, Python, C#, Java, JavaScript/Node.js, Rust, Ruby, C and C++. TypeScript declarations belong to the JavaScript package; the Go Web backend is an additional example. The prepared SDK is version locked and has a file/hash manifest. Local wrapper package version is 0.1.0; no registry publishing occurred.

The final unified local run passed all nine language targets. Evidence and individual logs are in `artifacts/integration-checks/nine-languages-verified-20261022/report.json`. Each language exercised the actual official SDK against locally signed Ed25519 fixtures, including restart/offline authorization, protected-feature denial and tampered-cache rejection. Python passed 12 tests; JavaScript passed 12; Rust passed 10; Ruby passed 12 wrapper checks plus its official runtime test. C# passed its wrapper suite and five real SDK runtime groups; Java passed seven lifecycle groups and its real SDK runtime suite; Go and the shared C/C++ suite also passed. These checks ran on Windows amd64 with Go 1.26.6, Python 3.12.14, .NET SDK 8.0.426, JDK 17.0.20.1, Node.js 24.15.0, Rust 1.99.0, Ruby 3.4.11 and CMake 4.4.4.

The earlier unified run is retained as failure evidence. It caught a nonblocking accepted socket in the Rust fixture and conflicting MinGW runtime DLLs for the C++ test. The fixture now explicitly uses blocking reads with a timeout; the runner supports a separate native-runtime DLL path. The final run above passed after those corrections. Successful local checks do not certify other operating systems or an external commercial workflow.

The local distribution is under `artifacts/integration-packages-0.11.0/`: Python wheels, .NET NuGet packages, npm tarballs, Ruby gems, Java JARs/POMs, a Rust crate, Go source module and C/C++ CMake installation, plus individual and combined source archives. Its manifest records all nine languages, wrapper/upstream versions and artifact types. The native binary archive targets this Windows x64 build toolchain; no cross-platform binary qualification or registry publication is claimed.

The complete Go server suite passed with fresh isolated MySQL databases after the integration/status changes. An attempted reuse of an earlier populated test schema correctly failed its empty-database safety check; a new run using `scripts/test-mysql.ps1` passed. Targeted Go vet checks, JavaScript syntax checks, and the OpenAPI checker passed. OpenAPI now documents 18 account, integration, system and basic order operations, with local references and server route correspondence checked; this is not a claim of full administrative API coverage or validation against every OpenAPI rule.

The acceptance CLI's eleven local tests passed. These cover sandbox-only configuration, no credential redirects, secret rejection/redaction, missing-server failure, status/credential consistency, exact denial HTTP status, five-attempt pending bounds, incompatible command option rejection, and mandatory synchronous SDK online refresh for the external activation probe. Reports distinguish a local merchant probe from an activation probe that contacts the external licensing service. The tool cannot mistake a cached offline grant for first online activation evidence.

The CLI also passed actual HTTP probes against the isolated local preview: readiness, sandbox catalog, a new disposable customer's login, `not_purchased` status, rejected credential access, and automatic probe-session revocation. Evidence is under `artifacts/acceptance/local-20261022/`. Its remote product ID is a local placeholder unused by these probes; no remote activation is claimed. Reports deliberately retain `acceptance_complete: false`.

Browser review of the integration wizard verified all nine language options, the separate Go Web example, inline invalid-public-key JSON feedback, missing mapping/configuration checks, and absence of download controls for an unready configuration. The 1265-pixel-wide page had no horizontal overflow. The screenshot is retained in the same local evidence directory. Public configuration accepts copied official product IDs/signing public keys or existing public JSON, without requiring the removed upstream configuration-download workflow.

External payment, refund settlement and live Licentivo acceptance were deferred at the user's request. The reproducible sandbox tool and matrix are in [acceptance.md](acceptance.md). Local SDK fixtures and native compilation do not establish real checkout or production acceptance.

## Direct initialization verification — 2026-10-23

Launch now reads its effective configured public origin and the selected software's existing published product/policy mapping. The management adapter retrieves and validates that product's Ed25519 signing-key bundle. Neither internal product ID nor a public configuration file is entered by the integrator. The same pure generator supplies direct connection functions to the manual integration page and English AI prompts for all nine languages.

The isolated MySQL suite passed with automatic mapping/key retrieval, admin-only deployment context, rejection of user-supplied credential/configuration objects, and sandbox/live separation. All 20 prompt/state tests passed, including different merchant domains/IPs/ports, stale origins, public-field sanitization and English-only output. go vet and the updated server build also passed.

Fresh consumer installations passed 9/9 against the locally built managed artifacts; the generated code is compiled/executed without startup JSON files or a separate official SDK package. The test includes escaped Unicode/quote/interpolation input as public data. Evidence is in artifacts/integration-checks/direct-installs-20261023/report.json. The isolated browser fixture exercised nine manual code views and nine auto-filled English prompts; its evidence is in artifacts/integration-checks/direct-ui-20261023/. This browser fixture is UI evidence, not an external merchant deployment.

The C/C++ Release test drivers explicitly retain their regression assertions so optimized builds cannot skip authorization calls embedded in assertions. Per-language runtime logs record signed grant, offline restore, feature denial and tamper behavior. Windows amd64 GNU is the native qualification platform for this local distribution; external commercial acceptance remains unconfigured as requested.

## Public Launch SDK 0.1.1 publication, 2026-10-23

All nine language distributions are published in the separate [Launch SDK repository](https://github.com/spf86/licentivo-launch-sdk) and their normal registries: Go Modules, PyPI, NuGet, Maven Central, npm, crates.io and RubyGems, with C/C++ native CMake archives on GitHub Releases. The immutable v0.1.1 source commit is ee05e70fd67eafaca1a7ea2cbb619d0f501a2495. The original v0.1.0 tag and assets remain unchanged and are marked as superseded because their npm archive omitted TypeScript declarations.

Public download, clean installation and generated direct initialization passed 9/9. Python also passed a normal dependency installation in an isolated virtual environment. The public npm tarball passed strict TypeScript compilation. NuGet's repository signature changes the ZIP envelope, while all eight original payload files match the reviewed package. Maven Central's JAR, POM, sources, Javadoc, four PGP signatures and version index were verified; its portal shows PUBLISHED. The public Rust crate, Ruby platform gem and native archives match the release checksums. Go resolves the public tagged module without a local replace. Independent C and C++ consumers compiled against a relocated downloaded native installation.

The archived [machine-readable publication status](https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status-0.1.1.json) and [public installation evidence](https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-verification-0.1.1.json) retain package names, versions, checksums, installation results and platform limits. Windows fresh-artifact checks and Linux source/signed-runtime CI checks also passed 9/9. English AI prompts and the manual guide used the verified public installation commands; the 20 prompt/state checks and the Launch server build passed.

## Public Launch SDK 0.1.2 publication, 2026-10-23

Current installation commands, native download links, language READMEs and English AI prompts target Launch SDK 0.1.2 with embedded official SDK 0.11.1. The upstream update fixes installed C/C++ CMake targets across curl discovery modes; the licensing protocol and client API remain unchanged. Historical release and 0.1.1 verification records above retain their original versions.

`node --test scripts/integration-prompts.test.mjs` passed 17/17 after updating the pinned versions. The checks cover all nine languages plus the Go Web option, direct initialization, English-only output, configuration validation and sensitive-field rejection; generated prompts cannot recommend the superseded 0.1.1 / 0.11.0 versions.

The [v0.1.2 GitHub release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.2) is published at immutable source commit `ac983627784de4ca38c2e71e85b60526bb046251`. Public download, independent installation and generated initialization checks passed **9/9**: Go, Python, C#, Java, Node.js/TypeScript, Rust, Ruby, C and C++. Python installed normal dependencies in a new virtual environment; npm passed strict TypeScript compilation. NuGet's repository signature is present and all nine original payload files match the GitHub artifact. Ruby's platform gem matches the release checksum without an official SDK gem.

Go resolved v0.1.2 with the standard `https://proxy.golang.org,direct`, no local replacement, and checksum-database verification; `go mod verify` passed. Its first proxy-only attempt hit a cached proxy-side GitHub DNS failure on `.info`; `.mod` and `.zip` subsequently downloaded from the public proxy. The failed attempt is retained. The Ruby runner's initial misplaced `--norc` option was corrected and a new independent installation passed; this was a verification-script error, not a change to the published package.

Downloaded native archives passed checksum verification and were relocated before compiling independent C/C++ consumers with exact Launch 0.1.2 and upstream 0.11.1 requirements. The native checker and generated initialization snippets passed both default curl discovery and `CURL_NO_CURL_CMAKE=TRUE`. These public installation checks ran on Windows amd64 with GNU/MinGW.

Detailed local evidence is [artifacts/sdk-012-public-summary.json](../artifacts/sdk-012-public-summary.json), with per-language receipts and command logs. The committed [publication status](../sdk/release/docs/registry-status.json) and [verification receipt](../sdk/release/docs/registry-verification.json) summarize those observations. Maven Central deployment `cac96184-c336-4799-a759-9d59a73d3d95` has public JAR, POM, sources and Javadoc files; all four detached signatures verified against the release public key, the JAR/POM match GitHub, the version index includes 0.1.2, and independent JDK 17 compilation and generated initialization passed. Rust was installed from the exact crates.io version in an isolated Cargo home, with no local path or replacement; its crate matches the GitHub checksum and generated initialization passed. The [0.1.2 publication record](../sdk/release/docs/publication-0.1.2.md) records the completed nine-language public checks. Earlier 0.1.1 receipts remain archived and do not certify 0.1.2.

These publication and SDK checks do not replace the external merchant/payment/licensing workflow. Its acceptance remains deferred as requested, and reports retain external_acceptance_complete: false.

## Custom licensing provider verification, 2026-10-25

The source now supports merchant-owned HTTP licensing alongside the existing Licentivo integration. Each product/environment has an explicit provider and mapping; paid coverage and queued events retain the original external route. Custom authorization is implemented by the merchant service and its own runtime SDK.

- The isolated `scripts/test-mysql.ps1` run passed across all server/client packages, with separate fresh databases for custom licensing, the existing Licentivo purchase bridge, and product-mapping concurrency tests. The custom lifecycle covers verified purchase, a lost HTTP response and service restart, duplicate prevention, owner/environment delivery isolation, renewal, upgrade, refund coverage, account disable/enable, manual suspension, receipt mismatch and original-event recovery. No Licentivo management connection is configured in that custom test.
- Production-source static checks passed with `go vet ./cmd/... ./internal/... ./pkg/... ./docs/...`. Root-wide checks also enumerate unrelated ignored artifact source trees in this development checkout, so those artifact consumers are outside this server-change verification.
- All 40 frontend logic tests passed. The OpenAPI checker resolved 31 Launch operations and their local references/routes; the external receiver specification separately contains four operations.
- The Python adapter HTTP/SQLite test passed, checking request signatures, persistent receipts, event conflict rejection, idempotency and delivery. Its test backend is a fixture, not a license generator.
- Legacy backup compatibility tests passed. New custom state cannot be accepted from an archive missing its external tables; older archives without custom state may omit those additive tables.
- A disposable browser preview verified creating a signed loopback sandbox connection, saving product/plan mappings, persistence after refresh and continued access to the Licentivo settings page. A local screenshot is retained at artifacts/custom-licensing/settings.png. The preview uses synthetic credentials and disposable databases.

Payment, licensing and SDK runtime outcomes in this run use controlled fixtures. A merchant must complete acceptance against its own authorization service and application before live sales. See [custom licensing integration](external-licensing.md), [external OpenAPI](external-licensing-openapi.json) and [adapter example](../examples/custom-licensing/README.md).
