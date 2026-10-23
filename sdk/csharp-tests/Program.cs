using System.Net;
using System.Text;
using Licentivo;
using Licentivo.Launch;

static void Assert(bool condition, string message) { if (!condition) throw new Exception(message); }
var product = "11111111-1111-4111-8111-111111111111";
var config = new Config("https://licensing.example", "22222222-2222-4222-8222-222222222222", "", "", "", new() { ["key"] = "public" }, "", false);
foreach (var code in new[] { "ENTITLEMENT_EXPIRED", "ENTITLEMENT_SUSPENDED", "ENTITLEMENT_NOT_PURCHASED", "ENTITLEMENT_PENDING" })
{
    var handler = new Merchant(code);
    await using var app = new Application("https://merchant.example", product, config, handler: handler);
    Assert(!await app.Restore(), "Memory-only application unexpectedly restored");
    try { await app.Login("email", "secret", pendingAttempts: 2); throw new Exception("Expected API error"); }
    catch (LaunchException error) { Assert(error.Code == code && !error.Message.Contains("secret"), "Unsafe or incorrect business error"); }
    Assert(handler.CredentialCalls == (code == "ENTITLEMENT_PENDING" ? 2 : 1), "Incorrect bounded retry");
    Assert(handler.LogoutCalls == 1, "Failed activation left a merchant session open");
    try { await app.RunFeature("export", () => throw new Exception("Unauthorized action executed")); throw new Exception("Expected login required"); }
    catch (LaunchException error) { Assert(error.Code == "LOGIN_REQUIRED", "Failed login authorized action"); }
}
foreach (var credential in new[] { "{}", "{\"product_id\":\"11111111-1111-4111-8111-111111111111\",\"license_key\":\"key\",\"version\":0}", "{\"product_id\":\"11111111-1111-4111-8111-111111111111\",\"license_key\":\"key\",\"version\":1.5}" })
{
    var merchant = new Merchant("") { CredentialBody = "{\"data\":" + credential + "}" };
    await using var app = new Application("https://merchant.example", product, config, handler: merchant, runtimeFactory: _ => throw new Exception("Invalid credential reached runtime"));
    try { await app.Login("a", "b"); throw new Exception("Invalid credential accepted"); } catch (LaunchException error) { Assert(error.Code == "CREDENTIAL_MISMATCH", "Invalid credential not typed"); }
}
{
    var merchant = new Merchant("ENTITLEMENT_PENDING") { ErrorStatus = HttpStatusCode.ServiceUnavailable };
    await using var app = new Application("https://merchant.example", product, config, handler: merchant);
    try { await app.Login("a", "b"); throw new Exception("5xx accepted"); } catch (LaunchException error) { Assert(error.Status == 503, "Wrong 5xx error"); }
    Assert(merchant.CredentialCalls == 1, "5xx treated as business pending");
}
var store = new MemoryStore(); byte[] original = [1, 2, 3]; store.Save("key", original); original[0] = 9;
foreach (string retryAfter in new[] { "120", DateTimeOffset.UtcNow.AddMinutes(2).ToString("R") })
{
    var merchant = new Merchant("ENTITLEMENT_PENDING") { RetryAfter = retryAfter };
    await using var app = new Application("https://merchant.example", product, config, handler: merchant);
    try { await app.Login("a", "b"); throw new Exception("Expected pending error"); }
    catch (LaunchException error) { Assert(error.Code == "ENTITLEMENT_PENDING" && error.RetryAfterSeconds > 30, "Long Retry-After was shortened"); }
    Assert(merchant.CredentialCalls == 1, "Retried before server delay");
}
Assert(store.Load("key")![0] == 1, "Memory store retained caller mutable bytes"); store.Delete("key"); Assert(store.Load("key") is null, "Memory store failed to delete");
string lifecycleRoot = Path.Combine(Path.GetTempPath(), "launch-lifecycle-" + Guid.NewGuid());
Directory.CreateDirectory(lifecycleRoot);
try
{
    foreach (string mode in new[] { "offline-valid", "offline-missing", "denied", "ENTITLEMENT_EXPIRED", "ENTITLEMENT_SUSPENDED", "ENTITLEMENT_NOT_PURCHASED", "expired", "logout", "cancelled" })
    {
        var remembered = new MemoryStore(); var merchant = new Merchant(""); string? cache = null; int starts = 0;
        IRuntime Factory(Config cfg) { cache = cfg.CachePath; return new FakeRuntime(cfg, () => starts++); }
        await using (var first = new Application("https://merchant.example", product, config, store: remembered, cacheDirectory: lifecycleRoot, handler: merchant, runtimeFactory: Factory)) { await first.Login("a", "b"); }
        File.WriteAllText(cache!, "signed-cache-fixture");
        var restoringMerchant = new Merchant("");
        if (mode.StartsWith("offline")) restoringMerchant.Mode = "offline";
        if (mode == "offline-missing") File.Delete(cache!);
        if (mode == "denied") restoringMerchant.Mode = "denied";
        if (mode.StartsWith("ENTITLEMENT")) restoringMerchant.Code = mode;
        string scope = string.Join("\n", "https://merchant.example", product, "live", config.ProductId);
        if (mode == "expired")
        {
            var record = System.Text.Json.Nodes.JsonNode.Parse(remembered.Load(scope)!)!;
            record["ExpiresAt"] = "2000-01-01T00:00:00Z";
            remembered.Save(scope, Encoding.UTF8.GetBytes(record.ToJsonString()));
        }
        await using var second = new Application("https://merchant.example", product, config, store: remembered, cacheDirectory: lifecycleRoot, handler: restoringMerchant, runtimeFactory: Factory);
        if (mode == "logout") { await second.Logout(); Assert(!File.Exists(cache) && remembered.Load(scope) is null, "Logout without restore retained account"); continue; }
        if (mode == "cancelled")
        {
            try { await second.Restore(new CancellationToken(true)); throw new Exception("Cancellation ignored"); } catch (OperationCanceledException) { }
            Assert(starts == 1, "Cancelled restore started SDK"); continue;
        }
        if (mode == "offline-valid") { Assert(await second.Restore(), "Valid offline restore failed"); bool ran = false; await second.RunFeature("export", () => { ran = true; return Task.CompletedTask; }); Assert(ran, "SDK-approved feature failed"); }
        else
        {
            try { await second.Restore(); throw new Exception("Restore should fail: " + mode); } catch (LaunchException) { }
            Assert(starts == 1, "Rejected restore started SDK: " + mode);
            if (mode != "offline-missing") Assert(!File.Exists(cache) && remembered.Load(scope) is null, "Denial retained saved account: " + mode);
            try { await second.RunFeature("export", () => throw new Exception("Denied action ran")); throw new Exception("Denied restore authorized"); } catch (LaunchException) { }
        }
    }
}
finally
{
    foreach (var path in Directory.GetFiles(lifecycleRoot, "*", SearchOption.AllDirectories)) File.Delete(path);
    foreach (var path in Directory.GetDirectories(lifecycleRoot)) Directory.Delete(path);
    Directory.Delete(lifecycleRoot);
}
if (OperatingSystem.IsWindows())
{
    string root = Path.Combine(Path.GetTempPath(), "launch-store-tests-" + Guid.NewGuid());
    Directory.CreateDirectory(root);
    try
    {
        var secure = new WindowsDPAPIStore(root); var secret = Encoding.UTF8.GetBytes("sensitive-bearer-token"); secure.Save("key", secret);
        Assert(secure.Load("key")!.SequenceEqual(secret), "DPAPI round trip failed");
        var path = Directory.GetFiles(root).Single(); Assert(!Encoding.UTF8.GetString(File.ReadAllBytes(path)).Contains("sensitive-bearer-token"), "Secret written in plaintext");
        File.WriteAllText(path, "tampered");
        try { secure.Load("key"); throw new Exception("Tampered secret accepted"); } catch (System.Security.Cryptography.CryptographicException) { }
        secure.Delete("key"); Assert(secure.Load("key") is null, "DPAPI delete failed");
    }
    finally { foreach (var path in Directory.GetFiles(root)) File.Delete(path); Directory.Delete(root); }
}
Console.WriteLine("Launch .NET: typed failures, bounded retry, failed-login cleanup, feature gating, store isolation and DPAPI passed.");

sealed class Merchant(string code) : HttpMessageHandler
{
    public int CredentialCalls, LogoutCalls;
    public string Mode = "online", Code = code;
    public string? RetryAfter = "3", CredentialBody;
    public HttpStatusCode? ErrorStatus;
    protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken ct)
    {
        string body; var status = HttpStatusCode.OK;
        if (Mode == "offline") throw new HttpRequestException("Transport outage");
        if (request.RequestUri!.AbsolutePath.EndsWith("/me")) { body = "{\"data\":{\"user\":{\"id\":\"alice\"}}}"; if (Mode == "denied") { status = HttpStatusCode.Unauthorized; body = "{\"error\":{\"code\":\"SESSION_REJECTED\"}}"; } }
        else if (request.RequestUri.AbsolutePath.EndsWith("/auth/login")) body = "{\"data\":{\"access_token\":\"secret\",\"user\":{\"id\":\"alice\"},\"expires_at\":\"2099-01-01T00:00:00Z\"}}";
        else if (request.RequestUri.AbsolutePath.EndsWith("/credential")) { CredentialCalls++; status = Code == "" ? HttpStatusCode.OK : ErrorStatus ?? (Code == "ENTITLEMENT_NOT_PURCHASED" ? HttpStatusCode.NotFound : HttpStatusCode.Forbidden); body = Code == "" ? CredentialBody ?? "{\"data\":{\"product_id\":\"11111111-1111-4111-8111-111111111111\",\"license_key\":\"lv_lic_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\",\"version\":1,\"valid_until\":null}}" : "{\"error\":{\"code\":\"" + Code + "\"}}"; }
        else { LogoutCalls++; body = "{\"data\":{}}"; }
        var response = new HttpResponseMessage(status) { Content = new StringContent(body, Encoding.UTF8, "application/json") };
        if (RetryAfter is not null) response.Headers.TryAddWithoutValidation("Retry-After", RetryAfter);
        return Task.FromResult(response);
    }
}

sealed class FakeRuntime(Config config, Action started) : IRuntime
{
    public Task Start(CancellationToken ct) { ct.ThrowIfCancellationRequested(); started(); return Task.CompletedTask; }
    public AuthorizationStatus Status() => new("fixture", File.Exists(config.CachePath));
    public Task RunFeature(string feature, Func<Task> action) => feature == "export" ? action() : throw new Exception("SDK denied feature");
    public Task Deactivate(CancellationToken ct) => Task.CompletedTask;
    public ValueTask DisposeAsync() => ValueTask.CompletedTask;
}
