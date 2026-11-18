import test from 'node:test';
import assert from 'node:assert/strict';
import {renderPolicyBody,policyURL,policyLinks,policyNotice,policyEditorValue} from '../internal/httpapi/web/policy-content.js';
import {policySettingsPanel} from '../internal/httpapi/web/store-policies.js';
import {workspaceAccountMenu} from '../internal/httpapi/web/workspace.js';

test('published policy formatting supports headings, emphasis, lists and safe links',()=>{
 const html=renderPolicyBody('# Terms\n\n## Scope\nA **bold** word and *emphasis*.\n\n- One\n- Two\n\n1. First\n2. Second\n\n[Support](https://example.com/help)');
 assert.match(html,/<h1>Terms<\/h1>/);assert.match(html,/<h2>Scope<\/h2>/);
 assert.match(html,/<strong>bold<\/strong>/);assert.match(html,/<em>emphasis<\/em>/);
 assert.match(html,/<ul><li>One<\/li><li>Two<\/li><\/ul>/);
 assert.match(html,/<ol><li>First<\/li><li>Second<\/li><\/ol>/);
 assert.match(html,/href="https:\/\/example.com\/help"/);
 assert.equal(renderPolicyBody('# Terms\n\nBody','Terms'),'<p>Body</p>');
 assert.equal(renderPolicyBody('# Scope\n\nBody','Terms'),'<h1>Scope</h1><p>Body</p>');
});

test('policy body never executes raw HTML or unsafe link schemes',()=>{
 const html=renderPolicyBody('<img src=x onerror=alert(1)>\n\n<script>alert(1)</script>\n\n[bad](javascript:alert)\n[bad](data:text/html,evil)\n[ok](https://example.com/\"x)');
 assert.ok(!html.includes('<img'));assert.ok(!html.includes('<script'));
 assert.ok(!html.includes('href="javascript:'));assert.ok(!html.includes('href="data:'));
 assert.match(html,/&lt;img/);assert.match(html,/&quot;x/);
 for(const value of ['javascript:alert(1)','data:text/html,x','file:///etc/passwd','https://user:password@example.com','java\nscript:alert'])assert.equal(policyURL(value),'');
 assert.equal(policyURL('#/home'),'#/home');
});

test('only published store policies appear in public links',()=>{
 const product={slug:'editor',content:{terms_of_service:'old terms',refund_policy:'legacy refund',privacy_policy:''}};
 const links=policyLinks({policies:[{key:'terms',title:'Current terms',body:'published'}]},product);
 assert.deepEqual(links,[{key:'terms',title:'Current terms',href:'#/policies/terms'}]);
 assert.equal(policyLinks({policies:[]},product).length,0);
 assert.equal(policyNotice({policies:[]},null),'');
 assert.equal(policyNotice({policies:[]},product),'');
 const all=policyLinks({policies:['terms','refunds','privacy'].map(key=>({key,title:key,body:'Published'}))},product);
 assert.deepEqual(all.map(link=>link.href),['#/policies/terms','#/policies/refunds','#/policies/privacy']);
});

test('admin policy drafts escape titles and HTML in the editing and preview surface',()=>{
 const html=policySettingsPanel([{key:'terms',version:1,draft:{title:'<img src=x>',body:'<script>bad</script>'},published:null,has_draft_changes:true}]);
 assert.ok(!html.includes('<script>'));assert.ok(!html.includes('<img src=x>'));
 assert.match(html,/&lt;script&gt;/);assert.match(html,/&lt;img src=x&gt;/);
 assert.match(html,/contenteditable="true"/);assert.match(html,/data-policy-publish/);
 assert.match(html,/data-policy-preview-dialog/);
});

test('rich text serializes emphasis and safe links without including executable nodes',()=>{
 const text=value=>({nodeType:3,textContent:value});
 const element=(tag,children=[],attributes={})=>({nodeType:1,tagName:tag,childNodes:children,children:children.filter(node=>node.nodeType===1),getAttribute:name=>attributes[name]||null});
 const root=element('DIV',[element('H2',[text('Scope')]),element('P',[text('A '),element('STRONG',[text('bold')]),text(' word.')]),element('P',[element('A',[text('Support')],{href:'https://example.com'}),element('SCRIPT',[text('evil')])])]);
 assert.equal(policyEditorValue(root),'## Scope\n\nA **bold** word.\n\n[Support](https://example.com)');
 const list=element('DIV',[element('OL',[element('LI',[element('H2',[text('First')])]),element('LI',[element('P',[element('STRONG',[text('Second')])])])])]);
 assert.equal(policyEditorValue(list),'1. First\n2. **Second**');
});

test('account menu groups existing actions and escapes administrator identity',()=>{
 const html=workspaceAccountMenu({role:'admin',email:'<img src=x>@example.invalid'});
 assert.match(html,/aria-label="账号菜单"/);assert.match(html,/账号中心/);assert.match(html,/查看商店/);assert.match(html,/data-action="logout"/);
 assert.ok(!html.includes('<img src=x>'));assert.match(html,/&lt;img src=x&gt;/);
});

test('account menu receives its active locale without browser storage globals',()=>{
 const user={role:'admin',email:'owner@example.test'};
 assert.match(workspaceAccountMenu(user,'en'),/<option value="en" selected>English<\/option>/);
 assert.match(workspaceAccountMenu(user),/<option value="zh-CN" selected>简体中文<\/option>/);
});

 test("footer always offers all three store policy pages",()=>{
 const links=policyLinks({policies:[]},null,true);
 assert.deepEqual(links.map(row=>row.href),["#/policies/terms","#/policies/refunds","#/policies/privacy"]);
 assert.equal(policyNotice({policies:[]},null),"");
 });
