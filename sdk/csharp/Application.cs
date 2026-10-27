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
    public async Task Deactivate(CancellationToken ct){var result=await client.Logout(ct);if(result.StorageError!="")throw new LaunchException("SDK_LOGOUT_STORAGE_FAILED");if(result.RevocationPending)throw new LaunchException("DEVICE_REVOCATION_PENDING");}
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
    private sealed record Session(int Version, string Scope, string UserId, string AccessToken, string ExpiresAt, string IdentityJson, bool LogoutPending = false);
    private readonly Config productConfig;
    private readonly string productId, environment, scope, cacheRoot;
    private readonly ISessionStore store;
    private readonly HttpClient http;
    private IRuntime? runtime;
    private readonly Func<Config, IRuntime> runtimeFactory;
    private Session? session;
    private string? cachePath;
    private bool disposed;
    private bool accountCleared;
    public Application(string origin, string productId, Config productConfig, string environment = "live", ISessionStore? store = null, string? cacheDirectory = null, HttpMessageHandler? handler = null, Func<Config, IRuntime>? runtimeFactory = null, bool allowHttp = false)
    {
        var uri = new Uri(origin);
        if (environment is not ("sandbox" or "live") || allowHttp && environment != "sandbox" || uri.UserInfo != "" || uri.Query != "" || uri.Fragment != "" || uri.AbsolutePath != "/" ||
            (uri.Scheme != "https" && !(uri.Scheme == "http" && (uri.IsLoopback || allowHttp) && environment == "sandbox"))) throw new ArgumentException("HTTPS required; HTTP opt-in is restricted to sandbox");
        if (!Guid.TryParseExact(productId, "D", out _)) throw new ArgumentException("Invalid Launch product ID");
        this.productId = productId; this.environment = environment; this.productConfig = productConfig;
        this.store = store ?? new MemoryStore();
        this.runtimeFactory = runtimeFactory ?? (cfg => new OfficialRuntime(cfg));
        scope = "launch-account-v2\n" + string.Join("\n", uri.GetLeftPart(UriPartial.Authority), productId, environment, productConfig.ProductId);
        cacheRoot = cacheDirectory ?? Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "LicentivoLaunch");
        http = new HttpClient(handler ?? new HttpClientHandler { AllowAutoRedirect = false }) { BaseAddress = uri, Timeout = TimeSpan.FromSeconds(15) };
    }
    public static Application Connect(string merchantOrigin, string productId, string licentivoProductId, Dictionary<string, string> trustedKeys, string environment = "live", ISessionStore? store = null, string? cacheDirectory = null, bool allowHttp = false)
        => new(merchantOrigin, productId, new Config("https://www.licentivo.com", licentivoProductId, "", "", "", trustedKeys, "", false), environment, store, cacheDirectory, allowHttp: allowHttp);

    public static Application FromFiles(string launchPath, string productPath, ISessionStore? store = null)
    {
        using var document = JsonDocument.Parse(File.ReadAllText(launchPath)); var launch = document.RootElement;
        var product = JsonSerializer.Deserialize<Config>(File.ReadAllText(productPath), LicentivoClient.JsonOptions) ?? throw new JsonException("Invalid product.json");
        if (launch.GetProperty("schema_version").GetInt32() != 1 || launch.GetProperty("licentivo_product_id").GetString() != product.ProductId) throw new ArgumentException("Launch and SDK configuration mismatch");
        return new(launch.GetProperty("merchant_origin").GetString()!, launch.GetProperty("launch_product_id").GetString()!, product, launch.GetProperty("environment").GetString()!, store, allowHttp: launch.TryGetProperty("allow_http", out var allowHttp) && allowHttp.GetBoolean());
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
            if (response.StatusCode != System.Net.HttpStatusCode.OK)
            {
                string code = json.RootElement.TryGetProperty("error", out var error) && error.TryGetProperty("code", out var value) ? value.GetString() ?? "REQUEST_FAILED" : "REQUEST_FAILED";
                double seconds = response.Headers.RetryAfter?.Delta?.TotalSeconds ?? (response.Headers.RetryAfter?.Date - DateTimeOffset.UtcNow)?.TotalSeconds ?? 0;
                int delay = (int)Math.Clamp(Math.Ceiling(seconds), 0, int.MaxValue);
                throw new LaunchException(code, (int)response.StatusCode, delay);
            }
            var data = json.RootElement.GetProperty("data");
            if (data.ValueKind != JsonValueKind.Object) throw new LaunchException("INVALID_RESPONSE");
            return data.Clone();
        }
        catch (HttpRequestException) { throw new LaunchException("NETWORK_UNAVAILABLE"); }
        catch (TaskCanceledException) when (!ct.IsCancellationRequested) { throw new LaunchException("NETWORK_UNAVAILABLE"); }
        catch (Exception e) when (e is JsonException or KeyNotFoundException or InvalidOperationException) { throw new LaunchException("INVALID_RESPONSE"); }
    }
    private async Task<JsonElement> Credential(string token,int attempts,CancellationToken ct){
 var own=await Call($"/software/products/{productId}/identity",new {environment},token,ct);
 if(own.GetProperty("product_id").GetString()!=productId||own.GetProperty("licentivo_product_id").GetString()!=productConfig.ProductId||own.GetProperty("environment").GetString()!=environment||!Guid.TryParse(own.GetProperty("launch_connection_id").GetString(),out _)||own.GetProperty("identity_token").GetString() is not {Length:>=32 and <=12288})throw new LaunchException("IDENTITY_MISMATCH");return own;
 }
    private static string CurrentUserId(JsonElement response)
    {
        if (!response.TryGetProperty("user", out var user) || user.ValueKind != JsonValueKind.Object ||
            !user.TryGetProperty("id", out var id) || id.ValueKind != JsonValueKind.String || string.IsNullOrEmpty(id.GetString()))
            throw new LaunchException("INVALID_RESPONSE");
        return id.GetString()!;
    }
 private static string PublicIdentity(JsonElement identity){var fields=JsonSerializer.Deserialize<Dictionary<string,JsonElement>>(identity.GetRawText())!;foreach(var key in fields.Keys.ToArray())if(!new[]{"product_id","licentivo_product_id","launch_connection_id","environment","expires_at"}.Contains(key))fields.Remove(key);return JsonSerializer.Serialize(fields);}
 private Config RuntimeConfig(Session saved,string identity=""){using var context=JsonDocument.Parse(saved.IdentityJson);return productConfig with{LicenseKey="",LaunchConnectionId=context.RootElement.GetProperty("launch_connection_id").GetString()!,IdentityToken=identity,Environment=environment,CachePath=CacheFor(saved)};}
    private string CacheFor(Session saved)
    {
        var accountScope = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(scope + "\n" + saved.UserId))).ToLowerInvariant();
        return Path.Combine(cacheRoot, accountScope, "lease.json");
    }
    private async Task Start(Session next,CancellationToken ct,bool offline=false,string identity=""){
 ObjectDisposedException.ThrowIf(disposed,this);ct.ThrowIfCancellationRequested();if(runtime is not null){await runtime.DisposeAsync();runtime=null;}
 string directory=Path.GetDirectoryName(CacheFor(next))!;Directory.CreateDirectory(directory);if(!OperatingSystem.IsWindows())File.SetUnixFileMode(directory,UnixFileMode.UserRead|UnixFileMode.UserWrite|UnixFileMode.UserExecute);
 cachePath=CacheFor(next);var client=runtimeFactory(RuntimeConfig(next,identity));try{ct.ThrowIfCancellationRequested();await client.Start(ct);ct.ThrowIfCancellationRequested();}catch{await client.DisposeAsync();throw;}runtime=client;session=next;accountCleared=false;
 }
    public async Task Login(string email, string password, int pendingAttempts = 3, CancellationToken ct = default)
    {
        using var guard = await AccountGuard.Take(scope, ct);
        await LoginCore(email, password, pendingAttempts, ct);
    }
    private async Task LoginCore(string email, string password, int pendingAttempts, CancellationToken ct)
    {
        ObjectDisposedException.ThrowIf(disposed, this);
        if (pendingAttempts is < 1 or > 5) throw new ArgumentOutOfRangeException(nameof(pendingAttempts));
        await LogoutCore(ct);
        var grant = await Call("/auth/login", new { email, password, kind = "desktop" }, "", ct);
        var token = grant.GetProperty("access_token").GetString()!;
        session = new Session(2, scope, grant.GetProperty("user").GetProperty("id").GetString()!, token, grant.GetProperty("expires_at").GetString()!, "{}");
        accountCleared=false;
        try
        {
            var own=await Credential(token,pendingAttempts,ct);
            var next = new Session(2, scope, grant.GetProperty("user").GetProperty("id").GetString()!, token, grant.GetProperty("expires_at").GetString()!, PublicIdentity(own));
            session=next;await Start(next, ct,identity:own.GetProperty("identity_token").GetString()!); store.Save(scope, JsonSerializer.SerializeToUtf8Bytes(next));
        }
        catch
        {
            try { if(session is not null || runtime is not null) await LogoutCore(CancellationToken.None); else await Call("/auth/logout",new{},token,CancellationToken.None); } catch { }
            throw;
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
        if ((next.Version != 2 && !(next.Version == 3 && next.LogoutPending)) || next.Scope != scope || string.IsNullOrEmpty(next.AccessToken) || !next.LogoutPending && (string.IsNullOrEmpty(next.UserId) || string.IsNullOrEmpty(next.IdentityJson))) throw new LaunchException("INVALID_SAVED_SESSION");
        return next;
    }
    public async Task<bool> Restore(CancellationToken ct=default){using var guard=await AccountGuard.Take(scope,ct);return await RestoreCore(ct);}
    private async Task<bool> RestoreCore(CancellationToken ct){
 if(runtime is not null)await runtime.DisposeAsync();runtime=null;var next=session?.LogoutPending==true?session:Saved();session=null;next??=PendingAccounts().FirstOrDefault();if(next is null)return false;
 var task=PendingAccounts().FirstOrDefault(value=>value.AccessToken==next.AccessToken);if(task is not null)next=task;
 if(AccountGuard.HasIntent(scope,next.AccessToken))next=next with{Version=3,LogoutPending=true};
 if(next.LogoutPending){session=next;await LogoutCore(ct);return false;}
 await RetryOtherAccounts(next.AccessToken,ct);
 if(!DateTimeOffset.TryParse(next.ExpiresAt,out var expires)||expires<=DateTimeOffset.UtcNow){await Forget(next);throw new LaunchException("SESSION_EXPIRED");}
 JsonElement own;try{var current=await Call("/me",null,next.AccessToken,ct,get:true);if(CurrentUserId(current)!=next.UserId)throw new LaunchException("SESSION_REJECTED",401);own=await Credential(next.AccessToken,1,ct);}catch(LaunchException e){if(e.Status==401||e.Status==403&&e.Code is not ("SDK_HTTP_DISABLED" or "SDK_HTTPS_REQUIRED"))await Forget(next);throw;}
 next=next with{IdentityJson=PublicIdentity(own)};await Start(next,ct,identity:own.GetProperty("identity_token").GetString()!);try{store.Save(scope,JsonSerializer.SerializeToUtf8Bytes(next));}catch{await Forget(next);throw;}return true;
 }
    public Task RunFeature(string feature, Func<Task> action) => (runtime ?? throw new LaunchException("LOGIN_REQUIRED")).RunFeature(feature, action);
    public AuthorizationStatus? Status() => runtime?.Status();
    public Task ReleaseDevice(CancellationToken ct=default)=>Logout(ct);
    private async Task Forget(Session? saved){saved??=session;try{if(runtime is not null)await runtime.DisposeAsync();}finally{runtime=null;session=null;accountCleared=true;if(saved is not null)DeleteOwned(saved);}}
    private sealed record AccountQueue(int Version, List<Session> Records);
    private string QueueKey => scope+"\naccount-logout-queue-v1";
    private List<Session> PendingAccounts(){var raw=store.Load(QueueKey);if(raw is null)return [];try{var queue=JsonSerializer.Deserialize<AccountQueue>(raw);if(queue is null||queue.Version!=1||queue.Records.Count>64||queue.Records.Any(value=>!value.LogoutPending||value.Scope!=scope||string.IsNullOrEmpty(value.AccessToken)))throw new LaunchException("INVALID_SAVED_SESSION");return queue.Records;}catch(JsonException){throw new LaunchException("INVALID_SAVED_SESSION");}finally{CryptographicOperations.ZeroMemory(raw);}}
    private void UpdatePending(Session saved,bool remove=false){var records=PendingAccounts().Where(value=>value.AccessToken!=saved.AccessToken).ToList();if(!remove)records.Add(saved with{Version=3,LogoutPending=true});if(records.Count>64)throw new LaunchException("ACCOUNT_LOGOUT_QUEUE_FULL");if(records.Count==0){store.Delete(QueueKey);return;}var raw=JsonSerializer.SerializeToUtf8Bytes(new AccountQueue(1,records));try{if(raw.Length>60000)throw new LaunchException("ACCOUNT_LOGOUT_QUEUE_FULL");store.Save(QueueKey,raw);}finally{CryptographicOperations.ZeroMemory(raw);}}
    private bool Owns(Session saved)=>Saved()?.AccessToken==saved.AccessToken;
    private void DeleteOwned(Session saved){if(Owns(saved))store.Delete(scope);}
    private void MarkLogout(Session saved){try{AccountGuard.Remember(scope,saved.AccessToken);}catch{}Exception? queueFailure=null;try{UpdatePending(saved);}catch(Exception e){queueFailure=e;}if(Owns(saved)){try{var raw=JsonSerializer.SerializeToUtf8Bytes(saved);try{store.Save(scope,raw);}finally{CryptographicOperations.ZeroMemory(raw);}return;}catch{try{DeleteOwned(saved);}catch{}}}if(queueFailure is not null)throw new LaunchException("ACCOUNT_LOGOUT_STORAGE_FAILED");}
    private void FinishAccount(Session saved){DeleteOwned(saved);UpdatePending(saved,true);AccountGuard.ClearIntent(scope,saved.AccessToken);}
    private async Task RetryOtherAccounts(string? skip,CancellationToken ct){foreach(var saved in PendingAccounts()){if(saved.AccessToken==skip)continue;try{await Call("/auth/logout",new{},saved.AccessToken,ct);}catch(LaunchException e)when(e.Status==401){}FinishAccount(saved);}}
    public async Task Logout(CancellationToken ct=default){AccountGuard? guard=null;try{if(session is not null)try{AccountGuard.Remember(scope,session.AccessToken);}catch{}guard=await AccountGuard.Take(scope,CancellationToken.None);if(accountCleared&&session is null)await RetryOtherAccounts(null,ct);else await LogoutCore(ct);}catch{if(session is not null)session=session with{Version=3,LogoutPending=true};throw;}finally{try{if(runtime is not null){try{await runtime.Deactivate(CancellationToken.None);}catch{}await runtime.DisposeAsync();}runtime=null;}finally{guard?.Dispose();}}}
    private async Task LogoutCore(CancellationToken ct){
 Session? saved;try{saved=session??Saved()??PendingAccounts().FirstOrDefault();}catch{await Forget(null);throw;}Exception? failure=null,persistFailure=null,otherFailure=null;bool confirmed=true;
 try{await RetryOtherAccounts(saved?.AccessToken,ct);}catch(Exception e){otherFailure=e;}
 if(saved is not null){saved=saved with{Version=3,LogoutPending=true};session=saved;try{MarkLogout(saved);}catch(Exception e){persistFailure=e;}}
 try{var active=Saved();if(runtime is null&&saved is not null&&(active is null||active.AccessToken==saved.AccessToken)&&saved.IdentityJson!="{}"&&!string.IsNullOrEmpty(saved.IdentityJson))runtime=runtimeFactory(RuntimeConfig(saved));if(runtime is not null)await runtime.Deactivate(ct);}catch(Exception e){failure=e;}
 try{if(runtime is not null)await runtime.DisposeAsync();}catch(Exception e){failure??=e;}finally{runtime=null;}
 if(saved is not null)try{await Call("/auth/logout",new{},saved.AccessToken,ct);}catch(Exception e){confirmed=e is LaunchException api&&api.Status==401;}
 if(confirmed){if(saved is not null)FinishAccount(saved);await Forget(saved);}else if(persistFailure is not null)failure=new LaunchException("ACCOUNT_LOGOUT_STORAGE_FAILED");else failure??=new LaunchException("ACCOUNT_LOGOUT_PENDING");
 failure??=otherFailure;
 if(failure is not null)throw failure;
 }
    public async ValueTask DisposeAsync()
    {
        if (disposed) return; disposed = true;
        var current = runtime;
        runtime = null; session = null;
        try { if (current is not null) await current.DisposeAsync(); }
        finally { http.Dispose(); }
    }
}
