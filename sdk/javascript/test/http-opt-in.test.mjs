import test from 'node:test';
import assert from 'node:assert/strict';
import {validateConfig} from '../src/index.mjs';

test('remote merchant HTTP requires sandbox opt-in without weakening the licensing origin',()=>{
 const launch={schema_version:1,merchant_origin:'http://customer-test-server:18080',launch_product_id:'11111111-1111-4111-8111-111111111111',licentivo_product_id:'22222222-2222-4222-8222-222222222222',environment:'sandbox'};
 const product={product_id:launch.licentivo_product_id,base_url:'https://www.licentivo.com',trusted_keys:{test:'A'.repeat(43)}};
 assert.throws(()=>validateConfig(launch,product));
 assert.equal(validateConfig({...launch,allow_http:true},product).launch.merchant_origin,launch.merchant_origin);
 assert.throws(()=>validateConfig({...launch,allow_http:true,environment:'live'},product));
 assert.throws(()=>validateConfig({...launch,allow_http:true},{...product,base_url:'http://licentivo.example'}));
});
