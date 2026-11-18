import test from 'node:test';
import assert from 'node:assert/strict';
import {renderDocument,documentURL,documentHeadings,publishedArticles,editorMarkdown} from '../internal/httpapi/web/document-content.js';

test('documents support arbitrary headings, code, tables, images and safe links',()=>{
 const html=renderDocument('## Intro\n\n**Hello**\n\n| A | B |\n| --- | --- |\n| One | Two |\n\n```\n<img src=x>\n```\n\n![Screenshot](/api/v1/product-document-images/abc-123)\n[Guide](https://example.com/help)');
 assert.match(html,/<h2 id="section-1">Intro<\/h2>/);assert.match(html,/<table>/);assert.match(html,/<strong>Hello<\/strong>/);assert.match(html,/<pre><code>&lt;img/);assert.match(html,/src="\/api\/v1\/product-document-images\/abc-123"/);assert.match(html,/href="https:\/\/example.com\/help"/);
 assert.deepEqual(documentHeadings('```\n## Hidden\n```\n## Actual'),[{id:'section-1',title:'Actual'}]);
});
test('documents do not execute raw HTML, injected attributes or unsafe URL schemes',()=>{
 const html=renderDocument('<script>bad</script>\n[bad](javascript:alert)\n![bad](data:text/html,evil)\n![x\" onerror=bad](https://example.com/x)');
 assert.ok(!html.includes('<script>'));assert.ok(!html.includes('href="javascript:'));assert.ok(!html.includes('src="data:'));assert.match(html,/&quot; onerror=bad/);
 for(const url of ['//evil.test','javascript:alert(1)','https://user:pass@example.com','https://example.com/\nx'])assert.equal(documentURL(url),'');
});
test('public reader consumes only the published document projection',()=>{
 assert.deepEqual(publishedArticles({content:{installation_guide:'legacy'},document_groups:[]}),[]);
 const rows=publishedArticles({document_groups:[{title:'Custom',articles:[{title:'Public',slug:'public',body:'Published'}]}]});assert.equal(rows[0].group,'Custom');assert.equal(rows[0].body,'Published');
});
test('editor serializes tables, code and images without allowing executable nodes',()=>{
 const text=value=>({nodeType:3,textContent:value});
 const el=(tag,children=[],attrs={})=>({nodeType:1,tagName:tag.toUpperCase(),childNodes:children,textContent:children.map(c=>c.textContent).join(''),getAttribute:key=>attrs[key]});
 const root=el('div',[el('h2',[text('Heading')]),el('pre',[el('code',[text('a < b')])]),el('p',[el('img',[],{src:'/api/v1/product-document-images/abc',alt:'Image'})]),el('script',[text('bad')])]);
 const md=editorMarkdown(root);assert.match(md,/## Heading/);assert.match(md,/```\na < b\n```/);assert.match(md,/!\[Image\]/);assert.ok(!md.includes('bad'));
});
