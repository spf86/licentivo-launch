use super::*;
use std::{cell::RefCell, rc::Rc};
const PRODUCT: &str = "11111111-1111-4111-8111-111111111111";
const REMOTE: &str = "22222222-2222-4222-8222-222222222222";
const KEY: &str = "33333333-3333-4333-8333-333333333333";
#[derive(Default)]
struct Shared {
    mode: String,
    user: String,
    offline: bool,
    credentials: usize,
    logouts: usize,
    logout_status: u16,
    opens: usize,
    releases: usize,
    checks: usize,
    paths: Vec<String>,
    waits: Vec<Duration>,
    values: HashMap<String, Vec<u8>>,
    fail_saves: bool,
    fail_deletes: bool,
    logins: usize,
    blocked_token: String,
    token_prefix: String,
    me_data: Option<Value>,
}
struct TestStore(Rc<RefCell<Shared>>);
impl Store for TestStore {
    fn load(&mut self, key: &str) -> Result<Option<Vec<u8>>, Failure> {
        Ok(self.0.borrow().values.get(key).cloned())
    }
    fn save(&mut self, key: &str, value: &[u8]) -> Result<(), Failure> {
        if self.0.borrow().fail_saves { return Err(Failure::new("TEST_STORAGE_FAILED")); }
        self.0.borrow_mut().values.insert(key.into(), value.into());
        Ok(())
    }
    fn delete(&mut self, key: &str) -> Result<(), Failure> {
        if self.0.borrow().fail_deletes { return Err(Failure::new("TEST_STORAGE_FAILED")); }
        self.0.borrow_mut().values.remove(key);
        Ok(())
    }
}
struct TestTransport(Rc<RefCell<Shared>>);
impl Transport for TestTransport {
    fn request(&mut self, _: &str, url: &str, bearer: &str, _: &[u8]) -> Result<Response, Failure> {
        let mut s = self.0.borrow_mut();
        if s.mode == "offline" {
            return Err(Failure::service());
        }
        if url.ends_with("/logout") && !s.blocked_token.is_empty() && bearer==s.blocked_token { return Err(Failure::service()); }
        let data = if url.ends_with("/login") {
            s.logins+=1;
            json!({"access_token":format!("{}{}",s.token_prefix,s.logins),"expires_at":"2099-01-01T00:00:00Z","user":{"id":s.user,"email":"a@example.invalid"}})
        } else if url.ends_with("/logout") {
            s.logouts += 1;
            json!({"signed_out":true})
        } else if url.ends_with("/me") {
            if ["SDK_HTTP_DISABLED", "SDK_HTTPS_REQUIRED"].contains(&s.mode.as_str()) {
                return Ok(Response { status: 403, body: json!({"error":{"code":s.mode}}).to_string().into_bytes(), retry_after: String::new() });
            }
            if s.mode == "unauthorized" {
                return Ok(Response {
                    status: 401,
                    body: json!({"error":{"code":"UNAUTHENTICATED"}})
                        .to_string()
                        .into_bytes(),
                    retry_after: String::new(),
                });
            }
            s.me_data.clone().unwrap_or_else(|| json!({"user":{"id":s.user}}))
        } else {
            s.credentials += 1;
            if !s.mode.is_empty() {
                return Ok(Response {
                    status: 403,
                    body: json!({"error":{"code":s.mode}}).to_string().into_bytes(),
                    retry_after: "3".into(),
                });
            }
            json!({"product_id":PRODUCT,"licentivo_product_id":REMOTE,"environment":"sandbox","launch_connection_id":KEY,"identity_token":"I".repeat(64),"expires_at":"2099-01-01T00:00:00Z"})
        };
        Ok(Response {
            status: if url.ends_with("/logout") && s.logout_status != 0 { s.logout_status } else { 200 },
            body: json!({"data":data}).to_string().into_bytes(),
            retry_after: String::new(),
        })
    }
}
struct TestFactory(Rc<RefCell<Shared>>);
struct TestRuntime(Rc<RefCell<Shared>>);
impl RuntimeFactory for TestFactory {
    fn open(&mut self, settings: &Value, cached: bool) -> Result<Box<dyn Runtime>, Failure> {
        let mut s = self.0.borrow_mut();
        if cached && !s.offline {
            return Err(Failure::new("OFFLINE_UNAVAILABLE"));
        }
        let path = text(settings, "cache_path");
        s.opens += 1;
        s.paths.push(path.into());
        fs::write(path, "SDK-owned grant")?;
        Ok(Box::new(TestRuntime(self.0.clone())))
    }
}
impl Runtime for TestRuntime {
    fn run_feature(
        &mut self,
        feature: &str,
        action: &mut dyn FnMut() -> Result<(), Failure>,
    ) -> Result<(), Failure> {
        self.0.borrow_mut().checks += 1;
        if feature != "export" {
            return Err(Failure::new("FEATURE_DENIED"));
        }
        action()
    }
    fn release_device(&mut self) -> Result<(), Failure> {
        self.0.borrow_mut().releases += 1;
        Ok(())
    }
    fn close(&mut self) {}
}
struct TestSleeper(Rc<RefCell<Shared>>);
impl Sleeper for TestSleeper {
    fn sleep(&mut self, wait: Duration) -> Result<(), Failure> {
        self.0.borrow_mut().waits.push(wait);
        Ok(())
    }
}
fn temp(name: &str) -> PathBuf {
    std::env::temp_dir().join(format!(
        "launch-rust-{}-{}-{}",
        name,
        std::process::id(),
        OffsetDateTime::now_utc().unix_timestamp_nanos()
    ))
}
fn product() -> Value {
    json!({"product_id":REMOTE,"trusted_keys":{KEY:URL_SAFE_NO_PAD.encode([0u8;32])}})
}
fn create(shared: Rc<RefCell<Shared>>, root: PathBuf, environment: &str) -> Application {
    Application::with_components(
        Config::new("https://merchant.example", PRODUCT, REMOTE, environment).unwrap(),
        product(),
        root,
        Box::new(TestStore(shared.clone())),
        Box::new(TestTransport(shared.clone())),
        Box::new(TestFactory(shared.clone())),
        Box::new(TestSleeper(shared)),
    )
    .unwrap()
}
fn fixture(name: &str) -> (Application, Rc<RefCell<Shared>>, PathBuf) {
    let shared = Rc::new(RefCell::new(Shared {
        user: "account-a".into(),
        offline: true,
        token_prefix: format!("private-bearer-{}-",OffsetDateTime::now_utc().unix_timestamp_nanos()),
        ..Shared::default()
    }));
    let root = temp(name);
    (
        create(shared.clone(), root.clone(), "sandbox"),
        shared,
        root,
    )
}
#[test]
fn malformed_current_user_preserves_account_for_logout() {
    for data in [json!({}),json!({"user":null}),json!({"user":{}}),json!({"user":{"id":""}}),
                 json!({"user":{"id":null}}),json!({"user":{"id":42}}),json!({"user":[]}),json!({"user":"invalid"})] {
        let (mut app, shared, root) = fixture("malformed-user");
        app.login("a", "b").unwrap();
        let before = shared.borrow().values.clone();
        let opens = shared.borrow().opens;
        let credentials = shared.borrow().credentials;
        shared.borrow_mut().me_data = Some(data);
        assert_eq!(app.restore().unwrap_err().code, "INVALID_RESPONSE");
        assert_eq!(shared.borrow().values, before);
        assert_eq!(shared.borrow().opens, opens);
        assert_eq!(shared.borrow().credentials, credentials);
        assert!(app.runtime.is_none());
        assert_eq!(app.run_feature("export", || -> Result<(),Failure> { panic!("failed restore executed work") }).unwrap_err().code,"NOT_STARTED");
        let logouts = shared.borrow().logouts;
        app.logout().unwrap();
        assert_eq!(shared.borrow().logouts, logouts + 1);
        assert!(shared.borrow().values.is_empty());
        drop(app);
        fs::remove_dir_all(root).unwrap();
    }
}
#[test]
fn different_current_user_still_clears_account() {
    let (mut app, shared, root) = fixture("different-current-user");
    app.login("a", "b").unwrap();
    shared.borrow_mut().me_data = Some(json!({"user":{"id":"account-b"}}));
    assert_eq!(app.restore().unwrap_err().code, "SESSION_MISMATCH");
    assert!(shared.borrow().values.is_empty());
    assert!(app.runtime.is_none());
    drop(app);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn transport_policy_denial_preserves_account_for_logout() {
    for code in ["SDK_HTTP_DISABLED", "SDK_HTTPS_REQUIRED"] {
        let (mut app, shared, root) = fixture(code);
        app.login("a", "b").unwrap();
        let before = shared.borrow().values.clone();
        let opens = shared.borrow().opens;
        shared.borrow_mut().mode = code.into();
        assert_eq!(app.restore().unwrap_err().code, code);
        assert_eq!(shared.borrow().values, before);
        assert_eq!(shared.borrow().opens, opens);
        assert!(app.runtime.is_none());
        let logouts = shared.borrow().logouts;
        app.logout().unwrap();
        assert_eq!(shared.borrow().logouts, logouts + 1);
        assert!(shared.borrow().values.is_empty());
        drop(app);
        fs::remove_dir_all(root).unwrap();
    }
}
#[test]
fn accepted_logout_keeps_its_task_across_restart() {
    let (mut app, shared, root) = fixture("accepted-logout");
    app.login("a", "b").unwrap();
    shared.borrow_mut().logout_status = 202;
    assert_eq!(app.logout().unwrap_err().status, 202);
    assert!(app.runtime.is_none());
    assert!(!shared.borrow().values.is_empty());
    let credentials = shared.borrow().credentials;
    drop(app);
    let mut restarted = create(shared.clone(), root.clone(), "sandbox");
    assert_eq!(restarted.restore().unwrap_err().status, 202);
    shared.borrow_mut().logout_status = 200;
    assert_eq!(restarted.restore().unwrap_err().code, "NO_SESSION");
    assert_eq!(shared.borrow().credentials, credentials);
    assert!(shared.borrow().values.is_empty());
    drop(restarted);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn failed_saves_and_offline_logout_never_restore_the_old_account() {
    for delete_fails in [false,true] {
        let(mut app,shared,root)=fixture("failed-store");app.login("a","b").unwrap();
        {let mut s=shared.borrow_mut();s.fail_saves=true;s.fail_deletes=delete_fails;s.mode="offline".into();}
        assert_eq!(app.logout().unwrap_err().code,"ACCOUNT_LOGOUT_STORAGE_FAILED");
        assert_eq!(app.run_feature("export",||{panic!("signed-out callback");#[allow(unreachable_code)]Ok(())}).unwrap_err().code,"NOT_STARTED");
        if !delete_fails {assert!(shared.borrow().values.is_empty());shared.borrow_mut().fail_saves=false;let mut restarted=create(shared.clone(),root.clone(),"sandbox");assert_eq!(restarted.restore().unwrap_err().code,"NO_SESSION");}
        else{let mut restarted=create(shared.clone(),root.clone(),"sandbox");assert_eq!(restarted.restore().unwrap_err().code,"ACCOUNT_LOGOUT_STORAGE_FAILED");}
        drop(app);fs::remove_dir_all(root).unwrap();
    }
}
#[test]
fn stale_instance_preserves_new_login_and_retries_its_own_account() {
    let(mut old,shared,root)=fixture("stale-instance");old.login("a","b").unwrap();let mut newer=create(shared.clone(),root.clone(),"sandbox");newer.login("a","b").unwrap();
    let active=newer.scope.clone();let before=shared.borrow().values[&active].clone();let old_token=old.token().unwrap().to_string();shared.borrow_mut().blocked_token=old_token;assert!(old.logout().is_err());assert_eq!(shared.borrow().values[&active],before);
    old.close();shared.borrow_mut().blocked_token.clear();let count=shared.borrow().opens;let mut restarted=create(shared.clone(),root.clone(),"sandbox");restarted.restore().unwrap();assert_eq!(shared.borrow().opens,count+1);assert_eq!(shared.borrow().values[&active],before);assert_eq!(shared.borrow().values.len(),1);
    drop((old,newer,restarted));fs::remove_dir_all(root).unwrap();
}
#[test]
fn completed_old_logout_never_adopts_a_newer_login(){let(mut old,shared,root)=fixture("completed-logout");old.login("a","b").unwrap();old.logout().unwrap();let mut newer=create(shared.clone(),root.clone(),"sandbox");newer.login("a","b").unwrap();let token=newer.token().unwrap().to_string();old.logout().unwrap();assert!(!shared.borrow().values.is_empty());newer.restore().unwrap();assert_eq!(newer.token().unwrap(),token);drop((old,newer));fs::remove_dir_all(root).unwrap();}
#[test]
fn restart_requires_identity_and_feature_authority_is_sdk_owned() {
 let (mut app,shared,root)=fixture("identity");app.login("a@example.invalid","password").unwrap();
 let saved=shared.borrow().values.values().next().unwrap().clone();let saved=String::from_utf8(saved).unwrap();assert!(!saved.contains(&"I".repeat(64)));assert!(!saved.contains("license_key"));
 app.close();assert_eq!(shared.borrow().logouts,0);shared.borrow_mut().mode="offline".into();
 assert_eq!(app.restore().unwrap_err().code,"SERVICE_UNAVAILABLE");assert_eq!(shared.borrow().opens,1);
 shared.borrow_mut().mode.clear();assert!(!app.restore().unwrap().offline_fallback);
 let mut ran=false;assert_eq!(app.run_feature("other",||{ran=true;Ok(())}).unwrap_err().code,"FEATURE_DENIED");assert!(!ran);
 assert_eq!(shared.borrow().paths[0],shared.borrow().paths[1]);drop(app);fs::remove_dir_all(root).unwrap();
}
#[test]
fn denials_clear_account_but_preserve_sdk_files() {
    for code in [
        "unauthorized",
        "ENTITLEMENT_EXPIRED",
        "ENTITLEMENT_SUSPENDED",
        "ENTITLEMENT_NOT_PURCHASED",
    ] {
        let (mut app, shared, root) = fixture(code);
        app.login("a@example.invalid", "password").unwrap();
        app.close();
        shared.borrow_mut().mode = code.into();
        assert!(app.restore().is_err());
        assert_eq!(shared.borrow().opens, 1);
        assert!(shared.borrow().values.is_empty());
        assert!(Path::new(&shared.borrow().paths[0]).exists());
        drop(app);
        fs::remove_dir_all(root).unwrap();
    }
}
#[test]
fn pending_is_bounded_and_keeps_login() {
    let (mut app, shared, root) = fixture("pending");
    app.login("a@example.invalid", "password").unwrap();
    app.close();
    shared.borrow_mut().mode = "ENTITLEMENT_PENDING".into();
    assert_eq!(app.restore().unwrap_err().code, "ENTITLEMENT_PENDING");
    assert_eq!(shared.borrow().credentials, 2);
    assert!(shared.borrow().waits.is_empty());
    assert!(!shared.borrow().values.is_empty());
    assert_eq!(shared.borrow().opens, 1);
    drop(app);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn expiry_and_identity_reject_restore() {
    for expiry in [false, true] {
        let (mut app, shared, root) = fixture("identity");
        app.login("a@example.invalid", "password").unwrap();
        app.close();
        if expiry {
            let mut s = shared.borrow_mut();
            let raw = s.values.values_mut().next().unwrap();
            let mut data: Value = serde_json::from_slice(raw).unwrap();
            data["grant"]["expires_at"] = json!("2000-01-01T00:00:00Z");
            *raw = data.to_string().into_bytes();
        } else {
            shared.borrow_mut().user = "account-b".into();
        }
        assert_eq!(
            app.restore().unwrap_err().code,
            if expiry {
                "SESSION_EXPIRED"
            } else {
                "SESSION_MISMATCH"
            }
        );
        assert_eq!(shared.borrow().opens, 1);
        assert!(shared.borrow().values.is_empty());
        drop(app);
        fs::remove_dir_all(root).unwrap();
    }
}
#[test]
fn explicit_release_scope_and_offline_logout() {
    let (mut app, shared, root) = fixture("actions");
    app.login("a@example.invalid", "password").unwrap();
    app.release_device().unwrap();
    assert_eq!(shared.borrow().releases, 1);
    shared.borrow_mut().user = "account-b".into();
    app.login("a@example.invalid", "password").unwrap();
    assert_ne!(shared.borrow().paths[0], shared.borrow().paths[1]);
    let mut live = create(shared.clone(), root.clone(), "live");
    assert_eq!(live.restore().unwrap_err().code, "NO_SESSION");
    shared.borrow_mut().mode = "offline".into();
    assert_eq!(app.logout().unwrap_err().code, "SERVICE_UNAVAILABLE");
    assert!(!shared.borrow().values.is_empty());
    assert!(Path::new(&shared.borrow().paths[1]).exists());
    drop(app);
    let mut restarted=create(shared.clone(),root.clone(),"sandbox");
    let before=shared.borrow().credentials;
    assert_eq!(restarted.restore().unwrap_err().code,"SERVICE_UNAVAILABLE");
    assert_eq!(shared.borrow().credentials,before);
    shared.borrow_mut().mode.clear();
    assert_eq!(restarted.restore().unwrap_err().code,"NO_SESSION");
    assert!(shared.borrow().values.is_empty());
    drop(restarted);
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn secure_store_restart_scope_and_tamper() {
    let root = temp("store");
    let key = [42u8; 32];
    let mut store = EncryptedFileStore::new(root.clone(), key).unwrap();
    let secret = b"private-bearer private-license";
    store.save("scope-a", secret).unwrap();
    let path = fs::read_dir(&root).unwrap().next().unwrap().unwrap().path();
    let ciphertext = fs::read(&path).unwrap();
    assert!(!String::from_utf8_lossy(&ciphertext).contains("private-bearer"));
    let mut restored = EncryptedFileStore::new(root.clone(), key).unwrap();
    assert_eq!(restored.load("scope-a").unwrap().unwrap(), secret);
    fs::write(
        root.join(format!("{}.encrypted", hash("scope-b"))),
        &ciphertext,
    )
    .unwrap();
    assert_eq!(
        restored.load("scope-b").unwrap_err().code,
        "SESSION_STORE_CORRUPT"
    );
    store.save("scope-a", b"updated").unwrap();
    assert_eq!(store.load("scope-a").unwrap().unwrap(), b"updated");
    let mut damaged = fs::read(&path).unwrap();
    let last = damaged.len() - 1;
    damaged[last] ^= 1;
    fs::write(&path, damaged).unwrap();
    assert_eq!(
        store.load("scope-a").unwrap_err().code,
        "SESSION_STORE_CORRUPT"
    );
    fs::remove_dir_all(root).unwrap();
}
#[test]
fn strict_config_and_safe_errors() {
    for origin in [
        "http://merchant.example",
        "https://user:private@merchant.example",
        "https://merchant.example/path",
        "https://merchant.example?token=private",
    ] {
        assert!(Config::new(origin, PRODUCT, REMOTE, "live").is_err());
    }
    assert_eq!(
        Failure::new("lv_lic_private\npassword").to_string(),
        "Launch: HTTP_ERROR"
    );
    let mut bad = product();
    bad["vendor_api_key"] = json!("private");
    let config = Config::new("https://merchant.example", PRODUCT, REMOTE, "live").unwrap();
    let result = Application::with_components(
        config,
        bad,
        temp("config"),
        Box::<MemoryStore>::default(),
        Box::new(CurlTransport::new().unwrap()),
        Box::new(OfficialRuntimeFactory),
        Box::new(ThreadSleeper),
    );
    assert!(result.is_err());
}
#[test]
fn official_sdk_signed_vectors() {
    let fixture: Value = serde_json::from_str(include_str!("../tests/lease-vectors.json")).unwrap();
    for vector in fixture["vectors"].as_array().unwrap() {
        let valid = licentivo::verify_lease(
            &vector["envelope"],
            &fixture["trusted_keys"],
            text(vector, "product_id"),
            text(vector, "device_id"),
            vector["now"].as_i64().unwrap(),
        )
        .unwrap();
        assert_eq!(
            valid,
            vector["valid"].as_bool().unwrap(),
            "{}",
            text(vector, "name")
        );
    }
}
#[test]
fn curl_transport_keeps_bearer_on_origin_and_bounds_response() {
    use std::io::{Read, Write};
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap();
    let worker = std::thread::spawn(move || {
        let (mut socket, _) = listener.accept().unwrap();
        let mut raw = [0u8; 8192];
        let count = socket.read(&mut raw).unwrap();
        let request = String::from_utf8_lossy(&raw[..count]);
        assert!(request.contains("Authorization: Bearer private-bearer"));
        socket.write_all(b"HTTP/1.1 302 Found\r\nLocation: https://example.invalid/steal\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}").unwrap();
    });
    let mut http = CurlTransport::new().unwrap();
    let response = http
        .request(
            "GET",
            &format!("http://{}/api/v1/me", address),
            "private-bearer",
            b"",
        )
        .unwrap();
    assert_eq!(response.status, 302);
    worker.join().unwrap();
}

#[test]
fn official_runtime_online_restart_offline_and_tamper() {
    use std::{
        io::{Read, Write},
        sync::{
            atomic::{AtomicBool, Ordering},
            Arc,
        },
    };
    extern "C" {
        fn crypto_ed25519_key_pair(secret: *mut u8, public: *mut u8, seed: *mut u8);
        fn crypto_ed25519_sign(
            signature: *mut u8,
            secret: *const u8,
            message: *const u8,
            length: usize,
        );
    }
    let mut seed = [7u8; 32];
    let mut secret = [0u8; 64];
    let mut public = [0u8; 32];
    unsafe {
        crypto_ed25519_key_pair(secret.as_mut_ptr(), public.as_mut_ptr(), seed.as_mut_ptr());
    }
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    listener.set_nonblocking(true).unwrap();
    let address = listener.local_addr().unwrap();
    let offline = Arc::new(AtomicBool::new(false));
    let stop = Arc::new(AtomicBool::new(false));
    let server_offline = offline.clone();
    let server_stop = stop.clone();
    let worker = std::thread::spawn(move || {
        while !server_stop.load(Ordering::SeqCst) {
            let (mut socket, _) = match listener.accept() {
                Ok(value) => value,
                Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(5));
                    continue;
                }
                Err(error) => panic!("{error}"),
            };
            // Windows can inherit the listener's nonblocking mode on accept.
            // Only accept is polled; a complete HTTP request uses bounded blocking reads.
            socket.set_nonblocking(false).unwrap();
            socket
                .set_read_timeout(Some(Duration::from_secs(5)))
                .unwrap();
            let mut raw = Vec::new();
            let mut chunk = [0u8; 4096];
            let end = loop {
                let count = socket.read(&mut chunk).unwrap();
                assert!(count > 0);
                raw.extend_from_slice(&chunk[..count]);
                if let Some(position) = raw.windows(4).position(|part| part == b"\r\n\r\n") {
                    break position + 4;
                }
                assert!(raw.len() < 65536);
            };
            let header = String::from_utf8_lossy(&raw[..end]);
            let path = header.split_whitespace().nth(1).unwrap().to_string();
            let length = header
                .lines()
                .find_map(|line| {
                    line.split_once(':')
                        .filter(|(key, _)| key.eq_ignore_ascii_case("content-length"))
                        .map(|(_, length)| length.trim().parse::<usize>().unwrap())
                })
                .unwrap_or(0);
            while raw.len() < end + length {
                let count = socket.read(&mut chunk).unwrap();
                assert!(count > 0);
                raw.extend_from_slice(&chunk[..count]);
            }
            let request: Value =
                serde_json::from_slice(&raw[end..end + length]).unwrap_or(Value::Null);
            let (status, body) = if server_offline.load(Ordering::SeqCst) {
                (503, json!({"error":{"code":"UNAVAILABLE"}}))
            } else {
                let data = if path.ends_with("/auth/login") {
                    json!({"access_token":"private-token","expires_at":"2099-01-01T00:00:00Z","user":{"id":"account-a","email":"a@example.invalid"}})
                } else if path.ends_with("/auth/logout") {
                    json!({"signed_out":true})
                } else if path.ends_with("/me") {
                    json!({"user":{"id":"account-a"}})
                } else if path.ends_with("/identity") {
                    json!({"product_id":PRODUCT,"licentivo_product_id":REMOTE,"launch_connection_id":"66666666-6666-4666-8666-666666666666","environment":"sandbox","identity_token":"I".repeat(64),"expires_at":"2099-01-01T00:00:00Z"})
                } else {
                    if path.ends_with("/launch/exchange") || path.ends_with("/validate") || path.ends_with("/heartbeat") {
                        assert!(request["device_name"].as_str().is_some_and(|name| !name.trim().is_empty()), "SDK device name missing");
                        assert!(request["device_info"]["system_type"].as_str().is_some_and(|arch| !arch.is_empty()), "SDK display details missing");
                    }
                    let now = OffsetDateTime::now_utc().unix_timestamp();
                    let claims = json!({"version":1,"key_id":"signing-test","tenant_id":"33333333-3333-4333-8333-333333333333","product_id":REMOTE,"license_id":"44444444-4444-4444-8444-444444444444","device_id":request["device_id"],"activation_id":"55555555-5555-4555-8555-555555555555","mode":"timed","features":["export"],"max_devices":1,"issued_at":now,"expires_at":now+3600,"runtime_policy":{"offline_mode":"limited","offline_seconds":3600,"heartbeat_seconds":600},"capabilities":{"license_model":"node_locked","launch_connection_id":"66666666-6666-4666-8666-666666666666","environment":"sandbox","session_id":"77777777-7777-4777-8777-777777777777","session_expires_at":now+3600,"binding_version":1}});
                    let payload = claims.to_string();
                    let message = [b"LicenovaLease/v1\n".as_slice(), payload.as_bytes()].concat();
                    let mut signature = [0u8; 64];
                    unsafe {
                        crypto_ed25519_sign(
                            signature.as_mut_ptr(),
                            secret.as_ptr(),
                            message.as_ptr(),
                            message.len(),
                        );
                    }
                    json!({"activation_id":"55555555-5555-4555-8555-555555555555","device_key":format!("lv_dev_{}","d".repeat(43)),"lease":{"version":1,"key_id":"signing-test","payload":URL_SAFE_NO_PAD.encode(payload.as_bytes()),"signature":URL_SAFE_NO_PAD.encode(signature)},"heartbeat_seconds":600,"logout_ticket":{"operation_id":"88888888-8888-4888-8888-888888888888","product_id":REMOTE,"activation_id":"55555555-5555-4555-8555-555555555555","expected_generation":1,"release_token":format!("lv_out_{}","o".repeat(43))}})
                };
                (200, json!({"data":data}))
            };
            let bytes = body.to_string();
            let response=format!("HTTP/1.1 {} Status\r\nContent-Length: {}\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n{}",status,bytes.len(),bytes);
            socket.write_all(response.as_bytes()).unwrap();
        }
    });
    struct Cleanup(Arc<AtomicBool>, Option<std::thread::JoinHandle<()>>);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            self.0.store(true, Ordering::SeqCst);
            if let Some(worker) = self.1.take() {
                worker.join().unwrap();
            }
        }
    }
    let _cleanup = Cleanup(stop, Some(worker));
    let root = temp("real-sdk");
    let shared = Rc::new(RefCell::new(Shared::default()));
    let config = Config::new(&format!("http://{address}"), PRODUCT, REMOTE, "sandbox").unwrap();
    let product = json!({"product_id":REMOTE,"trusted_keys":{"signing-test":URL_SAFE_NO_PAD.encode(public)},"base_url":format!("http://{address}"),"allow_http":true});
    let create = || {
        Application::with_components(
            config.clone(),
            product.clone(),
            root.clone(),
            Box::new(TestStore(shared.clone())),
            Box::new(CurlTransport::new().unwrap()),
            Box::new(OfficialRuntimeFactory),
            Box::new(ThreadSleeper),
        )
        .unwrap()
    };
    let mut app = create();
    app.login("a@example.invalid", "password").unwrap();
    app.run_feature("export", || Ok(())).unwrap();
    drop(app);
    offline.store(true, Ordering::SeqCst);
    let mut app = create();
    assert!(app.restore().is_err());drop(app);
    let cache = fs::read_dir(&root)
        .unwrap()
        .next()
        .unwrap()
        .unwrap()
        .path()
        .join("lease.json");
    let mut settings=product.clone();settings["license_key"]=json!("");settings["identity_token"]=json!("");settings["launch_connection_id"]=json!("66666666-6666-4666-8666-666666666666");settings["environment"]=json!("sandbox");settings["cache_path"]=json!(cache.to_str().unwrap());
    let mut runtime=OfficialRuntimeFactory.open(&settings,false).unwrap();runtime.run_feature("export",&mut ||Ok(())).unwrap();
    let mut ran=false;assert!(runtime.run_feature("absent",&mut ||{ran=true;Ok(())}).is_err());assert!(!ran);runtime.close();
    let mut value: Value = serde_json::from_slice(&fs::read(&cache).unwrap()).unwrap();
    value["lease"]["signature"] = json!(URL_SAFE_NO_PAD.encode([0u8; 64]));
    fs::write(cache, value.to_string()).unwrap();
    assert!(OfficialRuntimeFactory.open(&settings,false).is_err());
    fs::remove_dir_all(root).unwrap();
}
