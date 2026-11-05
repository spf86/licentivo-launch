import test from 'node:test';
import assert from 'node:assert/strict';
import {salesCSV,salesCSVCell} from '../internal/httpapi/web/sales-analytics.js';
import {keywordList,seoPreview,seoKeywordExamples} from '../internal/httpapi/web/seo-settings.js';

test('sales export escapes spreadsheet formulas and retains integer minor units',()=>{
 assert.equal(salesCSVCell('=HYPERLINK("https://invalid")'),'"\'=HYPERLINK(""https://invalid"")"');
 assert.equal(salesCSVCell(-1200),'"-1200"');
 const report={query:{from:'2026-11-01',to:'2026-11-05',time_zone:'UTC',currency:'USD',environment:'live'},metrics:{gross_minor:12345,net_minor:12000,orders:2,customers:1,average_minor:6172,refund_minor:345,reversal_minor:0},days:[],products:[{name:'=cmd()',orders:2,gross_minor:12345,net_minor:12000,refund_minor:345,reversal_minor:0}],plans:[],channels:[]};
 const csv=salesCSV(report);assert.ok(csv.includes('12345'));assert.ok(csv.includes("'=cmd()"));assert.ok(csv.includes('minor units'));
});

test('SEO previews and keyword examples follow the selected language',()=>{
 const locales=['zh-CN','en','ja','ko','de','fr','es','pt-BR','it','ru'];
 assert.equal(new Set(locales.map(seoKeywordExamples)).size,locales.length);
 assert.ok(seoKeywordExamples('ja').includes('ライセンス'));
 const view={locale:'ja',public_url:'https://store.example',brand:{name:'Shop'},defaults:{keywords:['日本向け']}};
 assert.deepEqual(seoPreview(view,{}).keywords,['日本向け']);
 assert.deepEqual(seoPreview(view,{keywords:['正規ライセンス']}).keywords,['正規ライセンス']);
 assert.equal(seoPreview(view,{}).url,'https://store.example/ja/');
});
test('SEO previews use per-page overrides then product and global defaults',()=>{
 const view={locale:'en',public_url:'https://store.example',brand:{name:'My Store',description:'Store description'},product:{name:'My App',slug:'my-app',content:{summary:'App summary'}},defaults:{product_title_template:'{product} | {brand}',description:'Fallback'}};
 const preview=seoPreview(view,{});assert.equal(preview.title,'My App | My Store');assert.equal(preview.description,'App summary');assert.equal(preview.url,'https://store.example/en/products/my-app');
 assert.equal(seoPreview(view,{title:'Custom',canonical_path:'/en/app'}).url,'https://store.example/en/app');
 assert.deepEqual(keywordList('export, reports， export\nsoftware'),['export','reports','software']);
});
