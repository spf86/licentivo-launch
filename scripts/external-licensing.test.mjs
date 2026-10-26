import test from 'node:test';
import assert from 'node:assert/strict';
import {externalLicensingForms,licensingSettingsPanel} from '../internal/httpapi/web/external-licensing.js';
import {settingsWorkspace,paymentSettingsPanel} from '../internal/httpapi/web/workspace.js';

test('external licensing settings preserve a real choice and escape untrusted metadata',()=>{
 const data={default_provider:'custom_http',connections:[],bindings:[],mappings:[]};
 const html=licensingSettingsPanel({licensing:[]},data,{products:[{id:'product',name:'<img src=x onerror=alert(1)>'}],plans:[]});
 assert.equal((html.match(/aria-pressed="true"/g)||[]).length,1);
 assert.match(html,/data-licensing-provider="licentivo"/);
 assert.match(html,/data-licensing-provider="custom_http"/);
 assert.ok(!html.includes('data-form="license-settings"'));
 assert.ok(!html.includes('<img src=x'));
 assert.match(html,/&lt;img/);
 assert.match(html,/\/api\/v1\/licensing\/protocol/);
});
test('editing a connection never repopulates its signing key',()=>{
 const connection={id:'saved',name:'Saved',environment:'sandbox',signing_key:'SHOULD-NEVER-APPEAR',activation_origins:[]};
 const html=externalLicensingForms({connections:[connection],bindings:[],mappings:[]},{products:[],plans:[]},{},connection);
 assert.ok(!html.includes('SHOULD-NEVER-APPEAR'));
 assert.match(html,/留空保留现有密钥/);
});
test('product mappings are selected by product, connection and environment',()=>{
 const html=externalLicensingForms({default_provider:'custom_http',connections:[{id:'sandbox',environment:'sandbox',name:'Sandbox',activation_origins:[]},{id:'live',environment:'live',name:'Live'}],bindings:[{product_id:'p',environment:'sandbox',provider:'custom_http',connection_id:'sandbox'}],mappings:[{version_id:'v',environment:'live',connection_id:'live',product_ref:'WRONG-LIVE-ROUTE',plan_ref:'wrong'},{version_id:'v',environment:'sandbox',connection_id:'sandbox',product_ref:'editor',plan_ref:'basic'}]},{products:[{id:'p',name:'Editor'}],plans:[{product_id:'p',version_id:'v',name:'Basic',version:1}]});
 assert.match(html,/type="hidden" name="provider" value="custom_http"/);
 assert.match(html,/value="editor"/);
 assert.ok(!html.includes('WRONG-LIVE-ROUTE'));
});

test('previewing another provider requires explicit activation and keeps the current provider visible',()=>{
 const data={default_provider:'licentivo',connections:[],bindings:[],mappings:[]},catalog={products:[],plans:[]};
 const current=licensingSettingsPanel({licensing:[]},data,catalog);
 assert.match(current,/data-form="license-settings"/);
 assert.ok(!current.includes('data-form="external-connection"'));
 const preview=licensingSettingsPanel({licensing:[]},data,catalog,{provider:'custom_http'});
 assert.match(preview,/当前启用：Licentivo/);
 assert.match(preview,/启用 自定义授权服务/);
 assert.match(preview,/data-external-form="provider"/);
 assert.ok(!preview.includes('data-form="external-binding"'));
 assert.ok(!preview.includes('data-form="license-settings"'));
});

test('both settings routes show one unified licensing tab',()=>{
 const html=settingsWorkspace({licensing:'UNIFIED-LICENSING'},'customlicensing');
 assert.equal((html.match(/href="#\/admin\/settings\/licensing"/g)||[]).length,1);
 assert.ok(!html.includes('href="#/admin/settings/customlicensing"'));
 assert.match(html,/data-settings-current="licensing">UNIFIED-LICENSING/);
});

test('payment settings open the active provider for the selected environment',()=>{
 const data={payments:[{provider:'stripe',environment:'sandbox',enabled:true},{provider:'paddle',environment:'sandbox',enabled:false},{provider:'paypal',environment:'live',enabled:true}]};
 const sandbox=paymentSettingsPanel(data);
 assert.match(sandbox,/name="provider" value="stripe"/);
 assert.match(sandbox,/只能启用一个支付供应商/);
 const live=paymentSettingsPanel(data,'live');
 assert.match(live,/name="provider" value="paypal"/);
 const inactive=paymentSettingsPanel(data,'sandbox','paddle');
 assert.match(inactive,/验证并启用/);
 assert.match(inactive,/已停用/);
});
