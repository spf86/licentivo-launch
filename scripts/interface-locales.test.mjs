import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import test from 'node:test';
import {interfaceLanguages,interfaceLanguage,prepareInterfaceLocale,interfaceText} from '../internal/httpapi/web/interface-locale.js';
import {changePublicLanguage,migratePublicHash} from '../internal/httpapi/web/public-routing.js';

const directory=new URL('../internal/httpapi/web/interface-locales/',import.meta.url);
const markers=value=>[...value.matchAll(/\{(?:\d+|[a-z_]+)\}/g)].map(match=>match[0]).sort();
const originalFetch=globalThis.fetch;
test.after(()=>{globalThis.fetch=originalFetch;});
globalThis.fetch=async url=>{
 const filename=String(url).split('/').at(-1);
 return {ok:true,json:async()=>JSON.parse(await fs.readFile(new URL(filename,directory),'utf8'))};
};

test('functional interface catalogs cover fourteen languages and retain placeholders',async()=>{
 assert.equal(interfaceLanguages.length,14);
 const required=['初始化代码','查看授权交付','受保护功能名','授权连接编号','商家内容暂时显示原文。','{count} 个月'];
 for(const language of interfaceLanguages){
  if(language.catalog==='zh')continue;
  const values=JSON.parse(await fs.readFile(new URL(language.catalog+'.json',directory),'utf8'));
  for(const key of required)assert.ok(values[key],language.code+': '+key);
  for(const [source,value]of Object.entries(values))assert.deepEqual(markers(value),markers(source),language.code+': '+source);
 }
});

test('requested interface language persists independently from merchant content and leaves unknown values alone',async()=>{
 await prepareInterfaceLocale('ar');await prepareInterfaceLocale('de');
 assert.equal(interfaceLanguage('ar').catalog,'ar');
 assert.notEqual(interfaceText('查看授权交付','ar'),'查看授权交付');
 assert.notEqual(interfaceText('查看授权交付','de'),'查看授权交付');
 assert.equal(interfaceText('Studio 专业版','de'),'Studio 专业版');
 assert.equal(interfaceText('查看授权交付','zh-CN'),'查看授权交付');
});

test('policy check messages keep product identifiers and version numbers during localization',async()=>{
 await prepareInterfaceLocale('de');
 const message=interfaceText('套餐 studio-pro：Licentivo 产品与策略版本 3 已在线验证','de');
 assert.ok(message.includes('studio-pro'));assert.ok(message.includes('3'));
 assert.ok(!/[\u3400-\u9fff]/.test(message));
});

test('public language changes preserve sandbox and clear stale interface overrides on native routes',()=>{
 const originalLocation=globalThis.location,originalHistory=globalThis.history;
 let next='';
 try{
  globalThis.location={pathname:'/en/products/studio',search:'?preview=sandbox&ui_locale=ar',hash:'',assign:value=>next=value,replace:value=>next=value};
  assert.equal(changePublicLanguage('de'),true);assert.equal(next,'/de/products/studio?preview=sandbox');
  assert.equal(changePublicLanguage('ur'),true);assert.equal(next,'/?preview=sandbox&ui_locale=ur#/product/studio');
  globalThis.location={...globalThis.location,pathname:'/',search:'?preview=sandbox&ui_locale=ar',hash:'#/product/studio'};
  globalThis.history={replaceState:(_state,_unused,value)=>next=value};
  assert.equal(changePublicLanguage('ur'),false);assert.equal(next,'/?preview=sandbox&ui_locale=ur#/product/studio');
  assert.equal(migratePublicHash('en'),true);assert.equal(next,'/en/products/studio?preview=sandbox');
 }finally{globalThis.location=originalLocation;globalThis.history=originalHistory;}
});
