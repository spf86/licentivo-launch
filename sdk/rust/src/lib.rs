//! Launch account lifecycle. All protected work is authorized by Licentivo 0.11.0.
//! Defaults to memory-only sessions; inject an OS-protected Store to remember login.
mod store;
mod transport;
#[allow(dead_code)]
#[path = "../vendor/rust/lib.rs"]
mod licentivo;
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    collections::HashMap,
    fmt, fs,
    path::{Path, PathBuf},
    time::Duration,
};
pub use store::EncryptedFileStore;
use time::{format_description::well_known::Rfc3339, OffsetDateTime};
pub use transport::CurlTransport;

pub const VERSION: &str = "0.1.1";
#[derive(Debug, Clone)]
pub struct Failure {
    pub code: String,
    pub status: u16,
    pub request_id: String,
    pub retry_after: Duration,
    pub transport: bool,
}
impl Failure {
    pub fn new(code: &str) -> Self {
        Self {
            code: safe_code(code),
            status: 0,
            request_id: String::new(),
            retry_after: Duration::ZERO,
            transport: false,
        }
    }
    fn unavailable(&self) -> bool {
        self.transport || (500..600).contains(&self.status)
    }
    fn service() -> Self {
        let mut error = Self::new("SERVICE_UNAVAILABLE");
        error.transport = true;
        error
    }
}
impl fmt::Display for Failure {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Launch: {}", safe_code(&self.code))
    }
}
impl std::error::Error for Failure {}
impl From<std::io::Error> for Failure {
    fn from(_: std::io::Error) -> Self {
        Self::new("LOCAL_IO_ERROR")
    }
}
impl From<licentivo::Error> for Failure {
    fn from(error: licentivo::Error) -> Self {
        let mut failure = Self::new(&error.code);
        failure.status = error.http_status.max(0) as u16;
        failure.request_id = safe_id(&error.request_id);
        failure.retry_after = Duration::from_secs(error.retry_after_seconds.max(0) as u64);
        failure
    }
}
pub struct Response {
    pub status: u16,
    pub body: Vec<u8>,
    pub retry_after: String,
}
impl fmt::Debug for Response {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Launch HTTP response ({})", self.status)
    }
}
pub trait Transport {
    fn request(
        &mut self,
        method: &str,
        url: &str,
        bearer: &str,
        body: &[u8],
    ) -> Result<Response, Failure>;
}
/// Implementations must protect confidentiality AND integrity for the OS user.
/// A plaintext file is not a valid remembered-session implementation.
pub trait Store {
    fn load(&mut self, scope: &str) -> Result<Option<Vec<u8>>, Failure>;
    fn save(&mut self, scope: &str, secret: &[u8]) -> Result<(), Failure>;
    fn delete(&mut self, scope: &str) -> Result<(), Failure>;
}
#[derive(Default)]
pub struct MemoryStore {
    values: HashMap<String, Vec<u8>>,
}
impl Store for MemoryStore {
    fn load(&mut self, scope: &str) -> Result<Option<Vec<u8>>, Failure> {
        Ok(self.values.get(scope).cloned())
    }
    fn save(&mut self, scope: &str, secret: &[u8]) -> Result<(), Failure> {
        self.delete(scope)?;
        self.values.insert(scope.into(), secret.to_vec());
        Ok(())
    }
    fn delete(&mut self, scope: &str) -> Result<(), Failure> {
        if let Some(mut old) = self.values.remove(scope) {
            old.fill(0);
        }
        Ok(())
    }
}
impl Drop for MemoryStore {
    fn drop(&mut self) {
        for value in self.values.values_mut() {
            value.fill(0);
        }
    }
}
pub trait Runtime {
    fn run_feature(
        &mut self,
        feature: &str,
        action: &mut dyn FnMut() -> Result<(), Failure>,
    ) -> Result<(), Failure>;
    fn release_device(&mut self) -> Result<(), Failure>;
    fn close(&mut self);
}
pub trait RuntimeFactory {
    /// cached_only requires an existing, cryptographically verified SDK offline grant.
    fn open(&mut self, settings: &Value, cached_only: bool) -> Result<Box<dyn Runtime>, Failure>;
}
pub struct OfficialRuntimeFactory;
struct OfficialRuntime {
    sdk: Option<licentivo::Client>,
}
impl RuntimeFactory for OfficialRuntimeFactory {
    fn open(&mut self, settings: &Value, cached_only: bool) -> Result<Box<dyn Runtime>, Failure> {
        if cached_only && !Path::new(text(settings, "cache_path")).is_file() {
            return Err(Failure::new("OFFLINE_UNAVAILABLE"));
        }
        let sdk = licentivo::Client::open_config(&settings.to_string()).map_err(Failure::from)?;
        if cached_only {
            let state = sdk.status().map_err(Failure::from)?;
            // open_config starts automatically. A fresh online activation does
            // not qualify as restored offline state. The native core verifies
            // signature, device/product binding, policy and expiry.
            if state["allowed"] != true || state["state"] != "offline_valid" {
                return Err(Failure::new("OFFLINE_UNAVAILABLE"));
            }
        }
        Ok(Box::new(OfficialRuntime { sdk: Some(sdk) }))
    }
}
impl Runtime for OfficialRuntime {
    fn run_feature(
        &mut self,
        feature: &str,
        action: &mut dyn FnMut() -> Result<(), Failure>,
    ) -> Result<(), Failure> {
        let mut business_error = None;
        let result = self
            .sdk
            .as_mut()
            .ok_or_else(|| Failure::new("NOT_STARTED"))?
            .run_feature(feature, || match action() {
                Ok(()) => Ok(()),
                Err(error) => {
                    business_error = Some(error);
                    Err(licentivo::Error::business("Business action failed"))
                }
            });
        if let Some(error) = business_error {
            return Err(error);
        }
        result.map_err(Failure::from)
    }
    fn release_device(&mut self) -> Result<(), Failure> {
        self.sdk
            .as_mut()
            .ok_or_else(|| Failure::new("NOT_STARTED"))?
            .deactivate()
            .map_err(Failure::from)
    }
    fn close(&mut self) {
        self.sdk.take();
    }
}
pub trait Sleeper {
    fn sleep(&mut self, duration: Duration) -> Result<(), Failure>;
}
pub struct ThreadSleeper;
impl Sleeper for ThreadSleeper {
    fn sleep(&mut self, duration: Duration) -> Result<(), Failure> {
        std::thread::sleep(duration);
        Ok(())
    }
}
#[derive(Debug, Clone)]
pub struct State {
    pub user_id: String,
    pub email: String,
    pub expires_at: String,
    pub offline_fallback: bool,
}
#[derive(Clone)]
pub struct Config {
    merchant_origin: String,
    launch_product_id: String,
    licentivo_product_id: String,
    environment: String,
}
impl Config {
    pub fn new(
        origin: &str,
        product: &str,
        runtime_product: &str,
        environment: &str,
    ) -> Result<Self, Failure> {
        if !valid_id(product)
            || !valid_id(runtime_product)
            || !matches!(environment, "sandbox" | "live")
        {
            return Err(Failure::new("CONFIGURATION_ERROR"));
        }
        Ok(Self {
            merchant_origin: validate_origin(origin, environment == "sandbox")?,
            launch_product_id: product.into(),
            licentivo_product_id: runtime_product.into(),
            environment: environment.into(),
        })
    }
    pub fn load(path: impl AsRef<Path>) -> Result<Self, Failure> {
        let value = read_public(path)?;
        if value["schema_version"] != 1
            || !value.as_object().is_some_and(|object| {
                object.keys().all(|key| {
                    [
                        "schema_version",
                        "merchant_origin",
                        "launch_product_id",
                        "licentivo_product_id",
                        "environment",
                    ]
                    .contains(&key.as_str())
                })
            })
        {
            return Err(Failure::new("CONFIGURATION_ERROR"));
        }
        Self::new(
            text(&value, "merchant_origin"),
            text(&value, "launch_product_id"),
            text(&value, "licentivo_product_id"),
            text(&value, "environment"),
        )
    }
}
pub fn read_public(path: impl AsRef<Path>) -> Result<Value, Failure> {
    if fs::metadata(path.as_ref())?.len() > 65536 {
        return Err(Failure::new("CONFIGURATION_ERROR"));
    }
    serde_json::from_slice(&fs::read(path)?).map_err(|_| Failure::new("CONFIGURATION_ERROR"))
}
pub struct Application {
    config: Config,
    product: Value,
    cache_root: PathBuf,
    scope: String,
    store: Box<dyn Store>,
    transport: Box<dyn Transport>,
    factory: Box<dyn RuntimeFactory>,
    sleeper: Box<dyn Sleeper>,
    session: Option<Value>,
    runtime: Option<Box<dyn Runtime>>,
}
impl Application {
    pub fn connect(origin: &str, product_id: &str, licentivo_product_id: &str, trusted_keys: &[(&str, &str)], environment: &str) -> Result<Self, Failure> {
        let config = Config::new(origin, product_id, licentivo_product_id, environment)?;
        let keys: serde_json::Map<String, Value> = trusted_keys.iter().map(|(id, value)| ((*id).into(), Value::String((*value).into()))).collect();
        let cache = std::env::var_os("LOCALAPPDATA").or_else(|| std::env::var_os("XDG_CACHE_HOME")).map(PathBuf::from)
            .or_else(|| std::env::var_os("HOME").or_else(|| std::env::var_os("USERPROFILE")).map(|home| PathBuf::from(home).join(".cache")))
            .ok_or_else(|| Failure::new("PRIVATE_CACHE_UNAVAILABLE"))?.join("LicentivoLaunch");
        Self::new(config, json!({"base_url":"https://www.licentivo.com", "product_id":licentivo_product_id,"trusted_keys":keys}), cache)
    }

    pub fn new(config: Config, product: Value, cache_root: PathBuf) -> Result<Self, Failure> {
        Self::with_components(
            config,
            product,
            cache_root,
            Box::<MemoryStore>::default(),
            Box::new(CurlTransport::new()?),
            Box::new(OfficialRuntimeFactory),
            Box::new(ThreadSleeper),
        )
    }
    pub fn with_components(
        config: Config,
        mut product: Value,
        cache_root: PathBuf,
        store: Box<dyn Store>,
        transport: Box<dyn Transport>,
        factory: Box<dyn RuntimeFactory>,
        sleeper: Box<dyn Sleeper>,
    ) -> Result<Self, Failure> {
        if text(&product, "product_id") != config.licentivo_product_id
            || !product["trusted_keys"].as_object().is_some_and(|pins| {
                !pins.is_empty()
                    && pins.iter().all(|(id, key)| {
                        !id.is_empty()
                            && id.len() <= 128
                            && key.as_str().is_some_and(|key| {
                                URL_SAFE_NO_PAD.decode(key).is_ok_and(|bytes| {
                                    bytes.len() == 32 && URL_SAFE_NO_PAD.encode(bytes) == key
                                })
                            })
                    })
            })
        {
            return Err(Failure::new("CONFIGURATION_ERROR"));
        }
        let settings = product
            .as_object_mut()
            .ok_or_else(|| Failure::new("CONFIGURATION_ERROR"))?;
        for (name, value) in settings.iter() {
            if ["license_key", "cache_path", "device_id", "device_name"].contains(&name.as_str()) {
                if value != "" {
                    return Err(Failure::new("CONFIGURATION_ERROR"));
                }
            } else if ![
                "base_url",
                "product_id",
                "trusted_keys",
                "app_version",
                "allow_http",
            ]
            .contains(&name.as_str())
            {
                return Err(Failure::new("CONFIGURATION_ERROR"));
            }
        }
        if settings.get("allow_http").is_some_and(|v| !v.is_boolean())
            || settings
                .get("app_version")
                .is_some_and(|v| !v.as_str().is_some_and(|s| s.len() <= 80))
        {
            return Err(Failure::new("CONFIGURATION_ERROR"));
        }
        if let Some(base) = settings.get("base_url") {
            validate_origin(
                base.as_str()
                    .ok_or_else(|| Failure::new("CONFIGURATION_ERROR"))?,
                config.environment == "sandbox"
                    && settings.get("allow_http") == Some(&Value::Bool(true)),
            )?;
        }
        settings.remove("license_key");
        settings.remove("device_id");
        settings.remove("cache_path");
        settings.remove("device_name");
        if config.environment == "live" {
            settings.insert("allow_http".into(), Value::Bool(false));
        }
        let cache_root = if cache_root.is_absolute() {
            cache_root
        } else {
            std::env::current_dir()?.join(cache_root)
        };
        let scope = hash(&format!(
            "launch-v1\n{}\n{}\n{}\n{}",
            config.merchant_origin,
            config.launch_product_id,
            config.licentivo_product_id,
            config.environment
        ));
        Ok(Self {
            config,
            product,
            cache_root,
            scope,
            store,
            transport,
            factory,
            sleeper,
            session: None,
            runtime: None,
        })
    }
    fn call(
        &mut self,
        method: &str,
        path: &str,
        body: Option<Value>,
        bearer: &str,
    ) -> Result<Value, Failure> {
        let mut raw = body
            .map(|value| value.to_string().into_bytes())
            .unwrap_or_default();
        let result = self.transport.request(
            method,
            &format!("{}/api/v1{}", self.config.merchant_origin, path),
            bearer,
            &raw,
        );
        raw.fill(0);
        let response = result?;
        if response.body.len() > 1 << 20 {
            return Err(Failure::new("INVALID_RESPONSE"));
        }
        let envelope: Value = serde_json::from_slice(&response.body).unwrap_or(Value::Null);
        if !(200..300).contains(&response.status) {
            let mut error = Failure::new(text(&envelope["error"], "code"));
            error.status = response.status;
            error.request_id = safe_id(text(&envelope, "request_id"));
            error.retry_after = retry_after(&response.retry_after);
            return Err(error);
        }
        if !envelope["data"].is_object() {
            return Err(Failure::new("INVALID_RESPONSE"));
        }
        Ok(envelope["data"].clone())
    }
    fn credential(&mut self) -> Result<Value, Failure> {
        for attempt in 0..5 {
            let token = self.token()?.to_string();
            match self.call(
                "POST",
                &format!(
                    "/software/products/{}/credential",
                    self.config.launch_product_id
                ),
                Some(json!({"environment":self.config.environment})),
                &token,
            ) {
                Ok(value) => {
                    if text(&value, "product_id") != self.config.launch_product_id
                        || text(&value, "license_key").is_empty()
                    {
                        return Err(Failure::new("INVALID_RESPONSE"));
                    }
                    return Ok(value);
                }
                Err(error) => {
                    if error.code != "ENTITLEMENT_PENDING" || attempt == 4 {
                        return Err(error);
                    }
                    let wait = Duration::from_secs(1 << attempt).max(error.retry_after);
                    if wait > Duration::from_secs(10) {
                        return Err(error);
                    }
                    self.sleeper.sleep(wait)?;
                }
            }
        }
        unreachable!()
    }
    pub fn login(&mut self, email: &str, password: &str) -> Result<State, Failure> {
        self.logout()?;
        let result = self.login_inner(email, password);
        if result.is_err() {
            let _ = self.logout();
        }
        result
    }
    fn login_inner(&mut self, email: &str, password: &str) -> Result<State, Failure> {
        let grant = self.call(
            "POST",
            "/auth/login",
            Some(json!({"email":email,"password":password,"kind":"desktop"})),
            "",
        )?;
        if text(&grant, "access_token").is_empty() || text(&grant["user"], "id").is_empty() {
            return Err(Failure::new("INVALID_RESPONSE"));
        }
        self.session = Some(json!({"scope":self.scope,"grant":grant}));
        self.require_fresh()?;
        let credential = self.credential()?;
        self.session.as_mut().unwrap()["credential"] = credential;
        let state = self.start(false)?;
        self.save()?;
        Ok(state)
    }
    pub fn restore(&mut self) -> Result<State, Failure> {
        self.close();
        self.load()?;
        self.require_fresh()?;
        let result = self.restore_online();
        match result {
            Err(error) if error.unavailable() => {
                self.close();
                self.start(true)
            }
            Err(error) => {
                self.close();
                if denied(&error) {
                    self.clear()?;
                }
                Err(error)
            }
            success => success,
        }
    }
    fn restore_online(&mut self) -> Result<State, Failure> {
        let token = self.token()?.to_string();
        let data = self.call("GET", "/me", None, &token)?;
        if text(&data["user"], "id") != self.user_id()? {
            return Err(Failure::new("SESSION_MISMATCH"));
        }
        let credential = self.credential()?;
        self.session.as_mut().unwrap()["credential"] = credential;
        let state = self.start(false)?;
        self.save()?;
        Ok(state)
    }
    fn start(&mut self, offline: bool) -> Result<State, Failure> {
        let session = self
            .session
            .as_ref()
            .ok_or_else(|| Failure::new("NO_SESSION"))?;
        let mut settings = self.product.clone();
        settings["license_key"] = session["credential"]["license_key"].clone();
        let directory = self.cache_directory()?;
        private_directory(&directory)?;
        settings["cache_path"] = Value::String(
            directory
                .join("lease.json")
                .to_str()
                .ok_or_else(|| Failure::new("CONFIGURATION_ERROR"))?
                .into(),
        );
        self.runtime = Some(self.factory.open(&settings, offline)?);
        let grant = &self.session.as_ref().unwrap()["grant"];
        Ok(State {
            user_id: text(&grant["user"], "id").into(),
            email: text(&grant["user"], "email").into(),
            expires_at: text(grant, "expires_at").into(),
            offline_fallback: offline,
        })
    }
    pub fn run_feature<T, F: FnOnce() -> Result<T, Failure>>(
        &mut self,
        feature: &str,
        action: F,
    ) -> Result<T, Failure> {
        let runtime = self
            .runtime
            .as_mut()
            .ok_or_else(|| Failure::new("NOT_STARTED"))?;
        let mut action = Some(action);
        let mut output = None;
        runtime.run_feature(feature, &mut || {
            output = Some(action
                .take()
                .ok_or_else(|| Failure::new("CALLBACK_REPEATED"))?(
            )?);
            Ok(())
        })?;
        output.ok_or_else(|| Failure::new("FEATURE_DENIED"))
    }
    pub fn release_device(&mut self) -> Result<(), Failure> {
        let result = self
            .runtime
            .as_mut()
            .ok_or_else(|| Failure::new("NOT_STARTED"))?
            .release_device();
        let cleanup = self.logout();
        result.and(cleanup)
    }
    pub fn logout(&mut self) -> Result<(), Failure> {
        let result = (|| {
            if self.session.is_none() {
                if let Err(error) = self.load() {
                    if error.code != "NO_SESSION" {
                        return Err(error);
                    }
                }
            }
            if self.session.is_some() {
                let token = self.token()?.to_string();
                if let Err(error) = self.call("POST", "/auth/logout", Some(json!({})), &token) {
                    if error.status != 401 {
                        return Err(error);
                    }
                }
            }
            Ok(())
        })();
        let cleanup = self.clear();
        result.and(cleanup)
    }
    pub fn close(&mut self) {
        if let Some(mut runtime) = self.runtime.take() {
            runtime.close();
        }
    }
    fn save(&mut self) -> Result<(), Failure> {
        let mut secret = self.session.as_ref().unwrap().to_string().into_bytes();
        let result = self.store.save(&self.scope, &secret);
        secret.fill(0);
        result
    }
    fn load(&mut self) -> Result<(), Failure> {
        let mut raw = self
            .store
            .load(&self.scope)?
            .ok_or_else(|| Failure::new("NO_SESSION"))?;
        let parsed = if raw.len() <= 1 << 20 {
            serde_json::from_slice::<Value>(&raw).ok()
        } else {
            None
        };
        raw.fill(0);
        let session = parsed.ok_or_else(|| Failure::new("SESSION_MISMATCH"))?;
        if text(&session, "scope") != self.scope
            || text(&session["grant"], "access_token").is_empty()
            || text(&session["grant"]["user"], "id").is_empty()
            || text(&session["credential"], "product_id") != self.config.launch_product_id
            || text(&session["credential"], "license_key").is_empty()
        {
            return Err(Failure::new("SESSION_MISMATCH"));
        }
        self.session = Some(session);
        Ok(())
    }
    fn require_fresh(&mut self) -> Result<(), Failure> {
        let session = self
            .session
            .as_ref()
            .ok_or_else(|| Failure::new("NO_SESSION"))?;
        if !OffsetDateTime::parse(text(&session["grant"], "expires_at"), &Rfc3339)
            .is_ok_and(|expiry| expiry > OffsetDateTime::now_utc())
        {
            self.clear()?;
            return Err(Failure::new("SESSION_EXPIRED"));
        }
        Ok(())
    }
    fn clear(&mut self) -> Result<(), Failure> {
        self.close();
        let mut failure = None;
        if self.session.is_some() {
            match self.cache_directory() {
                Ok(path) if path.starts_with(&self.cache_root) && path != self.cache_root => {
                    if path.exists() {
                        if let Err(error) = fs::remove_dir_all(path) {
                            failure = Some(Failure::from(error));
                        }
                    }
                }
                _ => failure = Some(Failure::new("CACHE_SCOPE_ERROR")),
            }
        }
        self.session = None;
        let deleted = self.store.delete(&self.scope);
        if let Some(error) = failure {
            return Err(error);
        }
        deleted
    }
    fn token(&self) -> Result<&str, Failure> {
        Ok(text(
            &self
                .session
                .as_ref()
                .ok_or_else(|| Failure::new("NO_SESSION"))?["grant"],
            "access_token",
        ))
    }
    fn user_id(&self) -> Result<&str, Failure> {
        Ok(text(
            &self
                .session
                .as_ref()
                .ok_or_else(|| Failure::new("NO_SESSION"))?["grant"]["user"],
            "id",
        ))
    }
    fn cache_directory(&self) -> Result<PathBuf, Failure> {
        Ok(self
            .cache_root
            .join(hash(&format!("{}\n{}", self.scope, self.user_id()?))))
    }
}
impl Drop for Application {
    fn drop(&mut self) {
        self.close();
    }
}
fn text<'a>(value: &'a Value, name: &str) -> &'a str {
    value.get(name).and_then(Value::as_str).unwrap_or("")
}
fn hash(value: &str) -> String {
    format!("{:x}", Sha256::digest(value.as_bytes()))
}
fn valid_id(value: &str) -> bool {
    value.len() == 36
        && value.bytes().enumerate().all(|(i, c)| {
            if [8, 13, 18, 23].contains(&i) {
                c == b'-'
            } else {
                c.is_ascii_hexdigit()
            }
        })
}
fn safe_code(code: &str) -> String {
    if !code.is_empty()
        && code.len() <= 64
        && code.as_bytes()[0].is_ascii_uppercase()
        && code
            .bytes()
            .all(|c| c.is_ascii_uppercase() || c.is_ascii_digit() || c == b'_')
    {
        code.into()
    } else {
        "HTTP_ERROR".into()
    }
}
fn safe_id(id: &str) -> String {
    if id.len() <= 128
        && id
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || b"._:-".contains(&c))
    {
        id.into()
    } else {
        String::new()
    }
}
fn denied(error: &Failure) -> bool {
    error.code != "ENTITLEMENT_PENDING"
        && ([401, 403, 404].contains(&error.status) || error.code == "SESSION_MISMATCH")
}
fn retry_after(raw: &str) -> Duration {
    if let Ok(seconds) = raw.parse::<u64>() {
        return Duration::from_secs(seconds);
    }
    if let Ok(deadline) = OffsetDateTime::parse(raw, &time::format_description::well_known::Rfc2822)
    {
        return Duration::from_secs(
            (deadline.unix_timestamp() - OffsetDateTime::now_utc().unix_timestamp()).max(0) as u64,
        );
    }
    Duration::ZERO
}
fn validate_origin(raw: &str, local: bool) -> Result<String, Failure> {
    let origin = raw.strip_suffix('/').unwrap_or(raw);
    let (secure, authority) = if let Some(rest) = origin.strip_prefix("https://") {
        (true, rest)
    } else if let Some(rest) = origin.strip_prefix("http://") {
        (false, rest)
    } else {
        return Err(Failure::new("CONFIGURATION_ERROR"));
    };
    if authority.is_empty()
        || authority
            .bytes()
            .any(|c| !c.is_ascii() || c.is_ascii_whitespace() || b"/@?#\\%".contains(&c))
    {
        return Err(Failure::new("CONFIGURATION_ERROR"));
    }
    let (host, port) = if authority.starts_with('[') {
        let end = authority
            .find(']')
            .ok_or_else(|| Failure::new("CONFIGURATION_ERROR"))?;
        let ip = &authority[1..end];
        if ip.parse::<std::net::Ipv6Addr>().is_err() {
            return Err(Failure::new("CONFIGURATION_ERROR"));
        }
        let suffix = &authority[end + 1..];
        let port = if suffix.is_empty() {
            ""
        } else {
            suffix
                .strip_prefix(':')
                .filter(|port| !port.is_empty())
                .ok_or_else(|| Failure::new("CONFIGURATION_ERROR"))?
        };
        (ip, port)
    } else {
        let (host, port) = authority.split_once(':').unwrap_or((authority, ""));
        if host.is_empty()
            || !host
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || b".-".contains(&c))
        {
            return Err(Failure::new("CONFIGURATION_ERROR"));
        }
        (host, port)
    };
    if !port.is_empty() && !port.parse::<u16>().is_ok_and(|p| p > 0) {
        return Err(Failure::new("CONFIGURATION_ERROR"));
    }
    if !secure
        && !(local
            && (host == "localhost"
                || host
                    .parse::<std::net::IpAddr>()
                    .is_ok_and(|ip| ip.is_loopback())))
    {
        return Err(Failure::new("CONFIGURATION_ERROR"));
    }
    Ok(origin.into())
}
fn private_directory(path: &Path) -> Result<(), Failure> {
    fs::create_dir_all(path)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(path, fs::Permissions::from_mode(0o700))?;
    }
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        let who = std::process::Command::new("whoami.exe")
            .args(["/user", "/fo", "csv", "/nh"])
            .creation_flags(0x08000000)
            .output()?;
        let line = String::from_utf8_lossy(&who.stdout);
        let sid = line
            .split(',')
            .map(|part| part.trim().trim_matches('"'))
            .find(|part| part.starts_with("S-1-"))
            .ok_or_else(|| Failure::new("PRIVATE_CACHE_UNAVAILABLE"))?;
        if !sid
            .bytes()
            .all(|c| c.is_ascii_digit() || c == b'-' || c == b'S')
        {
            return Err(Failure::new("PRIVATE_CACHE_UNAVAILABLE"));
        }
        let result = std::process::Command::new("icacls.exe")
            .arg(path)
            .args([
                "/inheritance:r",
                "/grant:r",
                &format!("*{}:(OI)(CI)F", sid),
                "*S-1-5-18:(OI)(CI)F",
            ])
            .creation_flags(0x08000000)
            .output()?;
        if !result.status.success() {
            return Err(Failure::new("PRIVATE_CACHE_UNAVAILABLE"));
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests;
