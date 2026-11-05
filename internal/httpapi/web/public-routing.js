const locales=['zh-cn','en','ja','ko','de','fr','es','pt-br','it','ru'];
const localeCodes={'zh-cn':'zh-CN','pt-br':'pt-BR'};
export function pathLocale(){const first=location.pathname.split('/')[1]?.toLowerCase();return locales.includes(first)?localeCodes[first]||first:''}
export function routeParts(){
 if(location.hash.startsWith('#/'))return location.hash.slice(2).split('/');
 const parts=location.pathname.split('/').filter(Boolean);
 if(!locales.includes(parts[0]?.toLowerCase()))return parts[0]==='admin'?['admin']:parts[0]==='account'?['account']:['home'];
 if(parts.length===1)return ['home'];
 if(parts[1]!=='products')return ['home'];
 const slug=parts[2]||'';
 if(parts[3]==='download')return ['download',slug];
 if(parts[3]==='docs')return ['docs',slug,parts[4]||''];
 return ['product',slug];
}
export function publicRouteURL(parts,locale='zh-CN'){
 const base='/'+locale.toLowerCase()+'/';
 if(parts[0]==='home')return base;
 if(['product','download','docs'].includes(parts[0])&&parts[1]){
  const slug=encodeURIComponent(decodeURIComponent(parts[1]));
  return base+'products/'+slug+(parts[0]==='download'?'/download':parts[0]==='docs'?'/docs'+(parts[2]?'/'+encodeURIComponent(decodeURIComponent(parts[2])):''):'');
 }
 return null;
}
export function updatePublicLinks(root,locale){
 const brand=root.querySelector('.site-header .brand');if(brand)brand.setAttribute('href',publicRouteURL(['home'],locale));
 for(const link of root.querySelectorAll('a[href^="#/"]')){
  const href=link.getAttribute('href');let next;
  try{next=publicRouteURL(href.slice(2).split('/'),locale)}catch{continue}
  if(next){if(new URLSearchParams(location.search).get('preview')==='sandbox')next+='?preview=sandbox';link.setAttribute('href',next)}
  else if(pathLocale())link.setAttribute('href','/'+href);
 }
}
export function migratePublicHash(locale){
 if(!location.hash.startsWith('#/'))return false;
 let url;try{url=publicRouteURL(location.hash.slice(2).split('/'),locale)}catch{return false}
 if(!url)return false;
 location.replace(url+location.search);return true;
}
export function changePublicLanguage(locale){
 if(!pathLocale())return false;
 const url=publicRouteURL(routeParts(),locale);if(!url)return false;
 location.assign(url+location.search);return true;
}
