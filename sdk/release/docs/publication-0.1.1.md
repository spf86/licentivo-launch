# Launch SDK 0.1.1 publication

Release: [https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.1](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.1).

9/9 public installation checks passed. All nine language distributions are published and verified.

Go resolves the immutable v0.1.1 module. Python, npm, NuGet, crates.io and RubyGems use their standard package managers. C and C++ use the checksummed native GitHub release. Java uses the same signed JAR and POM in Maven Central.

Each package contains Licentivo SDK 0.11.0 and its default adapter. The integration uses direct parameters; no separate official SDK or client configuration file is required. Strict TypeScript compilation passed against the publicly installed npm package. The NuGet repository signature changes its ZIP envelope; all eight original package payload files match.

Windows clean-consumer checks and Linux source/signed-runtime checks passed 9/9. The public Windows GNU native archive was relocated and compiled in independent C and C++ consumers. The Linux native archive was built and checksummed in CI.

The original v0.1.0 tag and assets remain intact. The 0.1.1 packaging fix includes the TypeScript declaration file and rejects missing npm exports before publication.

See the archived [machine-readable status](registry-status-0.1.1.json), [installation evidence](registry-verification-0.1.1.json) and [manual integration](manual-integration.md). External merchant/payment/licensing acceptance remains deferred.
