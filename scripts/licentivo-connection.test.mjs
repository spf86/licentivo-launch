import test from 'node:test';
import assert from 'node:assert/strict';
import {licensingSettingsPanel} from '../internal/httpapi/web/external-licensing.js';

test('catalog reads never imply a working account connection and failure details are escaped',()=>{
 const license={catalog:{products:{items:[{id:'p'}]},policies:{items:[]}},account_protocol_ready:false,last_check_error:'建立 sandbox 账号连接失败：此 Key 未允许 HTTP <script>bad()</script>'};
 const connections={licentivo_url:'https://www.licentivo.com',licensing:[license]},data={default_provider:'licentivo',connections:[],bindings:[],mappings:[]};
 const failed=licensingSettingsPanel(connections,data,{products:[],plans:[]});
 assert.match(failed,/已读取 1 个产品/);
 assert.match(failed,/连接失败/);
 assert.match(failed,/此 Key 未允许 HTTP &lt;script&gt;/);
 assert.ok(!failed.includes('<script>'));
 assert.match(failed,/重新连接并读取策略/);
 assert.ok(!failed.includes('connection-state connected'));
 license.account_protocol_ready=true;license.last_check_error=null;
 const ready=licensingSettingsPanel(connections,data,{products:[],plans:[]});
 assert.match(ready,/账号授权连接已建立/);
 assert.match(ready,/connection-state connected/);
 assert.ok(!ready.includes('连接失败'));
});
