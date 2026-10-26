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
Console.WriteLine("C#: lifecycle, DPAPI, failed-save logout, total storage failure and stale-instance retry passed.");
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
 public bool Offline;public string? BlockBearer;static int Logins;static readonly string Prefix=Guid.NewGuid().ToString("N");
 protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request,CancellationToken ct){
  ct.ThrowIfCancellationRequested();if(Offline||request.RequestUri!.AbsolutePath.EndsWith("/logout")&&BlockBearer is not null&&request.Headers.Authorization?.Parameter==BlockBearer)throw new HttpRequestException("fixture unavailable");
  string path=request.RequestUri!.AbsolutePath;object value=path.EndsWith("/login")?new{user=new{id="33333333-3333-4333-8333-333333333333"},access_token=Prefix+Interlocked.Increment(ref Logins).ToString().PadLeft(11,'0'),expires_at=DateTimeOffset.UtcNow.AddHours(1).ToString("O")}:
   path.EndsWith("/identity")?new{product_id="11111111-1111-4111-8111-111111111111",licentivo_product_id="22222222-2222-4222-8222-222222222222",launch_connection_id="44444444-4444-4444-8444-444444444444",environment="live",identity_token=new string('I',64),expires_at=DateTimeOffset.UtcNow.AddMinutes(2).ToString("O")}:
   path.EndsWith("/me")?new{user=new{id="33333333-3333-4333-8333-333333333333"}}:new{};
  return Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK){Content=new StringContent(JsonSerializer.Serialize(new{data=value}),Encoding.UTF8,"application/json")});
 }
}
sealed class FakeRuntime(Config config):IRuntime{
 public Config Config=config;public int Releases;public bool Pending;
 public Task Start(CancellationToken ct){ct.ThrowIfCancellationRequested();return Task.CompletedTask;}
 public AuthorizationStatus Status()=>new("fixture",true);
 public Task RunFeature(string feature,Func<Task> action)=>feature=="export"?action():throw new LaunchException("FEATURE_DENIED");
 public Task Deactivate(CancellationToken ct){Releases++;if(Pending)throw new LaunchException("DEVICE_REVOCATION_PENDING");return Task.CompletedTask;}
 public ValueTask DisposeAsync()=>ValueTask.CompletedTask;
}
