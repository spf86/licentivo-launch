import {createHash, createCipheriv, createDecipheriv, randomBytes, randomUUID} from 'node:crypto';
import {readFile, writeFile, mkdir, rename, rm, stat} from 'node:fs/promises';
import {resolve, join, dirname} from 'node:path';
import {homedir} from 'node:os';
import {setTimeout as delay} from 'node:timers/promises';
import {AsyncLocalStorage} from 'node:async_hooks';

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

function origin(value, environment) {
  let parsed; try { parsed = new URL(value); } catch { throw new LaunchError('INVALID_CONFIG'); }
  if (parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== '/' ||
      (parsed.protocol !== 'https:' && !(environment === 'sandbox' && parsed.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(parsed.hostname)))) throw new LaunchError('INVALID_CONFIG');
  return parsed.origin;
}
export function validateConfig(launch, product) {
  if (!launch || Object.keys(launch).some(k => !['schema_version','merchant_origin','launch_product_id','licentivo_product_id','environment'].includes(k)) || launch.schema_version !== 1 || !UUID.test(launch.launch_product_id) || !UUID.test(launch.licentivo_product_id) || !['sandbox','live'].includes(launch.environment)) throw new LaunchError('INVALID_CONFIG');
  if (!product || product.product_id !== launch.licentivo_product_id || Object.keys(product).some(k => !['product_id','trusted_keys','base_url','app_version','license_key','cache_path','device_name','allow_http'].includes(k)) || product.license_key || product.cache_path) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  if (['license_key','cache_path'].some(k => product[k] !== undefined && product[k] !== null && typeof product[k] !== 'string')) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  const keys = product.trusted_keys;
  if (product.app_version !== undefined && (typeof product.app_version !== 'string' || product.app_version.length > 80) || product.device_name !== undefined && (typeof product.device_name !== 'string' || product.device_name.length > 128) || product.allow_http !== undefined && typeof product.allow_http !== 'boolean') throw new LaunchError('PUBLIC_CONFIG_INVALID');
  if (!keys || typeof keys !== 'object' || Array.isArray(keys) || !Object.keys(keys).length || Object.keys(keys).length > 32 || Object.entries(keys).some(([id,key]) => !id || id.length > 128 || typeof key !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(key) || Buffer.from(key,'base64url').toString('base64url') !== key)) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  const runtimeOrigin = origin(product.base_url || 'https://www.licentivo.com', launch.environment);
  if (runtimeOrigin.startsWith('http:') && product.allow_http !== true) throw new LaunchError('PUBLIC_CONFIG_INVALID');
  return {launch: {...clone(launch), merchant_origin: origin(launch.merchant_origin, launch.environment)}, product: {product_id: product.product_id, trusted_keys: clone(keys), base_url: runtimeOrigin, ...(product.app_version ? {app_version: product.app_version} : {}), ...(product.allow_http === true ? {allow_http:true} : {})}};
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
const denied = error => error instanceof LaunchError && error.code !== 'ENTITLEMENT_PENDING' && ([401,403,404].includes(error.status) || ['SESSION_EXPIRED','SESSION_MISMATCH','ENTITLEMENT_EXPIRED','ENTITLEMENT_SUSPENDED','ENTITLEMENT_NOT_PURCHASED'].includes(error.code));
const unavailable = error => error instanceof LaunchError && (error.code === 'NETWORK_UNAVAILABLE' || error.status >= 500 && error.status <= 599);

export class Application {
  #config; #store; #transport; #factory; #root; #scope; #record = null; #runtime = null; #closed = false; #queue = Promise.resolve(); #wait; #attempts; #callbacks = new AsyncLocalStorage();
  constructor(launch, product, options = {}) {
    this.#config = validateConfig(launch, product); this.#store = options.store ?? new MemoryStore();
    if (this.#store.persistent !== false && (this.#store.persistent !== true || this.#store.secure !== true)) throw new LaunchError('SECURE_STORE_REQUIRED');
    this.#transport = options.transport ?? defaultTransport;
    this.#factory = options.runtimeFactory ?? (async config => { const {Client} = await import('./vendor/licentivo.mjs'); return new Client(config); });
    this.#root = resolve(options.cacheDir ?? join(homedir(), '.cache', 'LicentivoLaunch'));
    this.#scope = ['launch-session-v1',this.#config.launch.merchant_origin,launch.launch_product_id,launch.licentivo_product_id,launch.environment].join('\n');
    this.#wait = options.sleep ?? (async (ms, signal) => { try { await delay(ms, undefined, {signal}); } catch { cancelled(signal); throw new LaunchError('CANCELLED'); } });
    this.#attempts = options.pendingAttempts ?? 5;
    if (!Number.isInteger(this.#attempts) || this.#attempts < 1 || this.#attempts > 5) throw new LaunchError('INVALID_RETRY_POLICY');
  }
  static async fromFiles(launchPath, productPath, options) { const {launch,product} = await loadConfig(launchPath,productPath); return new Application(launch,product,options); }
  static connect(merchantOrigin, productID, licentivoProductID, trustedKeys, {environment = 'live', ...options} = {}) {
    return new Application({schema_version:1,merchant_origin:merchantOrigin,launch_product_id:productID,licentivo_product_id:licentivoProductID,environment},
      {base_url:'https://www.licentivo.com',product_id:licentivoProductID,trusted_keys:trustedKeys}, options);
  }
  #serial(action) { if(this.#callbacks.getStore()) return Promise.reject(new LaunchError('WORKFLOW_BUSY')); const result = this.#queue.then(action); this.#queue = result.catch(() => {}); return result; }
  #open() { if (this.#closed) throw new LaunchError('APPLICATION_CLOSED'); }
  #cache(record) { return join(this.#root, hash(this.#scope), hash(record.user.id), 'lease.json'); }
  #valid(record) { return record?.version === 1 && record.scope === this.#scope && UUID.test(record.user?.id) && /^[A-Za-z0-9_-]{43}$/.test(record.token) && typeof record.expires_at === 'string' && /(?:Z|[+-]\d\d:\d\d)$/.test(record.expires_at) && Number.isFinite(Date.parse(record.expires_at)) && record.credential?.product_id === this.#config.launch.launch_product_id && typeof record.credential.license_key === 'string' && !!record.credential.license_key; }
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
    if (!data?.data || typeof data.data !== 'object') throw new LaunchError('INVALID_RESPONSE'); return data.data;
  }
  async #credential(signal) {
    for (let attempt = 1; ; attempt++) {
      try {
        const result = await this.#call('/software/products/' + this.#config.launch.launch_product_id + '/credential', 'POST', {environment:this.#config.launch.environment}, signal);
        if (result.product_id !== this.#config.launch.launch_product_id || typeof result.license_key !== 'string' || !result.license_key || !Number.isInteger(result.version) || result.version < 1) throw new LaunchError('CREDENTIAL_MISMATCH'); return result;
      } catch (error) {
        if (!(error instanceof LaunchError) || error.status !== 403 || error.code !== 'ENTITLEMENT_PENDING' || attempt >= this.#attempts || error.retryAfterSeconds > 30) throw error;
        await this.#wait(Math.max(3,error.retryAfterSeconds)*1000,signal); cancelled(signal);
      }
    }
  }
  async #start(offline, signal) {
    cancelled(signal); const path = this.#cache(this.#record); await mkdir(dirname(path), {recursive:true,mode:0o700});
    const runtime = await this.#factory({...clone(this.#config.product),license_key:this.#record.credential.license_key,cache_path:path});
    try {
      cancelled(signal);
      if (offline) { const state = await runtime.status(); if (state?.allowed !== true || state.state !== 'offline_valid') throw new LaunchError('OFFLINE_UNAVAILABLE'); }
      await runtime.start(() => {}, signal); cancelled(signal); this.#runtime = runtime;
      return {user:clone(this.#record.user), expiresAt:this.#record.expires_at, offlineFallback:offline};
    } catch (error) { await runtime.close(); throw error; }
  }
  async #stop() { const runtime=this.#runtime; this.#runtime=null; if(runtime) await runtime.close(); }
  async #clear(record=this.#record) {
    try { await this.#stop(); } finally {
      this.#record=null;
      try { await this.#store.delete(this.#scope); } finally { if (UUID.test(record?.user?.id)) await rm(dirname(this.#cache(record)),{recursive:true,force:true}); }
    }
  }
  async #saved() { let saved; try { saved=await this.#store.load(this.#scope); } catch(error) { await this.#clear(); throw error; } if(saved && !this.#valid(saved)) { await this.#clear(saved); throw new LaunchError('INVALID_SAVED_SESSION'); } return saved; }
  async #logout(signal) {
    const saved=this.#record ?? await this.#saved(); await this.#clear(saved);
    if (!saved) return true;
    try { await this.#call('/auth/logout','POST',{},signal,saved.token); return true; } catch { return false; }
  }
  login(email,password,{signal}={}) { return this.#serial(async()=>{
    this.#open(); cancelled(signal); await this.#logout(signal);
    const grant=await this.#call('/auth/login','POST',{email,password,kind:'desktop'},signal);
    this.#record={version:1,scope:this.#scope,user:grant.user,token:grant.access_token,expires_at:grant.expires_at};
    try {
      if (!UUID.test(grant.user?.id) || !/^[A-Za-z0-9_-]{43}$/.test(grant.access_token) || typeof grant.expires_at !== 'string' || !/(?:Z|[+-]\d\d:\d\d)$/.test(grant.expires_at) || !Number.isFinite(Date.parse(grant.expires_at)) || Date.parse(grant.expires_at)<=Date.now()) throw new LaunchError('INVALID_SESSION');
      this.#record.credential=await this.#credential(signal); const state=await this.#start(false,signal); await this.#store.save(this.#scope,this.#record); return state;
    } catch(error) { await this.#logout(); throw error; }
  }); }
  restore({signal}={}) { return this.#serial(async()=>{
    this.#open(); cancelled(signal); await this.#stop(); this.#record=await this.#saved(); if(!this.#record) return null;
    if(Date.parse(this.#record.expires_at)<=Date.now()) {await this.#clear(); throw new LaunchError('SESSION_EXPIRED');}
    try {
      const me=await this.#call('/me','GET',undefined,signal);
      if(me.user?.id!==this.#record.user.id) throw new LaunchError('SESSION_MISMATCH',401);
      this.#record.credential=await this.#credential(signal); this.#record.user=me.user;
    } catch(error) { cancelled(signal); if(unavailable(error)) return this.#start(true,signal); if(denied(error)) await this.#clear(); throw error; }
    try { const state=await this.#start(false,signal); await this.#store.save(this.#scope,this.#record); return state; }
    catch(error) { await this.#clear(); throw error; }
  }); }
  runFeature(feature,action) { return this.#serial(async()=>{ this.#open(); if(!this.#runtime) throw new LaunchError('LOGIN_REQUIRED'); return this.#runtime.runFeature(feature,()=>this.#callbacks.run(true,action)); }); }
  logout({signal}={}) { return this.#serial(async()=>{this.#open();return this.#logout(signal);}); }
  releaseDevice({signal}={}) { return this.#serial(async()=>{this.#open();cancelled(signal);if(!this.#runtime) throw new LaunchError('LOGIN_REQUIRED'); try {await this.#runtime.deactivate(signal);} finally {await this.#logout(signal);} }); }
  close() { return this.#serial(async()=>{if(this.#closed)return;this.#closed=true;await this.#stop();this.#record=null;}); }
}
