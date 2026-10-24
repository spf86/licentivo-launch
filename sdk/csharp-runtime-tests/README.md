# C# official SDK account tests

Run `dotnet run --project sdk/csharp-runtime-tests/Launch.RuntimeTests.csproj`.

The local fixture uses the real bundled SDK and Ed25519 signed permissions. It covers identity exchange, protected feature execution, Launch outage rejection, SDK-owned offline recovery, tampered signature rejection, pending logout and ticket recovery after SDK restart. It does not contact production services or establish external acceptance.
