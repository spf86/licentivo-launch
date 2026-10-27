// Reviewed prose is translated; protocol fields and executable examples are shared.
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

const root=path.resolve(import.meta.dirname,'..'),check=process.argv.includes('--check');
const siteIndex=process.argv.indexOf('--site'),site=siteIndex<0?null:path.resolve(process.argv[siteIndex+1]);
const source=path.join(root,'docs/http-testing/translations.json');
const catalog=JSON.parse(fs.readFileSync(source,'utf8'));
export const locales=[
 ['en','English','en','http-testing.md'],['de','Deutsch','de','http-testing.de.md'],
 ['it','Italiano','it','http-testing.it.md'],['zh','简体中文','zh-Hans','http-testing.zh-CN.md'],
 ['zh-hant','繁體中文','zh-Hant','http-testing.zh-hant.md'],['hi','हिन्दी','hi','http-testing.hi.md'],
 ['es','Español','es','http-testing.es.md'],['ar','العربية','ar','http-testing.ar.md'],
 ['fr','Français','fr','http-testing.fr.md'],['bn','বাংলা','bn','http-testing.bn.md'],
 ['pt','Português','pt','http-testing.pt.md'],['id','Bahasa Indonesia','id','http-testing.id.md'],
 ['ur','اردو','ur','http-testing.ur.md'],['ja','日本語','ja','http-testing.ja.md']
].map(([code,name,tag,file])=>({code,name,tag,file,dir:['ar','ur'].includes(code)?'rtl':'ltr'}));
assert.equal(catalog.schema_version,1);
assert.deepEqual(Object.keys(catalog.locales).sort(),locales.map(l=>l.code).sort(),'Exactly the 14 supported languages must be present');
const sourceHash=createHash('sha256').update(JSON.stringify(catalog.locales.en)).digest('hex');
if(!catalog.source_sha256&&!check){
 catalog.source_sha256=sourceHash;
 fs.writeFileSync(source,JSON.stringify(catalog,null,2)+'\n');
}
assert.equal(catalog.source_sha256,sourceHash,'English source changed: review all 14 translations and update source_sha256 together');
function shape(value,reference,label){
 if(Array.isArray(reference)){
  assert.ok(Array.isArray(value),label);assert.equal(value.length,reference.length,label+' entries');
  value.forEach((entry,i)=>shape(entry,reference[i],label+'['+i+']'));
 }else if(reference&&typeof reference==='object'){
  assert.deepEqual(Object.keys(value).sort(),Object.keys(reference).sort(),label+' fields');
  for(const key of Object.keys(reference))shape(value[key],reference[key],label+'.'+key);
 }else{
  assert.equal(typeof value,'string',label);assert.ok(value.trim(),label+' is empty');
  assert.ok(!value.includes('|')&&!value.includes('\n'),label+' must be one Markdown unit');
  assert.ok(!/ZXQ\d+QXZ|TODO|TRANSLATE_ME/.test(value),label+' contains an unfinished translation');
 }
}
const technical=[
 ['Go','launchclient.Config{Environment: "sandbox", AllowHTTP: true}'],
 ['Python','Application.connect(..., environment="sandbox", allow_http=True)'],
 ['JavaScript / TypeScript','Application.connect(..., {environment: "sandbox", allowHTTP: true})'],
 ['C#','Application.Connect(..., "sandbox", allowHttp: true)'],
 ['Java','Launch.connect(..., "sandbox", true)'],
 ['Rust','Application::connect_with_http(..., "sandbox", true)'],
 ['Ruby',"Application.connect(..., environment: 'sandbox', allow_http: true)"],
 ['C','launch_application_connect_http(..., "sandbox", 1, 0, NULL)'],
 ['C++','Application::ConnectHTTP(..., "sandbox")']
];
const errorCodes=['LAUNCH_HTTPS_REQUIRED','LICENSE_KEY_INVALID','LICENSE_KEY_FORBIDDEN','SDK_HTTP_DISABLED','SDK_HTTPS_REQUIRED','LICENTIVO_TIMEOUT / LICENTIVO_NETWORK_ERROR','LICENTIVO_TLS_ERROR'];
const example={schema_version:1,merchant_origin:'http://192.168.1.20:18080',launch_product_id:'11111111-1111-4111-8111-111111111111',licentivo_product_id:'22222222-2222-4222-8222-222222222222',environment:'sandbox',allow_http:true};
const table=rows=>rows.map((row,i)=>'| '+row.join(' | ')+' |'+(i===0?'\n| '+row.map(()=>'---').join(' | ')+' |':'')).join('\n');
function render(l){return [
 '# '+l.title,'['+l.languages+'](http-testing/README.md)',l.intro,l.protocol,
 '## '+l.switches,table([l.switchHeaders,l.apiSwitch,l.sdkSwitch]),l.scope,
 '## '+l.setup,l.steps.map((text,i)=>(i+1)+'. '+text).join('\n'),l.network,
 '## '+l.selection,l.configIntro,'```json\n'+JSON.stringify(example,null,2)+'\n```',l.configMeaning,
 table([l.sdkHeaders,...technical.map(([language,code])=>[language,'`'+code+'`'])]),l.sdkScope,
 '## '+l.production,l.risk,l.migration,l.closing,
 '## '+l.failures,l.diagnostics,table([l.errorHeaders,...errorCodes.map((code,i)=>[code.split(' / ').map(v=>'`'+v+'`').join(' / '),l.actions[i]])])
].join('\n\n')+'\n';}
const outputs=new Map();
for(const locale of locales){
 const l=catalog.locales[locale.code];shape(l,catalog.locales.en,locale.code);
 if(locale.code!=='en')assert.notEqual(JSON.stringify(l),JSON.stringify(catalog.locales.en),locale.code+' must not fall back to English');
 const text=render(l);
 for(const key of ['sandbox','live','allow_http','SDK_HTTP_DISABLED','SDK_HTTPS_REQUIRED'])assert.ok(text.includes('`'+key+'`')||text.includes('"'+key+'"'),locale.code+' missing '+key);
 outputs.set('docs/'+locale.file,text);
}
const manifest={schema_version:1,source_sha256:sourceHash,locales};
outputs.set('docs/http-testing/locales.json',JSON.stringify(manifest,null,2)+'\n');
const index=[
 '# HTTP / HTTPS testing · 14 languages / 14 种语言',
 'Choose a complete guide below. All examples refer to the customer’s own deployment. / 请选择完整说明；所有示例均指客户自己的部署环境。',
 table([['Language / 语言','Code / 代码','Guide / 说明'],...locales.map(l=>[l.name,'`'+l.code+'`','['+catalog.locales[l.code].title+'](../'+l.file+')'])]),
 '## Maintaining these guides / 维护说明',
 'All 14 guides must be updated together when HTTP behavior changes. Edit `translations.json`, review every language, and update its `source_sha256` after reviewing the English source. API field names, SDK options, JSON examples and error codes are generated unchanged for every language. / HTTP 行为变化时须同步更新全部 14 种语言；API 字段、SDK 参数、JSON 示例和错误码保持一致。',
 '```sh\nnode scripts/http-testing-docs.mjs\nnode scripts/http-testing-docs.mjs --check\n```',
 'The SDK source export and server release archive include every listed guide and this language index. / SDK 源码导出和服务端发布压缩包均包含全部译文及本目录。'
].join('\n\n')+'\n';
outputs.set('docs/http-testing/README.md',index);
function save(file,body){
 if(check){assert.ok(fs.existsSync(file),'Missing generated document: '+file);assert.equal(fs.readFileSync(file,'utf8').replace(/\r\n/g,'\n'),body,'Stale generated document: '+file);}
 else{fs.mkdirSync(path.dirname(file),{recursive:true});fs.writeFileSync(file,body);}
}
for(const [file,body]of outputs)save(path.join(root,file),body);
if(site){
 assert.ok(fs.existsSync(path.join(site,'web/launch-docs.html')),'--site must be the Licentivo website workspace');
 for(const [file,body]of outputs)save(path.join(site,'web/assets/launch',file.slice('docs/'.length)),body);
 const heading=catalog.locales.zh.title;
 const links=locales.map(l=>'<a href="/assets/launch/'+l.file+'" lang="'+l.tag+'" dir="'+l.dir+'" hreflang="'+l.tag+'" download>'+l.name+'</a>').join(' · ');
 const fragment='<h3 data-http-testing-guide>'+heading+'</h3><p data-http-testing-languages data-no-translate="true">'+links+'</p>';
 const htmlFile=path.join(site,'web/launch-docs.html');let html=fs.readFileSync(htmlFile,'utf8');
 if(check)assert.ok(html.includes(fragment),'Missing or stale 14-language website entry');
 else{
  const existing=/<h3 data-http-testing-guide>[\s\S]*?<\/h3><p data-http-testing-languages data-no-translate(?:="true")?>[\s\S]*?<\/p>/;
  if(existing.test(html))html=html.replace(existing,fragment);
  else{const start=/<section id="troubleshooting"><h2>[^<]+<\/h2>/;assert.ok(start.test(html),'Missing Launch troubleshooting section');html=html.replace(start,match=>match+fragment);}
  fs.writeFileSync(htmlFile,html);
 }
 const part={};
 for(const locale of locales.filter(l=>l.code!=='zh')){
  const values={[heading]:catalog.locales[locale.code].title};part[locale.code]=values;
  const file=path.join(site,'web/assets/locales',locale.code+'.json');const dictionary=JSON.parse(fs.readFileSync(file,'utf8'));
  if(check)assert.equal(dictionary[heading],values[heading],locale.code+' website heading translation');
  else fs.writeFileSync(file,JSON.stringify({...dictionary,...values},null,2)+'\n');
 }
 save(path.join(site,'web/assets/locales/reviewed/parts/20261027-http-testing-docs.json'),JSON.stringify(part,null,2)+'\n');
}
console.log(JSON.stringify({mode:check?'verified':'generated',languages:locales.length,markdown_files:15,sections_per_language:5,sdk_examples_per_language:technical.length,errors_per_language:errorCodes.length,website:!!site}));
