export interface LaunchConfig {
  schema_version: 1; merchant_origin: string; launch_product_id: string;
  licentivo_product_id: string; environment: 'sandbox' | 'live';
}
export interface PublicProductConfig {
  product_id: string; trusted_keys: Record<string, string>; base_url?: string;
  app_version?: string; allow_http?: boolean;
}
export interface User {id: string; email?: string; display_name?: string; [key: string]: unknown}
export interface SessionState {user: User; expiresAt: string; offlineFallback: boolean}
/** Secret record: never log it or serialize it outside an encrypted store. */
export interface SavedSession {
  version: 1; scope: string; user: User; token: string; expires_at: string;
  credential: {product_id: string; license_key: string; version: number};
}
export interface SessionStore {
  readonly persistent: boolean; readonly secure?: boolean;
  load(scope: string): Promise<SavedSession | null>;
  save(scope: string, value: SavedSession): Promise<void>;
  delete(scope: string): Promise<void>;
}
export class MemoryStore implements SessionStore {
  readonly persistent: false;
  load(scope: string): Promise<SavedSession | null>;
  save(scope: string, value: SavedSession): Promise<void>;
  delete(scope: string): Promise<void>;
}
export class EncryptedFileStore implements SessionStore {
  readonly persistent: true; readonly secure: true;
  constructor(directory: string, key: Uint8Array);
  load(scope: string): Promise<SavedSession | null>;
  save(scope: string, value: SavedSession): Promise<void>;
  delete(scope: string): Promise<void>;
}
export class LaunchError extends Error {
  readonly code: string; readonly status: number; readonly retryAfterSeconds: number;
  constructor(code: string, status?: number, retryAfterSeconds?: number);
}
export interface Request {
  url: string; method: string; headers: Record<string, string>; body?: string; signal?: AbortSignal;
}
export interface Response {status: number; headers: Record<string, string>; body: string}
export interface Runtime {
  /** Must be the official SDK's cryptographically verified status. */
  status(): {allowed: boolean; state: string} | Promise<{allowed: boolean; state: string}>;
  start(onError?: (error: unknown) => void, signal?: AbortSignal): Promise<unknown>;
  runFeature<T>(name: string, action: () => T | Promise<T>): Promise<T>;
  deactivate(signal?: AbortSignal): Promise<unknown>;
  close(): Promise<unknown>;
}
export interface Options {
  store?: SessionStore; cacheDir?: string; pendingAttempts?: number;
  transport?: (request: Request) => Promise<Response>;
  runtimeFactory?: (config: PublicProductConfig & {license_key: string; cache_path: string}) => Runtime | Promise<Runtime>;
  sleep?: (milliseconds: number, signal?: AbortSignal) => Promise<void>;
}
export class Application {
  static connect(merchantOrigin: string, productID: string, licentivoProductID: string, trustedKeys: Record<string, string>, options?: Options & {environment?: 'sandbox' | 'live'}): Application;
  constructor(launch: LaunchConfig, product: PublicProductConfig, options?: Options);
  static fromFiles(launchPath: string, productPath: string, options?: Options): Promise<Application>;
  login(email: string, password: string, options?: {signal?: AbortSignal}): Promise<SessionState>;
  restore(options?: {signal?: AbortSignal}): Promise<SessionState | null>;
  runFeature<T>(name: string, action: () => T | Promise<T>): Promise<T>;
  /** Local state is always removed; false means remote logout was not confirmed. */
  logout(options?: {signal?: AbortSignal}): Promise<boolean>;
  releaseDevice(options?: {signal?: AbortSignal}): Promise<void>;
  close(): Promise<void>;
}
export function validateConfig(launch: LaunchConfig, product: PublicProductConfig): {launch: LaunchConfig; product: PublicProductConfig};
export function loadConfig(launchPath: string, productPath: string): Promise<{launch: LaunchConfig; product: PublicProductConfig}>;
export function defaultTransport(request: Request): Promise<Response>;
