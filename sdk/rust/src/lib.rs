//! Launch account lifecycle. All protected work is authorized by Licentivo 0.11.2.
//! Defaults to memory-only sessions; inject an OS-protected Store to remember login.
mod store;
mod account_lock;
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

pub const VERSION: &str = "0.1.3";
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
    fn for_logout(&mut self, settings: &Value) -> Result<Box<dyn Runtime>, Failure> { self.open(settings, false) }
}
pub struct OfficialRuntimeFactory;
struct OfficialRuntime {
    sdk: Option<licentivo::Client>,
}
impl RuntimeFactory for OfficialRuntimeFactory {
    fn open(&mut self, settings: &Value, _cached_only: bool) -> Result<Box<dyn Runtime>, Failure> {
 let mut sdk=licentivo::Client::init_config(&settings.to_string()).map_err(Failure::from)?;sdk.start().map_err(Failure::from)?;Ok(Box::new(OfficialRuntime{sdk:Some(sdk)}))
 }
 fn for_logout(&mut self,settings:&Value)->Result<Box<dyn Runtime>,Failure>{let sdk=licentivo::Client::init_config(&settings.to_string()).map_err(Failure::from)?;Ok(Box::new(OfficialRuntime{sdk:Some(sdk)}))}
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
        let sdk = self.sdk.as_mut().ok_or_else(|| Failure::new("NOT_STARTED"))?;
        sdk.logout().map_err(Failure::from)?;
        if sdk.logout_pending() { return Err(Failure::new("DEVICE_REVOCATION_PENDING")); }
        Ok(())
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
    _sleeper: Box<dyn Sleeper>,
    session: Option<Value>,
    runtime: Option<Box<dyn Runtime>>,
    account_cleared: bool,
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
            "launch-account-v2\n{}\n{}\n{}\n{}",
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
            _sleeper: sleeper,
            session: None,
            runtime: None,
            account_cleared: false,
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
    fn credential(&mut self)->Result<Value,Failure>{
 let token=self.token()?.to_string();let value=self.call("POST",&format!("/software/products/{}/identity",self.config.launch_product_id),Some(json!({"environment":self.config.environment})),&token)?;
 if text(&value,"product_id")!=self.config.launch_product_id||text(&value,"licentivo_product_id")!=self.config.licentivo_product_id||text(&value,"environment")!=self.config.environment||!uuid(text(&value,"launch_connection_id"))||text(&value,"identity_token").len()<32{return Err(Failure::new("IDENTITY_MISMATCH"));}Ok(value)
 }
    pub fn login(&mut self, email: &str, password: &str) -> Result<State, Failure> {
        let _guard=account_lock::take(&self.scope)?;
        self.logout_account()?;
        let result = self.login_inner(email, password);
        if result.is_err() {
            let _ = self.logout_account();
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
        self.account_cleared=false;
        self.require_fresh()?;
        let credential = self.credential()?;
        self.session.as_mut().unwrap()["credential"] = credential;
        let state = self.start(false)?;
        self.save()?;
        Ok(state)
    }
    pub fn restore(&mut self) -> Result<State, Failure> {
        let _guard=account_lock::take(&self.scope)?;
        self.close();
        if !self.session.as_ref().is_some_and(|s| s["logout_pending"] == true) {if let Err(error)=self.load(){if error.code!="NO_SESSION"{return Err(error);}self.session=self.pending_accounts()?.into_iter().next();if self.session.is_none(){return Err(error);}}}
        let current=self.token()?.to_string();
        if account_lock::has_intent(&self.scope,&current)?{self.session.as_mut().unwrap()["logout_pending"]=json!(true);}
        if let Some(task)=self.pending_accounts()?.into_iter().find(|task|text(&task["grant"],"access_token")==current){self.session=Some(task);}
        if self.session.as_ref().is_some_and(|s| s["logout_pending"] == true) {
            self.logout_account()?;
            return Err(Failure::new("NO_SESSION"));
        }
        self.retry_other_accounts(&current)?;
        self.require_fresh()?;
        let result = self.restore_online();
        match result {
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
    fn start(&mut self, _offline: bool) -> Result<State, Failure> {
        let session = self
            .session
            .as_ref()
            .ok_or_else(|| Failure::new("NO_SESSION"))?;
        let mut settings = self.product.clone();
        settings["license_key"]=json!("");settings["identity_token"]=session["credential"].get("identity_token").cloned().unwrap_or(json!(""));settings["launch_connection_id"]=session["credential"]["launch_connection_id"].clone();settings["environment"]=json!(self.config.environment);
        let directory = self.cache_directory()?;
        private_directory(&directory)?;
        settings["cache_path"] = Value::String(
            directory
                .join("lease.json")
                .to_str()
                .ok_or_else(|| Failure::new("CONFIGURATION_ERROR"))?
                .into(),
        );
        self.runtime = Some(self.factory.open(&settings, false)?);
        let grant = &self.session.as_ref().unwrap()["grant"];
        Ok(State {
            user_id: text(&grant["user"], "id").into(),
            email: text(&grant["user"], "email").into(),
            expires_at: text(grant, "expires_at").into(),
            offline_fallback: false,
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
    pub fn release_device(&mut self)->Result<(),Failure>{self.logout()}
    pub fn logout(&mut self)->Result<(),Failure>{
 if let Some(saved)=self.session.as_ref(){let _=account_lock::remember(&self.scope,text(&saved["grant"],"access_token"));}let guard=account_lock::take(&self.scope);let result=match guard{Ok(_guard)=>if self.account_cleared&&self.session.is_none(){self.retry_other_accounts("")}else{self.logout_account()},Err(error)=>Err(error)};if result.is_err(){if let Some(runtime)=self.runtime.as_mut(){let _=runtime.release_device();}}self.close();if result.is_err(){if let Some(saved)=self.session.as_mut(){saved["logout_pending"]=json!(true);}}result
 }
 fn logout_account(&mut self)->Result<(),Failure>{
 if self.session.is_none(){if let Err(e)=self.load(){if e.code!="NO_SESSION"{let _=self.clear();return Err(e);}}}
 if self.session.is_none(){self.session=self.pending_accounts()?.into_iter().next();}
 let skip=self.session.as_ref().map(|s|text(&s["grant"],"access_token").to_string()).unwrap_or_default();let others=self.retry_other_accounts(&skip);
 let persist=if let Some(session)=self.session.as_mut(){session["logout_pending"]=json!(true);self.mark_account_logout()}else{Ok(())};
 let mut release=Ok(());
 if self.session.is_some(){
  let can_reopen=match self.active_account(){Ok(None)=>true,Ok(Some(active))=>text(&active["grant"],"access_token")==self.token()?,Err(_)=>false};
  if self.runtime.is_none()&&can_reopen&&self.session.as_ref().unwrap()["credential"].is_object(){let mut settings=self.product.clone();let context=&self.session.as_ref().unwrap()["credential"];settings["license_key"]=json!("");settings["identity_token"]=json!("");settings["launch_connection_id"]=context["launch_connection_id"].clone();settings["environment"]=json!(self.config.environment);settings["cache_path"]=json!(self.cache_directory()?.join("lease.json").to_str().ok_or_else(||Failure::new("CONFIGURATION_ERROR"))?);match self.factory.for_logout(&settings){Ok(runtime)=>self.runtime=Some(runtime),Err(e)=>release=Err(e)}}
  if let Some(runtime)=self.runtime.as_mut(){release=runtime.release_device();}
 }
 self.close();let account=if self.session.is_some(){let token=self.token()?.to_string();match self.call("POST","/auth/logout",Some(json!({})),&token){Err(e)if e.status!=401=>Err(e),_=>Ok(())}}else{Ok(())};let cleanup=if account.is_ok(){if let Some(saved)=self.session.clone(){self.finish_account(&saved)?;}self.clear()}else{persist};cleanup?;release?;account?;others
 }
    pub fn close(&mut self) {
        if let Some(mut runtime) = self.runtime.take() {
            runtime.close();
        }
    }
    fn save(&mut self)->Result<(),Failure>{let mut saved=self.session.as_ref().unwrap().clone();if let Some(context)=saved["credential"].as_object_mut(){context.retain(|key,_|["product_id","licentivo_product_id","launch_connection_id","environment","expires_at"].contains(&key.as_str()));}if saved["logout_pending"]==true{saved["version"]=json!(3);if let Some(context)=saved.as_object_mut().unwrap().remove("credential"){saved["logout_context"]=context;}}let mut secret=saved.to_string().into_bytes();let result=self.store.save(&self.scope,&secret);secret.fill(0);result}
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
        let mut session = parsed.ok_or_else(|| Failure::new("SESSION_MISMATCH"))?;
        if session["logout_pending"] == true && text(&session,"scope")==self.scope && !text(&session["grant"],"access_token").is_empty() {
            if let Some(context)=session.as_object_mut().unwrap().remove("logout_context"){session["credential"]=context;}
            self.session=Some(session);return Ok(());
        }
        if text(&session, "scope") != self.scope
            || text(&session["grant"], "access_token").is_empty()
            || session["logout_pending"] != true && (text(&session["grant"]["user"], "id").is_empty()
            || text(&session["credential"], "product_id") != self.config.launch_product_id
            || text(&session["credential"], "licentivo_product_id") != self.config.licentivo_product_id || text(&session["credential"], "environment") != self.config.environment
            || !text(&session["credential"], "identity_token").is_empty() || !uuid(text(&session["credential"], "launch_connection_id")))
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
    fn clear(&mut self)->Result<(),Failure>{self.close();let saved=self.session.take();self.account_cleared=true;if let Some(saved)=saved{self.delete_owned(&saved)?;}Ok(())}
    fn queue_key(&self)->String{format!("{}\naccount-logout-queue-v1",self.scope)}
    fn active_account(&mut self)->Result<Option<Value>,Failure>{let Some(mut raw)=self.store.load(&self.scope)? else{return Ok(None)};let value=serde_json::from_slice::<Value>(&raw).map_err(|_|Failure::new("SESSION_MISMATCH"));raw.fill(0);let value=value?;if text(&value,"scope")!=self.scope{return Err(Failure::new("SESSION_MISMATCH"));}Ok(Some(value))}
    fn owns(&mut self,saved:&Value)->Result<bool,Failure>{Ok(self.active_account()?.is_some_and(|active|text(&active["grant"],"access_token")==text(&saved["grant"],"access_token")))}
    fn delete_owned(&mut self,saved:&Value)->Result<(),Failure>{if self.owns(saved)?{self.store.delete(&self.scope)?;}Ok(())}
    fn public_account(saved:&Value)->Value{let mut clean=saved.clone();if let Some(context)=clean["credential"].as_object_mut(){context.retain(|key,_|["product_id","licentivo_product_id","launch_connection_id","environment","expires_at"].contains(&key.as_str()));}if clean["logout_pending"]==true{clean["version"]=json!(3);if let Some(context)=clean.as_object_mut().unwrap().remove("credential"){clean["logout_context"]=context;}}clean}
    fn pending_accounts(&mut self)->Result<Vec<Value>,Failure>{let Some(mut raw)=self.store.load(&self.queue_key())? else{return Ok(vec![])};let parsed=serde_json::from_slice::<Value>(&raw).map_err(|_|Failure::new("SESSION_MISMATCH"));raw.fill(0);let value=parsed?;let records=value["records"].as_array().ok_or_else(||Failure::new("SESSION_MISMATCH"))?;if value["version"]!=1||records.len()>64{return Err(Failure::new("SESSION_MISMATCH"));}let mut result=vec![];for record in records{if record["logout_pending"]!=true||text(record,"scope")!=self.scope||text(&record["grant"],"access_token").is_empty(){return Err(Failure::new("SESSION_MISMATCH"));}let mut record=record.clone();if let Some(context)=record.as_object_mut().unwrap().remove("logout_context"){record["credential"]=context;}result.push(record);}Ok(result)}
    fn update_pending(&mut self,saved:&Value,remove:bool)->Result<(),Failure>{let mut records=self.pending_accounts()?;records.retain(|record|text(&record["grant"],"access_token")!=text(&saved["grant"],"access_token"));if !remove{let mut record=saved.clone();record["logout_pending"]=json!(true);records.push(record);}if records.len()>64{return Err(Failure::new("ACCOUNT_LOGOUT_QUEUE_FULL"));}if records.is_empty(){return self.store.delete(&self.queue_key());}let records:Vec<Value>=records.iter().map(Self::public_account).collect();let mut raw=json!({"version":1,"records":records}).to_string().into_bytes();let result=if raw.len()>60000{Err(Failure::new("ACCOUNT_LOGOUT_QUEUE_FULL"))}else{self.store.save(&self.queue_key(),&raw)};raw.fill(0);result}
    fn mark_account_logout(&mut self)->Result<(),Failure>{let saved=self.session.as_ref().unwrap().clone();let _=account_lock::remember(&self.scope,text(&saved["grant"],"access_token"));let queue=self.update_pending(&saved,false);if self.owns(&saved)?{if self.save().is_ok(){return Ok(());}let _=self.delete_owned(&saved);}queue.map_err(|_|Failure::new("ACCOUNT_LOGOUT_STORAGE_FAILED"))}
    fn finish_account(&mut self,saved:&Value)->Result<(),Failure>{self.delete_owned(saved)?;self.update_pending(saved,true)?;account_lock::clear_intent(&self.scope,text(&saved["grant"],"access_token"))}
    fn retry_other_accounts(&mut self,skip:&str)->Result<(),Failure>{for saved in self.pending_accounts()?{let token=text(&saved["grant"],"access_token");if token==skip{continue;}if let Err(error)=self.call("POST","/auth/logout",Some(json!({})),token){if error.status!=401{return Err(error);}}self.finish_account(&saved)?;}Ok(())}
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

fn uuid(v:&str)->bool{v.len()==36&&v.bytes().enumerate().all(|(i,b)|if [8,13,18,23].contains(&i){b==b'-'}else{b.is_ascii_hexdigit()})}
