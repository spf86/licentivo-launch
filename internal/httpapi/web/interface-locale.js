// Interface language is independent from merchant-authored content. If the
// server has no content translation, keep the requested UI language and show
// the source-content notice already used by the storefront.
export const interfaceLanguages=[
 {code:'zh-CN',name:'简体中文',catalog:'zh'}, {code:'en',name:'English',catalog:'en'},
 {code:'de',name:'Deutsch',catalog:'de'}, {code:'it',name:'Italiano',catalog:'it'},
 {code:'zh-TW',name:'繁體中文',catalog:'zh-hant'}, {code:'hi',name:'हिन्दी',catalog:'hi'},
 {code:'es',name:'Español',catalog:'es'}, {code:'ar',name:'العربية',catalog:'ar'},
 {code:'fr',name:'Français',catalog:'fr'}, {code:'bn',name:'বাংলা',catalog:'bn'},
 {code:'pt-BR',name:'Português',catalog:'pt'}, {code:'id',name:'Bahasa Indonesia',catalog:'id'},
 {code:'ur',name:'اردو',catalog:'ur'}, {code:'ja',name:'日本語',catalog:'ja'}
];
const catalogs=new Map(),templates=new Map(),pending=new Map();
export function interfaceLanguage(locale){return interfaceLanguages.find(item=>item.code.toLowerCase()===String(locale).toLowerCase()||item.catalog===locale);}
export async function prepareInterfaceLocale(locale){
 const language=interfaceLanguage(locale);if(!language||language.catalog==='zh')return;
 if(catalogs.has(language.catalog))return;
 if(!pending.has(language.catalog))pending.set(language.catalog,fetch('/assets/interface-locales/'+language.catalog+'.json').then(async response=>{
  if(!response.ok)throw Error('Interface translation unavailable: '+language.catalog);
  const values=await response.json();catalogs.set(language.catalog,values);
  const rules=[];
  for(const [source,translation]of Object.entries(values))if(/\{\d+\}/.test(source)){
   const names=[];
   const pattern=source.replace(/[.*+?^${}()|[\]\\]/g,'\\$&').replace(/\\\{(\d+)\\\}/g,(_match,id)=>{names.push(id);return '(.+?)';});
   rules.push({pattern:new RegExp('^'+pattern+'$'),names,translation});
  }
  templates.set(language.catalog,rules);return values;
 }).finally(()=>pending.delete(language.catalog)));
 await pending.get(language.catalog);
}
export function interfaceText(source,locale,fallback){
 if(typeof source!=='string')return fallback??source??'';
 const language=interfaceLanguage(locale);if(language?.catalog==='zh')return source;
 const dictionary=catalogs.get(language?.catalog);
 if(dictionary?.[source])return dictionary[source];
 for(const rule of templates.get(language?.catalog)||[]){const match=source.match(rule.pattern);if(match){const values={};rule.names.forEach((name,index)=>values[name]=match[index+1]);return rule.translation.replace(/\{(\d+)\}/g,(_match,id)=>values[id]??'');}}
 return fallback??source;
}
export function applyInterfaceLocale(locale){
 document.documentElement.lang=locale;
 document.documentElement.dir=['ar','ur'].includes(locale)?'rtl':'ltr';
 document.documentElement.style.setProperty('--settings-expand',JSON.stringify(interfaceText('展开',locale)));
 document.documentElement.style.setProperty('--settings-collapse',JSON.stringify(interfaceText('收起',locale)));
}
