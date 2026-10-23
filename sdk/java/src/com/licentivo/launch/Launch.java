package com.licentivo.launch;

import licentivo.Client;
import licentivo.Json;
import java.io.*;
import java.net.*;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.nio.ByteBuffer;
import java.nio.file.*;
import java.nio.file.attribute.*;
import java.security.*;
import java.time.*;
import java.time.format.DateTimeFormatter;
import java.util.*;
import java.util.concurrent.*;

/** Launch account lifecycle. Runtime authorization always belongs to Licentivo 0.11.0. */
public final class Launch implements AutoCloseable {
    public static final String VERSION = "0.1.1";
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
    /** Must provide OS-user confidentiality/integrity. Never implement as plain JSON on disk. */
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
        this.scope = hash("launch-v1\n" + config.merchantOrigin + "\n" + config.launchProductID + "\n" + config.licentivoProductID + "\n" + config.environment);
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
        public void releaseDevice() throws Exception { sdk.close(); sdk.deactivate(); }
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
    private Map<String, Object> credential() throws Exception {
        for (int attempt = 0; ; attempt++) {
            try {
                var result = call("POST", "/software/products/" + config.launchProductID + "/credential", Map.of("environment", config.environment), token());
                if (!config.launchProductID.equals(result.get("product_id")) || text(result, "license_key").isEmpty()) throw new Failure("INVALID_RESPONSE");
                return result;
            } catch (Failure error) {
                if (!error.code.equals("ENTITLEMENT_PENDING") || attempt >= 4) throw error;
                long wait = Math.max(1000L << attempt, error.retryAfterMillis);
                if (wait > 10000) throw error;
                try { sleeper.sleep(wait); } catch (InterruptedException stopped) { Thread.currentThread().interrupt(); throw new Failure("CANCELED"); }
            }
        }
    }
    public synchronized State login(String email, char[] password) throws Exception {
        logout();
        try {
            var grant = call("POST", "/auth/login", Map.of("email", email, "password", new String(password), "kind", "desktop"), "");
            if (text(grant, "access_token").isEmpty() || text(Json.object(grant.get("user")), "id").isEmpty()) throw new Failure("INVALID_RESPONSE");
            session = new LinkedHashMap<>(); session.put("scope", scope); session.put("grant", grant);
            requireFresh(); session.put("credential", credential());
            State result = start(false); save(); return result;
        } catch (Exception error) { try { logout(); } catch (Exception cleanup) { error.addSuppressed(cleanup); } throw error; }
        finally { Arrays.fill(password, '\0'); }
    }
    public synchronized State restore() throws Exception {
        close(); load(); requireFresh();
        try {
            var user = Json.object(call("GET", "/me", null, token()).get("user"));
            if (!text(user, "id").equals(userID())) throw new Failure("SESSION_MISMATCH");
            session.put("credential", credential());
            State result = start(false); save(); return result;
        } catch (Failure error) {
            if (error.unavailable()) { close(); return start(true); }
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
        var settings = new LinkedHashMap<>(product); settings.put("license_key", Json.text(Json.object(session.get("credential")), "license_key"));
        Path path = cacheDirectory(); privateDirectory(path); settings.put("cache_path", path.resolve("lease.json").toString());
        Runtime candidate = factory.create(settings);
        try {
            if (offline && !candidate.canStartOffline()) throw new Failure("OFFLINE_UNAVAILABLE");
            candidate.start(); runtime = candidate;
        } catch (Exception error) { candidate.close(); throw error; }
        var user = Json.object(grant().get("user"));
        return new State(userID(), text(user, "email"), Instant.parse(text(grant(), "expires_at")), offline);
    }
    public synchronized void runFeature(String feature, Client.FeatureAction action) throws Exception {
        if (runtime == null) throw new Failure("NOT_STARTED"); runtime.runFeature(feature, action);
    }
    public synchronized void releaseDevice() throws Exception {
        if (runtime == null) throw new Failure("NOT_STARTED");
        Exception failure = null;
        try { runtime.releaseDevice(); } catch (Exception error) { failure = error; }
        try { logout(); } catch (Exception error) { if (failure == null) failure = error; else failure.addSuppressed(error); }
        if (failure != null) throw failure;
    }
    public synchronized void logout() throws Exception {
        Exception failure = null;
        try {
            if (session == null) { try { load(); } catch (Failure missing) { if (!missing.code.equals("NO_SESSION")) throw missing; } }
            if (session != null) {
                try { call("POST", "/auth/logout", Map.of(), token()); }
                catch (Failure error) { if (error.status != 401) throw error; }
            }
        } catch (Exception error) { failure = error; }
        try { clear(); } catch (Exception error) { if (failure == null) failure = error; else failure.addSuppressed(error); }
        if (failure != null) throw failure;
    }
    /** Keeps remembered login; the official SDK handles floating-seat shutdown. */
    @Override public synchronized void close() { if (runtime != null) { runtime.close(); runtime = null; } }
    private void save() throws Exception {
        byte[] secret = Json.write(session).getBytes(StandardCharsets.UTF_8);
        try { store.save(scope, secret); } finally { Arrays.fill(secret, (byte) 0); }
    }
    private void load() throws Exception {
        byte[] raw = store.load(scope); if (raw == null) throw new Failure("NO_SESSION");
        try {
            var loaded = Json.object(Json.parse(new String(raw, StandardCharsets.UTF_8), LIMIT));
            if (!scope.equals(loaded.get("scope"))) throw new Failure("SESSION_MISMATCH");
            var savedGrant = Json.object(loaded.get("grant"));
            if (text(savedGrant, "access_token").isEmpty() || text(Json.object(savedGrant.get("user")), "id").isEmpty() || !config.launchProductID.equals(Json.object(loaded.get("credential")).get("product_id"))) throw new Failure("SESSION_MISMATCH");
            session = loaded;
        } catch (RuntimeException error) { throw new Failure("SESSION_MISMATCH"); }
        finally { Arrays.fill(raw, (byte) 0); }
    }
    private void clear() throws Exception {
        close(); Exception failure = null;
        try {
            if (session != null) {
                Path path = cacheDirectory();
                if (!path.startsWith(cacheRoot) || path.equals(cacheRoot)) throw new Failure("CACHE_SCOPE_ERROR");
                if (Files.exists(path)) try (var paths = Files.walk(path)) { for (Path item : paths.sorted(Comparator.reverseOrder()).toList()) Files.deleteIfExists(item); }
            }
        } catch (Exception error) { failure = error; }
        session = null;
        try { store.delete(scope); } catch (Exception error) { if (failure == null) failure = error; else failure.addSuppressed(error); }
        if (failure != null) throw failure;
    }
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
