import com.licentivo.launch.Launch;
import licentivo.Client;
import licentivo.Json;
import java.nio.file.*;
import java.util.*;
public class ClientJava {
 static final Path base=Path.of("/home/spf86/n-fixes-20261101");
 public static void main(String[] args)throws Exception{
  String name=args[0];int[] calls={0};Launch app=null;
  try{
   var report=Launch.readPublic(base.resolve(name+"-public.json"));
   var config=Json.object(report.get("config"));var product=new LinkedHashMap<String,Object>(Json.object(report.get("product_config")));product.put("allow_http",true);
   var secret=Launch.readPublic(base.resolve("deployment-private.json"));
   var cfg=new Launch.Config(Json.text(config,"merchant_origin"),Json.text(config,"launch_product_id"),Json.text(config,"licentivo_product_id"),"sandbox",true);
   var store=new Launch.MemoryStore();
   app=new Launch(cfg,product,base.resolve("java-"+name),store);
   app.login("alice@launch-joint.example",Json.text(secret,"customer_password").toCharArray());
   if(name.equals("fixed")){String operation="nfix-java-"+UUID.randomUUID();int[] count={0};Client.FeatureAction job=()->{count[0]++;Files.writeString(base.resolve("metered-java.txt"),operation);};app.runMeteredFeature("export",1,operation,job);app.runMeteredFeature("export",1,operation,job);app.resolveMeteredFeature(operation,true);if(count[0]!=1)throw new IllegalStateException("METERING_REPEATED");}

   Client.FeatureAction action=()->{calls[0]++;Files.writeString(base.resolve("java-"+name+"-business.txt"),"Verified Launch SDK business\n");};
   app.runFeature("export",action);boolean denied=false;try{app.runFeature("not_entitled",action);}catch(Exception failure){denied=true;}if(!denied||calls[0]!=1)throw new IllegalStateException();
   app.close();app=new Launch(cfg,product,base.resolve("java-"+name),store);app.restore();app.runFeature("export",action);app.logout();app.close();
   app=new Launch(cfg,product,base.resolve("java-"+name),store);boolean blocked=false;try{app.restore();}catch(Launch.Failure failure){blocked=failure.code.equals("NO_SESSION");}app.close();
   app=new Launch(cfg,product,base.resolve("java-"+name),store);String code="";try{app.login("bob@launch-joint.example",Json.text(secret,"customer_password").toCharArray());}catch(Client.APIException failure){code=failure.code;}
   System.out.println(Json.write(Map.of("ok",calls[0]==2&&blocked&&code.equals("LAUNCH_ENTITLEMENT_REQUIRED"),"language","Java","product",name,"callbacks",calls[0],"startup_denial",code,"logout_rejects_restore",blocked)));
  }catch(Exception failure){System.out.println("{\"ok\":false,\"language\":\"Java\",\"code\":\""+failure.getClass().getSimpleName()+"\"}");System.exit(1);}finally{if(app!=null)app.close();}
 }
}
