using System.Net;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using Licentivo;
using Licentivo.Launch;

static void Check(bool value,string message){if(!value)throw new Exception(message);}
const string Product="11111111-1111-4111-8111-111111111111";
const string Remote="22222222-2222-4222-8222-222222222222";
var config=new Config("https://www.licentivo.com",Remote,"","","",new(){["test"]=Convert.ToBase64String(new byte[32]).TrimEnd('=').Replace('+','-').Replace('/','_')},"",false);
string root=Path.Combine(Path.GetTempPath(),"launch-account-tests-"+Guid.NewGuid().ToString("N"));Directory.CreateDirectory(root);
var store=new InspectStore();var merchant=new Merchant();var runtimes=new List<FakeRuntime>();
IRuntime Factory(Config cfg){var r=new FakeRuntime(cfg);runtimes.Add(r);return r;}
Application Open(Merchant api)=>new("https://merchant.example",Product,config,store:store,cacheDirectory:root,handler:api,runtimeFactory:Factory);
await using(var app=Open(merchant)){
 await app.Login("a","password");
 Check(runtimes[0].Config.IdentityToken==new string('I',64)&&runtimes[0].Config.LicenseKey=="","SDK did not receive identity-only configuration");
 string saved=Encoding.UTF8.GetString(store.Record!);Check(!saved.Contains(new string('I',64))&&!saved.Contains("license_key"),"identity or license key persisted");
 bool ran=false;await app.RunFeature("export",()=>{ran=true;return Task.CompletedTask;});Check(ran,"SDK feature did not run");
 try{await app.RunFeature("absent",()=>throw new Exception("Callback bypassed SDK"));throw new Exception("Feature accepted");}catch(LaunchException e){Check(e.Code=="FEATURE_DENIED","Wrong feature failure");}
}
Check(runtimes[0].Releases==0&&store.Record is not null,"Ordinary close released the device or account");
var outage=new Merchant{Offline=true};
await using(var app=Open(outage)){
 try{await app.Restore();throw new Exception("Launch chose offline authorization");}catch(LaunchException e){Check(e.Code=="NETWORK_UNAVAILABLE"||e.Status==503,"Wrong outage failure");}
 Check(runtimes.Count==1,"Outage initialized another runtime");
}
await using(var app=Open(new Merchant())){
 Check(await app.Restore(),"Online restore failed");
 runtimes[^1].Pending=true;
 string journal=runtimes[^1].Config.CachePath+".logout.json";File.WriteAllText(journal,"SDK-owned pending logout");
 try{await app.Logout();throw new Exception("Pending logout reported success");}catch(LaunchException e){Check(e.Code=="DEVICE_REVOCATION_PENDING","Wrong logout failure");}
 Check(store.Record is null&&runtimes[^1].Releases==1&&File.Exists(journal),"Logout lost account cleanup or SDK recovery journal");
}
await using(var first=Open(new Merchant())){await first.Login("a","password");}
int count=runtimes.Count;
await using(var fresh=Open(new Merchant())){await fresh.Logout();}
Check(runtimes.Count==count+1&&runtimes[^1].Releases==1&&store.Record is null,"Fresh-process logout did not recover the SDK context");
if(OperatingSystem.IsWindows()){
 var secure=new WindowsDPAPIStore(Path.Combine(root,"dpapi"));var secret=Encoding.UTF8.GetBytes("private-account-token");secure.Save("account",secret);
 Check(secure.Load("account")!.SequenceEqual(secret),"DPAPI did not restore account");string path=Directory.GetFiles(Path.Combine(root,"dpapi")).Single();
 Check(!Encoding.UTF8.GetString(File.ReadAllBytes(path)).Contains("private-account-token"),"Account stored in plaintext");File.WriteAllText(path,"tampered");
 try{secure.Load("account");throw new Exception("Tampered account accepted");}catch(CryptographicException){}
}
await using(var first=Open(new Merchant())){await first.Login("a","password");}
var logoutOutage=new Merchant{Offline=true};
await using(var signedOut=Open(logoutOutage)){
 try{await signedOut.Logout();throw new Exception("Offline account logout reported success");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_PENDING","Wrong pending account error");}
 Check(store.Record is not null&&Encoding.UTF8.GetString(store.Record).Contains("\"LogoutPending\":true"),"Pending account logout not persisted");
 Check(signedOut.Status() is null,"Signed-out runtime retained");
}
await using(var restarted=Open(logoutOutage)){
 try{await restarted.Restore();throw new Exception("Pending account restored");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_PENDING","Restore did not retry logout");}
 logoutOutage.Offline=false;Check(!await restarted.Restore(),"Completed logout restored a login");Check(store.Record is null,"Completed account logout retained task");
}
foreach(var code in new[]{"SDK_HTTP_DISABLED","SDK_HTTPS_REQUIRED"}){
 var policyStore=new InspectStore();var policyMerchant=new Merchant();
 await using var app=new Application("https://merchant.example",Product,config,store:policyStore,cacheDirectory:root,handler:policyMerchant,runtimeFactory:Factory);
 await app.Login("a","b");var saved=policyStore.Record!.ToArray();int opens=runtimes.Count;policyMerchant.TransportDenial=code;
 try{await app.Restore();throw new Exception("Transport policy denial restored a login");}catch(LaunchException e){Check(e.Code==code,"Wrong transport policy failure");}
 Check(policyStore.Record is not null&&policyStore.Record.SequenceEqual(saved)&&runtimes.Count==opens&&app.Status() is null,"Transport policy denial lost account or started runtime");
 await app.Logout();Check(policyStore.Record is null,"Transport policy denial prevented explicit account logout");
}
var acceptedStore=new InspectStore();var acceptedMerchant=new Merchant();
Application Accepted()=>new("https://merchant.example",Product,config,store:acceptedStore,cacheDirectory:root,handler:acceptedMerchant,runtimeFactory:Factory);
await using(var app=Accepted()){
 await app.Login("a","b");acceptedMerchant.LogoutStatus=HttpStatusCode.Accepted;
 try{await app.Logout();throw new Exception("202 confirmed account logout");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_PENDING","Wrong unconfirmed logout error");}
 Check(acceptedStore.Record is not null&&app.Status() is null,"202 removed retry records or retained local authorization");
}
await using(var restarted=Accepted()){
 try{await restarted.Restore();throw new Exception("Accepted logout restored a login");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_PENDING","Restart lost its pending logout");}
 acceptedMerchant.LogoutStatus=HttpStatusCode.OK;Check(!await restarted.Restore(),"Confirmed logout restored a login");Check(acceptedStore.Record is null,"Confirmed logout retained its task");
}
foreach(var body in new[]{"null","{\"data\":null}","{\"data\":[]}","{\"error\":{\"code\":\"UNEXPECTED\"}}"}){
 var invalidStore=new InspectStore();var invalidMerchant=new Merchant{LogoutBody=body};
 await using var app=new Application("https://merchant.example",Product,config,store:invalidStore,cacheDirectory:root,handler:invalidMerchant,runtimeFactory:Factory);
 await app.Login("a","b");
 try{await app.Logout();throw new Exception("Malformed 200 confirmed account logout");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_PENDING","Wrong malformed logout result");}
 Check(invalidStore.Record is not null&&app.Status() is null,"Malformed logout discarded retry data or kept a runtime");
 invalidMerchant.LogoutBody=null;await app.Logout();Check(invalidStore.Record is null,"Valid confirmation did not finish pending logout");
}
var failingStore=new InspectStore();var failingMerchant=new Merchant();
await using(var app=new Application("https://merchant.example",Product,config,store:failingStore,cacheDirectory:root,handler:failingMerchant,runtimeFactory:Factory)){
 await app.Login("a","b");failingStore.FailSaves=true;failingMerchant.Offline=true;
 try{await app.Logout();throw new Exception("Failed logout storage reported success");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_STORAGE_FAILED","Wrong durable storage error");}
 Check(failingStore.Record is null&&app.Status() is null,"Old login or local runtime survived failed saves");
 failingStore.FailSaves=false;
 await using var restarted=new Application("https://merchant.example",Product,config,store:failingStore,cacheDirectory:root,handler:new Merchant(),runtimeFactory:Factory);
 Check(!await restarted.Restore(),"Failed-save logout restored the old account");
}
var sharedStore=new InspectStore();var sharedMerchant=new Merchant();
Application Shared()=>new("https://merchant.example",Product,config,store:sharedStore,cacheDirectory:root,handler:sharedMerchant,runtimeFactory:Factory);
await using(var old=Shared())await using(var newer=Shared()){await old.Login("a","b");await old.Logout();await newer.Login("a","b");var token=JsonDocument.Parse(sharedStore.Record!).RootElement.GetProperty("AccessToken").GetString();await old.Logout();Check(sharedStore.Record is not null&&JsonDocument.Parse(sharedStore.Record).RootElement.GetProperty("AccessToken").GetString()==token,"Completed old logout adopted new account");await newer.Logout();}
await using(var old=Shared())await using(var newer=Shared()){
 await old.Login("a","b");var oldToken=JsonDocument.Parse(sharedStore.Record!).RootElement.GetProperty("AccessToken").GetString()!;
 await newer.Login("a","b");var newToken=JsonDocument.Parse(sharedStore.Record!).RootElement.GetProperty("AccessToken").GetString()!;
 sharedMerchant.BlockBearer=oldToken;
 try{await old.Logout();throw new Exception("Expected old session retry");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_PENDING","Wrong late logout result");}
 Check(JsonDocument.Parse(sharedStore.Record!).RootElement.GetProperty("AccessToken").GetString()==newToken,"Old instance overwrote new account");
 sharedMerchant.BlockBearer=null;int before=runtimes.Count;
 await using var restarted=Shared();Check(await restarted.Restore(),"New account could not restore after old task recovery");
 Check(runtimes.Count==before+1,"Old account task reopened a licensing cache");
 Check(JsonDocument.Parse(sharedStore.Record!).RootElement.GetProperty("AccessToken").GetString()==newToken,"Old task removed new account");
 sharedStore.FailSaves=sharedStore.FailDeletes=true;sharedMerchant.Offline=true;
 try{await restarted.Logout();throw new Exception("Total storage failure reported success");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_STORAGE_FAILED","Total failure was not explicit");}
 Check(restarted.Status() is null,"Total storage failure retained runtime");
 await using var blocked=Shared();try{await blocked.Restore();throw new Exception("Total storage failure resurrected the account");}catch(LaunchException e){Check(e.Code=="ACCOUNT_LOGOUT_STORAGE_FAILED","Missing account-only exit marker");}
}
foreach(var data in new[]{"{\"user\":{\"id\":null}}","{}","{\"user\":null}","{\"user\":{}}","{\"user\":{\"id\":\"\"}}","{\"user\":{\"id\":42}}","{\"user\":[]}","{\"user\":\"invalid\"}"}){
 var currentStore=new InspectStore();var currentMerchant=new Merchant();
 await using var app=new Application("https://merchant.example",Product,config,store:currentStore,cacheDirectory:root,handler:currentMerchant,runtimeFactory:Factory);
 await app.Login("a","b");var saved=currentStore.Record!.ToArray();string token=JsonDocument.Parse(saved).RootElement.GetProperty("AccessToken").GetString()!;int opens=runtimes.Count;
 currentMerchant.MeBody="{\"data\":"+data+"}";
 try{await app.Restore();throw new Exception("Malformed user restored a login");}catch(LaunchException e){Check(e.Code=="INVALID_RESPONSE","Malformed user was treated as an account change");}
 Check(currentStore.Record is not null&&currentStore.Record.SequenceEqual(saved)&&runtimes.Count==opens&&app.Status() is null,"Malformed user discarded login or started runtime");
 try{await app.RunFeature("export",()=>throw new Exception("Failed restore executed work"));throw new Exception("Failed restore retained authorization");}catch(LaunchException e){Check(e.Code=="LOGIN_REQUIRED","Wrong restore failure");}
 await app.Logout();Check(currentStore.Record is null&&currentMerchant.LastLogoutBearer==token,"Malformed user prevented session revocation");
}
{
 var differentStore=new InspectStore();var differentMerchant=new Merchant();
 await using var app=new Application("https://merchant.example",Product,config,store:differentStore,cacheDirectory:root,handler:differentMerchant,runtimeFactory:Factory);
 await app.Login("a","b");differentMerchant.MeBody="{\"data\":{\"user\":{\"id\":\"55555555-5555-4555-8555-555555555555\"}}}";
 try{await app.Restore();throw new Exception("Different current account was accepted");}catch(LaunchException e){Check(e.Code=="SESSION_REJECTED","Wrong account mismatch result");}
 Check(differentStore.Record is null&&app.Status() is null,"Different account retained the old login");
}
foreach(var operation in new[]{"login","restore"}){
 var cancelStore=new InspectStore();var cancelMerchant=new Merchant();using var cancellation=new CancellationTokenSource();
 bool cancelStartup=false;FakeRuntime? candidate=null;
 IRuntime CancelFactory(Config cfg){var runtime=new FakeRuntime(cfg);runtimes.Add(runtime);if(cancelStartup)runtime.OnStart=()=>{candidate=runtime;cancellation.Cancel();};return runtime;}
 await using var app=new Application("https://merchant.example",Product,config,store:cancelStore,cacheDirectory:root,handler:cancelMerchant,runtimeFactory:CancelFactory);
 byte[]? saved=null;if(operation=="restore"){await app.Login("a","b");saved=cancelStore.Record!.ToArray();}
 cancelStartup=true;
 try{if(operation=="login")await app.Login("a","b",ct:cancellation.Token);else await app.Restore(cancellation.Token);throw new Exception("Canceled startup reported success");}catch(OperationCanceledException)when(cancellation.IsCancellationRequested){}
 Check(app.Status() is null&&candidate is not null&&candidate.Disposals==1,"Canceled startup retained an active runtime");
 try{await app.RunFeature("export",()=>throw new Exception("Canceled startup executed protected work"));throw new Exception("Canceled startup retained authorization");}catch(LaunchException e){Check(e.Code=="LOGIN_REQUIRED","Wrong canceled startup failure");}
 cancelStartup=false;
 if(operation=="restore"){
  Check(cancelStore.Record is not null&&cancelStore.Record.SequenceEqual(saved!),"Canceling restore discarded the remembered login");
  Check(await app.Restore(),"Restore could not be retried after cancellation");await app.Logout();
 }else Check(cancelStore.Record is null&&!string.IsNullOrEmpty(cancelMerchant.LastLogoutBearer),"Canceled login did not revoke its account session");
}
var shutdownStore=new InspectStore();var shutdownMerchant=new Merchant();
var shutdownApp=new Application("https://merchant.example",Product,config,store:shutdownStore,cacheDirectory:root,handler:shutdownMerchant,runtimeFactory:Factory);
await shutdownApp.Login("a","b");var shutdownRuntime=runtimes[^1];shutdownRuntime.FailDispose=true;
try{await shutdownApp.DisposeAsync();throw new Exception("Failed SDK shutdown reported success");}catch(IOException){}
Check(shutdownApp.Status() is null,"Failed shutdown retained an active runtime");
try{await shutdownApp.RunFeature("export",()=>throw new Exception("Closed application executed protected work"));throw new Exception("Closed application retained protected access");}catch(LaunchException e){Check(e.Code=="LOGIN_REQUIRED","Wrong closed application failure");}
await shutdownApp.DisposeAsync();Check(shutdownRuntime.Disposals==1,"Repeated shutdown retried a detached runtime");
Check(shutdownMerchant.Disposed,"Failed SDK shutdown leaked the merchant HTTP client");
Check(shutdownStore.Record is not null&&shutdownRuntime.Releases==0,"Ordinary failed shutdown released the device or account");
Console.WriteLine("C#: lifecycle, DPAPI, failed-save logout, total storage failure, stale-instance retry and failed shutdown passed.");
string resolved=Path.GetFullPath(root);if(!resolved.StartsWith(Path.GetFullPath(Path.Combine(Path.GetTempPath(),"launch-account-tests-")),StringComparison.OrdinalIgnoreCase))throw new Exception("Unsafe temporary path");Directory.Delete(resolved,true);

sealed class InspectStore:ISessionStore{
 readonly Dictionary<string,byte[]> values=new();string? active;
 public bool FailSaves,FailDeletes;
 public byte[]? Record=>active is not null&&values.TryGetValue(active,out var value)?value:null;
 public byte[]? Load(string scope)=>values.TryGetValue(scope,out var value)?value.ToArray():null;
 public void Save(string scope,byte[] value){if(FailSaves)throw new IOException("fixture atomic save failed");values[scope]=value.ToArray();if(!scope.EndsWith("\naccount-logout-queue-v1"))active=scope;}
 public void Delete(string scope){if(FailDeletes)throw new IOException("fixture delete failed");values.Remove(scope);}
}
sealed class Merchant:HttpMessageHandler{
 public bool Offline,Disposed;public string? BlockBearer,LogoutBody,TransportDenial,MeBody,LastLogoutBearer;public HttpStatusCode LogoutStatus=HttpStatusCode.OK;static int Logins;static readonly string Prefix=Guid.NewGuid().ToString("N");
 protected override void Dispose(bool disposing){if(disposing)Disposed=true;base.Dispose(disposing);}
 protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request,CancellationToken ct){
  ct.ThrowIfCancellationRequested();if(Offline||request.RequestUri!.AbsolutePath.EndsWith("/logout")&&BlockBearer is not null&&request.Headers.Authorization?.Parameter==BlockBearer)throw new HttpRequestException("fixture unavailable");
  string path=request.RequestUri!.AbsolutePath;
  if(path.EndsWith("/me")&&MeBody is not null)return Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK){Content=new StringContent(MeBody,Encoding.UTF8,"application/json")});
  if(path.EndsWith("/logout"))LastLogoutBearer=request.Headers.Authorization?.Parameter;
  if(path.EndsWith("/me")&&TransportDenial is not null)return Task.FromResult(new HttpResponseMessage(HttpStatusCode.Forbidden){Content=new StringContent(JsonSerializer.Serialize(new{error=new{code=TransportDenial}}),Encoding.UTF8,"application/json")});
  object value=path.EndsWith("/login")?new{user=new{id="33333333-3333-4333-8333-333333333333"},access_token=Prefix+Interlocked.Increment(ref Logins).ToString().PadLeft(11,'0'),expires_at=DateTimeOffset.UtcNow.AddHours(1).ToString("O")}:
   path.EndsWith("/identity")?new{product_id="11111111-1111-4111-8111-111111111111",licentivo_product_id="22222222-2222-4222-8222-222222222222",launch_connection_id="44444444-4444-4444-8444-444444444444",environment="live",identity_token=new string('I',64),expires_at=DateTimeOffset.UtcNow.AddMinutes(2).ToString("O")}:
   path.EndsWith("/me")?new{user=new{id="33333333-3333-4333-8333-333333333333"}}:new{};
  return Task.FromResult(new HttpResponseMessage(path.EndsWith("/logout")?LogoutStatus:HttpStatusCode.OK){Content=new StringContent(path.EndsWith("/logout")&&LogoutBody is not null?LogoutBody:JsonSerializer.Serialize(new{data=value}),Encoding.UTF8,"application/json")});
 }
}
sealed class FakeRuntime(Config config):IRuntime{
 public Config Config=config;public int Releases,Disposals;public bool Pending,FailDispose;
 public Action? OnStart;
 public Task Start(CancellationToken ct){ct.ThrowIfCancellationRequested();OnStart?.Invoke();return Task.CompletedTask;}
 public AuthorizationStatus Status()=>new("fixture",true);
 public Task RunFeature(string feature,Func<Task> action)=>feature=="export"?action():throw new LaunchException("FEATURE_DENIED");
 public Task Deactivate(CancellationToken ct){Releases++;if(Pending)throw new LaunchException("DEVICE_REVOCATION_PENDING");return Task.CompletedTask;}
 public ValueTask DisposeAsync(){Disposals++;if(FailDispose)throw new IOException("SDK shutdown failed");return ValueTask.CompletedTask;}
}
