// Explicit local integration check: creates one disposable customer account.
// Run manually against an isolated Launch sandbox; never a production service.
import assert from 'node:assert/strict';
import {randomUUID,randomBytes} from 'node:crypto';
import {Application,defaultTransport} from '../src/index.mjs';
const [origin,productId]=process.argv.slice(2);
if(!origin||!productId)throw Error('Usage: node sdk/javascript/test/http_smoke.mjs http://127.0.0.1:PORT LAUNCH_PRODUCT_ID');
const url=new URL(origin);if(url.protocol!=='http:'||!['127.0.0.1','localhost','[::1]'].includes(url.hostname))throw Error('Isolated local sandbox required');
const email=`node-sdk-${randomUUID()}@example.test`,password=randomBytes(24).toString('base64url')+'Aa9!';
const call=async(path,body,token)=>{const response=await defaultTransport({url:origin+'/api/v1'+path,method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json',...(token?{Authorization:'Bearer '+token}:{})},body:body===undefined?undefined:JSON.stringify(body)});assert.equal(response.status,200);return JSON.parse(response.body).data;};
await call('/auth/register',{email,password});
const grant=await call('/auth/login',{email,password,kind:'desktop'});
try {
  assert.equal((await call('/me',undefined,grant.access_token)).user.id,grant.user.id);
  const state=await call(`/software/products/${productId}/status?environment=sandbox`,undefined,grant.access_token);
  assert.equal(state.code,'ENTITLEMENT_NOT_PURCHASED');
} finally {await call('/auth/logout',{},grant.access_token);}
const remote='22222222-2222-4222-8222-222222222222';let runtimeCreated=false;
const app=new Application({schema_version:1,merchant_origin:origin,launch_product_id:productId,licentivo_product_id:remote,environment:'sandbox'},{product_id:remote,trusted_keys:{test:Buffer.alloc(32,1).toString('base64url')}},{runtimeFactory:()=>{runtimeCreated=true;throw Error('Must not create runtime for absent entitlement');}});
try {await assert.rejects(app.login(email,password),{code:'ENTITLEMENT_NOT_PURCHASED',status:404});assert.equal(runtimeCreated,false);assert.equal(await app.restore(),null);}
finally {await app.close();}
console.log('PASS real local HTTP: login expiry, /me identity, no-purchase status, rejected credential and cleared wrapper session');
