import {createHash, createCipheriv, createDecipheriv, randomBytes, randomUUID} from 'node:crypto';
import {readFile, writeFile, mkdir, rename, rm, stat} from 'node:fs/promises';
import {resolve, join, dirname} from 'node:path';
import {homedir} from 'node:os';
import {setTimeout as delay} from 'node:timers/promises';
import {AsyncLocalStorage} from 'node:async_hooks';
import {accountLock,rememberIntent,hasIntent,clearIntent} from './account-lock.mjs';

const LIMIT = 1 << 20;
const UUID = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/;
const hash = value => createHash('sha256').update(value).digest('hex');
const clone = value => structuredClone(value);
const cancelled = signal => { if (signal?.aborted) throw new LaunchError('CANCELLED'); };
const absent = error => error?.code === 'ENOENT';

export class LaunchError extends Error {
  constructor(code, status = 0, retryAfterSeconds = 0) {
    const safe = /^[A-Z][A-Z0-9_]{0,79}$/.test(String(code)) ? code : 'REQUEST_FAILED';
    super(`Launch: ${safe}`); this.name = 'LaunchError'; this.code = safe;
    this.status = status; this.retryAfterSeconds = Math.max(0, retryAfterSeconds);
  }
}
export class MemoryStore {
  persistent = false; #values = new Map();
  async load(scope) { return this.#values.has(scope) ? clone(this.#values.get(scope)) : null; }
  async save(scope, record) { this.#values.set(scope, clone(record)); }
  async delete(scope) { this.#values.delete(scope); }
}

// The caller obtains the key from its OS credential vault. Never save that key
// beside these ciphertexts or place it in product configuration.
export class EncryptedFileStore {
  persistent = true; secure = true; #key; #root;
  constructor(directory, key) {
    if (!(key instanceof Uint8Array) || key.byteLength !== 32) throw new LaunchError('STORE_KEY_REQUIRED');
    this.#key = Buffer.from(key); this.#root = resolve(directory);
  }
  #path(scope) { return join(this.#root, hash(scope) + '.aesgcm'); }
  async load(scope) {
    let raw;
    try { if ((await stat(this.#path(scope))).size > LIMIT) throw new LaunchError('INVALID_SAVED_SESSION'); raw = await readFile(this.#path(scope)); }
    catch (error) { if (absent(error)) return null; throw error; }
    try {
      if (raw[0] !== 1 || raw.length < 30) throw Error();
      const decipher = createDecipheriv('aes-256-gcm', this.#key, raw.subarray(1, 13));
      decipher.setAAD(Buffer.from(scope)); decipher.setAuthTag(raw.subarray(13, 29));
      const plain = Buffer.concat([decipher.update(raw.subarray(29)), decipher.final()]);
      try { return JSON.parse(plain.toString('utf8')); } finally { plain.fill(0); }
    } catch { throw new LaunchError('INVALID_SAVED_SESSION'); }
  }
  async save(scope, record) {
    await mkdir(this.#root, {recursive: true, mode: 0o700});
    const iv = randomBytes(12), cipher = createCipheriv('aes-256-gcm', this.#key, iv);
    cipher.setAAD(Buffer.from(scope));
    const plain = Buffer.from(JSON.stringify(record));
    try {
      if (plain.length > LIMIT - 64) throw new LaunchError('INVALID_SAVED_SESSION');
      const encrypted = Buffer.concat([cipher.update(plain), cipher.final()]);
      const temporary = join(this.#root, '.session-' + randomUUID());
      try { await writeFile(temporary, Buffer.concat([Buffer.from([1]), iv, cipher.getAuthTag(), encrypted]), {flag: 'wx', mode: 0o600}); await rename(temporary, this.#path(scope)); }
      finally { await rm(temporary, {force: true}); }
    } finally { plain.fill(0); }
  }
  async delete(scope) { await rm(this.#path(scope), {force: true}); }
}

function origin(value, environment, allowHTTP=false) {
  let parsed; try { parsed = new URL(value); } catch { throw new LaunchError('INVALID_CONFIG'); }
  if (parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== '/' ||
      (parsed.protocol !== 'https:' && !(environment === 'sandbox' && parsed.protocol === 'http:' && (allowHTTP || ['localhost','127.0.0.1','[::1]'].includes(parsed.hostname))))) throw new LaunchError('INVALID_CONFIG');
  return parsed.origin;
}
export function validateConfig(launch, product) {
  if (!launch || Object.keys(launch).some(k => !['schema_version','merchant_origin','launch_product_id','licentivo_product_id','environment','allow_http'].includes(k)) || launch.schema_version !== 1 || !UUID.test(launch.launch_product_id) || !UUID.test(launch.licentivo_product_id) || !['sandbox','live'].includes(launch.environment) || launch.allow_http!==undefined&&typeof launch.allow_http!=='boolean' || launch.allow_http===true&&launch.environment!=='sandbox') throw new LaunchError('INVALID_CONFIG');
  if (!product || product.product_id !== launch.licentivo_product_id || Object.keys(product).some(k => !['product_id','trusted_keys','base_url','app_version','license_key','cache_path','device_name','allow_http'].includes(k)) || product.license_key || product.cache_path) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  if (['license_key','cache_path'].some(k => product[k] !== undefined && product[k] !== null && typeof product[k] !== 'string')) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  const keys = product.trusted_keys;
  if (product.app_version !== undefined && (typeof product.app_version !== 'string' || product.app_version.length > 80) || product.device_name !== undefined && (typeof product.device_name !== 'string' || product.device_name.length > 128) || product.allow_http !== undefined && typeof product.allow_http !== 'boolean') throw new LaunchError('PUBLIC_CONFIG_INVALID');
  if (!keys || typeof keys !== 'object' || Array.isArray(keys) || !Object.keys(keys).length || Object.keys(keys).length > 32 || Object.entries(keys).some(([id,key]) => !id || id.length > 128 || typeof key !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(key) || Buffer.from(key,'base64url').toString('base64url') !== key)) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  const runtimeOrigin = origin(product.base_url || 'https://www.licentivo.com', launch.environment);
  if (runtimeOrigin.startsWith('http:') && product.allow_http !== true) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  return {launch: {...clone(launch), merchant_origin: origin(launch.merchant_origin, launch.environment, launch.allow_http===true)}, product: {product_id: product.product_id, trusted_keys: clone(keys), base_url: runtimeOrigin, ...(product.app_version ? {app_version: product.app_version} : {}), ...(product.allow_http === true ? {allow_http:true} : {})}};
}
export async function loadConfig(launchPath, productPath) {
  const read = async path => { if ((await stat(path)).size > 65536) throw new LaunchError('CONFIG_TOO_LARGE'); return JSON.parse((await readFile(path,'utf8')).replace(/^\uFEFF/,'')); };
  return validateConfig(await read(launchPath), await read(productPath));
}

export async function defaultTransport(request) {
  cancelled(request.signal);
  const signal = request.signal ? AbortSignal.any([request.signal, AbortSignal.timeout(15000)]) : AbortSignal.timeout(15000);
  try {
    const response = await fetch(request.url, {method:request.method, headers:request.headers, body:request.body, redirect:'manual', signal});
    if (response.status >= 300 && response.status < 400) { await response.body?.cancel(); throw new LaunchError('REDIRECT_REJECTED', response.status); }
    const reader = response.body?.getReader(), chunks = []; let size = 0;
    if (reader) { try { while (true) { const part = await reader.read(); if (part.done) break; size += part.value.length; if (size > LIMIT) { await reader.cancel(); throw new LaunchError('RESPONSE_TOO_LARGE'); } chunks.push(Buffer.from(part.value)); } } finally { reader.releaseLock(); } }
    return {status:response.status, headers:Object.fromEntries(response.headers), body:Buffer.concat(chunks).toString('utf8')};
  } catch (error) { cancelled(request.signal); if (error instanceof LaunchError) throw error; throw new LaunchError('NETWORK_UNAVAILABLE'); }
}
const denied = error => error instanceof LaunchError && error.code !== 'ENTITLEMENT_PENDING' && !(error.status === 403 && ['SDK_HTTP_DISABLED','SDK_HTTPS_REQUIRED'].includes(error.code)) && ([401,403,404].includes(error.status) || ['SESSION_EXPIRED','SESSION_MISMATCH','ENTITLEMENT_EXPIRED','ENTITLEMENT_SUSPENDED','ENTITLEMENT_NOT_PURCHASED'].includes(error.code));
const unavailable = error => error instanceof LaunchError && (error.code === 'NETWORK_UNAVAILABLE' || error.status >= 500 && error.status <= 599);

export class Application {
  #config; #store; #transport; #factory; #root; #scope; #record = null; #runtime = null; #closed = false; #accountCleared=false; #queue = Promise.resolve(); #wait; #attempts; #callbacks = new AsyncLocalStorage();
  constructor(launch, product, options = {}) {
    this.#config = validateConfig(launch, product); this.#store = options.store ?? new MemoryStore();
    if (this.#store.persistent !== false && (this.#store.persistent !== true || this.#store.secure !== true)) throw new LaunchError('SECURE_STORE_REQUIRED');
    this.#transport = options.transport ?? defaultTransport;
    this.#factory = options.runtimeFactory ?? (async config => { const {Client} = await import('./vendor/licentivo.mjs'); return new Client(config); });
    this.#root = resolve(options.cacheDir ?? join(homedir(), '.cache', 'LicentivoLaunch'));
    this.#scope = ['launch-account-v2',this.#config.launch.merchant_origin,launch.launch_product_id,launch.licentivo_product_id,launch.environment].join('\n');
    this.#wait = options.sleep ?? (async (ms, signal) => { try { await delay(ms, undefined, {signal}); } catch { cancelled(signal); throw new LaunchError('CANCELLED'); } });
    this.#attempts = options.pendingAttempts ?? 5;
    if (!Number.isInteger(this.#attempts) || this.#attempts < 1 || this.#attempts > 5) throw new LaunchError('INVALID_RETRY_POLICY');
  }
  static async fromFiles(launchPath, productPath, options) { const {launch,product} = await loadConfig(launchPath,productPath); return new Application(launch,product,options); }
  static connect(merchantOrigin, productID, licentivoProductID, trustedKeys, {environment = 'live', allowHTTP=false, ...options} = {}) {
    return new Application({schema_version:1,merchant_origin:merchantOrigin,launch_product_id:productID,licentivo_product_id:licentivoProductID,environment,...(allowHTTP?{allow_http:allowHTTP}:{})},
      {base_url:'https://www.licentivo.com',product_id:licentivoProductID,trusted_keys:trustedKeys}, options);
  }
  #serial(action) { if(this.#callbacks.getStore()) return Promise.reject(new LaunchError('WORKFLOW_BUSY')); const result = this.#queue.then(action); this.#queue = result.catch(() => {}); return result; }
  #account(action,logout=false) {return this.#serial(async()=>{let unlock;if(logout&&this.#record?.token)try{await rememberIntent(this.#scope,this.#record.token);}catch{}try{unlock=await accountLock(this.#scope);return await action();}catch(error){if(logout&&!unlock&&this.#runtime)try{await (this.#runtime.logout?this.#runtime.logout():this.#runtime.deactivate());}catch{}if(logout||!unlock)await this.#stop();if(logout&&this.#record){this.#record.version=3;this.#record.logout_pending=true;}if(!unlock)throw new LaunchError('ACCOUNT_LOCK_BUSY');throw error;}finally{if(unlock)await unlock();}});}
  #open() { if (this.#closed) throw new LaunchError('APPLICATION_CLOSED'); }
  #cache(record) { return join(this.#root, hash(this.#scope), hash(record.user.id), 'lease.json'); }
  #valid(record) { return (record?.version === 2 || record?.version === 3 && record.logout_pending === true) && record.scope === this.#scope && typeof record.token === 'string' && !!record.token && !record.identityContext?.identity_token && (record.logout_pending === true || UUID.test(record.user?.id) && typeof record.expires_at === 'string' && Number.isFinite(Date.parse(record.expires_at)) && record.identityContext?.product_id === this.#config.launch.launch_product_id && record.identityContext.environment === this.#config.launch.environment); }
  async #save() { const saved=clone(this.#record); if(saved.identityContext) delete saved.identityContext.identity_token; await this.#store.save(this.#scope,saved); }
  async #call(path, method = 'GET', body, signal, token = this.#record?.token) {
    cancelled(signal);
    const headers = {'Accept':'application/json','Content-Type':'application/json'}; if (token) headers.Authorization = 'Bearer ' + token;
    const response = await this.#transport({url:this.#config.launch.merchant_origin + '/api/v1' + path, method, headers, body:body === undefined ? undefined : JSON.stringify(body), signal});
    cancelled(signal);
    if (!Number.isInteger(response.status) || typeof response.body !== 'string' || Buffer.byteLength(response.body) > LIMIT) throw new LaunchError('INVALID_RESPONSE');
    if (response.status >= 300 && response.status < 400) throw new LaunchError('REDIRECT_REJECTED', response.status);
    let data; try { data = JSON.parse(response.body); } catch { throw new LaunchError(response.status >= 500 && response.status <= 599 ? 'SERVICE_UNAVAILABLE' : 'INVALID_RESPONSE', response.status); }
    if (response.status !== 200) {
      const retry = response.headers?.['retry-after']; let seconds = 0;
      if (/^\d+$/.test(retry ?? '')) seconds = Number(retry); else if (retry && Number.isFinite(Date.parse(retry))) seconds = Math.max(0, Math.ceil((Date.parse(retry)-Date.now())/1000));
      throw new LaunchError(data?.error?.code ?? 'REQUEST_FAILED', response.status, Number.isFinite(seconds) ? seconds : 0);
    }
    if (!data?.data || typeof data.data !== 'object' || Array.isArray(data.data)) throw new LaunchError('INVALID_RESPONSE'); return data.data;
  }
  async #credential(signal) {
    const result=await this.#call('/software/products/'+this.#config.launch.launch_product_id+'/identity','POST',{environment:this.#config.launch.environment},signal);
    if(result.product_id!==this.#config.launch.launch_product_id || result.licentivo_product_id!==this.#config.product.product_id || result.environment!==this.#config.launch.environment || !UUID.test(result.launch_connection_id??'') || typeof result.identity_token!=='string' || result.identity_token.length<32 || result.identity_token.length>12288 || !(Date.parse(result.expires_at)>Date.now())) throw new LaunchError('IDENTITY_MISMATCH');
    return result;
  }
  async #start(identity,signal) {
    cancelled(signal); const path=this.#cache(this.#record);await mkdir(dirname(path),{recursive:true,mode:0o700});
    const runtime=await this.#factory({...clone(this.#config.product),license_key:'',identity_token:identity.identity_token??'',launch_connection_id:identity.launch_connection_id,environment:this.#config.launch.environment,cache_path:path});
    try{await runtime.start(()=>{},signal);cancelled(signal);this.#runtime=runtime;return {user:clone(this.#record.user),expiresAt:this.#record.expires_at};}catch(error){await runtime.close();throw error;}
  }
  async #stop() { const runtime=this.#runtime; this.#runtime=null; if(runtime) await runtime.close(); }
  async #clear(saved=this.#record) { try {await this.#stop();} finally {this.#record=null;this.#accountCleared=true;if(saved)await this.#deleteOwned(saved);} }
  async #saved() { let saved; try { saved=await this.#store.load(this.#scope); } catch(error) { await this.#clear(); throw error; } if(saved && !this.#valid(saved)) { await this.#clear(saved); throw new LaunchError('INVALID_SAVED_SESSION'); } return saved; }
  #queueKey(){return this.#scope+'\naccount-logout-queue-v1';}
  async #pending(){const queue=await this.#store.load(this.#queueKey());if(queue==null)return [];if(queue.version!==1||!Array.isArray(queue.records)||queue.records.length>64||queue.records.some(record=>!record.logout_pending||!this.#valid(record)))throw new LaunchError('INVALID_SAVED_SESSION');return queue.records;}
  async #updatePending(record,remove=false){const records=(await this.#pending()).filter(value=>value.token!==record.token);if(!remove){const value=clone(record);value.version=3;value.logout_pending=true;if(value.identityContext)delete value.identityContext.identity_token;records.push(value);}if(records.length>64||Buffer.byteLength(JSON.stringify({version:1,records}))>60000)throw new LaunchError('ACCOUNT_LOGOUT_QUEUE_FULL');if(records.length)await this.#store.save(this.#queueKey(),{version:1,records});else await this.#store.delete(this.#queueKey());}
  async #owns(record){const active=await this.#store.load(this.#scope);return !!record?.token&&active?.token===record.token;}
  async #deleteOwned(record){if(await this.#owns(record))await this.#store.delete(this.#scope);}
  async #mark(record){try{await rememberIntent(this.#scope,record.token);}catch{}let queueError,activeError;try{await this.#updatePending(record);}catch(error){queueError=error;}if(await this.#owns(record)){try{await this.#save();}catch(error){activeError=error;try{await this.#deleteOwned(record);}catch{}}if(!activeError||!queueError)return;}else if(!queueError)return;throw new LaunchError('ACCOUNT_LOGOUT_STORAGE_FAILED');}
  async #finish(record){await this.#deleteOwned(record);await this.#updatePending(record,true);await clearIntent(this.#scope,record.token);}
  async #retryOthers(skip,signal){for(const record of await this.#pending()){if(record.token===skip)continue;try{await this.#call('/auth/logout','POST',{},signal,record.token);}catch(error){if(error?.status!==401)throw new LaunchError('ACCOUNT_LOGOUT_PENDING');}await this.#finish(record);}}
  async #logout(signal) {
    const saved=this.#record??await this.#saved()??(await this.#pending())[0];let releaseError,persistError,otherError,accountConfirmed=true,deviceConfirmed=true;
    try{await this.#retryOthers(saved?.token,signal);}catch(error){otherError=error;}
    if(saved){saved.logout_pending=true;saved.version=3;this.#record=saved;try{await this.#mark(saved);}catch(error){persistError=error;}}
    try {
      const active=await this.#store.load(this.#scope);
      if(!this.#runtime && saved?.identityContext && (!active||active.token===saved.token)) this.#runtime=await this.#factory({...clone(this.#config.product),license_key:'',identity_token:'',launch_connection_id:saved.identityContext.launch_connection_id,environment:this.#config.launch.environment,cache_path:this.#cache(saved)});
      if(this.#runtime){const result=await (this.#runtime.logout?this.#runtime.logout(signal):this.#runtime.deactivate(signal));deviceConfirmed=result?.revocation_pending!==true;if(result?.storage_error)throw new LaunchError('SDK_LOGOUT_STORAGE_FAILED');}
    }catch(error){releaseError=error;}
    try{await this.#stop();}catch(error){releaseError??=error;}
    if(saved)try{await this.#call('/auth/logout','POST',{},signal,saved.token);}catch(error){accountConfirmed=error?.status===401;}
    if(accountConfirmed){this.#record=null;this.#accountCleared=true;if(saved)await this.#finish(saved);}
    if(!accountConfirmed&&persistError)throw persistError;if(releaseError)throw releaseError;if(otherError)throw otherError;return accountConfirmed&&deviceConfirmed;
  }
  login(email,password,{signal}={}) { return this.#account(async()=>{
    this.#open(); cancelled(signal); if(!await this.#logout(signal))throw new LaunchError('LOGOUT_PENDING');
    const grant=await this.#call('/auth/login','POST',{email,password,kind:'desktop'},signal);
    this.#record={version:2,scope:this.#scope,user:grant.user,token:grant.access_token,expires_at:grant.expires_at};
    this.#accountCleared=false;
    try {
      if (!UUID.test(grant.user?.id) || !/^[A-Za-z0-9_-]{43}$/.test(grant.access_token) || typeof grant.expires_at !== 'string' || !/(?:Z|[+-]\d\d:\d\d)$/.test(grant.expires_at) || !Number.isFinite(Date.parse(grant.expires_at)) || Date.parse(grant.expires_at)<=Date.now()) throw new LaunchError('INVALID_SESSION');
      const identity=await this.#credential(signal);this.#record.identityContext=Object.fromEntries(['product_id','licentivo_product_id','launch_connection_id','environment','expires_at'].map(key=>[key,identity[key]]));const state=await this.#start(identity,signal);await this.#save();return state;
    } catch(error) { await this.#logout(); throw error; }
  }); }
  restore({signal}={}) {return this.#account(async()=>{
    this.#open();cancelled(signal);await this.#stop();this.#record=this.#record?.logout_pending?this.#record:await this.#saved();const pending=await this.#pending();const task=pending.find(value=>!this.#record||value.token===this.#record.token);if(task)this.#record=task;if(!this.#record)return null;
    if(await hasIntent(this.#scope,this.#record.token)){this.#record.version=3;this.#record.logout_pending=true;}
    if(this.#record.logout_pending){if(!await this.#logout(signal))throw new LaunchError('ACCOUNT_LOGOUT_PENDING');return null;}
    this.#accountCleared=false;
    await this.#retryOthers(this.#record.token,signal);
    if(Date.parse(this.#record.expires_at)<=Date.now()){await this.#clear();throw new LaunchError('SESSION_EXPIRED');}
    let identity;try{const me=await this.#call('/me','GET',undefined,signal);if(typeof me.user?.id!=='string'||!me.user.id)throw new LaunchError('INVALID_RESPONSE');if(me.user.id!==this.#record.user.id)throw new LaunchError('SESSION_MISMATCH',401);identity=await this.#credential(signal);this.#record.user=me.user;}catch(error){if(denied(error))await this.#clear();throw error;}
    this.#record.identityContext=Object.fromEntries(['product_id','licentivo_product_id','launch_connection_id','environment','expires_at'].map(key=>[key,identity[key]]));
    try{const state=await this.#start(identity,signal);await this.#save();return state;}catch(error){await this.#clear();throw error;}
  });}
  runFeature(feature,action) { return this.#serial(async()=>{ this.#open(); if(!this.#runtime) throw new LaunchError('LOGIN_REQUIRED'); return this.#runtime.runFeature(feature,()=>this.#callbacks.run(true,action)); }); }
  logout({signal}={}) { return this.#account(async()=>{this.#open();if(this.#accountCleared&&!this.#record){await this.#retryOthers(undefined,signal);return true;}return this.#logout(signal);},true); }
  releaseDevice(options={}) {return this.logout(options).then(confirmed=>{if(!confirmed)throw new LaunchError('DEVICE_REVOCATION_PENDING');});}
  close() { return this.#serial(async()=>{if(this.#closed)return;this.#closed=true;await this.#stop();this.#record=null;}); }
}
