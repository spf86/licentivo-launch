import com.licentivo.launch.*;
import licentivo.*;
import javax.crypto.KeyGenerator;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.time.*;
import java.util.*;

public final class LaunchTests {
    static final String PRODUCT="11111111-1111-4111-8111-111111111111", REMOTE="22222222-2222-4222-8222-222222222222", KEY="33333333-3333-4333-8333-333333333333";
    static void check(boolean condition,String message){if(!condition)throw new AssertionError(message);}
    interface Checked {void run() throws Exception;}
    static void expect(String code,Checked action)throws Exception{try{action.run();throw new AssertionError("expected "+code);}catch(Launch.Failure failure){check(failure.code.equals(code),"wrong failure: "+failure.code);}}
    static final class Store implements Launch.Store {
        final Map<String,byte[]> values=new HashMap<>();
        boolean failSaves,failDeletes;
        public byte[] load(String key){return values.containsKey(key)?values.get(key).clone():null;}
        public void save(String key,byte[] value)throws Launch.Failure{if(failSaves)throw new Launch.Failure("TEST_STORAGE_FAILED");values.put(key,value.clone());}
        public void delete(String key)throws Launch.Failure{if(failDeletes)throw new Launch.Failure("TEST_STORAGE_FAILED");values.remove(key);}
    }
    static final class Runtime implements Launch.Runtime {
        boolean offline=true,denied=false;int releases,closes,starts,checks;
        public boolean canStartOffline(){return offline;}
        public void start(){starts++;}
        public void runFeature(String name,Client.FeatureAction action)throws Exception{checks++;if(denied)throw new Launch.Failure("FEATURE_DENIED");action.run();}
        public void releaseDevice(){releases++;}
        public void close(){closes++;}
    }
    static final class Fixture implements AutoCloseable {
        final Store store=new Store();final Path root=Files.createTempDirectory("launch-java-");
        final String tokenPrefix="private-bearer-"+UUID.randomUUID()+"-";
        final List<Runtime> runtimes=new ArrayList<>();final List<String> paths=new ArrayList<>();final List<Long> waits=new ArrayList<>();
        String mode="",user="account-a",retry="",blockedToken="";int credentials,logouts,logins;boolean offline=true;
        final Launch.Config config=new Launch.Config("https://merchant.example",PRODUCT,REMOTE,"sandbox");
        final Map<String,Object> product=Map.of("product_id",REMOTE,"trusted_keys",Map.of(KEY,Base64.getUrlEncoder().withoutPadding().encodeToString(new byte[32])));
        Launch app;
        Fixture()throws Exception{app=create(config,product);}
        Launch create(Launch.Config cfg,Map<String,Object> prod)throws Exception{
            return new Launch(cfg,prod,root,store,(method,uri,token,body)->{
                if(mode.equals("offline"))throw new Launch.Failure("SERVICE_UNAVAILABLE",0,"",0,true);
                String path=uri.getPath();Object data=Map.of();int status=200;String code="";
                if(path.endsWith("/logout")&&!blockedToken.isEmpty()&&token.equals(blockedToken))throw new Launch.Failure("SERVICE_UNAVAILABLE",0,"",0,true);
                if(path.endsWith("/login"))data=Map.of("access_token",tokenPrefix+(++logins),"expires_at",Instant.now().plusSeconds(3600).toString(),"user",Map.of("id",user,"email","a@example.invalid"));
                else if(path.endsWith("/logout")){logouts++;data=Map.of("signed_out",true);}
                else if(path.endsWith("/me")){data=Map.of("user",Map.of("id",user));if(mode.equals("unauthorized")){status=401;code="UNAUTHENTICATED";}}
                else {credentials++;data=Map.of("product_id",PRODUCT,"licentivo_product_id",REMOTE,"environment",cfg.environment(),"launch_connection_id",KEY,"identity_token","I".repeat(64),"expires_at",Instant.now().plusSeconds(120).toString());if(!mode.isEmpty()){status=403;code=mode;}}
                Object envelope=status==200?Map.of("data",data):Map.of("error",Map.of("code",code),"request_id","request-1");
                return new Launch.Response(status,Json.write(envelope).getBytes(StandardCharsets.UTF_8),retry);
            },settings->{Runtime runtime=new Runtime();runtime.offline=offline;runtimes.add(runtime);paths.add((String)settings.get("cache_path"));Files.writeString(Path.of(paths.get(paths.size()-1)),"SDK-owned grant");return runtime;},waits::add);
        }
        void login()throws Exception{char[] password="private-password".toCharArray();app.login("a@example.invalid",password);check(password[0]=='\0',"password retained in caller buffer");}
        public void close()throws Exception{app.close();if(Files.exists(root))try(var paths=Files.walk(root)){for(var path:paths.sorted(Comparator.reverseOrder()).toList())Files.deleteIfExists(path);}}
    }
    static void lifecycle()throws Exception{
        try(var f=new Fixture()){
            f.login();String saved=new String(f.store.values.values().iterator().next(),StandardCharsets.UTF_8);
            check(!saved.contains("I".repeat(64))&&!saved.contains("license_key"),"saved authorization credential");
            f.app.close();check(f.logouts==0&&f.runtimes.get(0).releases==0,"Close released device or signed out");
            f.mode="offline";f.app=f.create(f.config,f.product);expect("SERVICE_UNAVAILABLE",f.app::restore);
            check(f.runtimes.size()==1,"Launch selected offline authorization");
            f.mode="";f.app.restore();Runtime runtime=f.runtimes.get(1);runtime.denied=true;boolean[] executed={false};
            expect("FEATURE_DENIED",()->f.app.runFeature("export",()->executed[0]=true));check(!executed[0]&&runtime.checks==1,"feature bypass");
            check(f.paths.get(0).equals(f.paths.get(1)),"restart lost scope");
        }
    }
    static void denials()throws Exception{
        for(String code:List.of("unauthorized","ENTITLEMENT_EXPIRED","ENTITLEMENT_SUSPENDED","ENTITLEMENT_NOT_PURCHASED"))try(var f=new Fixture()){
            f.login();f.app.close();f.mode=code;expect(code.equals("unauthorized")?"UNAUTHENTICATED":code,f.app::restore);
            check(f.runtimes.size()==1&&f.store.values.isEmpty()&&Files.exists(Path.of(f.paths.get(0))),"denial did not clear state");
        }
    }
    static void pending()throws Exception{
        try(var f=new Fixture()){
            f.login();f.app.close();f.mode="ENTITLEMENT_PENDING";f.retry="3";int before=f.credentials;expect("ENTITLEMENT_PENDING",f.app::restore);
            check(f.credentials-before==1&&f.waits.isEmpty(),"retry bound/header");check(!f.store.values.isEmpty()&&f.runtimes.size()==1,"pending lost login or allowed fallback");
            f.retry="30";before=f.credentials;expect("ENTITLEMENT_PENDING",f.app::restore);check(f.credentials-before==1,"server delay shortened");
        }
    }
    static void identityAndExpiry()throws Exception{
        try(var f=new Fixture()){
            f.login();f.app.close();f.user="account-b";expect("SESSION_MISMATCH",f.app::restore);check(f.runtimes.size()==1,"wrong account started runtime");
        }
        try(var f=new Fixture()){
            f.login();f.app.close();String key=f.store.values.keySet().iterator().next();var saved=Json.object(Json.parse(new String(f.store.values.get(key),StandardCharsets.UTF_8)));Json.object(saved.get("grant")).put("expires_at","2000-01-01T00:00:00Z");f.store.save(key,Json.write(saved).getBytes(StandardCharsets.UTF_8));expect("SESSION_EXPIRED",f.app::restore);check(f.store.values.isEmpty(),"expired session retained");
        }
    }
    static void actionsAndScope()throws Exception{
        try(var f=new Fixture()){
            f.login();f.app.releaseDevice();check(f.runtimes.get(0).releases==1&&f.logouts==1,"release not explicit");f.user="account-b";f.login();check(!f.paths.get(0).equals(f.paths.get(1)),"accounts share cache");
            try(var live=f.create(new Launch.Config("https://merchant.example",PRODUCT,REMOTE,"live"),f.product)){expect("NO_SESSION",live::restore);}
            f.mode="offline";expect("SERVICE_UNAVAILABLE",f.app::logout);check(!f.store.values.isEmpty()&&Files.exists(Path.of(f.paths.get(1))),"offline logout lost its retry task");
            f.app.close();f.app=f.create(f.config,f.product);int before=f.credentials;expect("SERVICE_UNAVAILABLE",f.app::restore);check(f.credentials==before,"pending account fetched identity");
            f.mode="";expect("NO_SESSION",f.app::restore);check(f.store.values.isEmpty(),"recovered account logout was not cleared");
        }
    }
    static void configAndRedaction()throws Exception{
        check(new Launch.Failure("lv_lic_secret\nprivate").getMessage().equals("Launch: HTTP_ERROR"),"error leak");
        try(var f=new Fixture()){
            var bad=new HashMap<>(f.product);bad.put("vendor_api_key","private");expect("CONFIGURATION_ERROR",()->f.create(f.config,bad));
            bad.remove("vendor_api_key");bad.put("license_key","private");expect("CONFIGURATION_ERROR",()->f.create(f.config,bad));
            var wrong=new HashMap<>(f.product);wrong.put("trusted_keys",Map.of(KEY,"bad"));expect("CONFIGURATION_ERROR",()->f.create(f.config,wrong));
        }
    }
    static void encryptedStore()throws Exception{
        Path root=Files.createTempDirectory("launch-java-store-");var generator=KeyGenerator.getInstance("AES");generator.init(256);var key=generator.generateKey();
        var store=new EncryptedFileStore(root,key);byte[] secret="private-bearer private-license".getBytes(StandardCharsets.UTF_8);store.save("account-product-sandbox",secret);
        Path file;try(var files=Files.list(root)){file=files.findFirst().orElseThrow();}
        check(!new String(Files.readAllBytes(file),StandardCharsets.UTF_8).contains("private-bearer"),"plaintext session");
        check(Arrays.equals(new EncryptedFileStore(root,key).load("account-product-sandbox"),secret),"encrypted restart");
        store.save("account-product-sandbox","updated".getBytes(StandardCharsets.UTF_8));check(new String(store.load("account-product-sandbox")).equals("updated"),"atomic overwrite");
        byte[] bytes=Files.readAllBytes(file);bytes[bytes.length-1]^=1;Files.write(file,bytes);expect("SESSION_STORE_CORRUPT",()->store.load("account-product-sandbox"));
        store.delete("account-product-sandbox");Files.delete(root);
    }
    public static void main(String[] args)throws Exception{
        lifecycle();denials();pending();identityAndExpiry();actionsAndScope();configAndRedaction();encryptedStore();
        failedStoreLogout();staleInstance();
        System.out.println("Java: 9 lifecycle/security test groups passed");
    }
    static void failedStoreLogout()throws Exception{
        for(boolean deleteFails:List.of(false,true))try(var f=new Fixture()){
            f.login();f.store.failSaves=true;f.store.failDeletes=deleteFails;f.mode="offline";
            expect("ACCOUNT_LOGOUT_STORAGE_FAILED",f.app::logout);
            expect("NOT_STARTED",()->f.app.runFeature("export",()->{throw new AssertionError("signed-out callback");}));
            if(!deleteFails){check(f.store.values.isEmpty(),"old login remained after failed saves");f.store.failSaves=false;f.app.close();try(var restarted=f.create(f.config,f.product)){expect("NO_SESSION",restarted::restore);}}
            else{try(var restarted=f.create(f.config,f.product)){expect("ACCOUNT_LOGOUT_STORAGE_FAILED",restarted::restore);}}
        }
    }
    static void staleInstance()throws Exception{
        try(var f=new Fixture()){f.login();f.app.logout();try(var newer=f.create(f.config,f.product)){newer.login("a","b".toCharArray());f.app.logout();check(!f.store.values.isEmpty(),"completed old logout adopted newer login");newer.restore();}}
        try(var f=new Fixture()){
            f.login();String oldToken=f.tokenPrefix+"1";
            try(var newer=f.create(f.config,f.product)){
                newer.login("a","b".toCharArray());String active=f.store.values.keySet().stream().filter(key->!key.endsWith("\naccount-logout-queue-v1")).findFirst().orElseThrow();byte[] before=f.store.values.get(active).clone();
                f.blockedToken=oldToken;expect("SERVICE_UNAVAILABLE",f.app::logout);check(Arrays.equals(before,f.store.values.get(active)),"old instance overwrote new account");
                f.app.close();f.blockedToken="";int opens=f.runtimes.size();try(var restarted=f.create(f.config,f.product)){restarted.restore();check(f.runtimes.size()==opens+1,"old task opened a licensing cache");var current=Json.object(Json.parse(new String(f.store.values.get(active),StandardCharsets.UTF_8)));check((f.tokenPrefix+"2").equals(Json.object(current.get("grant")).get("access_token")),"old retry removed the new login");check(f.store.values.size()==1,"old logout queue retained after confirmation");}
            }
        }
    }
}
