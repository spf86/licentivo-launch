using System.Net;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using Licentivo;
using Licentivo.Launch;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Microsoft.Extensions.Logging;
using Org.BouncyCastle.Crypto.Parameters;
using Org.BouncyCastle.Crypto.Signers;
using LaunchApplication = Licentivo.Launch.Application;

// No runtimeFactory override: every authorization below uses the official SDK.
string root = Path.Combine(Path.GetTempPath(), "launch-csharp-official-" + Guid.NewGuid().ToString("N"));
Directory.CreateDirectory(root);
try
{
    await using var fixture = await Fixture.Start();
    string runtimeRoot = Path.Combine(root, "runtime");
    string storeRoot = Path.Combine(root, "sessions");
    var memory = new MemoryStore();
    ISessionStore Store() => OperatingSystem.IsWindows() ? new WindowsDPAPIStore(storeRoot) : memory;
    var config = new Config(fixture.Origin, Fixture.Remote, "", "", "", new() { [Fixture.KeyId] = fixture.PublicKey }, "", true, TimeoutSeconds: 2);
    LaunchApplication Open() => new(fixture.Origin, Fixture.Product, config, "sandbox", Store(), runtimeRoot);
    string scope = string.Join("\n", fixture.Origin, Fixture.Product, "sandbox", Fixture.Remote);
    string Cache() => Directory.GetFiles(runtimeRoot, "lease.json", SearchOption.AllDirectories).Single();
    async Task Login()
    {
        fixture.Mode = FixtureMode.Online;
        await using var app = Open();
        await app.Login("a@example.invalid", Fixture.Password);
        Check(app.Status() is { Allowed: true, State: "active" }, "Official online activation failed");
        bool executed = false;
        await app.RunFeature("export", () => { executed = true; return Task.CompletedTask; });
        Check(executed, "Official SDK did not execute an entitled feature");
    }

    await Login();
    Check(fixture.Activations > 0 && File.Exists(Cache() + ".credential.json"), "Online activation did not save official SDK state");
    if (OperatingSystem.IsWindows())
    {
        byte[] ciphertext = File.ReadAllBytes(Directory.GetFiles(storeRoot).Single());
        Check(!Encoding.UTF8.GetString(ciphertext).Contains(Fixture.Token, StringComparison.Ordinal), "Remembered bearer was stored in plaintext");
        Check(Store().Load(scope) is not null, "A fresh DPAPI store could not reopen the saved session");
    }
    Console.WriteLine("C#: official SDK signed online activation and remembered session passed");

    fixture.Mode = FixtureMode.Offline;
    await using (var app = Open())
    {
        Check(await app.Restore(), "Fresh application failed to restore a remembered session");
        Check(app.Status() is { Allowed: true, State: "offline_valid" }, "SDK did not verify the cached offline grant");
        bool executed = false;
        await app.RunFeature("export", () => { executed = true; return Task.CompletedTask; });
        Check(executed, "Verified offline feature was not executed");
        bool deniedCallback = false;
        try
        {
            await app.RunFeature("unlicensed", () => { deniedCallback = true; return Task.CompletedTask; });
            throw new Exception("Official SDK accepted an unlicensed feature");
        }
        catch (InvalidOperationException) { }
        Check(!deniedCallback, "Denied feature callback ran");
    }
    Console.WriteLine("C#: official SDK restart/offline restoration and feature denial passed");

    string cache = Cache();
    byte[] authentic = File.ReadAllBytes(cache);
    var tampered = JsonNode.Parse(authentic)!;
    tampered["lease"]!["signature"] = Fixture.Base64Url(new byte[64]);
    await File.WriteAllTextAsync(cache, tampered.ToJsonString());
    await using (var app = Open())
    {
        await ExpectLaunch("OFFLINE_UNAVAILABLE", () => app.Restore());
        await ExpectLaunch("LOGIN_REQUIRED", () => app.RunFeature("export", () => throw new Exception("Tampered grant authorized a callback")));
    }
    File.Delete(cache);
    await using (var app = Open()) await ExpectLaunch("OFFLINE_UNAVAILABLE", () => app.Restore());
    await File.WriteAllBytesAsync(cache, authentic);
    Console.WriteLine("C#: official SDK rejects tampered signatures and absent offline cache");

    foreach (var (mode, code) in new[]
    {
        (FixtureMode.Unauthorized, "SESSION_REJECTED"),
        (FixtureMode.Suspended, "ENTITLEMENT_SUSPENDED"),
        (FixtureMode.NotPurchased, "ENTITLEMENT_NOT_PURCHASED"),
        (FixtureMode.WrongAccount, "SESSION_REJECTED")
    })
    {
        await Login();
        cache = Cache();
        fixture.Mode = mode;
        await using var app = Open();
        await ExpectLaunch(code, () => app.Restore());
        Check(Store().Load(scope) is null && !File.Exists(cache) && !File.Exists(cache + ".credential.json"), "Explicit rejection retained remembered state or SDK cache");
        await ExpectLaunch("LOGIN_REQUIRED", () => app.RunFeature("export", () => throw new Exception("Rejected restore authorized a callback")));
    }
    Console.WriteLine("C#: 401/403/404 and account mismatch clear signed SDK cache without offline fallback");

    await Login();
    cache = Cache();
    await using (var app = Open())
    {
        Check(await app.Restore(), "Online restore failed before explicit device release");
        await app.ReleaseDevice();
    }
    Check(fixture.Deactivations == 1 && Store().Load(scope) is null && !File.Exists(cache) && !File.Exists(cache + ".credential.json"), "Explicit device release failed or retained local state");
    Console.WriteLine("C#: official SDK explicit device release and local cleanup passed");
    Console.WriteLine("C#: 5 real official SDK runtime test groups passed (no mocked authorization)");
}
finally
{
    // Only remove the exact uniquely-created test directory; no caller-supplied path.
    foreach (var file in Directory.GetFiles(root, "*", SearchOption.AllDirectories)) File.Delete(file);
    foreach (var directory in Directory.GetDirectories(root, "*", SearchOption.AllDirectories).OrderByDescending(path => path.Length)) Directory.Delete(directory);
    Directory.Delete(root);
}

static void Check(bool condition, string message) { if (!condition) throw new Exception(message); }
static async Task ExpectLaunch(string code, Func<Task> action)
{
    try { await action(); }
    catch (LaunchException error) when (error.Code == code) { return; }
    throw new Exception("Expected typed failure: " + code);
}

enum FixtureMode { Online, Offline, Unauthorized, Suspended, NotPurchased, WrongAccount }

sealed class Fixture : IAsyncDisposable
{
    public const string Product = "11111111-1111-4111-8111-111111111111";
    public const string Remote = "22222222-2222-4222-8222-222222222222";
    public const string KeyId = "signing-key-1";
    public const string Token = "fixture-private-bearer";
    public const string Password = "fixture-private-password";
    private const string Activation = "55555555-5555-4555-8555-555555555555";
    private readonly Ed25519PrivateKeyParameters signingKey = new(RandomNumberGenerator.GetBytes(32), 0);
    private readonly WebApplication server;
    private int mode, activations, deactivations;
    public FixtureMode Mode { get => (FixtureMode)Volatile.Read(ref mode); set => Volatile.Write(ref mode, (int)value); }
    public int Activations => Volatile.Read(ref activations);
    public int Deactivations => Volatile.Read(ref deactivations);
    public string Origin { get; private set; } = "";
    public string PublicKey => Base64Url(signingKey.GeneratePublicKey().GetEncoded());
    private Fixture(WebApplication server) { this.server = server; }
    public static async Task<Fixture> Start()
    {
        var builder = WebApplication.CreateSlimBuilder();
        builder.Logging.ClearProviders();
        builder.WebHost.ConfigureKestrel(options => options.Listen(IPAddress.Loopback, 0));
        var app = builder.Build();
        var fixture = new Fixture(app);
        app.Run(fixture.Handle);
        try { await app.StartAsync(); fixture.Origin = app.Urls.Single().TrimEnd('/'); return fixture; }
        catch { await app.DisposeAsync(); throw; }
    }
    private async Task Handle(HttpContext context)
    {
        string path = context.Request.Path.Value!;
        var current = Mode;
        if (current == FixtureMode.Offline) { await Error(context, 503, "SERVICE_UNAVAILABLE"); return; }
        if (path == "/api/v1/auth/login")
        {
            using var input = await JsonDocument.ParseAsync(context.Request.Body, cancellationToken: context.RequestAborted);
            if (input.RootElement.GetProperty("password").GetString() != Password || input.RootElement.GetProperty("kind").GetString() != "desktop")
                throw new InvalidOperationException("Invalid login fixture request");
            await Reply(context, new { access_token = Token, expires_at = DateTimeOffset.UtcNow.AddHours(1).ToString("O"), user = new { id = "account-a", email = "a@example.invalid" } });
            return;
        }
        if (path is "/api/v1/me" or "/api/v1/auth/logout" || path.EndsWith("/credential", StringComparison.Ordinal))
        {
            if (context.Request.Headers.Authorization != "Bearer " + Token) { await Error(context, 401, "SESSION_REJECTED"); return; }
            if (path == "/api/v1/me")
            {
                if (current == FixtureMode.Unauthorized) { await Error(context, 401, "SESSION_REJECTED"); return; }
                await Reply(context, new { user = new { id = current == FixtureMode.WrongAccount ? "account-b" : "account-a" } });
            }
            else if (path.EndsWith("/credential", StringComparison.Ordinal))
            {
                if (current == FixtureMode.Suspended) { await Error(context, 403, "ENTITLEMENT_SUSPENDED"); return; }
                if (current == FixtureMode.NotPurchased) { await Error(context, 404, "ENTITLEMENT_NOT_PURCHASED"); return; }
                await Reply(context, new { product_id = Product, license_key = "lv_lic_" + new string('a', 43), version = 1 });
            }
            else await Reply(context, new { signed_out = true });
            return;
        }
        if (path is "/api/v1/activate" or "/api/v1/validate" or "/api/v1/heartbeat")
        {
            using var input = await JsonDocument.ParseAsync(context.Request.Body, cancellationToken: context.RequestAborted);
            if (input.RootElement.GetProperty("product_id").GetString() != Remote) throw new InvalidOperationException("SDK used the wrong product");
            Interlocked.Increment(ref activations);
            await Reply(context, new { activation_id = Activation, device_key = "lv_dev_" + new string('d', 43), lease = Sign(input.RootElement.GetProperty("device_id").GetString()!), heartbeat_seconds = 600 });
            return;
        }
        if (path == "/api/v1/deactivate") { Interlocked.Increment(ref deactivations); await Reply(context, new { deactivated = true }); return; }
        await Error(context, 404, "UNKNOWN_ROUTE");
    }
    private object Sign(string device)
    {
        long now = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
        byte[] payload = JsonSerializer.SerializeToUtf8Bytes(new
        {
            version = 1, key_id = KeyId, tenant_id = "33333333-3333-4333-8333-333333333333",
            product_id = Remote, license_id = "44444444-4444-4444-8444-444444444444",
            device_id = device, activation_id = Activation, mode = "timed", features = new[] { "export" },
            max_devices = 1, issued_at = now, expires_at = now + 3600
        });
        var signer = new Ed25519Signer();
        signer.Init(true, signingKey);
        byte[] prefix = Encoding.UTF8.GetBytes("LicenovaLease/v1\n");
        signer.BlockUpdate(prefix, 0, prefix.Length);
        signer.BlockUpdate(payload, 0, payload.Length);
        return new { version = 1, key_id = KeyId, payload = Base64Url(payload), signature = Base64Url(signer.GenerateSignature()) };
    }
    public static string Base64Url(byte[] bytes) => Convert.ToBase64String(bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_');
    private static Task Reply(HttpContext context, object data) => context.Response.WriteAsJsonAsync(new { data }, context.RequestAborted);
    private static Task Error(HttpContext context, int status, string code)
    {
        context.Response.StatusCode = status;
        return context.Response.WriteAsJsonAsync(new { error = new { code } }, context.RequestAborted);
    }
    public async ValueTask DisposeAsync() { await server.StopAsync(); await server.DisposeAsync(); }
}
