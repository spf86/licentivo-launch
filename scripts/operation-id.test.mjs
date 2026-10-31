import test from 'node:test';
import assert from 'node:assert/strict';
import {webcrypto} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {operationID} from '../internal/httpapi/web/operation-id.js';

test('HTTP test origins without randomUUID generate distinct RFC 4122 version 4 IDs',()=>{
 const httpCrypto={getRandomValues:bytes=>webcrypto.getRandomValues(bytes)};
 const ids=Array.from({length:256},()=>operationID(httpCrypto));
 assert.equal(new Set(ids).size,ids.length);
 for(const id of ids)assert.match(id,/^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/);
});

test('secure contexts may use the native UUID implementation',()=>{
 const id='12345678-1234-4234-8234-123456789abc';
 assert.equal(operationID({randomUUID:()=>id,getRandomValues:()=>assert.fail('unexpected fallback')}),id);
});

test('missing secure randomness fails rather than generating predictable operation IDs',()=>{
 assert.throws(()=>operationID({}),/Secure random/);
});

test('browser workflows use the HTTP-compatible operation ID helper',()=>{
 const app=readFileSync(new URL('../internal/httpapi/web/app.js',import.meta.url),'utf8');
 assert.doesNotMatch(app,/crypto\.randomUUID/);
 assert.match(app,/name="idempotency_key" value="\$\{operationID\(\)\}"/);
});
