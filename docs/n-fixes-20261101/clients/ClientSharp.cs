using System.Text.Json;
using Licentivo;
using Licentivo.Launch;
const string root="/home/spf86/n-fixes-20261101";
string name=args[0];int calls=0;Application? app=null;
try{
 using var report=JsonDocument.Parse(File.ReadAllText(Path.Combine(root,name+"-public.json")));
 using var secrets=JsonDocument.Parse(File.ReadAllText(Path.Combine(root,"deployment-private.json")));
 var cfg=report.RootElement.GetProperty("config");
 var product=JsonSerializer.Deserialize<Config>(report.RootElement.GetProperty("product_config").GetRawText(),LicentivoClient.JsonOptions)! with{AllowHttp=true};
 var store=new MemoryStore();
 Application Create()=>new(cfg.GetProperty("merchant_origin").GetString()!,cfg.GetProperty("launch_product_id").GetString()!,product,"sandbox",store,Path.Combine(root,"csharp-"+name),allowHttp:true);
 Func<Task> action=async()=>{calls++;await File.WriteAllTextAsync(Path.Combine(root,"csharp-"+name+"-business.txt"),"Verified Launch SDK business\n");};
 app=Create();await app.Login("alice@launch-joint.example",secrets.RootElement.GetProperty("customer_password").GetString()!);
 if(name=="fixed"){string operation="nfix-csharp-"+Guid.NewGuid();int count=0;Func<Task> job=async()=>{count++;await File.WriteAllTextAsync(Path.Combine(root,"metered-csharp.txt"),operation);};await app.RunMeteredFeature("export",1,operation,job);await app.RunMeteredFeature("export",1,operation,job);await app.ResolveMeteredFeature(operation,true);if(count!=1)throw new Exception("METERING_REPEATED");}
await app.RunFeature("export",action);
 bool denied=false;try{await app.RunFeature("not_entitled",action);}catch{denied=true;}if(!denied||calls!=1)throw new Exception("FEATURE_GATE_FAILED");
 await app.DisposeAsync();app=Create();await app.Restore();await app.RunFeature("export",action);await app.Logout();await app.DisposeAsync();
 app=Create();bool blocked=!await app.Restore();await app.DisposeAsync();app=Create();string code="";
 try{await app.Login("bob@launch-joint.example",secrets.RootElement.GetProperty("customer_password").GetString()!);}catch(ApiException error){code=error.Code;}
 Console.WriteLine(JsonSerializer.Serialize(new{ok=calls==2&&blocked&&code=="LAUNCH_ENTITLEMENT_REQUIRED",language="C#",product=name,callbacks=calls,startup_denial=code,logout_rejects_restore=blocked}));
}catch(Exception error){Console.WriteLine(JsonSerializer.Serialize(new{ok=false,language="C#",product=name,code=error is LaunchException launch?launch.Code:error.GetType().Name}));Environment.ExitCode=1;}
finally{if(app is not null)await app.DisposeAsync();}
