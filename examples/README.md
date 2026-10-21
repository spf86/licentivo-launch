# Application integration examples

Read the [application integration guide](../docs/software-sdk-integration.md) for the API sequence, security rules, SDK preparation, and language-specific commands.

The official Licentivo SDK handles machine identity, signatures, activation, heartbeats, offline cache, and metering. Launch's account adapter signs in to the merchant service and retrieves only the authenticated account's product credential.

| Technology | Reference implementation |
| --- | --- |
| C | `native/account.c` and `c/main.c` |
| C++ | `native/account.c` and `cpp/main.cpp` |
| C#/.NET 8+ | `csharp/Program.cs` |
| Go | `../pkg/launchclient` and the separate module in `go/` |
| Python | `python/main.py` |
| Web backend | Separate module in `web-backend/` |

## Prepare the SDK

The upstream SDK is obtained separately; `examples/vendor/` is ignored by Git. Point the helper at a compatible SDK source directory containing `c/licentivo.h` and `go/go.mod.template`:

```powershell
./scripts/prepare-examples.ps1 -Source 'C:\path\to\licentivo-sdk\sdk'
```

Run the command from the repository root. See the full guide for CMake, .NET, Go, and Python commands. The Launch server itself does not require the SDK vendor directory.

Installers contain only public product configuration. End users sign in rather than manually entering license keys. Credentials and cache state are isolated by merchant origin, account, product, and payment environment; logout clears the current account's local session and scoped cache.

These are local reference implementations. Before a release, test real registration/verification, payment-to-activation, pending issuance, credential recovery, restart, denied access, renewal, upgrade, logout, account switching, device limits, and offline expiry. Record OS, compiler/framework, and API/SDK versions. Successful compilation is not production acceptance.
