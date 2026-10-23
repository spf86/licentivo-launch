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

/** Local fixtures only: real Ed25519 signatures and the unmodified official SDK. */
public final class OfficialSDKTests {
    static final String PRODUCT="11111111-1111-4111-8111-111111111111", REMOTE="22222222-2222-4222-8222-222222222222", KEY="signing-key-1";
    static void reply(HttpExchange exchange,int status,Object body)throws Exception{byte[] bytes=Json.write(body).getBytes(StandardCharsets.UTF_8);exchange.sendResponseHeaders(status,bytes.length);try(var output=exchange.getResponseBody()){output.write(bytes);}}
    static Map<String,Object> sign(KeyPair key,String device)throws Exception{
        long now=Instant.now().getEpochSecond();var claims=new LinkedHashMap<String,Object>();
        claims.put("version",1L);claims.put("key_id",KEY);claims.put("tenant_id","33333333-3333-4333-8333-333333333333");claims.put("product_id",REMOTE);claims.put("license_id","44444444-4444-4444-8444-444444444444");claims.put("device_id",device);claims.put("activation_id","55555555-5555-4555-8555-555555555555");claims.put("mode","timed");claims.put("features",List.of("export"));claims.put("max_devices",1L);claims.put("issued_at",now);claims.put("expires_at",now+3600);
        byte[] payload=Json.write(claims).getBytes(StandardCharsets.UTF_8);Signature signer=Signature.getInstance("Ed25519");signer.initSign(key.getPrivate());signer.update("LicenovaLease/v1\n".getBytes(StandardCharsets.UTF_8));signer.update(payload);
        var encoder=Base64.getUrlEncoder().withoutPadding();return Map.of("version",1L,"key_id",KEY,"payload",encoder.encodeToString(payload),"signature",encoder.encodeToString(signer.sign()));
    }
    public static void main(String[] args)throws Exception{
        Path root=Files.createTempDirectory("launch-java-official-");KeyPair key=KeyPairGenerator.getInstance("Ed25519").generateKeyPair();byte[] der=key.getPublic().getEncoded();String pin=Base64.getUrlEncoder().withoutPadding().encodeToString(Arrays.copyOfRange(der,der.length-32,der.length));
        var envelope=sign(key,MachineIdentity.deviceID(REMOTE));var offline=new AtomicBoolean();
        var merchant=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);var licensing=HttpServer.create(new InetSocketAddress("127.0.0.1",0),0);
        merchant.createContext("/",exchange->{try{
            if(offline.get()){reply(exchange,503,Map.of("error",Map.of("code","UNAVAILABLE")));return;}
            String path=exchange.getRequestURI().getPath();Object data;
            if(path.endsWith("/login"))data=Map.of("access_token","private-bearer","expires_at",Instant.now().plusSeconds(3600).toString(),"user",Map.of("id","account-a","email","a@example.invalid"));
            else if(path.endsWith("/me"))data=Map.of("user",Map.of("id","account-a"));
            else if(path.endsWith("/logout"))data=Map.of("signed_out",true);
            else data=Map.of("product_id",PRODUCT,"license_key","lv_lic_"+"a".repeat(43),"version",1L);
            reply(exchange,200,Map.of("data",data));
        }catch(Exception error){exchange.close();}});
        licensing.createContext("/",exchange->{try{
            if(offline.get()){reply(exchange,503,Map.of("error",Map.of("code","SERVER_UNAVAILABLE")));return;}
            reply(exchange,200,Map.of("data",Map.of("activation_id","55555555-5555-4555-8555-555555555555","device_key","lv_dev_"+"d".repeat(43),"lease",envelope,"heartbeat_seconds",600L)));
        }catch(Exception error){exchange.close();}});
        merchant.start();licensing.start();
        try{
            var config=new Launch.Config("http://127.0.0.1:"+merchant.getAddress().getPort(),PRODUCT,REMOTE,"sandbox");
            var product=Map.<String,Object>of("product_id",REMOTE,"trusted_keys",Map.of(KEY,pin),"base_url","http://127.0.0.1:"+licensing.getAddress().getPort(),"allow_http",true);
            var store=new Launch.MemoryStore();Path cache=root.resolve("runtime");
            try(var app=new Launch(config,product,cache,store)){app.login("a@example.invalid","private-password".toCharArray());app.runFeature("export",()->{});}
            offline.set(true);
            try(var app=new Launch(config,product,cache,store)){
                if(!app.restore().offlineFallback())throw new AssertionError("offline route missing");app.runFeature("export",()->{});
                boolean[] ran={false};try{app.runFeature("absent",()->ran[0]=true);throw new AssertionError("feature accepted");}catch(IllegalStateException expected){}if(ran[0])throw new AssertionError("unlicensed callback executed");
            }
            Path cacheFile;try(var files=Files.walk(cache)){cacheFile=files.filter(path->path.getFileName().toString().equals("lease.json")).findFirst().orElseThrow();}
            var saved=Json.object(Json.parse(Files.readString(cacheFile)));Json.object(saved.get("lease")).put("signature",Base64.getUrlEncoder().withoutPadding().encodeToString(new byte[64]));Files.writeString(cacheFile,Json.write(saved));
            try(var app=new Launch(config,product,cache,store)){try{app.restore();throw new AssertionError("tampered signature accepted");}catch(Launch.Failure expected){if(!expected.code.equals("OFFLINE_UNAVAILABLE"))throw expected;}}
            System.out.println("Java: official SDK online/restart/offline/feature-denial/tamper test passed");
        }finally{merchant.stop(0);licensing.stop(0);try(var paths=Files.walk(root)){for(Path path:paths.sorted(Comparator.reverseOrder()).toList())Files.deleteIfExists(path);}}
    }
}
