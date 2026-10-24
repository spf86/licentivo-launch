package com.licentivo.launch;

import licentivo.Client;
import licentivo.Json;
import java.io.*;
import java.net.*;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.nio.ByteBuffer;
import java.nio.channels.*;
import java.nio.file.*;
import java.nio.file.attribute.*;
import java.security.*;
import java.time.*;
import java.time.format.DateTimeFormatter;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.locks.ReentrantLock;

/** Launch account lifecycle. Runtime authorization always belongs to Licentivo 0.11.1. */
public final class Launch implements AutoCloseable {
    public static final String VERSION = "0.1.2";
    private static final int LIMIT = 1 << 20;
    public static final class Failure extends Exception {
        public final String code, requestID;
        public final int status;
        public final long retryAfterMillis;
        public final boolean transport;
        public Failure(String code) { this(code, 0, "", 0, false); }
        public Failure(String code, int status, String requestID, long retryAfterMillis, boolean transport) {
            super("Launch: " + safeCode(code)); this.code = safeCode(code); this.status = status;
            this.requestID = requestID != null && requestID.matches("[A-Za-z0-9._:-]{1,128}") ? requestID : "";
            this.retryAfterMillis = Math.max(0, retryAfterMillis); this.transport = transport;
        }
        public boolean unavailable() { return transport || status >= 500 && status <= 599; }
    }
    public record State(String userID, String email, Instant expiresAt, boolean offlineFallback) {}
    public record Response(int status, byte[] body, String retryAfter) {
        @Override public String toString() { return "Launch HTTP response (" + status + ")"; }
    }
    @FunctionalInterface public interface Transport {
        Response request(String method, URI uri, String bearer, byte[] body) throws Exception;
    }
    /** OS-user/host vault: atomic replacement, multiple opaque keys for login and
     * account logout tasks. Never implement as a single slot or plaintext JSON. */
    public interface Store {
        byte[] load(String scope) throws Exception;
        void save(String scope, byte[] secret) throws Exception;
        void delete(String scope) throws Exception;
    }
    /** Default: no bearer or license snapshot is written to disk. */
    public static final class MemoryStore implements Store {
        private final Map<String, byte[]> values = new HashMap<>();
        public synchronized byte[] load(String scope) { byte[] value = values.get(scope); return value == null ? null : value.clone(); }
        public synchronized void save(String scope, byte[] value) { delete(scope); values.put(scope, value.clone()); }
        public synchronized void delete(String scope) { byte[] old = values.remove(scope); if (old != null) Arrays.fill(old, (byte) 0); }
    }
    public interface Runtime extends AutoCloseable {
        boolean canStartOffline();
        void start() throws Exception;
        void runFeature(String feature, Client.FeatureAction action) throws Exception;
        void releaseDevice() throws Exception;
        void close();
    }
    @FunctionalInterface public interface RuntimeFactory { Runtime create(Map<String, Object> settings) throws Exception; }
    @FunctionalInterface public interface Sleeper { void sleep(long milliseconds) throws InterruptedException; }
    public record Config(String merchantOrigin, String launchProductID, String licentivoProductID, String environment) {
        public Config {
            if (!uuid(launchProductID) || !uuid(licentivoProductID) || !Set.of("sandbox", "live").contains(environment)) throw new IllegalArgumentException("invalid Launch configuration");
            merchantOrigin = origin(merchantOrigin, environment.equals("sandbox"));
        }
        public static Config load(Path path) throws Exception {
            var data = readPublic(path);
            if (!Set.of("schema_version", "merchant_origin", "launch_product_id", "licentivo_product_id", "environment").containsAll(data.keySet())) throw new Failure("CONFIGURATION_ERROR");
            if (!Long.valueOf(1).equals(data.get("schema_version"))) throw new Failure("CONFIGURATION_ERROR");
            return new Config(Json.text(data, "merchant_origin"), Json.text(data, "launch_product_id"), Json.text(data, "licentivo_product_id"), Json.text(data, "environment"));
        }
    }

    private final Config config;
    private final Map<String, Object> product;
    private final Path cacheRoot;
    private final Store store;
    private final Transport transport;
    private final RuntimeFactory factory;
    private final Sleeper sleeper;
    private final String scope;
    private Map<String, Object> session;
    private boolean accountCleared;
    private Runtime runtime;

    public Launch(Config config, Map<String, Object> product, Path cacheRoot) throws Exception {
        this(config, product, cacheRoot, new MemoryStore(), new HTTPTransport(), OfficialRuntime::new, Thread::sleep);
    }
    public Launch(Config config, Map<String, Object> product, Path cacheRoot, Store protectedStore) throws Exception {
        this(config, product, cacheRoot, protectedStore, new HTTPTransport(), OfficialRuntime::new, Thread::sleep);
    }
    public Launch(Config config, Map<String, Object> product, Path cacheRoot, Store store, Transport transport, RuntimeFactory factory, Sleeper sleeper) throws Exception {
        this.config = Objects.requireNonNull(config); this.product = new LinkedHashMap<>(product);
        if (!config.licentivoProductID.equals(product.get("product_id")) || !(product.get("trusted_keys") instanceof Map<?, ?> pins) || pins.isEmpty()) throw new Failure("CONFIGURATION_ERROR");
        for (var pin : pins.entrySet()) {
            if (!(pin.getKey() instanceof String id) || id.isEmpty() || id.getBytes(StandardCharsets.UTF_8).length > 128 || !(pin.getValue() instanceof String value)) throw new Failure("CONFIGURATION_ERROR");
            try { if (Client.decode(value).length != 32) throw new Failure("CONFIGURATION_ERROR"); } catch (RuntimeException error) { throw new Failure("CONFIGURATION_ERROR"); }
        }
        for (String name : product.keySet()) {
            if (Set.of("license_key", "cache_path", "device_id", "device_name").contains(name)) {
                if (!"".equals(product.get(name))) throw new Failure("CONFIGURATION_ERROR"); this.product.remove(name);
            } else if (!Set.of("base_url", "product_id", "trusted_keys", "app_version", "allow_http").contains(name)) throw new Failure("CONFIGURATION_ERROR");
        }
        if (product.containsKey("allow_http") && !(product.get("allow_http") instanceof Boolean)) throw new Failure("CONFIGURATION_ERROR");
        if (product.containsKey("base_url")) origin(Json.text(product, "base_url"), config.environment.equals("sandbox") && Boolean.TRUE.equals(product.get("allow_http")));
        if (product.containsKey("app_version") && (!(product.get("app_version") instanceof String version) || version.getBytes(StandardCharsets.UTF_8).length > 80)) throw new Failure("CONFIGURATION_ERROR");
        // This is Launch's public settings format, not an upstream export file.
        this.product.remove("license_key"); this.product.remove("cache_path"); this.product.remove("device_id");
        this.product.put("allow_http", config.environment.equals("sandbox") && Boolean.TRUE.equals(product.get("allow_http")));
        this.cacheRoot = cacheRoot.toAbsolutePath().normalize();
        this.store = Objects.requireNonNull(store); this.transport = Objects.requireNonNull(transport);
        this.factory = Objects.requireNonNull(factory); this.sleeper = Objects.requireNonNull(sleeper);
        this.scope = hash("launch-account-v2\n" + config.merchantOrigin + "\n" + config.launchProductID + "\n" + config.licentivoProductID + "\n" + config.environment);
    }
    public static Launch connect(String merchantOrigin, String productId, String licentivoProductId, Map<String, String> trustedKeys, String environment) throws Exception {
        return new Launch(new Config(merchantOrigin, productId, licentivoProductId, environment),
            Map.of("base_url", "https://www.licentivo.com", "product_id", licentivoProductId, "trusted_keys", trustedKeys),
            Path.of(System.getProperty("user.home"), ".cache", "LicentivoLaunch"));
    }

    public static Map<String, Object> readPublic(Path path) throws Exception {
        if (Files.size(path) > 65536) throw new Failure("CONFIGURATION_ERROR");
        return Json.object(Json.parse(Files.readString(path), 65536));
    }
    private static final class OfficialRuntime implements Runtime {
        private final Client sdk;
        OfficialRuntime(Map<String, Object> config) throws Exception { sdk = new Client(config); }
        public boolean canStartOffline() { var status = sdk.status(); return Boolean.TRUE.equals(status.get("allowed")) && "offline_valid".equals(status.get("state")); }
        public void start() throws Exception { sdk.start(error -> {}); }
        public void runFeature(String feature, Client.FeatureAction action) throws Exception { sdk.runFeature(feature, action); }
        public void releaseDevice() throws Exception { var result=sdk.logout();if(Boolean.TRUE.equals(result.get("revocation_pending")))throw new Failure("DEVICE_REVOCATION_PENDING");if(result.containsKey("storage_error"))throw new Failure("SDK_LOGOUT_STORAGE_FAILED"); }
        public void close() { sdk.close(); }
    }
    public static final class HTTPTransport implements Transport {
        private final HttpClient http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(5)).followRedirects(HttpClient.Redirect.NEVER).build();
        public Response request(String method, URI uri, String bearer, byte[] body) throws Exception {
            var builder = HttpRequest.newBuilder(uri).timeout(Duration.ofSeconds(15)).header("Content-Type", "application/json");
            if (!bearer.isEmpty()) builder.header("Authorization", "Bearer " + bearer);
            var request = builder.method(method, body == null ? HttpRequest.BodyPublishers.noBody() : HttpRequest.BodyPublishers.ofByteArray(body)).build();
            try {
                var pending = http.sendAsync(request, info -> new LimitedBody());
                try {
                    var response = pending.get(15, TimeUnit.SECONDS);
                    return new Response(response.statusCode(), response.body(), response.headers().firstValue("Retry-After").orElse(""));
                } catch (Exception error) { pending.cancel(true); throw error; }
            } catch (InterruptedException error) { Thread.currentThread().interrupt(); throw new Failure("CANCELED"); }
            catch (ExecutionException error) { if (error.getCause() instanceof BodyLimit) throw new Failure("INVALID_RESPONSE"); throw new Failure("SERVICE_UNAVAILABLE", 0, "", 0, true); }
            catch (TimeoutException error) { throw new Failure("SERVICE_UNAVAILABLE", 0, "", 0, true); }
        }
        private static final class BodyLimit extends IOException {}
        private static final class LimitedBody implements HttpResponse.BodySubscriber<byte[]> {
            private final CompletableFuture<byte[]> completed = new CompletableFuture<>();
            private final ByteArrayOutputStream bytes = new ByteArrayOutputStream();
            private Flow.Subscription subscription;
            public CompletionStage<byte[]> getBody() { return completed; }
            public void onSubscribe(Flow.Subscription value) { subscription = value; subscription.request(1); }
            public void onNext(List<ByteBuffer> chunks) {
                for (var chunk : chunks) {
                    if (chunk.remaining() > LIMIT - bytes.size()) { subscription.cancel(); completed.completeExceptionally(new BodyLimit()); return; }
                    byte[] part = new byte[chunk.remaining()]; chunk.get(part); bytes.writeBytes(part);
                }
                subscription.request(1);
            }
            public void onError(Throwable error) { completed.completeExceptionally(error); }
            public void onComplete() { completed.complete(bytes.toByteArray()); }
        }
    }
    private Map<String, Object> call(String method, String path, Map<String, Object> body, String bearer) throws Exception {
        if (Thread.currentThread().isInterrupted()) throw new Failure("CANCELED");
        Response response;
        byte[] raw = body == null ? null : Json.write(body).getBytes(StandardCharsets.UTF_8);
        try { response = transport.request(method, URI.create(config.merchantOrigin + "/api/v1" + path), bearer, raw); }
        catch (InterruptedException error) { Thread.currentThread().interrupt(); throw new Failure("CANCELED"); }
        catch (IOException error) { throw new Failure("SERVICE_UNAVAILABLE", 0, "", 0, true); }
        finally { if (raw != null) Arrays.fill(raw, (byte) 0); }
        if (response.body.length > LIMIT) throw new Failure("INVALID_RESPONSE");
        Map<String, Object> envelope;
        try { envelope = Json.object(Json.parse(new String(response.body, StandardCharsets.UTF_8), LIMIT)); }
        catch (RuntimeException error) {
            if (response.status < 200 || response.status >= 300) throw new Failure("HTTP_ERROR", response.status, "", retryAfter(response.retryAfter), false);
            throw new Failure("INVALID_RESPONSE");
        }
        if (response.status < 200 || response.status >= 300) {
            var detail = envelope.get("error") instanceof Map<?, ?> ? Json.object(envelope.get("error")) : Map.<String, Object>of();
            throw new Failure(text(detail, "code"), response.status, text(envelope, "request_id"), retryAfter(response.retryAfter), false);
        }
        if (!(envelope.get("data") instanceof Map<?, ?>)) throw new Failure("INVALID_RESPONSE");
        return Json.object(envelope.get("data"));
    }
    private Map<String,Object> credential()throws Exception{
 var result=call("POST","/software/products/"+config.launchProductID+"/identity",Map.of("environment",config.environment),token());
 if(!config.launchProductID.equals(result.get("product_id"))||!config.licentivoProductID.equals(result.get("licentivo_product_id"))||!config.environment.equals(result.get("environment"))||!uuid(text(result,"launch_connection_id"))||text(result,"identity_token").length()<32)throw new Failure("IDENTITY_MISMATCH");return result;
 }
    public synchronized State login(String email, char[] password) throws Exception {
        try(var guard=lockAccount()){return loginAccount(email,password);}finally{Arrays.fill(password,'\0');}
    }
    private State loginAccount(String email,char[] password)throws Exception{
        logoutAccount();
        try {
            var grant = call("POST", "/auth/login", Map.of("email", email, "password", new String(password), "kind", "desktop"), "");
            if (text(grant, "access_token").isEmpty() || text(Json.object(grant.get("user")), "id").isEmpty()) throw new Failure("INVALID_RESPONSE");
            session = new LinkedHashMap<>(); accountCleared=false;session.put("scope", scope); session.put("grant", grant);
            requireFresh(); session.put("credential", credential());
            State result = start(false); save(); return result;
        } catch (Exception error) { try { logoutAccount(); } catch (Exception cleanup) { error.addSuppressed(cleanup); } throw error; }
        finally { Arrays.fill(password, '\0'); }
    }
    public synchronized State restore() throws Exception {
        try(var guard=lockAccount()){return restoreAccount();}
    }
    private State restoreAccount()throws Exception{
        close(); if(session==null||!Boolean.TRUE.equals(session.get("logout_pending"))){try{load();}catch(Failure missing){if(!missing.code.equals("NO_SESSION"))throw missing;var tasks=pendingAccounts();if(tasks.isEmpty())throw missing;session=tasks.get(0);}}
        for(var task:pendingAccounts())if(recordToken(task).equals(token())){session=task;break;}
        if(hasIntent(token()))session.put("logout_pending",true);
        if(Boolean.TRUE.equals(session.get("logout_pending"))){logoutAccount();throw new Failure("NO_SESSION");}
        retryOtherAccounts(token());
        requireFresh();
        try {
            var user = Json.object(call("GET", "/me", null, token()).get("user"));
            if (!text(user, "id").equals(userID())) throw new Failure("SESSION_MISMATCH");
            session.put("credential", credential());
            State result = start(false); save(); return result;
        } catch (Failure error) {

            if (denied(error)) { try { clear(); } catch (Exception cleanup) { error.addSuppressed(cleanup); } }
            throw error;
        } catch (Exception error) { close(); throw error;
        }
    }
    private void requireFresh() throws Exception {
        try {
            if (!Instant.parse(text(grant(), "expires_at")).isAfter(Instant.now())) throw new Failure("SESSION_EXPIRED");
        } catch (RuntimeException invalid) { clear(); throw new Failure("SESSION_EXPIRED"); }
        catch (Failure expired) { clear(); throw expired; }
    }
    private State start(boolean offline) throws Exception {
        var settings = runtimeSettings();
        Path path = cacheDirectory(); privateDirectory(path); settings.put("cache_path", path.resolve("lease.json").toString());
        Runtime candidate = factory.create(settings);
        try {

            candidate.start(); runtime = candidate;
        } catch (Exception error) { candidate.close(); throw error; }
        var user = Json.object(grant().get("user"));
        return new State(userID(), text(user, "email"), Instant.parse(text(grant(), "expires_at")), offline);
    }
    public synchronized void runFeature(String feature, Client.FeatureAction action) throws Exception {
        if (runtime == null) throw new Failure("NOT_STARTED"); runtime.runFeature(feature, action);
    }
    public synchronized void releaseDevice()throws Exception{logout();}
    public synchronized void logout()throws Exception{
        if(session!=null)session.put("logout_pending",true);
        if(session!=null)try{rememberIntent(token());}catch(Exception ignored){}
        try(var guard=lockAccount()){if(accountCleared&&session==null)retryOtherAccounts("");else logoutAccount();}finally{if(runtime!=null)try{runtime.releaseDevice();}catch(Exception ignored){}close();}
    }
    private void logoutAccount()throws Exception{
 Exception failure=null;if(session==null)try{load();}catch(Failure missing){if(!missing.code.equals("NO_SESSION")){clear();throw missing;}}
 if(session==null){var tasks=pendingAccounts();if(!tasks.isEmpty())session=tasks.get(0);}
 Exception otherFailure=null;try{retryOtherAccounts(session==null?"":token());}catch(Exception e){otherFailure=e;}
 Exception persistFailure=null;if(session!=null){session.put("logout_pending",true);try{markLogout();}catch(Exception e){persistFailure=e;}}
 try{var active=activeAccount();if(runtime==null&&session!=null&&(active==null||recordToken(active).equals(token()))&&session.get("credential") instanceof Map)runtime=factory.create(runtimeSettings());if(runtime!=null)runtime.releaseDevice();}catch(Exception e){failure=e;}
 close();boolean confirmed=true;
 if(session!=null)try{call("POST","/auth/logout",Map.of(),token());}catch(Exception e){confirmed=e instanceof Failure api&&api.status==401;if(!confirmed&&failure==null)failure=e;}
 if(confirmed){try{if(session!=null)finishAccount(session);clear();}catch(Exception e){if(failure==null)failure=e;else failure.addSuppressed(e);}}else if(persistFailure!=null){if(failure!=null)persistFailure.addSuppressed(failure);failure=persistFailure;}if(failure==null)failure=otherFailure;if(failure!=null)throw failure;
 }
    /** Keeps remembered login; the official SDK handles floating-seat shutdown. */
    @Override public synchronized void close() { if (runtime != null) { runtime.close(); runtime = null; } }
    private void save()throws Exception{
 var clean=Json.object(Json.parse(Json.write(session),LIMIT));if(clean.get("credential") instanceof Map)Json.object(clean.get("credential")).keySet().retainAll(Set.of("product_id","licentivo_product_id","launch_connection_id","environment","expires_at"));if(Boolean.TRUE.equals(clean.get("logout_pending"))){clean.put("version",3);if(clean.containsKey("credential"))clean.put("logout_context",clean.remove("credential"));}byte[] secret=Json.write(clean).getBytes(StandardCharsets.UTF_8);try{store.save(scope,secret);}finally{Arrays.fill(secret,(byte)0);}
 }
 private Map<String,Object> runtimeSettings()throws Exception{
 var identity=Json.object(session.get("credential"));var settings=new LinkedHashMap<>(product);settings.put("license_key","");settings.put("identity_token",text(identity,"identity_token"));settings.put("launch_connection_id",text(identity,"launch_connection_id"));settings.put("environment",config.environment);Path path=cacheDirectory();privateDirectory(path);settings.put("cache_path",path.resolve("lease.json").toString());return settings;
 }
    private void load() throws Exception {
        byte[] raw = store.load(scope); if (raw == null) throw new Failure("NO_SESSION");
        try {
            var loaded = Json.object(Json.parse(new String(raw, StandardCharsets.UTF_8), LIMIT));
            if (!scope.equals(loaded.get("scope"))) throw new Failure("SESSION_MISMATCH");
            var savedGrant = Json.object(loaded.get("grant"));
            if(Boolean.TRUE.equals(loaded.get("logout_pending"))){if(text(savedGrant,"access_token").isEmpty())throw new Failure("SESSION_MISMATCH");if(loaded.get("logout_context") instanceof Map)loaded.put("credential",loaded.remove("logout_context"));session=loaded;return;}
            if (text(savedGrant, "access_token").isEmpty() || !Boolean.TRUE.equals(loaded.get("logout_pending")) && (text(Json.object(savedGrant.get("user")), "id").isEmpty() || (!config.launchProductID.equals(Json.object(loaded.get("credential")).get("product_id")) || !text(Json.object(loaded.get("credential")),"identity_token").isEmpty() || !uuid(text(Json.object(loaded.get("credential")),"launch_connection_id"))))) throw new Failure("SESSION_MISMATCH");
            session = loaded;
        } catch (RuntimeException error) { throw new Failure("SESSION_MISMATCH"); }
        finally { Arrays.fill(raw, (byte) 0); }
    }
    private void clear()throws Exception{close();var saved=session;session=null;accountCleared=true;if(saved!=null)deleteOwned(saved);}
    private static final ConcurrentHashMap<String,ReentrantLock> ACCOUNT_LOCKS=new ConcurrentHashMap<>();
    private static final class AccountGuard implements AutoCloseable{
        final ReentrantLock local;final FileChannel channel;final FileLock lock;
        AccountGuard(ReentrantLock local,FileChannel channel,FileLock lock){this.local=local;this.channel=channel;this.lock=lock;}
        public void close()throws IOException{try{lock.release();}finally{try{channel.close();}finally{local.unlock();}}}
    }
    private AccountGuard lockAccount()throws Exception{
        var local=ACCOUNT_LOCKS.computeIfAbsent(scope,key->new ReentrantLock());if(!local.tryLock(5,TimeUnit.SECONDS))throw new Failure("ACCOUNT_LOCK_BUSY");FileChannel channel=null;
        try{Path root=Path.of(System.getProperty("user.home"),".cache","LicentivoLaunch","account-locks","java");privateDirectory(root);channel=FileChannel.open(root.resolve(hash(scope)+".lock"),StandardOpenOption.CREATE,StandardOpenOption.WRITE);long deadline=System.nanoTime()+TimeUnit.SECONDS.toNanos(5);while(true){FileLock lock;try{lock=channel.tryLock();}catch(OverlappingFileLockException busy){lock=null;}if(lock!=null)return new AccountGuard(local,channel,lock);if(System.nanoTime()>=deadline)throw new Failure("ACCOUNT_LOCK_BUSY");Thread.sleep(20);}}
        catch(Exception e){if(channel!=null)channel.close();local.unlock();throw e;}
    }
    private String queueKey(){return scope+"\naccount-logout-queue-v1";}
    private Path intentPath(String token)throws Exception{return Path.of(System.getProperty("user.home"),".cache","LicentivoLaunch","account-intents","java",hash(scope)+"."+hash(token)+".signedout");}
    private void rememberIntent(String token)throws Exception{var path=intentPath(token);privateDirectory(path.getParent());try(var file=FileChannel.open(path,StandardOpenOption.CREATE,StandardOpenOption.WRITE)){file.write(ByteBuffer.wrap(new byte[]{1}));file.force(true);}}
    private boolean hasIntent(String token)throws Exception{try{Files.readAttributes(intentPath(token),BasicFileAttributes.class);return true;}catch(NoSuchFileException missing){return false;}}
    private void clearIntent(String token)throws Exception{Files.deleteIfExists(intentPath(token));}
    private static String recordToken(Map<String,Object> record){return text(Json.object(record.get("grant")),"access_token");}
    private Map<String,Object> publicRecord(Map<String,Object> record){var clean=Json.object(Json.parse(Json.write(record),LIMIT));if(clean.get("credential") instanceof Map)Json.object(clean.get("credential")).keySet().retainAll(Set.of("product_id","licentivo_product_id","launch_connection_id","environment","expires_at"));if(Boolean.TRUE.equals(clean.get("logout_pending"))){clean.put("version",3);if(clean.containsKey("credential"))clean.put("logout_context",clean.remove("credential"));}return clean;}
    private Map<String,Object> activeAccount()throws Exception{byte[] raw=store.load(scope);if(raw==null)return null;try{var value=Json.object(Json.parse(new String(raw,StandardCharsets.UTF_8),LIMIT));if(!scope.equals(value.get("scope")))throw new Failure("SESSION_MISMATCH");return value;}finally{Arrays.fill(raw,(byte)0);}}
    private boolean owns(Map<String,Object> record)throws Exception{var active=activeAccount();return active!=null&&recordToken(active).equals(recordToken(record));}
    private void deleteOwned(Map<String,Object> record)throws Exception{if(owns(record))store.delete(scope);}
    private List<Map<String,Object>> pendingAccounts()throws Exception{byte[] raw=store.load(queueKey());if(raw==null)return new ArrayList<>();try{var queue=Json.object(Json.parse(new String(raw,StandardCharsets.UTF_8),LIMIT));if(!(queue.get("version") instanceof Number version)||version.intValue()!=1||!(queue.get("records") instanceof List<?> records)||records.size()>64)throw new Failure("SESSION_MISMATCH");var result=new ArrayList<Map<String,Object>>();for(Object item:records){var record=Json.object(item);if(!scope.equals(record.get("scope"))||!Boolean.TRUE.equals(record.get("logout_pending"))||recordToken(record).isEmpty())throw new Failure("SESSION_MISMATCH");if(record.get("logout_context") instanceof Map)record.put("credential",record.remove("logout_context"));result.add(record);}return result;}finally{Arrays.fill(raw,(byte)0);}}
    private void updatePending(Map<String,Object> record,boolean remove)throws Exception{var records=pendingAccounts();records.removeIf(value->recordToken(value).equals(recordToken(record)));if(!remove){var value=publicRecord(record);value.put("version",3);value.put("logout_pending",true);records.add(value);}if(records.size()>64)throw new Failure("ACCOUNT_LOGOUT_QUEUE_FULL");if(records.isEmpty()){store.delete(queueKey());return;}var persisted=new ArrayList<Map<String,Object>>();for(var value:records)persisted.add(publicRecord(value));byte[] raw=Json.write(Map.of("version",1,"records",persisted)).getBytes(StandardCharsets.UTF_8);try{if(raw.length>60000)throw new Failure("ACCOUNT_LOGOUT_QUEUE_FULL");store.save(queueKey(),raw);}finally{Arrays.fill(raw,(byte)0);}}
    private void markLogout()throws Exception{try{rememberIntent(token());}catch(Exception ignored){}Exception queueFailure=null;try{updatePending(session,false);}catch(Exception e){queueFailure=e;}if(owns(session)){try{save();return;}catch(Exception e){try{deleteOwned(session);}catch(Exception ignored){}}}if(queueFailure!=null)throw new Failure("ACCOUNT_LOGOUT_STORAGE_FAILED");}
    private void finishAccount(Map<String,Object> record)throws Exception{deleteOwned(record);updatePending(record,true);clearIntent(recordToken(record));}
    private void retryOtherAccounts(String skip)throws Exception{for(var record:pendingAccounts()){if(recordToken(record).equals(skip))continue;try{call("POST","/auth/logout",Map.of(),recordToken(record));}catch(Failure error){if(error.status!=401)throw error;}finishAccount(record);}}
    private Path cacheDirectory() throws Exception { return cacheRoot.resolve(hash(scope + "\n" + userID())); }
    private Map<String, Object> grant() { return Json.object(session.get("grant")); }
    private String token() { return Json.text(grant(), "access_token"); }
    private String userID() { return Json.text(Json.object(grant().get("user")), "id"); }
    private static boolean denied(Failure error) { return !error.code.equals("ENTITLEMENT_PENDING") && (error.status == 401 || error.status == 403 || error.status == 404 || error.code.equals("SESSION_MISMATCH")); }
    private static String text(Map<String, Object> value, String name) { return value.get(name) instanceof String ? (String)value.get(name) : ""; }
    private static String safeCode(String code) { return code != null && code.matches("[A-Z][A-Z0-9_]{0,63}") ? code : "HTTP_ERROR"; }
    private static boolean uuid(String value) { return value != null && value.matches("[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}"); }
    private static String hash(String value) throws Exception { return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(value.getBytes(StandardCharsets.UTF_8))); }
    private static String origin(String origin, boolean local) {
        URI uri;
        try { uri = URI.create(origin); } catch (RuntimeException invalid) { throw new IllegalArgumentException("HTTPS merchant origin required"); }
        String host = uri.getHost();
        if (host == null || uri.getUserInfo() != null || uri.getQuery() != null || uri.getFragment() != null || !(uri.getPath().isEmpty() || uri.getPath().equals("/")) || !("https".equals(uri.getScheme()) || local && "http".equals(uri.getScheme()) && Set.of("localhost", "127.0.0.1", "[::1]", "::1").contains(host))) throw new IllegalArgumentException("HTTPS merchant origin required");
        return origin.replaceAll("/$", "");
    }
    private static long retryAfter(String raw) {
        try { return Math.multiplyExact(Math.max(0, Long.parseLong(raw)), 1000); }
        catch (Exception ignored) { try { return Math.max(0, Duration.between(Instant.now(), ZonedDateTime.parse(raw, DateTimeFormatter.RFC_1123_DATE_TIME).toInstant()).toMillis()); } catch (Exception invalid) { return 0; } }
    }
    static void privateDirectory(Path path) throws Exception {
        Files.createDirectories(path);
        if (Files.getFileAttributeView(path, PosixFileAttributeView.class) != null) { Files.setPosixFilePermissions(path, PosixFilePermissions.fromString("rwx------")); return; }
        var acl = Files.getFileAttributeView(path, AclFileAttributeView.class);
        if (acl == null) throw new Failure("PRIVATE_CACHE_UNAVAILABLE");
        var owner = Files.getOwner(path);
        var entry = AclEntry.newBuilder().setType(AclEntryType.ALLOW).setPrincipal(owner).setPermissions(EnumSet.allOf(AclEntryPermission.class)).setFlags(AclEntryFlag.FILE_INHERIT, AclEntryFlag.DIRECTORY_INHERIT).build();
        acl.setAcl(List.of(entry));
    }
}
