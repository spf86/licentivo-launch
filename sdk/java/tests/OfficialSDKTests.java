import com.licentivo.launch.*;
import licentivo.*;
import com.sun.net.httpserver.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.security.*;
import java.time.*;
import java.util.*;
import java.util.concurrent.atomic.AtomicBoolean;

public final class OfficialSDKTests {
 static final String PRODUCT="11111111-1111-4111-8111-111111111111",REMOTE="22222222-2222-4222-8222-222222222222",CONNECTION="33333333-3333-4333-8333-333333333333",ACTIVATION="55555555-5555-4555-8555-555555555555",OPERATION="66666666-6666-4666-8666-666666666666",KEY="signing-test";
 static void check(boolean value,String text){if(!value)throw new AssertionError(text);}
 static void reply(HttpExchange e,Object data)throws Exception{byte[] bytes=Json.write(Map.of("data",data)).getBytes(StandardCharsets.UTF_8);e.sendResponseHeaders(200,bytes.length);try(var out=e.getResponseBody()){out.write(bytes);}}
 static Map<String,Object> sign(KeyPair key)throws Exception{
  long now=Instant.now().getEpochSecond();var claims=new LinkedHashMap<String,Object>();
  claims.put("version",1L);claims.put("key_id",KEY);claims.put("tenant_id",CONNECTION);claims.put("product_id",REMOTE);claims.put("license_id","44444444-4444-4444-8444-444444444444");claims.put("device_id",MachineIdentity.deviceID(REMOTE));claims.put("activation_id",ACTIVATION);claims.put("mode","timed");claims.put("features",List.of("export"));claims.put("max_devices",1L);claims.put("issued_at",now);claims.put("expires_at",now+3600);
  claims.put("runtime_policy",Map.of("offline_mode","limited","offline_seconds",3600L,"heartbeat_seconds",600L));
  claims.put("capabilities",Map.of("license_model","node_locked","launch_connection_id",CONNECTION,"environment","sandbox","session_id","77777777-7777-4777-8777-777777777777","session_expires_at",now+3600,"binding_version",1L));
  byte[] raw=Json.write(claims).getBytes(StandardCharsets.UTF_8);Signature signer=Signature.getInstance("Ed25519");signer.initSign(key.getPrivate());signer.update("LicenovaLease/v1\n".getBytes(StandardCharsets.UTF_8));signer.update(raw);var b64=Base64.getUrlEncoder().withoutPadding();return Map.of("version",1L,"key_id",KEY,"payload",b64.encodeToString(raw),"signature",b64.encodeToString(signer.sign()));
 }
 public static void main(String[] args)throws Exception{
  Path root=Files.createTempDirectory("launch-java-account-");var key=KeyPairGenerator.getInstance("Ed25519").generateKeyPair();byte[] der=key.getPublic().getEncoded();String pin=Base64.getUrlEncoder().withoutPadding().encodeToString(Arrays.copyOfRange(der,der.length-32,der.length));var lease=sign(key);var offline=new AtomicBoolean();var released=new AtomicBoolean();
  var server=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);
  server.createContext("/",e->{try{
   byte[] raw=e.getRequestBody().readAllBytes();
   var input=Json.object(Json.parse(raw.length==0?"{}":new String(raw,StandardCharsets.UTF_8)));
   if(offline.get()){e.sendResponseHeaders(503,-1);e.close();return;}
   String path=e.getRequestURI().getPath();Object value;
   if(path.endsWith("/auth/login"))value=Map.of("access_token","T".repeat(43),"expires_at",Instant.now().plusSeconds(3600).toString(),"user",Map.of("id",CONNECTION));
   else if(path.endsWith("/me"))value=Map.of("user",Map.of("id",CONNECTION));
   else if(path.endsWith("/identity"))value=Map.of("product_id",PRODUCT,"licentivo_product_id",REMOTE,"launch_connection_id",CONNECTION,"environment","sandbox","identity_token","I".repeat(64),"expires_at",Instant.now().plusSeconds(120).toString());
   else if(path.endsWith("/auth/logout"))value=Map.of("signed_out",true);
   else if(path.endsWith("/sessions/logout")){released.set(true);value=Map.of("operation_id",OPERATION,"activation_id",ACTIVATION,"expected_generation",1L,"status","succeeded");}
   else if(path.endsWith("/launch/exchange")||path.endsWith("/validate")||path.endsWith("/heartbeat")){
    if(path.endsWith("/launch/exchange"))check("I".repeat(64).equals(input.get("identity_token")),"SDK did not exchange account identity");
    check(input.get("device_name") instanceof String name&&!name.isBlank(),"SDK did not supply the optional device name");
    check(input.get("device_info") instanceof Map<?,?> info&&info.get("system_type") instanceof String type&&!type.isBlank(),"SDK omitted display details");
    value=Map.of("activation_id",ACTIVATION,"device_key","lv_dev_"+"d".repeat(43),"lease",lease,"heartbeat_seconds",600L,"logout_ticket",Map.of("operation_id",OPERATION,"product_id",REMOTE,"activation_id",ACTIVATION,"expected_generation",1L,"release_token","lv_out_"+"o".repeat(43)));
   }
   else throw new AssertionError("Unexpected endpoint "+path);
   reply(e,value);
  }catch(Exception error){e.close();}});server.start();
  try{
   String origin="http://127.0.0.1:"+server.getAddress().getPort();var config=new Launch.Config(origin,PRODUCT,REMOTE,"sandbox");var product=Map.<String,Object>of("product_id",REMOTE,"trusted_keys",Map.of(KEY,pin),"base_url",origin,"allow_http",true);var store=new Launch.MemoryStore();Path cache=root.resolve("runtime");
   try(var app=new Launch(config,product,cache,store)){app.login("a@example.invalid","password".toCharArray());app.runFeature("export",()->{});}
   Path file;try(var paths=Files.walk(cache)){file=paths.filter(p->p.getFileName().toString().equals("lease.json")).findFirst().orElseThrow();}
   offline.set(true);try(var app=new Launch(config,product,cache,store)){try{app.restore();throw new AssertionError("Launch granted offline permission");}catch(Launch.Failure expected){}}
   var sdkConfig=new HashMap<String,Object>(product);sdkConfig.put("license_key","");sdkConfig.put("identity_token","");sdkConfig.put("launch_connection_id",CONNECTION);sdkConfig.put("environment","sandbox");sdkConfig.put("cache_path",file.toString());
   try(var sdk=new Client(sdkConfig)){sdk.start(error->{});sdk.runFeature("export",()->{});boolean[] ran={false};try{sdk.runFeature("absent",()->ran[0]=true);throw new AssertionError("Absent feature allowed");}catch(IllegalStateException expected){}check(!ran[0],"Unauthorized callback ran");}
   offline.set(false);try(var app=new Launch(config,product,cache,store)){app.logout();}check(released.get()&&Files.exists(Path.of(file+".logout.json")),"SDK logout was not retained and confirmed");
   System.out.println("Java official SDK: account exchange, signed permission, SDK-owned offline checks, feature denial and logout passed.");
  }finally{server.stop(0);try(var paths=Files.walk(root)){for(var path:paths.sorted(Comparator.reverseOrder()).toList())Files.deleteIfExists(path);}}
 }
}
