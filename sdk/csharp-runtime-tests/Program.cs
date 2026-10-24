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

const string Product="11111111-1111-4111-8111-111111111111",Remote="22222222-2222-4222-8222-222222222222",Connection="33333333-3333-4333-8333-333333333333",Activation="55555555-5555-4555-8555-555555555555",Operation="66666666-6666-4666-8666-666666666666";
static string B64(byte[] value)=>Convert.ToBase64String(value).TrimEnd('=').Replace('+','-').Replace('/','_');
static void Check(bool value,string text){if(!value)throw new Exception(text);}
var signing=new Ed25519PrivateKeyParameters(new byte[32],0);string pin=B64(signing.GeneratePublicKey().GetEncoded());
bool offline=false;int releases=0;
var builder=WebApplication.CreateBuilder();builder.Logging.ClearProviders();builder.WebHost.UseUrls("http://127.0.0.1:0");await using var server=builder.Build();
server.Run(async context=>{
 if(offline){context.Response.StatusCode=503;await context.Response.WriteAsync("{}");return;}
 string path=context.Request.Path;object result;
 JsonNode? body=context.Request.ContentLength>0?await JsonNode.ParseAsync(context.Request.Body):null;
 if(path.EndsWith("/auth/login"))result=new{user=new{id=Connection},access_token=new string('T',43),expires_at=DateTimeOffset.UtcNow.AddHours(1).ToString("O")};
 else if(path.EndsWith("/me"))result=new{user=new{id=Connection}};
 else if(path.EndsWith("/identity"))result=new{product_id=Product,licentivo_product_id=Remote,launch_connection_id=Connection,environment="sandbox",identity_token=new string('I',64),expires_at=DateTimeOffset.UtcNow.AddMinutes(2).ToString("O")};
 else if(path.EndsWith("/auth/logout"))result=new{signed_out=true};
 else if(path.EndsWith("/sessions/logout")){Interlocked.Increment(ref releases);result=new{operation_id=Operation,activation_id=Activation,expected_generation=1,status="succeeded"};}
 else if(path.EndsWith("/launch/exchange")||path.EndsWith("/validate")||path.EndsWith("/heartbeat")){
  if(path.EndsWith("/launch/exchange"))Check(body?["identity_token"]?.GetValue<string>()==new string('I',64),"SDK did not exchange identity");
  long now=DateTimeOffset.UtcNow.ToUnixTimeSeconds();var claims=new{version=1,key_id="test",tenant_id=Connection,product_id=Remote,license_id="44444444-4444-4444-8444-444444444444",device_id=body!["device_id"]!.GetValue<string>(),activation_id=Activation,mode="timed",features=new[]{"export"},max_devices=1,issued_at=now,expires_at=now+3600,runtime_policy=new{offline_mode="limited",offline_seconds=3600,heartbeat_seconds=600},capabilities=new{license_model="node_locked",launch_connection_id=Connection,environment="sandbox",session_id="77777777-7777-4777-8777-777777777777",session_expires_at=now+3600,binding_version=1}};
  byte[] payload=JsonSerializer.SerializeToUtf8Bytes(claims),message=Encoding.UTF8.GetBytes("LicenovaLease/v1\n").Concat(payload).ToArray();var signer=new Ed25519Signer();signer.Init(true,signing);signer.BlockUpdate(message,0,message.Length);
  result=new{activation_id=Activation,device_key="lv_dev_"+new string('d',43),lease=new{version=1,key_id="test",payload=B64(payload),signature=B64(signer.GenerateSignature())},heartbeat_seconds=600,logout_ticket=new{operation_id=Operation,product_id=Remote,activation_id=Activation,expected_generation=1,release_token="lv_out_"+new string('o',43)}};
 }else throw new Exception("Unexpected fixture path "+path);
 await context.Response.WriteAsJsonAsync(new{data=result});
});await server.StartAsync();string origin=server.Urls.Single();
string root=Path.Combine(Path.GetTempPath(),"launch-csharp-account-"+Guid.NewGuid().ToString("N"));Directory.CreateDirectory(root);var store=new MemoryStore();
var config=new Config(origin,Remote,"","","",new(){["test"]=pin},"",true,TimeoutSeconds:2);
Application Open()=>new(origin,Product,config,"sandbox",store,root);
try{
 await using(var app=Open()){await app.Login("a@example.invalid","password");await app.RunFeature("export",()=>Task.CompletedTask);}
 string file=Directory.GetFiles(root,"lease.json",SearchOption.AllDirectories).Single();byte[] original=File.ReadAllBytes(file);offline=true;
 await using(var app=Open()){try{await app.Restore();throw new Exception("Launch chose offline authorization");}catch(LaunchException){}}
 var sdkConfig=config with{CachePath=file,LaunchConnectionId=Connection,Environment="sandbox"};
 await using(var sdk=new LicentivoClient(sdkConfig)){
  await sdk.Start();Check(sdk.Status().Allowed,"Official SDK rejected valid offline permission");bool ran=false;try{await sdk.RunFeature("absent",()=>{ran=true;return Task.CompletedTask;});throw new Exception("Absent feature allowed");}catch(InvalidOperationException){}Check(!ran,"Unauthorized callback executed");
 }
 var tampered=JsonNode.Parse(original)!;tampered["lease"]!["signature"]=B64(new byte[64]);File.WriteAllText(file,tampered.ToJsonString());
 await using(var sdk=new LicentivoClient(sdkConfig)){try{await sdk.Start();}catch(Exception){}Check(!sdk.Status().Allowed,"Tampered cache granted permission");}
 File.WriteAllBytes(file,original);
 await using(var app=Open()){try{await app.Logout();throw new Exception("Pending logout reported success");}catch(LaunchException e){Check(e.Code=="DEVICE_REVOCATION_PENDING","Wrong logout error: "+e.Code);}}
 Check(File.Exists(file+".logout.json")&&File.Exists(file+".credential.json"),"SDK logout recovery state lost");
 offline=false;await using(var sdk=new LicentivoClient(sdkConfig)){await sdk.RetryPendingLogouts();Check(!sdk.Status().Allowed,"Recovery restored authorization");}Check(releases>0,"Pending logout was not delivered");
 Console.WriteLine("C# official SDK: account exchange, signed lease, SDK-owned offline checks, tamper rejection, pending logout and recovery passed.");
}finally{await server.StopAsync();string resolved=Path.GetFullPath(root);if(!resolved.StartsWith(Path.GetFullPath(Path.Combine(Path.GetTempPath(),"launch-csharp-account-")),StringComparison.OrdinalIgnoreCase))throw new Exception("Unsafe test path");Directory.Delete(resolved,true);}
