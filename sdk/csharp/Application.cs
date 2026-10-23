using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Licentivo.Launch;

public interface IRuntime : IAsyncDisposable
{
    Task Start(CancellationToken ct);
    AuthorizationStatus Status();
    Task RunFeature(string feature, Func<Task> action);
    Task Deactivate(CancellationToken ct);
}

internal sealed class OfficialRuntime(Config config) : IRuntime
{
    private readonly LicentivoClient client = new(config);
    public async Task Start(CancellationToken ct) => await client.Start(ct: ct);
    public AuthorizationStatus Status() => client.Status();
    public Task RunFeature(string feature, Func<Task> action) => client.RunFeature(feature, action);
    public Task Deactivate(CancellationToken ct) => client.Deactivate(ct);
    public ValueTask DisposeAsync() => client.DisposeAsync();
}

public sealed class LaunchException : Exception
{
    public string Code { get; }
    public int Status { get; }
    public int RetryAfterSeconds { get; }
    public bool Retryable => Code == "ENTITLEMENT_PENDING" || Status == 429 || Status >= 500;
    public LaunchException(string code, int status = 0, int retryAfterSeconds = 0) : base("Launch: " + SafeCode(code))
    { Code = SafeCode(code); Status = status; RetryAfterSeconds = Math.Max(retryAfterSeconds, 0); }
    private static string SafeCode(string code) => Regex.IsMatch(code, "^[A-Za-z0-9_]{1,80}$") ? code : "REQUEST_FAILED";
}

/// <summary>Single-account lifecycle. Serialize calls from your application UI.</summary>
public sealed class Application : IAsyncDisposable
{
    private sealed record Session(int Version, string Scope, string UserId, string AccessToken, string ExpiresAt, string LicenseKey);
    private readonly Config productConfig;
    private readonly string productId, environment, scope, cacheRoot;
    private readonly ISessionStore store;
    private readonly HttpClient http;
    private IRuntime? runtime;
    private readonly Func<Config, IRuntime> runtimeFactory;
    private Session? session;
    private string? cachePath;
    private bool disposed;
    public Application(string origin, string productId, Config productConfig, string environment = "live", ISessionStore? store = null, string? cacheDirectory = null, HttpMessageHandler? handler = null, Func<Config, IRuntime>? runtimeFactory = null)
    {
        var uri = new Uri(origin);
        if (environment is not ("sandbox" or "live") || uri.UserInfo != "" || uri.Query != "" || uri.Fragment != "" || uri.AbsolutePath != "/" ||
            (uri.Scheme != "https" && !(uri.Scheme == "http" && uri.IsLoopback && environment == "sandbox"))) throw new ArgumentException("Use an HTTPS merchant origin; local HTTP is only for sandbox");
        if (!Guid.TryParseExact(productId, "D", out _)) throw new ArgumentException("Invalid Launch product ID");
        this.productId = productId; this.environment = environment; this.productConfig = productConfig;
        this.store = store ?? new MemoryStore();
        this.runtimeFactory = runtimeFactory ?? (cfg => new OfficialRuntime(cfg));
        scope = string.Join("\n", uri.GetLeftPart(UriPartial.Authority), productId, environment, productConfig.ProductId);
        cacheRoot = cacheDirectory ?? Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "LicentivoLaunch");
        http = new HttpClient(handler ?? new HttpClientHandler { AllowAutoRedirect = false }) { BaseAddress = uri, Timeout = TimeSpan.FromSeconds(15) };
    }
    public static Application Connect(string merchantOrigin, string productId, string licentivoProductId, Dictionary<string, string> trustedKeys, string environment = "live", ISessionStore? store = null, string? cacheDirectory = null)
        => new(merchantOrigin, productId, new Config("https://www.licentivo.com", licentivoProductId, "", "", "", trustedKeys, "", false), environment, store, cacheDirectory);

    public static Application FromFiles(string launchPath, string productPath, ISessionStore? store = null)
    {
        using var document = JsonDocument.Parse(File.ReadAllText(launchPath)); var launch = document.RootElement;
        var product = JsonSerializer.Deserialize<Config>(File.ReadAllText(productPath), LicentivoClient.JsonOptions) ?? throw new JsonException("Invalid product.json");
        if (launch.GetProperty("schema_version").GetInt32() != 1 || launch.GetProperty("licentivo_product_id").GetString() != product.ProductId) throw new ArgumentException("Launch and SDK configuration mismatch");
        return new(launch.GetProperty("merchant_origin").GetString()!, launch.GetProperty("launch_product_id").GetString()!, product, launch.GetProperty("environment").GetString()!, store);
    }
    private async Task<JsonElement> Call(string path, object? body, string token, CancellationToken ct, bool get = false)
    {
        try
        {
            using var request = new HttpRequestMessage(get ? HttpMethod.Get : HttpMethod.Post, "/api/v1" + path) { Content = get ? null : JsonContent.Create(body) };
            if (token != "") request.Headers.Authorization = new("Bearer", token);
            using var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, ct);
            await using var stream = await response.Content.ReadAsStreamAsync(ct); using var buffer = new MemoryStream();
            var chunk = new byte[4096]; int count;
            while ((count = await stream.ReadAsync(chunk, ct)) != 0) { if (buffer.Length + count > 1048576) throw new LaunchException("INVALID_RESPONSE"); buffer.Write(chunk, 0, count); }
            using var json = JsonDocument.Parse(buffer.ToArray());
            if (!response.IsSuccessStatusCode)
            {
                string code = json.RootElement.TryGetProperty("error", out var error) && error.TryGetProperty("code", out var value) ? value.GetString() ?? "REQUEST_FAILED" : "REQUEST_FAILED";
                double seconds = response.Headers.RetryAfter?.Delta?.TotalSeconds ?? (response.Headers.RetryAfter?.Date - DateTimeOffset.UtcNow)?.TotalSeconds ?? 0;
                int delay = (int)Math.Clamp(Math.Ceiling(seconds), 0, int.MaxValue);
                throw new LaunchException(code, (int)response.StatusCode, delay);
            }
            return json.RootElement.GetProperty("data").Clone();
        }
        catch (HttpRequestException) { throw new LaunchException("NETWORK_UNAVAILABLE"); }
        catch (TaskCanceledException) when (!ct.IsCancellationRequested) { throw new LaunchException("NETWORK_UNAVAILABLE"); }
        catch (Exception e) when (e is JsonException or KeyNotFoundException or InvalidOperationException) { throw new LaunchException("INVALID_RESPONSE"); }
    }
    private async Task<string> Credential(string token, int attempts, CancellationToken ct)
    {
        for (int attempt = 0; ; attempt++)
        {
            try
            {
                var own = await Call($"/software/products/{productId}/credential", new { environment }, token, ct);
                if (!own.TryGetProperty("product_id", out var product) || product.ValueKind != JsonValueKind.String || product.GetString() != productId ||
                    !own.TryGetProperty("license_key", out var key) || key.ValueKind != JsonValueKind.String || string.IsNullOrWhiteSpace(key.GetString()) ||
                    !own.TryGetProperty("version", out var version) || version.ValueKind != JsonValueKind.Number || !version.TryGetUInt64(out var revision) || revision < 1) throw new LaunchException("CREDENTIAL_MISMATCH");
                return key.GetString()!;
            }
            catch (LaunchException e) when (e.Code == "ENTITLEMENT_PENDING" && e.Status is 403 or 409 && e.RetryAfterSeconds <= 30 && attempt + 1 < attempts)
            { await Task.Delay(TimeSpan.FromSeconds(Math.Max(3, e.RetryAfterSeconds > 0 ? e.RetryAfterSeconds : Math.Min(1 << attempt, 4))), ct); }
        }
    }
    private string CacheFor(Session saved)
    {
        var accountScope = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(scope + "\n" + saved.UserId))).ToLowerInvariant();
        return Path.Combine(cacheRoot, accountScope, "lease.json");
    }
    private async Task Start(Session next, CancellationToken ct, bool offline = false)
    {
        ObjectDisposedException.ThrowIf(disposed, this);
        if (runtime is not null) { await runtime.DisposeAsync(); runtime = null; }
        ct.ThrowIfCancellationRequested();
        string directory = Path.GetDirectoryName(CacheFor(next))!; Directory.CreateDirectory(directory);
        if (!OperatingSystem.IsWindows()) File.SetUnixFileMode(directory, UnixFileMode.UserRead | UnixFileMode.UserWrite | UnixFileMode.UserExecute);
        cachePath = Path.Combine(directory, "lease.json");
        var client = runtimeFactory(productConfig with { LicenseKey = next.LicenseKey, CachePath = cachePath });
        try { if (offline && !client.Status().Allowed) throw new LaunchException("OFFLINE_UNAVAILABLE"); await client.Start(ct); }
        catch { await client.DisposeAsync(); throw; }
        runtime = client; session = next;
    }
    public async Task Login(string email, string password, int pendingAttempts = 3, CancellationToken ct = default)
    {
        ObjectDisposedException.ThrowIf(disposed, this);
        if (pendingAttempts is < 1 or > 5) throw new ArgumentOutOfRangeException(nameof(pendingAttempts));
        await Logout(ct);
        var grant = await Call("/auth/login", new { email, password, kind = "desktop" }, "", ct);
        var token = grant.GetProperty("access_token").GetString()!;
        try
        {
            var next = new Session(1, scope, grant.GetProperty("user").GetProperty("id").GetString()!, token, grant.GetProperty("expires_at").GetString()!, await Credential(token, pendingAttempts, ct));
            await Start(next, ct); store.Save(scope, JsonSerializer.SerializeToUtf8Bytes(next));
        }
        catch
        {
            try { await Call("/auth/logout", new { }, token, ct); } catch (LaunchException) { }
            if (runtime is not null) await runtime.DisposeAsync(); runtime = null; session = null; throw;
        }
    }
    private Session? Saved()
    {
        ObjectDisposedException.ThrowIf(disposed, this);
        var saved = store.Load(scope); if (saved is null) return null;
        Session next;
        try { next = JsonSerializer.Deserialize<Session>(saved) ?? throw new JsonException(); }
        catch (JsonException) { throw new LaunchException("INVALID_SAVED_SESSION"); }
        finally { CryptographicOperations.ZeroMemory(saved); }
        if (next.Version != 1 || next.Scope != scope || string.IsNullOrEmpty(next.UserId) || string.IsNullOrEmpty(next.LicenseKey)) throw new LaunchException("INVALID_SAVED_SESSION");
        return next;
    }
    public async Task<bool> Restore(CancellationToken ct = default)
    {
        ct.ThrowIfCancellationRequested();
        if (runtime is not null) await runtime.DisposeAsync(); runtime = null; session = null;
        var next = Saved(); if (next is null) return false;
        if (!DateTimeOffset.TryParse(next.ExpiresAt, out var expires) || expires <= DateTimeOffset.UtcNow)
        { await Forget(next); throw new LaunchException("SESSION_EXPIRED"); }
        try
        {
            var current = await Call("/me", null, next.AccessToken, ct, get: true);
            if (current.GetProperty("user").GetProperty("id").GetString() != next.UserId) throw new LaunchException("SESSION_REJECTED", 401);
            next = next with { LicenseKey = await Credential(next.AccessToken, 3, ct) };
        }
        catch (LaunchException error)
        {
            if (error.Code == "NETWORK_UNAVAILABLE" || error.Status >= 500) { ct.ThrowIfCancellationRequested(); await Start(next, ct, offline: true); return true; }
            if (error.Code != "ENTITLEMENT_PENDING" && (error.Status is 401 or 403 or 404 || error.Code is "ENTITLEMENT_EXPIRED" or "ENTITLEMENT_SUSPENDED" or "ENTITLEMENT_NOT_PURCHASED")) await Forget(next);
            throw;
        }
        await Start(next, ct);
        try { store.Save(scope, JsonSerializer.SerializeToUtf8Bytes(next)); }
        catch { if (runtime is not null) await runtime.DisposeAsync(); runtime = null; session = null; throw; }
        return true;
    }
    public Task RunFeature(string feature, Func<Task> action) => (runtime ?? throw new LaunchException("LOGIN_REQUIRED")).RunFeature(feature, action);
    public AuthorizationStatus? Status() => runtime?.Status();
    public async Task ReleaseDevice(CancellationToken ct = default)
    {
        if (runtime is null) throw new LaunchException("LOGIN_REQUIRED");
        await runtime.Deactivate(ct); await Logout(ct);
    }
    private async Task Forget(Session? saved)
    {
        if (runtime is not null) await runtime.DisposeAsync(); runtime = null; session = null;
        store.Delete(scope);
        var cache = saved is null ? cachePath : CacheFor(saved);
        if (cache is not null) { File.Delete(cache); File.Delete(cache + ".credential.json"); }
    }
    public async Task Logout(CancellationToken ct = default)
    {
        Session? saved;
        try { saved = session ?? Saved(); }
        catch { await Forget(null); throw; }
        var token = saved?.AccessToken ?? "";
        await Forget(saved);
        if (token != "") try { await Call("/auth/logout", new { }, token, ct); } catch (LaunchException) { }
    }
    public async ValueTask DisposeAsync()
    {
        if (disposed) return; disposed = true;
        if (runtime is not null) await runtime.DisposeAsync(); runtime = null; session = null; http.Dispose();
    }
}
