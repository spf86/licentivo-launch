# Release Launch SDKs

Maintain one public client-only repository at
`https://github.com/spf86/licentivo-launch-sdk`. Use one immutable `vVERSION` tag
for the nine language packages. Never push a Launch server checkout, startup
configuration, account/device cache, signing private key or publishing token.

## Build and verify

The Launch server workspace exports this repository with
`scripts/export-integration-repository.py --out NEW_DIRECTORY`. The exporter uses
an explicit source allowlist and rewrites only the public Go module imports.
Prepared SDK implementations and their LICENSE/NOTICE/VERSION files are included;
consumers do not download another official SDK repository.

Install the normal language runtimes and build dependencies. Linux needs C11,
libcurl/OpenSSL development packages; Windows uses GNU/MinGW, RubyInstaller UCRT
and a matching curl distribution. Ordinary package dependencies are required.

```sh
python -m pip install build twine 'cryptography>=44.0.1,<49'
python scripts/check-source.py
python scripts/build-packages.py --out dist
python scripts/verify-release.py --packages dist
python -m twine check dist/*.whl dist/*.tar.gz
```

For Windows, pass `--curl-root`, `--cc`, `--java-home`, `--dotnet`, `--ruby` and
`--cargo` as needed. Warm the ordinary Cargo dependency cache before packaging.
The builder includes the Windows curl headers/import libraries/runtime DLLs and
their notices in native, Rust and Ruby distributions. Each output directory must
be new. Test built packages in fresh consumers before creating a Release; the
PowerShell runner `scripts/test-managed-installations.ps1` covers the Windows
nine-language distribution. Keep its evidence outside the public source tree.

The package builder also runs `scripts/check-native-consumers.py` against the
installed native package. It compiles and executes independent C and C++
consumers in the default curl discovery mode and with `CURL_NO_CURL_CMAKE=ON`
to force FindCURL module discovery. Both consumers require the exact Launch
package version. These checks cover the SDK 0.11.1 curl target compatibility
fix; keep compiler, architecture and runtime dependencies matched to the package.

The source CI verifies all nine languages against local signed license fixtures,
then builds and inspects their distributions. No test calls a real payment or
production license-activation endpoint.

## Publish the version

Create the public GitHub repository and push the reviewed client source. Wait for
the nine-language CI to pass, then create the matching annotated tag and GitHub
Release. Upload the exact verified artifacts, `integration-packages.json` and
`SHA256SUMS`; do not replace assets or repoint a published tag. A failed build
must be fixed before first publication. After publication, changed package bytes
require a new version.

Go Modules resolves the tagged public module. C/C++ use the compiler-compatible
native CMake ZIP from the same Release. The other language registries are:

| Registry | Package | GitHub environment |
| --- | --- | --- |
| PyPI | `licentivo-launch` | `pypi` |
| npm | `@licentivo/launch` | `npm` |
| NuGet.org | `Licentivo.Launch` | `nuget` |
| Maven Central | `com.licentivo.launch:launch-client` | Manual signed Portal upload |
| crates.io | `licentivo-launch` | `crates` |
| RubyGems | `licentivo-launch` | `rubygems` |

For trusted publishers, restrict owner to `spf86`, repository to
`licentivo-launch-sdk`, workflow to **`release.yml`** and environment to the
specific value above. PyPI and RubyGems support a pending publisher for the first
package. NuGet policy must allow creating/pushing **only `Licentivo.Launch`**.
Confirm the account/namespace on the website before enabling publishing access.

Run **Publish Launch packages** with the exact existing tag and one registry.
The workflow reruns the nine-language checks, downloads that Release, verifies
all artifact hashes, and publishes the selected archive through the scoped
trusted publisher. Publication is explicit rather than triggered by source-only
documentation commits. npm and crates.io can require an owner-authenticated
first publication before trusted publishing can be configured. Use their normal
CLI login or a temporary package-scoped credential saved privately; never paste
tokens into issues, documentation or chat. Do not reuse another project's token.

Java requires the complete Central POM, sources and Javadoc JARs, detached OpenPGP
signatures and checksums. Prepare the signed Portal ZIP, verify it locally, then
upload and wait for `PUBLISHED`. Keep the protected signing key and passphrase
outside Git. Namespace approval, bundle validation and public propagation are
separate steps.

## Verify public installation

Read each registry's public version metadata and install that exact version in a
fresh application. Confirm the installed file is from the registry, it contains
the managed SDK, and the generated direct connection function compiles/runs.
Compare registry downloads with the signed/hashed local artifact where the
registry preserves its bytes. Record each result independently in
`docs/registry-status.json`. Pending/login-required states must not be advertised
as completed releases. Package installation does not establish external merchant,
payment or licensing acceptance.

## Methods used for 0.1.1

Public installation checks passed for all nine languages. PyPI, NuGet and RubyGems
used the scoped GitHub trusted publishers for release.yml. npm used its normal
CLI with the owner's two-factor confirmation. The first crates.io publication
used a temporary publish-new token restricted to licentivo-launch and expiring
on 2026-10-24; the exact reviewed crate bytes were uploaded through Cargo's
published registry API. Maven Central received the signed bundle through its
Portal. Its JAR, POM, sources, Javadoc, signatures and version index were checked
from the public repository. C/C++ use the checksummed GitHub release archives.

The optional npm and crates jobs in release.yml require a separately registered
trusted publisher before they can be used. Their presence in the workflow does
not claim those grants have been configured. No registry token or signing private
key belongs in this repository. See registry-status.json and
registry-verification.json for the actual published version and installation
results.
