import test from 'node:test';
import assert from 'node:assert/strict';
import {artifactCard,aiPresets,groupAIProviders,uploadCard} from '../internal/httpapi/web/ai-workspace.js';

test('generated media can only link to authenticated first-party artifact routes',()=>{
 const base={filename:'<script>report.xlsx',size:100,mime:'image/png'};
 for(const url of ['javascript:alert(1)','https://evil.invalid/image','/api/v1/product-document-images/123'])assert.equal(artifactCard({...base,url}),'');
 const card=artifactCard({...base,url:'/api/v1/admin/ai/artifacts/12345678-1234-1234-1234-123456789abc'});
 assert.ok(card.includes('&lt;script&gt;report.xlsx'));assert.ok(!card.includes('<script>'));assert.ok(card.includes('?preview=1'));
});

test('chat and media providers are configured separately',()=>{
 assert.equal(new Set(aiPresets.map(p=>p.id)).size,aiPresets.length);
 assert.ok(aiPresets.some(p=>p.id==='deepseek'&&p.capability==='chat'));
 assert.ok(aiPresets.some(p=>p.protocol==='anthropic'));
 assert.ok(aiPresets.some(p=>p.id==='wan-video'&&p.protocol==='dashscope'&&p.capability==='video'));
 assert.ok(!aiPresets.some(p=>p.capability==='video'&&p.url.includes('openai.com')));
});

test('service accounts and discovered models retain their own selection identities',()=>{
 const common={enabled:true,capability:'chat',protocol:'openai',base_url:'https://api.example.com/v1',model:'default'};
 const groups=groupAIProviders([{...common,id:'one',name:'Office account',models:[{id:'chat',name:'Chat'},{id:'vision',name:'Vision'}]},{...common,id:'two',name:'Other account'},{...common,id:'disabled',enabled:false}], 'chat');
 assert.equal(groups.length,2);assert.equal(groups[0].id,'one');assert.equal(groups[0].models[1].id,'vision');assert.equal(groups[1].models[0].id,'default');
});

test('upload links escape labels and reject external destinations',()=>{
 const upload={filename:'<img onerror=bad>.csv',size:10,url:'/api/v1/admin/ai/uploads/12345678-1234-1234-1234-123456789abc'};
 assert.ok(uploadCard(upload).includes('&lt;img onerror=bad&gt;.csv'));assert.equal(uploadCard({...upload,url:'https://elsewhere.invalid/file'}),'');
});
