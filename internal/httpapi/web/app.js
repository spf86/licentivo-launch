import {paymentNames,icon,paymentSettingsPanel,mailSettingsPanel,initializeMailForm,settingsWorkspace} from "/assets/workspace.js";
import {integrationWizard} from "/assets/integration.js";
import {mountExternalLicensing,mountExternalDeliveries} from "/assets/external-licensing.js";
import {integrationAssistant} from "/assets/integration-assistant.js";
import {resetIntegrationDraft} from "/assets/integration-state.js";
import {licensingContext,planLicenseStatus,planPricingMode,savePlanWithLicensing} from "/assets/plan-licensing.js";
"use strict";
const main=document.getElementById("main");
const knownLocales=["zh-CN","en","ja","ko","de","fr","es","pt-BR","it","ru"];
const preferredLocale=()=>{const saved=localStorage.getItem("launch_locale");if(knownLocales.includes(saved))return saved;for(const value of navigator.languages||[navigator.language]){const lower=String(value||"").toLowerCase();const match=knownLocales.find(code=>lower===code.toLowerCase()||lower.split("-")[0]===code.toLowerCase().split("-")[0]);if(match)return match}return "zh-CN"};
const state={site:null,user:null,admin:null,licenseContext:null,licenseCatalogSyncedAt:0,licenseCatalogSyncError:"",linkToken:null,linkAction:null,locale:preferredLocale()};
Object.assign(state,{paymentEnvironment:"sandbox",paymentProvider:null,paymentConnections:null,dirtyForm:false});
const formBaselines=new WeakMap(),editedForms=new WeakSet();
function formFingerprint(form){return JSON.stringify([...new FormData(form)].map(([name,value])=>[name,value instanceof File?(value.name?{name:value.name,size:value.size,type:value.type,lastModified:value.lastModified}:null):value]))}
function captureFormBaselines(saved=false){
 for(const form of main.querySelectorAll('form[data-form]')){
  if(saved)editedForms.delete(form);
  if(saved||!editedForms.has(form)||!formBaselines.has(form))formBaselines.set(form,formFingerprint(form));
 }
}
function refreshDirtyState(){
 state.dirtyForm=[...main.querySelectorAll('form[data-form]')].some(form=>editedForms.has(form)&&formBaselines.has(form)&&formFingerprint(form)!==formBaselines.get(form));
 return state.dirtyForm;
}
function markFormsSaved(){captureFormBaselines(true);state.dirtyForm=false}
document.documentElement.lang=state.locale;
const t=(key,fallback)=>state.site?.ui?.[key]||fallback;
const themes=[["forest","森林绿"],["ocean","海洋蓝"],["indigo","靛青"],["amber","暖棕"],["rose","玫瑰"],["graphite","石墨"],["teal","青绿"],["violet","紫罗兰"]];
const esc=value=>String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const cookie=name=>document.cookie.split("; ").find(v=>v.startsWith(name+"="))?.slice(name.length+1)||"";
const toast=message=>{const el=document.getElementById("toast");el.textContent=message;el.classList.add("visible");setTimeout(()=>el.classList.remove("visible"),4000)};
const databaseBanner=document.getElementById("database-banner");
let databaseAvailable=true;
async function checkDatabaseConnection(){
 try{
  const response=await fetch("/readyz",{cache:"no-store"});
  if(!response.ok)throw new Error("database unavailable");
  databaseBanner.hidden=true;
  if(!databaseAvailable)toast("数据库连接已恢复");
  databaseAvailable=true;
 }catch{
  databaseBanner.hidden=false;
  if(databaseAvailable)toast("数据库连接已中断，请检查网络");
  databaseAvailable=false;
 }
}
window.addEventListener("offline",checkDatabaseConnection);
window.addEventListener("online",checkDatabaseConnection);
setInterval(checkDatabaseConnection,10000);
checkDatabaseConnection();
const navigate=path=>{location.hash="/"+path};
function confirmDiscardChanges(){
 if(refreshDirtyState()&&!window.confirm('当前修改尚未保存，确定离开并放弃修改吗？'))return false;
 markFormsSaved();return true;
}
document.querySelector('.skip').addEventListener('click',event=>{event.preventDefault();main.focus();main.scrollIntoView({block:'start'})});
const field=(label,name,value="",type="text",extra="")=>`<label>${esc(label)}<input name="${name}" type="${type}" value="${esc(value)}" ${extra}></label>`;
const area=(label,name,value="",extra="")=>`<label>${esc(label)}<textarea name="${name}" ${extra}>${esc(value)}</textarea></label>`;
const formError='<p class="form-error" role="alert" tabindex="-1"></p>';
const time=value=>value?new Intl.DateTimeFormat(state.locale,{dateStyle:"medium",timeStyle:"short"}).format(new Date(value)):"—";
const digits=currency=>new Intl.NumberFormat("en",{style:"currency",currency}).resolvedOptions().maximumFractionDigits;
const money=plan=>{try{return new Intl.NumberFormat(state.locale,{style:"currency",currency:plan.currency}).format(plan.amount_minor/(10**digits(plan.currency)))}catch{return t("common.price_pending","价格待确认")}};
const decimal=plan=>(plan.amount_minor/(10**digits(plan.currency))).toFixed(digits(plan.currency));
const term=plan=>plan.purchase_type==="perpetual"?t("common.permanent","永久使用"):`${plan.term_count} ${{day:t("common.days","天"),month:t("common.months","个月"),year:t("common.years","年")}[plan.term_unit]||""}`;
const moneyMinor=(currency,amount)=>money({currency,amount_minor:amount});
const periodLabel=periods=>periods===12?t("plans.one_year","1 年"):t("plans.month_count",`${periods} 个月`).replace("{count}",periods);
const productWizardSteps=["基本信息","产品介绍","产品文档与政策","平台与安装包","发布设置"];
const downloadPlatforms=[
 {key:"windows",label:"Windows",accept:".exe,.msi,.msix,.zip",formats:"EXE、MSI、MSIX 或 ZIP"},
 {key:"macos",label:"macOS",accept:".dmg,.pkg,.zip",formats:"DMG、PKG 或 ZIP"},
 {key:"ios",label:"iOS",accept:".ipa",formats:"IPA"},
 {key:"android",label:"Android",accept:".apk,.aab,.xapk",formats:"APK、AAB 或 XAPK"}
];
const hasProductDownloads=product=>Boolean(product?.downloads?.length||product?.content?.download_url);
const productVersionSummary=product=>{const versions=[...(product?.downloads||[])].filter(item=>item.platform!=="universal").map(item=>`${downloadPlatforms.find(platform=>platform.key===item.platform)?.label||item.platform} ${item.version}`).filter(Boolean);return versions.length?versions.join(" · "):(product?.content?.version||"—")};
const formatBytes=value=>{const bytes=Number(value)||0;if(!bytes)return "";const units=["B","KB","MB","GB"];const index=Math.min(Math.floor(Math.log(bytes)/Math.log(1024)),units.length-1);return `${(bytes/(1024**index)).toFixed(index?1:0)} ${units[index]}`};
const prose=value=>esc(value).replace(/\n/g,"<br>");
const productDocumentFields=[
 {key:"installation",field:"installation_guide",label:()=>t("product.installation","安装指南"),group:"help"},
 {key:"upgrade",field:"upgrade_guide",label:()=>t("product.upgrade","升级指南"),group:"help"},
 {key:"uninstall",field:"uninstall_guide",label:()=>t("product.uninstall","卸载指南"),group:"help"},
 {key:"troubleshooting",field:"troubleshooting_guide",label:()=>t("product.troubleshooting","故障排查"),group:"help"},
 {key:"terms",field:"terms_of_service",label:()=>t("product.terms","服务条款"),group:"policy"},
 {key:"privacy",field:"privacy_policy",label:()=>t("product.privacy","隐私政策"),group:"policy"},
 {key:"refunds",field:"refund_policy",label:()=>t("product.refunds","退款规则"),group:"policy"},
 {key:"invoices",field:"invoice_guide",label:()=>t("product.invoices","开票流程"),group:"policy"},
 {key:"legal",field:"legal_tax_notice",label:()=>t("product.legal_tax","法律与税务说明"),group:"policy"}
];
const publicDocumentImage=/^!\[([^\]\r\n]{0,160})\]\((\/api\/v1\/product-document-images\/[A-Za-z0-9-]+)\)$/;
function documentBody(value){
 const blocks=[],paragraph=[];
 const flush=()=>{if(paragraph.length){blocks.push(`<p>${paragraph.map(esc).join("<br>")}</p>`);paragraph.length=0}};
 for(const raw of String(value||"").split(/\r?\n/)){
  const line=raw.trim(),image=line.match(publicDocumentImage);
  if(image){flush();blocks.push(`<figure class="doc-image"><img src="${esc(image[2])}" alt="${esc(image[1])}" loading="lazy">${image[1]?`<figcaption>${esc(image[1])}</figcaption>`:""}</figure>`);continue}
  if(!line){flush();continue}
  paragraph.push(raw);
 }
 flush();
 return blocks.join("");
}
function documentEditor(label,name,value=""){
 return `<div class="document-editor">${area(label,name,value,"maxlength=16000 rows=7")}<div class="document-editor-actions"><button type="button" class="small secondary" data-action="insert-document-image" data-document-field="${esc(name)}">插入图片</button><input type="file" data-document-image-input="${esc(name)}" accept="image/png,image/jpeg,image/webp,image/gif" hidden><span class="field-help">图片会插入到当前光标位置；支持 PNG、JPEG、WebP、GIF，最大 8 MB。</span></div><div class="document-image-staging" data-document-image-staging="${esc(name)}"></div></div>`;
}
const planPurchaseOptions=plan=>plan.purchase_type==="perpetual"?[]:(plan.purchase_options?.length?plan.purchase_options:[1,3,6,12].map(periods=>({periods,amount_minor:plan.amount_minor*periods,enabled:true})));
const enabledPurchaseOptions=plan=>planPurchaseOptions(plan).filter(option=>option.enabled);
const planPriceSummary=plan=>{if(plan.purchase_type==="perpetual")return `${money(plan)} / ${term(plan)}`;const options=enabledPurchaseOptions(plan);if(!options.length)return t("common.price_pending","价格待确认");return `${moneyMinor(plan.currency,options[0].amount_minor)} / ${periodLabel(options[0].periods)}`};
async function api(path,method="GET",body){
 const headers={};if(body!==undefined)headers["Content-Type"]="application/json";headers["Accept-Language"]=document.documentElement.lang;if(method!=="GET")headers["X-CSRF-Token"]=cookie("launch_csrf");
 const response=await fetch("/api/v1"+path,{method,headers,body:body===undefined?undefined:JSON.stringify(body),credentials:"same-origin",cache:"no-store"});
 const result=await response.json();if(!response.ok){const error=new Error(result.error?.message||"服务暂不可用");error.code=result.error?.code;error.status=response.status;throw error}if(method!=="GET")markFormsSaved();return result.data;
}
async function uploadAPI(path,body){
 const response=await fetch("/api/v1"+path,{method:"POST",headers:{"Accept-Language":document.documentElement.lang,"X-CSRF-Token":cookie("launch_csrf")},body,credentials:"same-origin",cache:"no-store"});
 const result=await response.json();if(!response.ok){const error=new Error(result.error?.message||"上传失败，请稍后重试");error.code=result.error?.code;error.status=response.status;throw error}return result.data;
}
async function refresh(){
 const previousUser=state.user?.id;
 state.site=await siteFor();
	state.locale=state.site.locale||state.locale;document.documentElement.lang=state.locale;localStorage.setItem("launch_locale",state.locale);
 try{state.user=(await api("/me")).user}catch{state.user=null}
 if(previousUser!==state.user?.id)resetIntegrationDraft();
 applyBrand(state.site.brand);
}
async function siteFor(product=""){
 const query=new URLSearchParams();
 if(new URLSearchParams(location.search).get("preview")==="sandbox")query.set("environment","sandbox");
 if(product)query.set("product",product);
	query.set("locale",state.locale);
 return api("/site"+(query.size?"?"+query:""));
}
function applyBrand(brand){
 document.documentElement.dataset.theme=brand.theme||"forest";
 const mark=document.querySelector(".mark");
 mark.classList.toggle("has-logo",Boolean(brand.logo_data_url));
 if(brand.logo_data_url){const image=document.createElement("img");image.src=brand.logo_data_url;image.alt="";mark.replaceChildren(image)}else mark.textContent=(brand.name||"L").slice(0,1).toUpperCase();
 document.getElementById("brand-name").textContent=brand.name||"Licentivo Launch";
 document.getElementById("footer-brand").textContent=state.site.brand.support_email||t("footer.local","客户资料由商家保存");
 document.title=brand.name||"Licentivo Launch";
}
function routeProductSlug(){
 const route=location.hash.slice(2).split("/");
 if(["product","download","docs","checkout"].includes(route[0])&&route[1]){try{return decodeURIComponent(route[1])}catch{return route[1]}}
 return "";
}
function updateDocumentNavigation(){
 const slug=routeProductSlug(),footer=document.getElementById("footer-policy-links");
 footer.hidden=!slug;
 if(slug)footer.innerHTML=`<a href="#/docs/${encodeURIComponent(slug)}/terms">${esc(t("product.terms","服务条款"))}</a><a href="#/docs/${encodeURIComponent(slug)}/privacy">${esc(t("product.privacy","隐私政策"))}</a><a href="#/docs/${encodeURIComponent(slug)}/refunds">${esc(t("product.refunds","退款规则"))}</a>`;
 else footer.replaceChildren();
}
function empty(title,message){return `<div class="empty"><h2>${esc(title)}</h2><p>${esc(message)}</p></div>`}
function brandHeroMark(brand){return brand.logo_data_url?`<img src="${esc(brand.logo_data_url)}" alt="">`:esc((brand.name||"L").slice(0,1).toUpperCase())}
function nav(){
 const user=state.user,locales=state.site.supported_locales||[],page=location.hash.slice(2).split('/')[0]||'home';
 document.getElementById('nav').innerHTML=`<a href="#/home" ${['home','product','download','docs'].includes(page)?'aria-current="page"':''}>${esc(t('nav.products','产品'))}</a>${user?`<a href="#/account" ${page==='account'?'aria-current="page"':''}>${esc(t('nav.account','我的账号'))}</a>${user.role!=='customer'?`<a href="#/admin">${esc(t('nav.admin','管理后台'))}</a>`:''}<button class="text-link" data-action="logout">${esc(t('nav.logout','退出'))}</button>`:`<a href="#/login">${esc(t('nav.login','登录'))}</a><a class="button small" href="#/register">${esc(t('nav.register','创建账号'))}</a>`}<label class="language-picker"><span>${esc(t('language.label','语言'))}</span><select data-language aria-label="${esc(t('language.label','语言'))}">${locales.map(v=>`<option value="${esc(v.code)}" ${v.code===state.locale?'selected':''}>${esc(v.name)}</option>`).join('')}</select></label>`;
 updateDocumentNavigation();
}
function productCards(products){return `<div class="grid">${products.map(product=>{const docsStart=productDocsStart(product);return `<article class="card product-card"><div><p class="eyebrow">${esc(productVersionSummary(product)||t("home.product_fallback","产品"))}</p><h2>${esc(product.name)}</h2><p>${esc(product.content.summary||t("home.card_fallback","查看产品功能、套餐与下载信息。"))}</p></div><p class="product-card-scope">${esc(t("home.product_scope","独立产品 · 专属套餐、下载与授权"))}</p><div class="actions"><a class="button" href="#/product/${encodeURIComponent(product.slug)}">${esc(t("common.view_product","查看产品"))}</a>${docsStart?`<a class="button secondary" href="#/docs/${encodeURIComponent(product.slug)}/${encodeURIComponent(docsStart)}">${esc(t("nav.docs","产品文档"))}</a>`:""}${hasProductDownloads(product)?`<a class="button secondary" href="#/download/${encodeURIComponent(product.slug)}">${esc(t("common.download","下载"))}</a>`:""}</div></article>`}).join("")}</div>`}
function publicPlanBenefits(plan){
 if(plan.highlights?.length)return plan.highlights;
	if(plan.snapshot.license_model==="external")return [t("plans.external_licensing","使用商家自有授权系统；购买后在客户中心查看交付")];
 const unit=plan.snapshot.license_model==="floating"?t("common.seat","个同时使用席位"):t("common.device","台设备");
 const benefits=[`${plan.snapshot.max_devices} ${unit}`];
 if(plan.snapshot.offline_mode==="limited")benefits.push(t("plans.offline_limited","支持有限离线使用"));
 if(plan.snapshot.offline_mode==="permanent")benefits.push(t("plans.offline_permanent","支持长期离线使用"));
 if(plan.snapshot.self_release_limit)benefits.push(`${t("plans.self_release","可自助解绑设备")} ${plan.snapshot.self_release_limit} ${t("common.times","次")}`);
 if(plan.snapshot.features?.length)benefits.push(...plan.snapshot.features);
 return benefits;
}
function productDirectory(site){
 const {brand,products}=site;
 if(!products.length)return empty(t("home.preparing","产品正在准备"),t("home.preparing_body","站长发布产品后会显示在这里。"));
 const pending=!site.translation_ready?`<div class="notice warning">${esc(t("translation.preparing","所选语言的内容正在生成，当前显示原文。"))}</div>`:"";
 return `${pending}<section class="catalog-hero"><div><p class="eyebrow">${esc(brand.tagline||t("section.products","产品"))}</p><h1>${esc(t("home.choose_product","选择软件产品"))}</h1><p>${esc(t("home.choose_product_body","每个产品都有独立的介绍、安装包和销售套餐。进入产品后再选择对应套餐。"))}</p></div><span class="pill">${products.length} ${esc(t("home.product_count","个产品"))}</span></section><section class="section product-directory" aria-labelledby="product-directory-title"><div class="section-head"><div><p class="eyebrow">${esc(t("section.products","产品"))}</p><h2 id="product-directory-title">${esc(t("home.all_products","全部产品"))}</h2></div></div>${productCards(products)}</section>`;
}
function productLanding(site){
 const {brand,product,plans,environment}=site,content=product?.content||{};
 if(!product)return empty(t("home.preparing","产品正在准备"),t("home.preparing_body","站长发布产品后会显示在这里。"));
 const pending=!site.translation_ready?`<div class="notice warning">${esc(t("translation.preparing","所选语言的内容正在生成，当前显示原文。"))}</div>`:"";
 const downloadVersion=productVersionSummary(product);
 return `${pending}${environment==="sandbox"?'<div class="notice warning">'+esc(t("home.test_notice","测试环境：支付使用沙箱，不会产生真实扣款。"))+'</div>':""}<nav class="product-breadcrumb" aria-label="${esc(t("product.position","当前位置"))}"><a href="#/home">${esc(t("home.all_products","全部产品"))}</a><span aria-hidden="true">/</span><strong>${esc(product.name)}</strong></nav><section class="hero"><div><p class="eyebrow">${esc(brand.tagline||t("home.software_start","软件，从这里开始"))}</p><h1>${esc(product.name)}</h1><p>${esc(content.summary||brand.description||t("home.summary_fallback","产品正在准备中，欢迎稍后访问。"))}</p><div class="actions">${hasProductDownloads(product)?`<a class="button" href="#/download/${encodeURIComponent(product.slug)}">${esc(t("common.download","下载"))}${downloadVersion&&downloadVersion!=="—"?" · "+esc(downloadVersion):""}</a>`:""}<a class="button secondary" href="#/docs/${encodeURIComponent(product.slug)}/installation">${esc(t("nav.docs","产品文档"))}</a><a class="button secondary" href="#/account">${esc(t("common.account_center","进入账号中心"))}</a></div></div><aside class="hero-aside"><div class="large-mark" aria-hidden="true">${brandHeroMark(brand)}</div><p class="eyebrow">${esc(t("product.current","当前产品"))}</p><h2>${esc(product.name)}</h2><p>${esc(t("product.independent_scope","下方介绍、下载和套餐均只属于此产品。"))}</p></aside></section>${content.features?.length?`<section class="section grid">${content.features.map((v,i)=>`<article class="card feature"><p class="eyebrow">0${i+1}</p><h3>${esc(v)}</h3></article>`).join("")}</section>`:""}${content.description||content.requirements?`<section class="card section"><h2>${esc(t("product.about","关于产品"))}</h2>${content.description?`<p>${prose(content.description)}</p>`:""}${content.requirements?`<p class="hint">${esc(t("product.requirements","系统要求"))}：${prose(content.requirements)}</p>`:""}</section>`:""}<section class="section product-plans"><div class="section-head"><div><p class="eyebrow">${esc(product.name)} · ${esc(t("section.plans","套餐"))}</p><h2>${esc(t("plans.for_product","选择 {product} 套餐")).replace("{product}",esc(product.name))}</h2><p class="hint">${esc(t("plans.product_scope","以下套餐只适用于当前产品。"))}</p></div><p class="hint">${esc(t("plans.no_auto","固定期限购买不会自动扣款"))}</p></div>${plans.length?`<div class="grid">${plans.map(plan=>`<article class="card public-plan-card"><p class="eyebrow">${esc(product.name)}</p><h2>${esc(plan.name)}</h2><p class="price">${esc(planPriceSummary(plan))}</p><p class="plan-summary">${esc(plan.summary||t("plans.summary_fallback","购买后立即获得对应期限的软件使用权。"))}</p><ul class="plan-features">${publicPlanBenefits(plan).map(item=>`<li>${esc(item)}</li>`).join("")}</ul><p class="hint">${esc(plan.purchase_type==="perpetual"?term(plan):enabledPurchaseOptions(plan).map(option=>periodLabel(option.periods)).join(" / "))} · ${plan.snapshot.max_devices} ${esc(plan.snapshot.license_model==="floating"?t("common.seat","个同时使用席位"):t("common.device","台设备"))}</p><button class="secondary" data-action="${plan.preview_only?"purchase-unavailable":"begin-checkout"}" data-product="${esc(product.slug)}" data-version="${esc(plan.version_id)}">${esc(plan.preview_only?t("plans.preview","测试预览套餐"):t("plans.choose","选择套餐"))}</button></article>`).join("")}</div>`:empty(t("plans.soon","套餐即将开放"),t("plans.soon_body","商家正在准备销售套餐，可先创建账号。"))}</section>`;
}
function home(){main.innerHTML=productDirectory(state.site)}
async function productPage(slug){const site=await siteFor(slug);main.innerHTML=productLanding(site)}
async function downloadPage(slug){
 const site=await siteFor(slug),product=site.product,content=product.content;
 const downloads=product.downloads?.length?product.downloads:(content.download_url?[{platform:"universal",source:"external",version:content.version,url:content.download_url}]:[]);
 if(!downloads.length){main.innerHTML=empty(t("download.none","暂未提供下载"),t("download.none_body","请稍后再来查看当前产品版本。"));return}
 const label=platform=>downloadPlatforms.find(item=>item.key===platform)?.label||t("download.all_platforms","通用版本");
 main.innerHTML=`<section class="download-page"><p class="eyebrow">${esc(t("section.download","下载"))}</p><h1>${esc(product.name)}</h1><p>${esc(content.summary||"")}</p>${content.requirements?`<div class="notice"><strong>${esc(t("product.requirements","通用系统要求"))}</strong><br>${prose(content.requirements)}</div>`:""}<div class="download-grid">${downloads.map(item=>{const requirements=content.platform_requirements?.[item.platform]||"";return `<article class="card download-card"><div><p class="eyebrow">${esc(label(item.platform))}</p><h2>${esc(item.version||content.version||t("download.current","当前版本"))}</h2>${item.file_name?`<p>${esc(item.file_name)}</p>`:"<p>使用外部软件下载地址</p>"}<dl class="download-facts">${item.size_bytes?`<div><dt>${esc(t("download.file_size","文件大小"))}</dt><dd>${esc(formatBytes(item.size_bytes))}</dd></div>`:""}${item.sha256?`<div><dt>${esc(t("download.checksum","SHA-256 校验值"))}</dt><dd><code>${esc(item.sha256)}</code></dd></div>`:""}${requirements?`<div><dt>${esc(t("product.requirements","系统要求"))}</dt><dd>${prose(requirements)}</dd></div>`:""}</dl></div><a class="button" href="${esc(item.url)}" rel="noopener noreferrer">${esc(t("common.download","下载"))} ${esc(label(item.platform))}</a></article>`}).join("")}</div><div class="download-help-link"><div><strong>${esc(t("docs.need_help","需要安装帮助？"))}</strong><p>${esc(t("docs.need_help_body","查看安装、升级、卸载和故障排查文档。"))}</p></div><a class="button secondary" href="#/docs/${encodeURIComponent(product.slug)}/installation">${esc(t("nav.docs","产品文档"))}</a></div><p><a href="#/product/${encodeURIComponent(product.slug)}">${esc(t("common.back_product","返回产品介绍"))}</a></p></section>`;
}
function productRequirementDocument(content){
 const platformItems=Object.entries(content.platform_requirements||{}).filter(([,value])=>String(value||"").trim());
 const platformName=key=>downloadPlatforms.find(item=>item.key===key)?.label||key;
 return `${content.requirements?`<div class="docs-requirement-general"><h3>${esc(t("docs.general_requirements","通用要求"))}</h3>${documentBody(content.requirements)}</div>`:""}${platformItems.length?`<div class="docs-requirement-grid">${platformItems.map(([key,value])=>`<section><h3>${esc(platformName(key))}</h3>${documentBody(value)}</section>`).join("")}</div>`:""}`;
}
function productDocumentEntries(product){
 const content=product?.content||{};
 const requirements=Boolean(String(content.requirements||"").trim()||Object.values(content.platform_requirements||{}).some(value=>String(value||"").trim()));
 return [...(requirements?[{key:"requirements",label:t("product.requirements","系统要求"),group:"help",render:()=>productRequirementDocument(content)}]:[]),...productDocumentFields.filter(item=>String(content[item.field]||"").trim()).map(item=>({key:item.key,label:item.label(),group:item.group,render:()=>documentBody(content[item.field])}))];
}
function productDocsStart(product){
 const entries=productDocumentEntries(product);
 return (entries.find(item=>item.group==="help")||entries[0])?.key||"";
}
function docsDirectory(site){
 const products=site.products||[];
 if(!products.length){main.innerHTML=empty(t("docs.not_found","没有找到产品文档"),t("docs.not_found_body","请返回产品目录选择其他产品。"));return}
 const cards=products.map(product=>{const entries=productDocumentEntries(product),help=entries.filter(item=>item.group!=="policy").length,policies=entries.filter(item=>item.group==="policy").length,start=productDocsStart(product);return `<article class="card docs-product-card"><div><p class="eyebrow">${esc(productVersionSummary(product)||t("home.product_fallback","产品"))}</p><h2>${esc(product.name)}</h2><p>${esc(product.content?.summary||t("home.card_fallback","查看产品功能、套餐与下载信息。"))}</p></div><p class="docs-product-count">${entries.length?`${help} ${esc(t("docs.help_count","篇使用文档"))} · ${policies} ${esc(t("docs.policy_count","篇政策文档"))}`:esc(t("docs.empty","文档正在准备"))}</p><div class="actions">${start?`<a class="button" href="#/docs/${encodeURIComponent(product.slug)}/${encodeURIComponent(start)}">${esc(t("docs.open_product","查看该产品文档"))}</a>`:""}<a class="button secondary" href="#/product/${encodeURIComponent(product.slug)}">${esc(t("common.view_product","查看产品"))}</a></div></article>`}).join("");
 main.innerHTML=`<section class="docs-page docs-directory"><div class="docs-hero"><div><p class="eyebrow">${esc(t("section.products","产品"))}</p><h1>${esc(t("docs.center_title","产品文档中心"))}</h1><p>${esc(t("docs.center_subtitle","请选择软件产品。每个产品的安装、使用、故障排查和政策文档相互独立。"))}</p></div><span class="pill">${products.length} ${esc(t("home.product_count","个产品"))}</span></div><div class="grid docs-product-grid">${cards}</div></section>`;
}
async function docsPage(slug,requested=""){
 const site=await siteFor(slug),product=site.product,content=product?.content||{};
 if(!product){main.innerHTML=empty(t("docs.not_found","没有找到产品文档"),t("docs.not_found_body","请返回产品目录选择其他产品。"));return}
 const entries=productDocumentEntries(product);
 if(!entries.length){main.innerHTML=`<section class="docs-page"><div class="docs-hero"><div><p class="eyebrow">${esc(product.name)}</p><h1>${esc(t("docs.title","产品文档"))}</h1></div><a class="button secondary" href="#/product/${encodeURIComponent(product.slug)}">${esc(t("common.back_product","返回产品介绍"))}</a></div>${empty(t("docs.empty","文档正在准备"),t("docs.empty_body","管理员发布文档后会显示在这里。"))}</section>`;return}
 const selected=entries.find(item=>item.key===requested)||entries.find(item=>item.group==="help")||entries[0];
 const help=entries.filter(item=>item.group!=="policy"),policies=entries.filter(item=>item.group==="policy");
 const links=items=>items.map(item=>`<a class="${item.key===selected.key?"active":""}" href="#/docs/${encodeURIComponent(product.slug)}/${encodeURIComponent(item.key)}">${esc(item.label)}</a>`).join("");
 main.innerHTML=`<section class="docs-page"><nav class="product-breadcrumb" aria-label="${esc(t("product.position","当前位置"))}"><a href="#/docs">${esc(t("docs.center_title","产品文档中心"))}</a><span aria-hidden="true">/</span><strong>${esc(product.name)}</strong></nav><div class="docs-hero"><div><p class="eyebrow">${esc(product.name)}</p><h1>${esc(t("docs.title","产品文档"))}</h1><p>${esc(t("docs.subtitle","安装、更新、故障排查与购买政策集中在这里。"))}</p></div><div class="actions">${hasProductDownloads(product)?`<a class="button" href="#/download/${encodeURIComponent(product.slug)}">${esc(t("common.download","下载"))}</a>`:""}<a class="button secondary" href="#/product/${encodeURIComponent(product.slug)}">${esc(t("common.back_product","返回产品介绍"))}</a></div></div><div class="docs-layout"><aside class="docs-index" aria-label="${esc(t("docs.contents","文档目录"))}">${help.length?`<p>${esc(t("product.help","安装与使用"))}</p>${links(help)}`:""}${policies.length?`<p>${esc(t("product.policies","服务与政策"))}</p>${links(policies)}`:""}</aside><article class="docs-article"><p class="eyebrow">${esc(product.name)}</p><h2>${esc(selected.label)}</h2><div class="docs-content">${selected.render()}</div></article></div></section>`;
}
function authPage(kind){
 const mailRequired=state.site.email_verification_required;
 if(kind==='forgot'&&!state.site.mail_enabled){main.innerHTML=`<section class="panel auth-panel"><p class="eyebrow">${esc(t('section.account','账号'))}</p><h1>${esc(t('auth.forgot','找回密码'))}</h1><p>${esc(t('auth.mail_unavailable','本站未启用邮件，请联系商家找回密码。'))}</p><p class="hint">${esc(state.site.brand.support_email||'')}</p><a class="button secondary" href="#/login">${esc(t('auth.back_login','返回登录'))}</a></section>`;return}
 const meta={login:[t('auth.welcome','欢迎回来'),t('auth.login_body','登录商家的账号，查看产品与权益'),t('nav.login','登录')],register:[t('auth.create','创建账号'),mailRequired?t('auth.register_body','使用邮箱注册，购买前需要完成邮箱验证'):t('auth.register_direct','使用邮箱和密码创建账号，注册后即可登录。'),t('auth.register','注册')],forgot:[t('auth.forgot','找回密码'),t('auth.forgot_body','我们会向有效的账号邮箱发送重置链接'),t('auth.send_reset','发送重置邮件')]}[kind];
 main.innerHTML=`<section class="panel auth-panel"><p class="eyebrow">${esc(state.site.brand.name)}</p><h1>${esc(meta[0])}</h1><p class="hint">${esc(meta[1])}</p><form data-form="${kind}">${field(t('auth.email','邮箱'),'email','','email','required autocomplete=username maxlength=254')}${kind!=='forgot'?field(t('auth.password','密码'),'password','','password',`required ${kind==='register'?'minlength=12 autocomplete=new-password':'autocomplete=current-password'} maxlength=128`):''}${kind==='register'?`<p class="field-help">${esc(t('auth.password_help','密码至少 12 个字符。'))}</p>`:''}${formError}<button type="submit">${esc(meta[2])}</button></form><p class="form-foot">${kind==='login'?`<a href="#/register">${esc(t('auth.create','创建账号'))}</a>${state.site.mail_enabled?`<a href="#/forgot">${esc(t('auth.forgot_link','忘记密码'))}</a>`:''}`:`<a href="#/login">${esc(t('auth.back_login','返回登录'))}</a>`}</p></section>`;
}
function setup(){
 if(state.site.installed){authPage("login");return}
 main.innerHTML=empty("Server configuration required","Configure MySQL and the instance master key, then restart Launch. The initial administrator is created automatically before the server starts.");
}
function initialPasswordChange(){
 main.innerHTML=`<section class="panel"><p class="eyebrow">Complete installation</p><h1>Change the initial password</h1><p>Choose your own password to unlock administration.</p><p>${esc(state.user.email)}</p><form data-form="initial-password">${field("Current password","current_password","","password","required autocomplete=current-password maxlength=128")}${field("New password","new_password","","password","required minlength=12 maxlength=128 autocomplete=new-password")}${field("Confirm new password","confirm_password","","password","required minlength=12 maxlength=128 autocomplete=new-password")}${formError}<button>Save password and sign in again</button></form></section>`;
}
function emailLink(){
 const reset=state.linkAction==="reset";
 main.innerHTML=`<section class="panel"><p class="eyebrow">${esc(t("section.account","账号"))}</p><h1>${esc(reset?t("auth.reset_title","设置新密码"):t("auth.verify_title","确认邮箱"))}</h1><p class="hint">${esc(reset?t("auth.reset_body","设置新密码后，所有旧登录会话会失效。"):t("auth.verify_body","确认后即可使用该邮箱关联的账号。"))}</p><form data-form="complete-email">${reset?field(t("account.new_password","新密码"),"password","","password","required minlength=12 maxlength=128 autocomplete=new-password"):""}${formError}<button>${esc(reset?t("auth.save_password","保存新密码"):t("auth.verify_button","确认验证"))}</button></form></section>`;
}
async function account(){
 const user=state.user;if(!user){authPage("login");return}
 const [sessions,orders,products,refunds,invoices]=await Promise.all([api("/me/sessions"),api("/me/orders"),api("/me/products"),api("/me/refunds"),api("/me/invoices")]);
 const deviceResults=await Promise.all(products.map(async product=>{if(product.licensing_provider==="custom_http")return [product.product_id+":"+product.environment,null];try{return [product.product_id+":"+product.environment,await api(`/me/products/${encodeURIComponent(product.product_id)}/devices?environment=${encodeURIComponent(product.environment)}`)]}catch{return [product.product_id+":"+product.environment,null]}}));
 const devices=new Map(deviceResults);
 const entitlementStatus={active:t("account.active","已开通"),pending:t("account.pending","授权处理中"),expired:t("account.expired","已到期"),suspended:t("account.suspended","已暂停"),revoked:t("account.revoked","购买已终止")};
 const orderStatus={pending:t("account.order_waiting","等待付款"),paid:t("account.order_paid","已付款"),closed:t("account.order_closed","已关闭"),refunded:t("account.order_refunded","已退款"),refunding:t("account.order_refunding","退款中"),reversed:t("account.order_reversed","已撤款")};
 const orderAction={purchase:t("action.purchase","购买"),renew:t("action.renew","续费"),upgrade:t("action.upgrade","升级")};
 const invoiceFor=orderID=>invoices.find(item=>item.order_id===orderID);
 const refundFor=orderID=>refunds.find(item=>item.order_id===orderID);
 const invoiceStatus={requested:t("account.invoice_waiting","等待开票"),issued:t("account.invoice_issued","已开具"),rejected:t("account.invoice_rejected","未通过")};
 const productCards=products.map(p=>{const device=devices.get(p.product_id+":"+p.environment),active=device?.devices?.filter(item=>item.status==="active").length,downloadReady=Boolean(p.downloads?.length||p.download_url),version=(p.downloads||[]).map(item=>item.version).filter(Boolean).join(" · ")||p.version;return `<article class="card account-product" data-account-product="${esc(p.product_id)}" data-environment="${esc(p.environment)}"><div class="section-head"><div><p class="eyebrow">${esc(version||t("account.software_rights","软件权益"))}</p><h2>${esc(p.product_name||t("account.entitlements","软件权益"))}</h2></div><span class="pill ${p.status==="active"?"":"orange"}">${esc(entitlementStatus[p.status]||p.status)}${p.environment==="sandbox"?" · "+esc(t("common.test","测试")):""}</span></div><dl class="account-facts"><div><dt>${esc(t("account.license_validity","授权有效期"))}</dt><dd>${p.valid_until?time(p.valid_until):esc(t("common.permanent","永久"))}</dd></div><div><dt>${esc(t("account.license_sync","授权同步"))}</dt><dd>${esc(p.synchronized?t("account.synced","已同步"):t("account.syncing","处理中"))}</dd></div><div><dt>${esc(t("account.current_devices","当前设备"))}</dt><dd>${active===undefined?esc(t("account.device_unavailable","暂不可用")):active+" "+esc(t("account.device_unit","台/席位"))}</dd></div></dl><div class="actions">${downloadReady?`<a class="button" href="#/download/${encodeURIComponent(p.product_slug)}">${esc(t("account.download","下载"))} ${esc(version||"")}</a>`:""}<a class="button secondary" href="#/change/${esc(p.product_id)}/${esc(p.environment)}">${esc(t("account.renew_upgrade","续费或升级"))}</a>${p.licensing_provider==="custom_http"?"":`<a href="#/devices/${esc(p.product_id)}/${esc(p.environment)}">${esc(t("account.manage_devices","管理设备"))}</a>`}<a href="#/product/${encodeURIComponent(p.product_slug)}">${esc(t("account.product_help","产品说明"))}</a></div></article>`}).join("");
 const orderRows=orders.map(o=>{const invoice=invoiceFor(o.id),refund=refundFor(o.id);const invoiceAction=invoice?(invoice.status==="issued"&&invoice.invoice_url?`<a href="${esc(invoice.invoice_url)}" target="_blank" rel="noopener">${esc(t("account.download_invoice","下载发票"))}</a>`:`<span class="pill ${invoice.status==="requested"?"orange":"gray"}">${esc(invoiceStatus[invoice.status]||invoice.status)}</span>`):(o.status==="paid"&&o.environment==="live"?`<button class="small secondary" data-action="open-invoice-request" data-order="${esc(o.id)}">${esc(t("account.request_invoice","申请发票"))}</button>`:"—");return `<tr><td><strong>${esc(o.product_name||"—")}</strong><br><span class="hint">${esc(o.plan_name||"—")} · v${esc(o.plan_version||"—")}</span></td><td>${time(o.created_at)}</td><td>${money(o)}<br><span class="hint">${o.purchase_periods>1?periodLabel(o.purchase_periods):""}</span></td><td>${esc(orderAction[o.action]||o.action)}</td><td><a href="#/order/${esc(o.id)}">${esc(orderStatus[o.status]||o.status)}</a>${refund?`<br><span class="hint">${esc(t("account.refund_progress","退款进度"))}：${esc(refundStatus(refund))}</span>`:""}</td><td>${invoiceAction}</td></tr>`});
 const refundRows=refunds.map(r=>`<tr><td>${time(r.created_at)}</td><td><a href="#/order/${esc(r.order_id)}">${esc(String(r.order_id).slice(0,8))}</a></td><td>${money(r)}</td><td><span class="pill ${r.status==="requested"?"orange":""}">${esc(refundStatus(r))}</span></td><td>${esc(r.reason)}</td></tr>`);
 main.innerHTML=`<div class="section-head account-heading"><div><p class="eyebrow">${esc(t("account.center","客户中心"))}</p><h1>${esc(t("account.title","我的账号"))}</h1><p>${esc(user.email)} <span class="pill ${user.verified||!state.site.email_verification_required?"":"orange"}">${esc(user.verified?t("account.verified","已验证"):state.site.email_verification_required?t("account.unverified","待验证"):t("account.email_optional","无需邮箱验证"))}</span></p></div>${!user.verified&&state.site.email_verification_required?'<button class="secondary" data-action="resend-verification">'+esc(t("account.resend","重发验证邮件"))+'</button>':""}</div><div class="stats account-stats">${[[t("account.products_count","软件权益"),products.length],[t("account.orders_count","订单"),orders.length],[t("account.refunds_pending","退款处理中"),refunds.filter(v=>!["succeeded","failed"].includes(v.status)).length],[t("account.invoices_pending","发票处理中"),invoices.filter(v=>v.status==="requested").length]].map(([label,value])=>`<div class="stat"><p>${esc(label)}</p><strong>${value}</strong></div>`).join("")}</div><section class="section"><div class="section-head"><div><p class="eyebrow">${esc(t("account.downloads_title","下载、授权与设备"))}</p><h2>${esc(t("account.my_software","我的软件"))}</h2></div></div>${products.length?`<div class="grid two">${productCards}</div>`:empty(t("account.none","你的软件权益"),t("account.none_body","当前还没有已购产品。购买后，权益和使用期限会显示在这里。"))}</section><section class="section"><h2>${esc(t("account.orders_invoices","订单、续费与发票"))}</h2>${orders.length?table([t("account.product_plan","产品与套餐"),t("account.order_time","订单时间"),t("account.amount_period","金额与期限"),t("common.type","类型"),t("common.status","状态"),t("account.invoice","发票")],orderRows):empty(t("account.no_orders","暂无订单"),t("account.no_orders_body","你的购买记录会显示在这里。"))}</section><section class="section"><h2>${esc(t("account.refund_progress","退款进度"))}</h2>${refundRows.length?table([t("account.request_time","申请时间"),t("account.orders_count","订单"),t("common.amount","金额"),t("common.status","状态"),t("account.reason","原因")],refundRows):empty(t("account.no_refunds","暂无退款申请"),t("account.no_refunds_body","申请退款后，审核和原渠道退款进度会显示在这里。"))}</section><dialog id="invoice-request-dialog" class="action-dialog"><form data-form="invoice-request"><input type="hidden" name="order_id"><div class="section-head"><h2>${esc(t("invoice.request_title","申请发票"))}</h2><button type="button" class="text-link" data-action="close-invoice-request">${esc(t("common.close","关闭"))}</button></div><label>${esc(t("invoice.type","发票类型"))}<select name="type"><option value="personal">${esc(t("invoice.personal","个人"))}</option><option value="company">${esc(t("invoice.company","企业"))}</option></select></label>${field(t("invoice.title","发票抬头"),"title",user.email,"text","required minlength=2 maxlength=200")}${field(t("invoice.tax_id","纳税人识别号（企业必填）"),"tax_id","","text","maxlength=64")}${field(t("invoice.email","接收邮箱"),"email",user.email,"email","required maxlength=254")}${formError}<div class="actions"><button>${esc(t("invoice.submit","提交开票申请"))}</button><button type="button" class="secondary" data-action="close-invoice-request">${esc(t("common.cancel","取消"))}</button></div></form></dialog><details class="card section account-security"><summary>${esc(t("account.security_sessions","账号安全与登录会话"))}</summary><div class="grid two section"><section><h2>${esc(t("account.change_password","修改密码"))}</h2><form data-form="password">${field(t("account.current_password","当前密码"),"current_password","","password","required autocomplete=current-password")}${field(t("account.new_password","新密码"),"new_password","","password","required minlength=12 maxlength=128 autocomplete=new-password")}${formError}<button>${esc(t("account.save_relogin","保存并重新登录"))}</button></form></section><section><h2>${esc(t("account.change_email","修改邮箱"))}</h2><form data-form="change-email">${field(t("account.new_email","新邮箱"),"email","","email","required")}${field(t("account.current_password","当前密码"),"password","","password","required autocomplete=current-password")}${formError}<button class="secondary">${esc(t("account.send_confirm","发送确认邮件"))}</button></form></section></div>${table([t("common.type","类型"),t("common.created","创建时间"),t("common.expires","到期时间"),t("common.action","操作")],sessions.map(s=>`<tr><td>${esc(s.kind==="website"?t("account.website","网站"):t("account.software","软件"))} ${s.current?'<span class="pill">'+esc(t("account.current_session","当前会话"))+'</span>':""}</td><td>${time(s.created_at)}</td><td>${time(s.expires_at)}</td><td><button class="small secondary" data-action="revoke-session" data-id="${esc(s.id)}">${esc(t("account.logout_session","退出此会话"))}</button></td></tr>`))}</details>`;
 enhanceAccountPanels(products.length>0);
	 mountExternalDeliveries(main,products,{api});
}
function enhanceAccountPanels(hasProducts){
 const sections=[...main.querySelectorAll(':scope > section.section')],security=main.querySelector(':scope > .account-security');
 if(sections.length!==3||!security)return;
 security.open=true;
 const keys=['software','orders','refunds','security'],panels=[...sections,security],labels=[t('account.my_software','我的软件'),t('account.orders_invoices','订单与发票'),t('account.refund_progress','退款进度'),t('account.security_sessions','账号安全与登录会话')];
 const requested=location.hash.slice(2).split('/')[1],selected=keys.includes(requested)?requested:'software';
 const nav=document.createElement('nav');nav.className='account-tabs';nav.setAttribute('aria-label',t('account.title','我的账号'));
 nav.innerHTML=keys.map((key,index)=>`<a href="#/account/${key}" ${selected===key?'aria-current="page"':''}>${esc(labels[index])}</a>`).join('');
 main.insertBefore(nav,sections[0]);
 for(let i=0;i<panels.length;i++){panels[i].hidden=keys[i]!==selected;panels[i].classList.add('account-panel')}
 if(!hasProducts)sections[0].insertAdjacentHTML('beforeend',`<div class="actions section"><a class="button" href="#/home">${esc(t('nav.products','浏览产品'))}</a></div>`);
 if(!state.site.mail_enabled){const form=security.querySelector('form[data-form="change-email"]');form.hidden=true;const notice=document.createElement('p');notice.className='hint';notice.textContent=t('account.email_change_unavailable','如需修改邮箱，请联系商家。');form.before(notice)}
}
function table(head,rows){return `<div class="table-wrap"><table><thead><tr>${head.map(v=>`<th scope="col">${esc(v)}</th>`).join("")}</tr></thead><tbody>${rows.join("")}</tbody></table></div>`}
const adminTabs={overview:"经营概览",readiness:"上线检查",product:"品牌与产品",plans:"销售套餐",integration:"程序接入","integration-ai":"AI 快速接入",orders:"订单与发票",refunds:"退款审核",operations:"同步任务",translations:"语言与翻译",users:"客户管理",audit:"操作记录",settings:"系统设置"};
function allowedAdminTabs(role){if(role==="admin")return Object.keys(adminTabs);if(role==="operations")return ["overview","readiness","product","plans","orders","operations"];if(role==="support")return ["overview","orders","users"];return []}
const readinessTarget=item=>({mail:"#/admin/settings/mail",licensing:"#/admin/settings/licensing",licentivo:"#/admin/settings/licensing",translation:"#/admin/settings/translation",deepl:"#/admin/settings/translation",sandbox_payment:"#/admin/settings/payments",live_payment:"#/admin/settings/payments",payments:"#/admin/settings/payments",tls:"#/admin/settings/website",backup:"#/admin/settings/data"}[item.key]||item.target);
function adminNavigation(tab){
 const allowed=allowedAdminTabs(state.user.role),names={...adminTabs,product:'产品',plans:'销售套餐',orders:'订单与发票',refunds:'退款',users:'客户'},icons={overview:'home',product:'box',plans:'sliders',orders:'shopping-cart',refunds:'rotate-ccw',users:'users',settings:'settings',readiness:'check-circle',operations:'key',translations:'globe',audit:'activity'};
 const link=id=>allowed.includes(id)?`<a href="#/admin/${id}" ${tab===id?'class="active" aria-current="page"':''}>${icon(icons[id])}<span>${names[id]}</span></a>`:'';
 icons.integration='key';
 icons['integration-ai']='key';
 const tools=['integration','integration-ai','readiness','operations','translations','audit'].filter(id=>allowed.includes(id));
 return `<aside class="sidebar" aria-label="后台导航"><a class="workspace-brand" href="#/admin/overview"><span class="mark" aria-hidden="true">L</span><span>Licentivo Launch</span></a><nav class="sidebar-links"><div class="nav-group">${['overview','product','plans','orders','refunds','users'].map(link).join('')}</div><div class="nav-group">${link('settings')}${tools.length?`<details class="nav-tools" ${tools.includes(tab)?'open':''}><summary>运营工具</summary>${tools.map(link).join('')}</details>`:''}</div></nav><div class="sidebar-account"><a href="#/account">${icon('user')}<span><strong>${state.user.role==='admin'?'店铺管理员':state.user.role==='operations'?'运营人员':'客服人员'}</strong><small>${esc(state.user.email)}</small></span></a><a href="#/home">${icon('external-link')}查看商店</a><button type="button" class="text-link" data-action="logout">退出登录</button></div></aside><div class="mobile-workspace-bar"><a class="workspace-brand" href="#/admin/overview">Licentivo Launch</a><label>管理页面<select data-admin-section aria-label="选择后台页面">${Object.entries(names).filter(([id])=>allowed.includes(id)).map(([id,name])=>`<option value="${id}" ${id===tab?'selected':''}>${name}</option>`).join('')}</select></label><a href="#/home">查看商店</a></div>`;
}
async function readinessView(content){
 const report=await api("/admin/readiness");
 const label={ready:"正常",warning:"待验证",failed:"异常"};
 const actions={mysql:"重新检测",mail:"发送真实测试邮件",licentivo:"检查读写权限",deepl:"检查额度接口",payments:"检查支付账号"};
 const rows=report.checks.map(check=>`<article class="card health-card ${esc(check.status)}"><div class="section-head"><div><p class="eyebrow">${esc(check.key)}</p><h2>${esc(check.label)}</h2></div><span class="pill ${check.status==="ready"?"":check.status==="warning"?"orange":"red"}">${esc(label[check.status]||check.status)}</span></div><p>${esc(check.detail)}</p><p class="hint">${check.checked_at?`最近检测：${time(check.checked_at)}`:"尚无成功检测记录"}</p><div class="actions">${actions[check.key]&&state.user.role==="admin"?`<button class="small secondary" data-action="run-readiness-check" data-key="${esc(check.key)}">${esc(actions[check.key])}</button>`:""}${check.target?`<a href="${esc(readinessTarget(check))}">打开相关设置</a>`:""}</div></article>`).join("");
 content.innerHTML=`<div class="section-head"><div><p class="eyebrow">Preflight</p><h1>上线检查</h1><p class="hint">检测结果来自真实数据库心跳、外部 API、Webhook 事件、备份记录和后台任务心跳。</p></div><span class="pill ${report.ready_count===report.total?"":"orange"}">${report.ready_count} / ${report.total} 正常</span></div><div class="notice">支付 Webhook 只有在收到并验证过渠道测试事件后才会显示正常；仅保存密钥不会被视为通过。邮件检测会真实发送一封邮件到当前管理员邮箱。</div><div class="health-grid">${rows}</div>`;
}
async function connectionsForPlans(force=false){
 let [connections,external]=await Promise.all([api("/admin/connections"),api("/admin/licensing/connections")]);
 if(connections.licensing_provider==="custom_http"){state.licenseCatalogSyncError="";return {...connections,external}}
 const recentlySynced=Date.now()-state.licenseCatalogSyncedAt<30000;
 if(force||!recentlySynced){
  try{
   await api("/admin/connections/licentivo","POST",{});
   state.licenseCatalogSyncedAt=Date.now();
   state.licenseCatalogSyncError="";
  }catch(error){
   state.licenseCatalogSyncedAt=Date.now();
   state.licenseCatalogSyncError=error.code==="LICENSE_API_KEY_REQUIRED"?"":error.message;
  }
 }
 connections=await api("/admin/connections");return {...connections,external};
}
function planLicenseNotice(context,inEditor=false){
 const status=planLicenseStatus(context,state.licenseCatalogSyncError);
 const configure=`<a class="button small secondary" href="#/admin/settings/licensing">配置授权系统</a>`;
 const retry=context.licensing?`<button type="button" class="small secondary" data-action="${inEditor?"refresh-plan-license-catalog":"refresh-license-catalog"}">重新读取策略</button>`:"";
 const detail=state.licenseCatalogSyncError&&context.licensing?`<p class="hint">${esc(state.licenseCatalogSyncError)}</p>`:"";
 return `<div class="notice ${status.kind==="ready"?"":"warning"} plan-license-notice"><strong>${esc(status.title)}</strong><p>${esc(status.message)}</p>${detail}<div class="actions">${configure}${retry}</div></div>`;
}
function planPolicyPicker(context,selectedPolicy="",savedMapping=null){
 const selected=context.selectablePolicies.find(policy=>policy.id===selectedPolicy);
 const missing=savedMapping&&!context.selectablePolicies.some(policy=>policy.id===savedMapping.policy_id);
 const warning=missing?'<div class="notice warning">原版本的授权策略已不在可用列表中。请选择新策略，或保存为待绑定草稿；新草稿不会继承旧授权绑定。</div>':"";
 const picker=context.selectablePolicies.length?`<label>连接到 Licentivo 产品与策略<select name="policy_id"><option value="">暂不绑定，仅保存草稿</option>${context.selectablePolicies.map(policy=>`<option value="${esc(policy.id)}" ${policy.id===selectedPolicy?"selected":""}>${esc(context.policyLabel(policy))}</option>`).join("")}</select></label><div class="policy-summary" data-policy-summary aria-live="polite">${planPolicySummary(selected,context)}</div>`:"";
 return `${planLicenseNotice(context,true)}${warning}${picker}`;
}
function planPolicySummary(policy,context){
 if(!policy)return '<p class="hint">尚未选择授权策略。本次保存为待绑定草稿；后续可编辑草稿并选择策略。</p>';
 const perpetual=policy.mode==="perpetual";
 const duration=perpetual?"永久有效":policy.duration_days?`${policy.duration_days} 天（由 Licentivo 定义）`:"限时授权";
 const devices=policy.max_devices?`${policy.max_devices} 台设备/席位`:"以 Licentivo 策略为准";
 const features=policy.features?.length?policy.features.join("、"):"以 Licentivo 策略为准";
 return `<div class="policy-summary-head"><div><span class="eyebrow">将连接到</span><strong>${esc(context.productName(policy.product_id))} · ${esc(policy.name)}</strong></div><span class="pill">${perpetual?"永久授权":"限时授权"}</span></div><div class="policy-summary-grid"><div><span>Licentivo 有效期</span><strong>${esc(duration)}</strong></div><div><span>设备或席位</span><strong>${esc(devices)}</strong></div><div><span>策略版本</span><strong>v${esc(policy.version)}</strong></div><div><span>功能</span><strong>${esc(features)}</strong></div></div><p>授权方式、设备数和功能均从 Licentivo 读取。保存后，这个销售套餐版本会自动绑定到该策略，付款成功时按此策略发放授权。</p>`;
}
function updatePlanPolicyForm(form){
 const context=state.licenseContext||licensingContext(null);
 const policy=context.selectablePolicies.find(v=>v.id===form.elements.namedItem("policy_id")?.value);
 const summary=form.querySelector("[data-policy-summary]");
 if(summary)summary.innerHTML=planPolicySummary(policy,context);
 const draftType=form.elements.namedItem("draft_purchase_type");
 const perpetual=planPricingMode(policy,draftType?.value)==="perpetual";
 const modePicker=form.querySelector("[data-draft-pricing-mode]");
 if(modePicker)modePicker.hidden=Boolean(policy);
 if(draftType){if(policy)draftType.value=planPricingMode(policy);draftType.disabled=Boolean(policy)}
 const pricingNotice=form.querySelector("[data-plan-pricing-notice]");
 if(pricingNotice)pricingNotice.innerHTML=policy?`<div class="summary-strip"><span>已选择 ${perpetual?"永久":"限期"}授权</span><strong>${esc(policy.name)}</strong><button type="button" class="small secondary" data-action="plan-step" data-step="2">更换策略</button></div>`:`<div class="notice warning"><strong>${esc(planLicenseStatus(context,state.licenseCatalogSyncError).title)}</strong><p>以下价格可先保存为草稿。选择 Licentivo 策略后，系统会按策略确定限期或永久授权。</p><button type="button" class="small secondary" data-action="plan-step" data-step="2">${context.selectablePolicies.length?"选择授权策略":"查看接入说明"}</button></div>`;
 const fixedPricing=form.querySelector("[data-fixed-pricing]");
 if(fixedPricing){fixedPricing.hidden=perpetual;fixedPricing.querySelectorAll("input,select").forEach(control=>control.disabled=perpetual);for(const periods of [1,3,6,12]){const price=form.elements.namedItem(`option_${periods}_price`);if(price)price.required=form.elements.namedItem(`option_${periods}_enabled`)?.value==="true"}}
 const perpetualPricing=form.querySelector("[data-perpetual-pricing]");
 if(perpetualPricing){perpetualPricing.hidden=!perpetual;perpetualPricing.querySelectorAll("input,select").forEach(control=>control.disabled=!perpetual)}
 const perpetualNotice=form.querySelector("[data-perpetual-notice]");if(perpetualNotice)perpetualNotice.textContent=policy?"当前 Licentivo 策略是永久授权，只收取一次永久套餐费用。":"当前草稿采用永久授权，填写一次支付总价；销售前需完成所选授权系统的永久套餐映射。";
 const upgrade=form.querySelector("[data-plan-upgrade]");
 if(upgrade){upgrade.hidden=!perpetual;upgrade.querySelectorAll("input").forEach(control=>control.disabled=!perpetual)}
 const busy=Boolean(form.dataset.saving||form.dataset.refreshing);
 const picker=form.querySelector("[data-plan-license-picker]");if(picker)picker.querySelectorAll("select,button").forEach(control=>control.disabled=busy);
 const save=form.querySelector("[data-plan-save]");if(save){save.textContent=form.dataset.saving?"正在保存…":policy?"保存套餐并绑定授权":"保存草稿";save.disabled=busy||!(state.admin?.products?.length)}
 const saveNote=form.querySelector("[data-plan-save-note]");if(saveNote)saveNote.textContent=policy?"保存后会绑定所选 Licentivo 策略。完成授权和收款配置、发布后才可购买。":"本次仅保存待绑定草稿。测试预览可以展示套餐；销售前需完成所选授权系统的套餐映射及收款配置。";
}
function showProductWizardStep(form,index){
 const next=Math.max(0,Math.min(productWizardSteps.length-1,Number(index)||0));
 form.dataset.step=String(next);
 form.querySelectorAll("[data-product-pane]").forEach(pane=>pane.hidden=Number(pane.dataset.productPane)!==next);
 form.querySelectorAll("[data-product-step]").forEach(button=>{const active=Number(button.dataset.step)===next;button.classList.toggle("active",active);if(active)button.setAttribute("aria-current","step");else button.removeAttribute("aria-current")});
 const progress=form.querySelector("[data-product-progress]");if(progress)progress.textContent=`${productWizardSteps[next]} · ${next+1} / ${productWizardSteps.length}`;
 form.querySelector("[data-product-pane]:not([hidden]) input:not([type=hidden]):not(:disabled), [data-product-pane]:not([hidden]) select:not(:disabled), [data-product-pane]:not([hidden]) textarea:not(:disabled)")?.focus();
}
function productDownloadEditor(platform,product){
 const current=(product.downloads||[]).find(item=>item.platform===platform.key),enabled=Boolean(current),source=current?.source||"upload",existingUpload=current?.source==="upload";
 const existing=existingUpload?`<div class="uploaded-file"><div><strong>${esc(current.file_name||"已上传安装包")}</strong><span>${esc([current.version,formatBytes(current.size_bytes)].filter(Boolean).join(" · "))}</span>${current.sha256?`<code>${esc(current.sha256)}</code>`:""}</div><a href="${esc(current.url)}">下载检查</a></div>`:"";
 const requirements=product.content.platform_requirements?.[platform.key]||"";
 return `<article class="platform-download-card ${enabled?"enabled":""}" data-platform-card="${platform.key}" data-existing-upload="${existingUpload}"><header><div><h4>${esc(platform.label)}</h4><p>支持 ${esc(platform.formats)}</p></div><label class="check platform-toggle"><input type="checkbox" name="${platform.key}_enabled" value="true" ${enabled?"checked":""}>提供此版本</label></header><div class="platform-download-fields">${area(`${platform.label} 系统要求`,`${platform.key}_requirements`,requirements,"maxlength=4000 rows=3 placeholder=填写操作系统版本、芯片、内存或设备要求")}<div class="form-row">${field("软件版本",`${platform.key}_version`,current?.version||product.content.version||"","text","maxlength=80 placeholder=例如：2.4.1") }<label>安装包来源<select name="${platform.key}_source"><option value="upload" ${source==="upload"?"selected":""}>上传到本服务器</option><option value="external" ${source==="external"?"selected":""}>外部 HTTPS 地址</option></select></label></div><div data-download-source="upload">${existing}<label>${existingUpload?"替换安装包（不选择则保留原文件）":"上传安装包"}<input type="file" name="${platform.key}_file" accept="${platform.accept}"><span class="field-help">最大 1 GB；文件大小与 SHA-256 会自动计算。</span></label></div><div data-download-source="external">${field("外部下载地址",`${platform.key}_url`,source==="external"?current?.url||"":"","url","placeholder=https://downloads.example.com/…")}<div class="form-row">${field("文件大小（字节，可选）",`${platform.key}_size_bytes`,source==="external"?current?.size_bytes||"":"","number","min=0 step=1 max=9007199254740991 placeholder=例如：104857600")}${field("SHA-256 校验值（可选）",`${platform.key}_sha256`,source==="external"?current?.sha256||"":"","text","maxlength=64 pattern=[A-Fa-f0-9]{64} placeholder=64 位十六进制字符")}</div></div></div></article>`;
}
function updateProductDownloadFields(form,platform){
 const card=form.querySelector(`[data-platform-card="${platform}"]`);if(!card)return;
 const enabled=card.querySelector(`[name="${platform}_enabled"]`).checked,source=card.querySelector(`[name="${platform}_source"]`).value;
 card.classList.toggle("enabled",enabled);
 const requirements=card.querySelector(`[name="${platform}_requirements"]`),version=card.querySelector(`[name="${platform}_version"]`),sourceSelect=card.querySelector(`[name="${platform}_source"]`),file=card.querySelector(`[name="${platform}_file"]`),url=card.querySelector(`[name="${platform}_url"]`),size=card.querySelector(`[name="${platform}_size_bytes"]`),checksum=card.querySelector(`[name="${platform}_sha256"]`);
 requirements.disabled=!enabled;requirements.required=enabled;
 version.disabled=!enabled;version.required=enabled;sourceSelect.disabled=!enabled;
 card.querySelector('[data-download-source="upload"]').hidden=!enabled||source!=="upload";
 card.querySelector('[data-download-source="external"]').hidden=!enabled||source!=="external";
 file.disabled=!enabled||source!=="upload";file.required=enabled&&source==="upload"&&card.dataset.existingUpload!=="true";
 url.disabled=!enabled||source!=="external";url.required=enabled&&source==="external";
 size.disabled=!enabled||source!=="external";checksum.disabled=!enabled||source!=="external";
}
function productEditor(product={}){
 const p={content:{},downloads:[],status:"draft",...product};p.content={...p.content,platform_requirements:{...(p.content?.platform_requirements||{})}};p.downloads=p.downloads||[];
 const nav=productWizardSteps.map((label,index)=>`<button type="button" class="plan-step" data-action="product-step" data-product-step data-step="${index}"><span>${index+1}</span>${esc(label)}</button>`).join("");
 const legacy=p.content.download_url&&!p.downloads.some(item=>item.platform!=="universal")?`<div class="notice warning">这个产品仍在使用旧的通用下载地址。选择并保存至少一个平台后，系统会改用平台安装包。</div>`:"";
 const el=document.getElementById("product-editor");
 el.innerHTML=`<form data-form="product" class="plan-wizard product-wizard" data-step="0" novalidate><input type="hidden" name="id" value="${esc(p.id||"")}"><header class="plan-wizard-header"><div><div class="title-line"><h2 id="product-editor-title">${p.id?"修改软件产品":"创建软件产品"}</h2><span class="pill">${p.id?"编辑":"新建"}</span></div><p>按步骤维护产品资料、独立文档、政策和各平台安装包。</p></div><button type="button" class="dialog-close" data-action="close-product" aria-label="关闭">×</button></header><div class="plan-wizard-body"><nav class="plan-steps" aria-label="产品设置步骤">${nav}</nav><div class="plan-wizard-content"><section data-product-pane="0"><h3>基本信息</h3><p class="hint">填写客户在产品目录、介绍页和结账流程中看到的名称。</p>${field("产品名称","name",p.name,"text","required minlength=2 maxlength=160 autofocus")}${field("产品页面简称","slug",p.slug,"text","required pattern=[a-z0-9-]+ maxlength=80 placeholder=例如：test-editor")}${field("一句话介绍","summary",p.content.summary,"text","maxlength=500 placeholder=用一句话说明这个软件解决什么问题")}</section><section data-product-pane="1" hidden><h3>产品介绍</h3><p class="hint">介绍和功能会显示在公开产品页；不同语言由系统设置中的 DeepL 自动处理。</p>${area("详细介绍","description",p.content.description,"maxlength=16000 rows=7")}${area("核心功能（每行一项）","features",p.content.features?.join("\n")||"","maxlength=6030 rows=7 placeholder=例如：跨设备同步")}${area("通用系统要求（可选）","requirements",p.content.requirements,"maxlength=4000 rows=4 placeholder=适用于全部平台的共同要求；各平台要求在后续步骤填写")}</section><section data-product-pane="2" hidden><h3>产品文档与政策</h3><p class="hint">安装与故障内容会进入独立产品文档页，可在光标位置插入说明图片；政策只通过文档页与页脚小入口提供。发布前请交由法律及财税人员复核。</p><h4>安装与使用文档</h4>${documentEditor("安装指南","installation_guide",p.content.installation_guide)}${documentEditor("升级指南","upgrade_guide",p.content.upgrade_guide)}${documentEditor("卸载指南","uninstall_guide",p.content.uninstall_guide)}${documentEditor("故障排查","troubleshooting_guide",p.content.troubleshooting_guide)}<h4>服务与交易政策</h4>${area("服务条款","terms_of_service",p.content.terms_of_service,"maxlength=16000 rows=5")}${area("隐私政策","privacy_policy",p.content.privacy_policy,"maxlength=16000 rows=5")}${area("退款规则","refund_policy",p.content.refund_policy,"maxlength=16000 rows=5")}${area("开票流程","invoice_guide",p.content.invoice_guide,"maxlength=16000 rows=5")}${area("法律与税务说明","legal_tax_notice",p.content.legal_tax_notice,"maxlength=16000 rows=5")}</section><section data-product-pane="3" hidden><h3>平台与安装包</h3><p class="hint">只启用实际提供的平台。每个平台分别填写系统要求、版本号、安装包、文件大小和校验值。</p>${legacy}<div class="platform-download-grid">${downloadPlatforms.map(platform=>productDownloadEditor(platform,p)).join("")}</div></section><section data-product-pane="4" hidden><h3>发布设置</h3><p class="hint">草稿只在后台可见；发布后客户可以访问介绍页，并下载已启用的平台版本。</p>${field("产品版本备注（可选）","version",p.content.version,"text","maxlength=80 placeholder=例如：2026 秋季版")}<label>展示状态<select name="status"><option value="draft" ${p.status==="draft"?"selected":""}>草稿</option><option value="published" ${p.status==="published"?"selected":""}>发布</option><option value="archived" ${p.status==="archived"?"selected":""}>下架</option></select></label><div class="notice">安装包会按平台单独保存。未启用的平台不会出现在客户下载页。</div></section></div></div><footer class="plan-wizard-footer"><div><span data-product-progress>基本信息 · 1 / 5</span>${formError}</div><div class="actions"><button type="button" class="secondary" data-action="close-product">取消</button><button>保存产品</button></div></footer></form>`;
 if(!el.open)el.showModal();
 const form=el.querySelector("form");form._documentImages=new Map();downloadPlatforms.forEach(platform=>updateProductDownloadFields(form,platform.key));showProductWizardStep(form,0);
}
function planList(plans,licenseContext,products=[]){
 if(!plans.length)return empty("还没有套餐","先创建产品，再添加销售套餐。");
 const status=p=>p.published?"已发布至测试":p.status==="archived"?"已下架":"草稿";
 const controls=p=>`<button class="small secondary" data-action="edit-plan" data-id="${esc(p.id)}">编辑</button><button class="small secondary" data-action="open-plan-versions" data-id="${esc(p.id)}" data-name="${esc(p.name)}">历史版本</button><button class="small" data-action="publish-plan" data-id="${esc(p.id)}" data-version="${esc(p.version_id)}">发布测试版</button><button class="small danger" data-action="archive-plan" data-id="${esc(p.id)}">下架</button>`;
 const strategy=p=>{const custom=licenseContext?.external?.mappings?.find(v=>v.version_id===p.version_id&&v.environment==="sandbox"&&licenseContext.external.bindings.some(b=>b.product_id===p.product_id&&b.environment==="sandbox"&&b.provider==="custom_http"&&b.connection_id===v.connection_id));if(custom)return "自定义 · "+custom.product_ref+" / "+custom.plan_ref;const mapping=licenseContext?.mappings.find(v=>v.version_id===p.version_id);const policy=mapping&&licenseContext?.policies.find(v=>v.id===mapping.policy_id);return policy?licenseContext.policyLabel(policy):mapping?"原策略不可用，请重新绑定":"待绑定授权（仅预览）"};
 const productName=p=>products.find(product=>product.id===p.product_id)?.name||"未知产品";
 return `<div class="desktop-plans">${table(["产品","套餐","价格","授权配置","版本","状态","操作"],plans.map(p=>`<tr><td>${esc(productName(p))}</td><td><strong>${esc(p.name)}</strong></td><td>${esc(planPriceSummary(p))}</td><td>${esc(strategy(p))}</td><td>v${p.version}</td><td><span class="pill ${p.published?"":"gray"}">${status(p)}</span></td><td><div class="actions">${controls(p)}</div></td></tr>`))}</div><div class="mobile-plans">${plans.map(p=>`<article class="plan-card"><div class="plan-card-heading"><h2>${esc(p.name)}</h2><span class="pill ${p.published?"":"gray"}">${status(p)}</span></div><p class="hint">${esc(productName(p))}</p><p class="plan-price">${esc(planPriceSummary(p))}</p><p class="plan-meta">${esc(strategy(p))} · 销售版本 v${p.version}</p><div class="actions">${controls(p)}</div></article>`).join("")}</div>`;
}
function stageDocumentImage(input){
 const file=input.files?.[0],form=input.closest('form[data-form="product"]');if(!file||!form)return;
 const allowed=["image/png","image/jpeg","image/webp","image/gif"];
 if(!allowed.includes(file.type)){input.value="";toast("文档图片只支持 PNG、JPEG、WebP 或 GIF");return}
 if(file.size<=0||file.size>8*1024*1024){input.value="";toast("文档图片不能为空且不能超过 8 MB");return}
 const fieldName=input.dataset.documentImageInput,textarea=form.elements.namedItem(fieldName);if(!textarea)return;
 const token=crypto.randomUUID?crypto.randomUUID():[...crypto.getRandomValues(new Uint8Array(18))].map(value=>value.toString(16).padStart(2,"0")).join("");
 const alt=file.name.replace(/\.[^.]+$/,"" ).replace(/[\[\]\(\)\r\n]/g," ").trim()||"说明图片",markup=`\n![${alt}](upload:${token})\n`,start=textarea.selectionStart??textarea.value.length,end=textarea.selectionEnd??start;
 if(textarea.value.length-(end-start)+markup.length>16000){input.value="";toast("文档内容已达到长度上限，无法继续插入图片");return}
 textarea.setRangeText(markup,start,end,"end");textarea.dispatchEvent(new Event("input",{bubbles:true}));
 form._documentImages??=new Map();form._documentImages.set(token,file);
 const staging=form.querySelector(`[data-document-image-staging="${fieldName}"]`),preview=URL.createObjectURL(file);
 if(staging)staging.insertAdjacentHTML("beforeend",`<span class="document-image-chip"><img src="${esc(preview)}" alt=""><span>${esc(file.name)}<small>${esc(formatBytes(file.size))}</small></span></span>`);
 input.value="";textarea.focus();toast("图片已插入文档，保存产品时会自动上传");
}
document.addEventListener("change",async event=>{
 if(event.target.matches("[data-admin-section]")){if(!confirmDiscardChanges()){event.target.value=event.target.querySelector('option[selected]')?.value||"overview";return}navigate("admin/"+event.target.value)}
 if(event.target.matches("[data-language]")){if(!confirmDiscardChanges()){event.target.value=state.locale;return}state.locale=event.target.value;localStorage.setItem("launch_locale",state.locale);document.documentElement.lang=state.locale;await render()}
 if(event.target.matches('[name="resend_encryption"]')){const port=event.target.form?.elements.namedItem("resend_port");if(port)port.value=event.target.value==="implicit_tls"?"465":"587"}
 if(event.target.matches('[name="resend_port"]')){const encryption=event.target.form?.elements.namedItem("resend_encryption");if(encryption)encryption.value=["465","2465"].includes(event.target.value)?"implicit_tls":"starttls"}
 if(event.target.matches('form[data-form="transport-settings"] [name="mode"]')){const form=event.target.form;const input=form?.elements.namedItem("public_url");if(input){try{const target=new URL(input.value);target.protocol=event.target.value+":";if(event.target.value==="https"&&target.port==="80")target.port="";if(event.target.value==="http"&&target.port==="443")target.port="";input.value=target.origin}catch{}}const button=form?.querySelector('button[type="submit"]');if(button)button.textContent=`保存并${event.target.value===form.dataset.currentMode?"应用":"切换到"} ${event.target.value.toUpperCase()}`}
 if(event.target.matches('form[data-form="plan"] [name="policy_id"], form[data-form="plan"] [name="draft_purchase_type"], form[data-form="plan"] [name$="_enabled"]'))updatePlanPolicyForm(event.target.form);
 if(event.target.matches('form[data-form="plan"] [name="currency"]')){const badge=event.target.form.querySelector("[data-plan-currency]");if(badge)badge.textContent=event.target.value.toUpperCase()}
 if(event.target.matches('[data-platform-card] input[type="checkbox"], [data-platform-card] select')){const card=event.target.closest("[data-platform-card]");if(card)updateProductDownloadFields(event.target.form,card.dataset.platformCard)}
 if(event.target.matches('[data-document-image-input]'))stageDocumentImage(event.target);
 if(event.target.matches('form[data-form="checkout"] [name="purchase_periods"]')){const price=document.querySelector("[data-checkout-price]");const duration=document.querySelector("[data-checkout-duration]");if(price)price.textContent=moneyMinor(event.target.form.dataset.currency,Number(event.target.dataset.amount));if(duration)duration.textContent=periodLabel(Number(event.target.value))}
 if(event.target.matches('form[data-form="sale-quote"] [name="action"], form[data-form="sale-quote"] [name="plan_version_id"]'))updateSaleQuoteForm(event.target.form);
});
async function admin(tab="overview"){
 if(!state.user){authPage("login");return}if(state.user.role==="customer"){main.innerHTML=empty("无法访问","当前账号没有后台权限。");return}
 if(tab==="connections")tab="settings";
 const allowed=allowedAdminTabs(state.user.role);if(!adminTabs[tab]||!allowed.includes(tab))tab=state.user.role==="support"?"users":"overview";
 main.innerHTML=`<div class="layout">${adminNavigation(tab)}<section class="workspace"><div class="loading">正在加载…</div></section></div>`;
 const content=main.querySelector(".workspace");
 if(tab==="integration"){await integrationWizard(content,api);return}
 if(tab==="integration-ai"){await integrationAssistant(content,api);return}
 if(["orders","refunds","operations"].includes(tab)){await commerceAdmin(content,tab);return}
	if(tab==="readiness"){await readinessView(content);return}
	if(tab==="translations"){await translationsView(content);return}
 if(tab==="overview"||tab==="settings"){
  const v=await api("/admin/dashboard");
  const readiness=v.readiness||[],readyCount=readiness.filter(item=>item.ready).length;
  content.innerHTML=tab==="overview"?`<p class="eyebrow">Workspace</p><div class="section-head"><h1>经营概览</h1>${state.user.full_access?'<span class="pill">管理员 · 完整权限</span>':""}</div><div class="stats">${[["注册客户",v.users],["正式订单",v.orders],["待发送邮件",v.pending_mail],["需处理邮件",v.failed_mail]].map(([label,value])=>`<div class="stat"><p>${label}</p><strong>${value}</strong></div>`).join("")}</div><section class="card readiness"><div class="section-head"><div><h2>上线检查</h2><p class="hint">此处显示配置概览；请进入独立页面执行真实连接与运行状态检测。</p></div><div class="actions"><span class="pill ${readyCount===readiness.length?"":"orange"}">${readyCount} / ${readiness.length} 就绪</span><a class="button secondary" href="#/admin/readiness">运行上线检查</a></div></div><div class="readiness-grid">${readiness.map(item=>`<a class="readiness-item ${item.ready?"ready":""}" href="${esc(readinessTarget(item))}"><span class="readiness-icon" aria-hidden="true">${item.ready?"✓":"!"}</span><span><strong>${esc(item.label)}</strong><small>${esc(item.key==="mail"&&!v.mail_configured?"未启用（可选）":item.ready?"已配置":item.detail)}</small></span></a>`).join("")}</div></section><div class="card"><h2>管理产品与套餐</h2><p>每个软件产品拥有自己的介绍、下载页和销售套餐；客户会在首页选择对应产品。</p><div class="actions"><a class="button" href="#/admin/product">管理产品</a><a class="button secondary" href="#/admin/plans">管理套餐</a></div></div>`:`<h1 class="settings-title">系统设置</h1>`;if(tab==="settings")await settingsForms(content,v);return;
 }
 if(tab==="product"){
	const [catalog,translation]=await Promise.all([api("/admin/catalog"),api("/admin/translations")]);state.admin=catalog;const {brand,products}=state.admin;const sourceName=translation.supported_locales.find(v=>v.code===translation.source_locale)?.name||translation.source_locale;
  const statusName=value=>({draft:"草稿",published:"已发布",archived:"已下架"}[value]||value);
  const productRows=products.map(p=>`<tr><td><strong>${esc(p.name)}</strong>${p.id===catalog.default_product_id?' <span class="pill">首页默认</span>':""}</td><td><code>${esc(p.slug)}</code></td><td>${esc(productVersionSummary(p))}</td><td><span class="pill ${p.status==="published"?"":"gray"}">${esc(statusName(p.status))}</span></td><td><div class="actions"><button class="small secondary" data-action="edit-product" data-id="${esc(p.id)}">编辑</button>${p.status==="published"&&p.id!==catalog.default_product_id?`<button class="small secondary" data-action="set-default-product" data-id="${esc(p.id)}">设为首页默认</button>`:""}${p.status==="published"?`<a href="#/product/${encodeURIComponent(p.slug)}">查看页面</a>`:""}</div></td></tr>`);
  content.innerHTML=`<div class="section-head"><div><h1>品牌与产品</h1><p class="hint">先管理软件产品；品牌资料可在下方按需展开修改。</p></div><button data-action="new-product">添加软件产品</button></div><div class="notice">当前内容原文：<strong>${esc(sourceName)}</strong>。你只需维护这一种语言；${translation.configured?"保存后 DeepL 会在后台自动更新另外九种译文。":"先到“系统设置”连接 DeepL，原文仍会正常保存。"} <a href="#/admin/settings/translation">打开翻译设置</a></div><section class="card section product-list-section"><div class="section-head"><div><h2>软件产品</h2><p class="hint">每个产品有独立的介绍、平台安装包、下载页和销售套餐。</p></div><span class="pill">${products.length} 个产品</span></div>${products.length?table(["产品","页面简称","平台版本","状态","操作"],productRows):empty("还没有产品","点击“添加软件产品”创建第一个软件产品。")}</section><details class="settings-group card section"><summary><span>品牌设置</span><span class="pill gray">按需修改</span></summary><div class="settings-group-body"><p class="hint">品牌名称、标语和支持邮箱会用于整个网站。</p><form data-form="brand">${field("品牌名称","name",brand.name,"text","required maxlength=160")}${field("简短标语","tagline",brand.tagline,"text","maxlength=240")}${area("品牌介绍","description",brand.description)}${field("支持邮箱","support_email",brand.support_email,"email")}${formError}<button>保存品牌</button></form></div></details><dialog id="product-editor" class="plan-editor-dialog" aria-labelledby="product-editor-title"></dialog>`;return;
 }
 if(tab==="plans"){
	const [catalog,translation,connections]=await Promise.all([api("/admin/catalog"),api("/admin/translations"),connectionsForPlans()]);state.admin=catalog;state.licenseContext=licensingContext(connections);const sourceName=translation.supported_locales.find(v=>v.code===translation.source_locale)?.name||translation.source_locale;
  const saveWarning=state.planSaveWarning?`<div class="notice warning" role="status">${esc(state.planSaveWarning)}</div>`:"";
  content.innerHTML=`<div class="section-head"><h1>销售套餐</h1><div class="actions">${state.licenseContext.licensing?'<button class="secondary" data-action="refresh-license-catalog">刷新授权策略</button>':""}<button data-action="new-plan" ${state.admin.products.length?"":"disabled"}>创建套餐</button></div></div>${saveWarning}<p><a href="#/admin/settings/licensing">选择 Licentivo 或自定义授权系统，并配置商品映射</a></p>${planLicenseNotice(state.licenseContext)}<p class="hint">每个套餐先选择所属软件产品。套餐名称请使用 ${esc(sourceName)}；保存后${translation.configured?"会自动翻译":"将在连接 DeepL 后翻译"}。编辑会创建新版本，旧价格不会被改写。</p>${planList(state.admin.plans,state.licenseContext,state.admin.products)}<dialog id="plan-editor" class="plan-editor-dialog" aria-labelledby="plan-editor-title"></dialog><dialog id="plan-versions-dialog" class="action-dialog wide-dialog" aria-labelledby="plan-versions-title"></dialog><dialog id="plan-version-detail-dialog" class="action-dialog" aria-labelledby="plan-version-detail-title"></dialog><div class="actions"><a href="/?preview=sandbox#/home" target="_blank" rel="noopener">查看测试预览 ↗</a></div>`;return;
 }
 if(tab==="users"){
  const users=await api("/admin/users");
  const statusLabel=u=>({active:"正常",disabled:"已停用",closed:"已关闭"}[u.status]||u.status);
  const userAction=u=>{if(state.user.role!=="admin")return "—";if(u.status==="closed")return '<span class="hint">不可操作</span>';const next=u.status==="active"?"disabled":"active";return `<button type="button" class="small ${next==="disabled"?"danger":"secondary"}" data-action="open-user-status" data-id="${esc(u.id)}" data-email="${esc(u.email)}" data-next-status="${next}">${next==="disabled"?"停用":"启用"}</button>`};
  const userRows=users.length?users.map(u=>`<tr><td>${esc(u.email)}</td><td>${u.verified?"已验证":"待验证"}</td><td><span class="pill ${u.status==="active"?"":"gray"}">${esc(statusLabel(u))}</span></td><td>${time(u.created_at)}</td><td>${userAction(u)}</td></tr>`):['<tr class="table-empty-row"><td colspan="5"><strong>暂无客户</strong><span class="hint">客户注册后会显示在这里。</span></td></tr>'];
  content.innerHTML=`<h1>客户管理</h1><p class="hint">这里只显示注册、购买和使用软件的客户。网站管理员、运营和客服请到“系统设置 → 网站人员管理”中维护。</p>${table(["客户邮箱","邮箱验证","账号状态","注册时间","操作"],userRows)}`;
  if(state.user.role==="admin"){
   content.insertAdjacentHTML("beforeend",`<dialog id="user-status-dialog" class="action-dialog"><form data-form="user-status"><input type="hidden" name="user_id"><input type="hidden" name="status"><div class="section-head"><h2 data-user-status-title>账号操作</h2><button type="button" class="text-link" data-action="close-user-status">关闭</button></div><p class="hint" data-user-status-message></p>${field("操作原因","reason","","text","required minlength=2 maxlength=500 autofocus")}${formError}<div class="actions"><button data-user-status-submit>确认</button><button type="button" class="secondary" data-action="close-user-status">取消</button></div></form></dialog>`);
  }
  return;
 }
 const rows=await api("/admin/audit");content.innerHTML=`<h1>操作记录</h1>${rows.length?table(["时间","操作","对象","原因"],rows.map(row=>`<tr><td>${time(row.created_at)}</td><td>${esc(row.action)}</td><td>${esc(row.object_type)}</td><td>${esc(row.reason||"—")}</td></tr>`)):empty("暂无操作记录","")}`;
}
const planWizardSteps=["基本信息","购买期限与总价","授权系统","套餐介绍","启用与续费"];
async function openPlanVersions(planID,planName){
 const versions=await api(`/admin/plans/${encodeURIComponent(planID)}/versions`);state.planVersions={planID,planName,versions};
 const dialog=document.getElementById("plan-versions-dialog");
 const rows=versions.map(version=>{const active=version.publications?.map(item=>item.environment==="live"?"正式销售":"测试预览").join("、")||"—";const policy=version.remote_policy_name?`${version.remote_product_name} · ${version.remote_policy_name} · v${version.remote_policy_version}`:version.remote_policy_id||"未绑定";return `<tr><td><strong>v${version.version}</strong>${version.version===versions[0]?.version?' <span class="pill gray">最新草稿</span>':""}</td><td>${esc(planPriceSummary(version))}</td><td>${esc(policy)}</td><td>${version.first_published_at?time(version.first_published_at):"未发布"}</td><td>${version.active_order_count}</td><td>${esc(active)}</td><td><div class="actions"><button class="small secondary" data-action="view-plan-version" data-version="${esc(version.version_id)}">查看</button><button class="small secondary" data-action="rollback-plan-version" data-plan="${esc(planID)}" data-version="${esc(version.version_id)}" data-environment="sandbox">回滚测试</button><button class="small" data-action="rollback-plan-version" data-plan="${esc(planID)}" data-version="${esc(version.version_id)}" data-environment="live">回滚正式</button></div></td></tr>`});
 dialog.innerHTML=`<div class="section-head"><div><p class="eyebrow">不可变销售快照</p><h2 id="plan-versions-title">${esc(planName)} · 历史版本</h2></div><button type="button" class="dialog-close" data-action="close-plan-versions" aria-label="关闭">×</button></div><p class="hint">订单始终引用购买时的版本。回滚只改变之后公开销售的版本，不会修改旧订单、价格或授权快照。</p>${table(["版本","价格","授权策略","首次发布时间","使用中订单","当前发布","操作"],rows)}<div class="actions"><button type="button" class="secondary" data-action="close-plan-versions">关闭</button></div>`;
 dialog.showModal();
}
function viewPlanVersion(versionID){
 const version=state.planVersions?.versions.find(item=>item.version_id===versionID);if(!version)return;
 const dialog=document.getElementById("plan-version-detail-dialog");
 dialog.innerHTML=`<div class="section-head"><div><p class="eyebrow">版本 v${version.version}</p><h2 id="plan-version-detail-title">${esc(version.name)}</h2></div><button type="button" class="dialog-close" data-action="close-plan-version-detail" aria-label="关闭">×</button></div><dl class="account-facts"><div><dt>价格</dt><dd>${esc(planPriceSummary(version))}</dd></div><div><dt>授权策略</dt><dd>${esc(version.remote_policy_name||version.remote_policy_id||"未绑定")} · v${esc(version.remote_policy_version||"—")}</dd></div><div><dt>创建时间</dt><dd>${time(version.created_at)}</dd></div><div><dt>使用中订单</dt><dd>${version.active_order_count}</dd></div><div><dt>购买期限</dt><dd>${esc(version.purchase_type==="perpetual"?"永久":enabledPurchaseOptions(version).map(option=>periodLabel(option.periods)+" "+moneyMinor(version.currency,option.amount_minor)).join("；"))}</dd></div><div><dt>包含内容</dt><dd>${esc((version.highlights||[]).join("、")||"—")}</dd></div></dl><div class="actions"><button type="button" class="secondary" data-action="close-plan-version-detail">关闭</button></div>`;
 dialog.showModal();
}
function showPlanWizardStep(form,index){
 const next=Math.max(0,Math.min(planWizardSteps.length-1,Number(index)||0));
 form.dataset.step=String(next);
 form.querySelectorAll("[data-plan-pane]").forEach(pane=>pane.hidden=Number(pane.dataset.planPane)!==next);
 form.querySelectorAll("[data-plan-step]").forEach(button=>{const active=Number(button.dataset.step)===next;button.classList.toggle("active",active);if(active)button.setAttribute("aria-current","step");else button.removeAttribute("aria-current")});
 const progress=form.querySelector("[data-plan-progress]");if(progress)progress.textContent=`${planWizardSteps[next]} · ${next+1} / ${planWizardSteps.length}`;
 form.querySelector("[data-plan-pane]:not([hidden]) input:not([type=hidden]):not(:disabled), [data-plan-pane]:not([hidden]) select:not(:disabled), [data-plan-pane]:not([hidden]) textarea:not(:disabled)")?.focus();
}
function planEditor(plan={}){
 const p={currency:"USD",amount_minor:0,purchase_type:"fixed_term",term_unit:"month",term_count:1,allow_purchase:true,allow_renewal:true,...plan};
 const context=state.licenseContext||licensingContext(null);
 const savedMapping=context.mappings.find(item=>item.version_id===p.version_id);
 const selectedPolicy=savedMapping&&context.selectablePolicies.find(policy=>policy.id===savedMapping.policy_id);
 const perpetual=planPricingMode(selectedPolicy,p.purchase_type)==="perpetual";
 const products=state.admin?.products||[];
 const selectedProductID=p.product_id||state.admin?.default_product_id||products[0]?.id||"";
 const selectedProduct=products.find(product=>product.id===selectedProductID);
 const productField=p.id?`<input type="hidden" name="product_id" value="${esc(selectedProductID)}"><div class="summary-strip"><span>所属产品</span><strong>${esc(selectedProduct?.name||"未知产品")}</strong></div>`:`<label>所属软件产品<select name="product_id" required><option value="">请选择产品</option>${products.map(product=>`<option value="${esc(product.id)}" ${product.id===selectedProductID?"selected":""}>${esc(product.name)}</option>`).join("")}</select></label>`;
 const configured=new Map(planPurchaseOptions(p).map(option=>[option.periods,option]));
 const priceOption=periods=>{
  const option=configured.get(periods)||{periods,amount_minor:0,enabled:periods===1};
  const title=periods===1?"一个月":periods===3?"一个季度（3 个月）":periods===6?"半年（6 个月）":"一年（12 个月）";
  return `<div class="plan-price-option"><label>${title} · 允许购买<select name="option_${periods}_enabled"><option value="true" ${option.enabled?"selected":""}>开启</option><option value="false" ${option.enabled?"":"selected"}>停用</option></select></label>${field(`${title} · 一次支付总价`,`option_${periods}_price`,decimal({currency:p.currency,amount_minor:option.amount_minor}),"text","inputmode=decimal")}</div>`;
 };
 const nav=planWizardSteps.map((label,index)=>`<button type="button" class="plan-step" data-action="plan-step" data-plan-step data-step="${index}"><span>${index+1}</span>${esc(label)}</button>`).join("");
 const el=document.getElementById("plan-editor");
 el.innerHTML=`<form data-form="plan" class="plan-wizard" data-step="0" novalidate>
  <input type="hidden" name="id" value="${esc(p.id||"")}"><input type="hidden" name="version" value="${p.version||0}">
  <header class="plan-wizard-header"><div><div class="title-line"><h2 id="plan-editor-title">${p.id?"修改套餐":"创建套餐"}</h2><span class="pill" data-plan-currency>${esc(p.currency)}</span></div><p>按步骤填写，一次保存全部设置。编辑会创建新版本，已付款订单保持原价格。</p></div><button type="button" class="dialog-close" data-action="close-plan" aria-label="关闭">×</button></header>
  <div class="plan-wizard-body"><nav class="plan-steps" aria-label="套餐设置步骤">${nav}</nav><div class="plan-wizard-content">
   <section data-plan-pane="0"><h3>基本信息</h3><p class="hint">先选择套餐所属的软件产品，再填写客户在产品页和结账页看到的名称与说明。</p>
    ${productField}${field("套餐名称","name",p.name,"text","required maxlength=160")}${field("币种","currency",p.currency,"text","required pattern=[A-Z]{3} maxlength=3")}${area("套餐简介","summary",p.summary||"","maxlength=500 rows=4 placeholder=用一句话说明这个套餐适合谁")}
   </section>
   <section data-plan-pane="1" hidden><h3>购买期限与总价</h3><p class="hint">分别启用客户可购买的期限，并填写一次支付的总价。后台会按所选期限计算授权截止时间。</p>
    <div data-plan-pricing-notice aria-live="polite"></div>
    <label data-draft-pricing-mode>草稿授权类型<select name="draft_purchase_type"><option value="fixed_term" ${p.purchase_type==="perpetual"?"":"selected"}>限期授权</option><option value="perpetual" ${p.purchase_type==="perpetual"?"selected":""}>永久授权</option></select><span class="field-help">尚未绑定策略时可先选择；绑定后以 Licentivo 策略为准。</span></label>
    <div data-fixed-pricing ${perpetual?"hidden":""}><div class="plan-price-grid">${[1,3,6,12].map(priceOption).join("")}</div></div>
    <div data-perpetual-pricing ${perpetual?"":"hidden"}><div class="notice" data-perpetual-notice></div>${field("永久套餐价格","perpetual_price",decimal(p),"text",`required inputmode=decimal ${perpetual?"":"disabled"}`)}</div>
   </section>
   <section data-plan-pane="2" hidden><h3>授权系统</h3><p class="hint">自定义授权可先保存草稿，再到 <a href="#/admin/settings/licensing">授权系统</a> 配置商品与套餐映射。</p><p class="hint">选择付款成功后实际发放的软件授权。设备数、功能和离线规则均从 Licentivo 读取。</p><div class="plan-license-picker" data-plan-license-picker>${planPolicyPicker(context,selectedPolicy?.id||"",savedMapping)}</div></section>
   <section data-plan-pane="3" hidden><h3>套餐介绍</h3><p class="hint">这些内容会显示在公开产品页，帮助客户理解套餐包含什么。</p>${area("套餐包含内容（每行一项）","highlights",p.highlights?.join("\n")||"","maxlength=2000 rows=8 placeholder=例如：支持 3 台设备")}</section>
   <section data-plan-pane="4" hidden><h3>启用与续费</h3><p class="hint">保存后仍需发布测试版或正式版，客户才会看到新版本。</p>
    <label class="check"><input type="checkbox" name="allow_purchase" ${p.allow_purchase?"checked":""}>允许新客户购买</label><label class="check"><input type="checkbox" name="allow_renewal" ${p.allow_renewal?"checked":""}>允许已有客户续费</label>
    <div data-plan-upgrade ${perpetual?"":"hidden"}>${field("永久套餐固定升级价（可留空）","upgrade_price",p.permanent_upgrade_minor?decimal({currency:p.currency,amount_minor:p.permanent_upgrade_minor}):"","text",`inputmode=decimal ${perpetual?"":"disabled"}`)}</div><div class="notice" data-plan-save-note></div>
   </section>
  </div></div>
  <footer class="plan-wizard-footer"><div><span data-plan-progress>基本信息 · 1 / 5</span>${formError}</div><div class="actions"><button type="button" class="secondary" data-action="close-plan">取消</button><button data-plan-save>保存草稿</button></div></footer>
 </form>`;
 const form=el.querySelector("form");form._savedMapping=savedMapping;
 if(!el.open)el.showModal();
 updatePlanPolicyForm(form);
 showPlanWizardStep(form,0);
}
async function refreshPlanLicenseCatalog(form){
 if(form.dataset.saving||form.dataset.refreshing)return;
 const picker=form.querySelector("[data-plan-license-picker]");
 const selectedID=form.elements.namedItem("policy_id")?.value||"";
 const previousPolicy=state.licenseContext?.selectablePolicies.find(policy=>policy.id===selectedID);
 const draftType=form.elements.namedItem("draft_purchase_type");
 if(previousPolicy&&draftType)draftType.value=planPricingMode(previousPolicy);
 form.dataset.refreshing="true";
 updatePlanPolicyForm(form);
 picker.setAttribute("aria-busy","true");
 try{
  state.licenseContext=licensingContext(await connectionsForPlans(true));
  picker.innerHTML=planPolicyPicker(state.licenseContext,selectedID,selectedID?{policy_id:selectedID}:form._savedMapping);
  updatePlanPolicyForm(form);
 }finally{delete form.dataset.refreshing;picker.removeAttribute("aria-busy");updatePlanPolicyForm(form)}
}
async function savePlanForm(form,values){
 const policy=state.licenseContext?.selectablePolicies.find(item=>item.id===values.policy_id);
 if(values.policy_id&&!policy){showPlanWizardStep(form,2);throw new Error("所选授权策略已不可用，请重新选择或暂不绑定并保存草稿")}
 const perpetual=planPricingMode(policy,values.draft_purchase_type)==="perpetual";
 const currency=values.currency.toUpperCase();
 let purchaseOptions,amount,upgrade;
 try{
  purchaseOptions=perpetual?[]:[1,3,6,12].map(periods=>({periods,enabled:values[`option_${periods}_enabled`]==="true",amount_minor:parsePrice(values[`option_${periods}_price`]||"0",currency)}));
  if(!perpetual&&!purchaseOptions.some(option=>option.enabled))throw new Error("请至少开启一个可购买期限");
  if(purchaseOptions.some(option=>option.enabled&&option.amount_minor<=0))throw new Error("已开启期限的一次支付总价必须大于 0");
  amount=perpetual?parsePrice(values.perpetual_price,currency):purchaseOptions[0].amount_minor;
  if(perpetual&&amount<=0)throw new Error("永久套餐价格必须大于 0");
 }catch(error){showPlanWizardStep(form,1);throw error}
 try{
  upgrade=perpetual&&values.upgrade_price?parsePrice(values.upgrade_price,currency):null;
  if(upgrade!==null&&(upgrade<=0||upgrade>amount))throw new Error("永久套餐固定升级价必须大于 0，且不能超过套餐价格");
 }catch(error){showPlanWizardStep(form,4);throw error}
 const result=await savePlanWithLicensing(api,{id:values.id,product_id:values.product_id,version:Number(values.version),name:values.name,summary:values.summary,highlights:(values.highlights||"").split("\n").map(value=>value.trim()).filter(Boolean),currency,amount_minor:amount,purchase_options:purchaseOptions,permanent_upgrade_minor:upgrade,purchase_type:perpetual?"perpetual":"fixed_term",term_unit:perpetual?null:"month",term_count:perpetual?null:1,allow_purchase:values.allow_purchase==="on",allow_renewal:values.allow_renewal==="on"},policy);
 state.planSaveWarning=result.mappingError?`套餐“${values.name}”的草稿已保存，但授权绑定失败：${result.mappingError}。请检查授权连接，再编辑草稿绑定策略后发布。`:"";
 toast(result.mapped?"套餐已保存，并已绑定 Licentivo 策略":result.mappingError?"草稿已保存，授权绑定待处理":"套餐草稿已保存，请配置所选授权系统的套餐映射");
 await admin("plans");
}

async function render(){
	clearInterval(state.poll);
 window.scrollTo(0,0);
 const hash=location.hash.slice(1);
 if(hash.startsWith("action=")){
  const params=new URLSearchParams(hash);state.linkToken=params.get("token");state.linkAction=params.get("action");history.replaceState(null,"",location.pathname+location.search+"#/email-link");
 }
 const returnedOrder=new URLSearchParams(location.search).get("checkout");
 if(returnedOrder){const search=new URLSearchParams(location.search);search.delete("checkout");search.delete("token");search.delete("PayerID");history.replaceState(null,"",location.pathname+(search.size?"?"+search:"")+"#/order/"+encodeURIComponent(returnedOrder))}
 const route=location.hash.slice(2).split("/");
 main.innerHTML='<div class="loading" role="status">正在加载…</div>';
 try{
  await refresh();document.body.classList.toggle("admin-view",route[0]==="admin"&&Boolean(state.user)&&state.user.role!=="customer");document.body.dataset.page=route[0]||"home";nav();
  if(!state.site.installed){setup();return}
  if(state.user?.password_change_required){initialPasswordChange();return}
  switch(route[0]){case "setup":setup();break;case "login":case "register":case "forgot":authPage(route[0]);break;case "email-link":if(state.linkToken)emailLink();else main.innerHTML=empty(t("error.link_closed","链接已关闭"),t("error.link_closed_body","请重新打开邮件中的链接。"));break;case "product":await productPage(route[1]);break;case "download":await downloadPage(route[1]);break;case "docs":route[1]?await docsPage(route[1],route[2]):docsDirectory(state.site);break;case "account":await account();break;case "admin":await admin(route[1]);break;case "checkout":await checkoutPage(route[1],route[2]);break;case "order":await orderPage(route[1]);break;case "change":await changePage(route[1],route[2]);break;case "devices":await devicesPage(route[1],route[2]);break;default:home()}
 }catch(error){main.innerHTML=`<section class="panel wide"><h1>${esc(t("error.service_preparing","服务正在准备"))}</h1><p>${esc(error.message)}</p></section>`}
}
function parsePrice(value,currency){const count=digits(currency);const pattern=new RegExp("^\\d+(?:\\.\\d{1,"+Math.max(count,1)+"})?$");if((count===0&&!/^\d+$/.test(value))||(count>0&&!pattern.test(value)))throw new Error("请填写有效价格，检查小数位数");const [whole,part=""]=value.split(".");const amount=BigInt(whole)*10n**BigInt(count)+BigInt(part.padEnd(count,"0")||"0");if(amount>BigInt(Number.MAX_SAFE_INTEGER))throw new Error("价格超出可提交范围");return Number(amount)}
async function saveProductForm(form,values){
 const original=(state.admin?.products||[]).find(product=>product.id===values.id),requestedStatus=values.status,enabled=downloadPlatforms.filter(platform=>form.elements.namedItem(`${platform.key}_enabled`)?.checked);
 const stagedStatus=original?.status||"draft",legacyURL=enabled.length?"":original?.content?.download_url||"";
 const platformRequirements={};
 for(const platform of enabled){platformRequirements[platform.key]=form.elements.namedItem(`${platform.key}_requirements`).value.trim()}
 const payload={id:values.id,name:values.name,slug:values.slug,status:stagedStatus,content:{summary:values.summary,description:values.description,features:values.features.split("\n").map(value=>value.trim()).filter(Boolean),requirements:values.requirements,platform_requirements:platformRequirements,installation_guide:values.installation_guide,upgrade_guide:values.upgrade_guide,uninstall_guide:values.uninstall_guide,troubleshooting_guide:values.troubleshooting_guide,terms_of_service:values.terms_of_service,privacy_policy:values.privacy_policy,refund_policy:values.refund_policy,invoice_guide:values.invoice_guide,legal_tax_notice:values.legal_tax_notice,download_url:legacyURL,version:values.version}};
 const imageFields=productDocumentFields.filter(item=>item.group==="help").map(item=>item.field),stagedImages=[...(form._documentImages||new Map())].filter(([token])=>imageFields.some(field=>String(payload.content[field]||"").includes(`upload:${token}`)));
 let saved;
 if(!values.id&&stagedImages.length){
  const initial=JSON.parse(JSON.stringify(payload));initial.status="draft";
  for(const field of imageFields)initial.content[field]=String(initial.content[field]||"").replace(/^!\[[^\]\r\n]{0,160}\]\(upload:[A-Za-z0-9-]+\)\s*$/gm,"").trim();
  saved=await api("/admin/product","PUT",initial);payload.id=saved.id;
 }else if(!values.id||!stagedImages.length){saved=await api("/admin/product","PUT",payload)}else{saved={id:values.id}}
 if(stagedImages.length){
  for(const [token,file] of stagedImages){const body=new FormData();body.append("file",file,file.name);const image=await uploadAPI(`/admin/products/${encodeURIComponent(saved.id)}/document-images`,body);for(const field of imageFields)payload.content[field]=String(payload.content[field]||"").replaceAll(`upload:${token}`,image.url)}
  payload.id=saved.id;saved=await api("/admin/product","PUT",payload);
 }
 for(const platform of downloadPlatforms){
  const active=form.elements.namedItem(`${platform.key}_enabled`)?.checked,existing=(original?.downloads||[]).find(item=>item.platform===platform.key),path=`/admin/products/${encodeURIComponent(saved.id)}/downloads/${encodeURIComponent(platform.key)}`;
  if(!active){if(existing)await api(path,"DELETE");continue}
  const version=form.elements.namedItem(`${platform.key}_version`).value.trim(),source=form.elements.namedItem(`${platform.key}_source`).value;
  if(source==="external"){
   const sizeValue=form.elements.namedItem(`${platform.key}_size_bytes`).value.trim(),sizeBytes=sizeValue?Number(sizeValue):0;
   if(!Number.isSafeInteger(sizeBytes)||sizeBytes<0)throw new Error(`${platform.label} 文件大小必须是有效的非负字节数`);
   await api(path,"PUT",{source:"external",version,url:form.elements.namedItem(`${platform.key}_url`).value.trim(),size_bytes:sizeBytes,sha256:form.elements.namedItem(`${platform.key}_sha256`).value.trim().toLowerCase()});
  }else{
   const file=form.elements.namedItem(`${platform.key}_file`).files[0];
   if(file){const body=new FormData();body.append("version",version);body.append("file",file,file.name);await uploadAPI(path,body)}
   else if(existing?.source==="upload"){await api(path,"PUT",{source:"retain",version,url:""})}
   else throw new Error(`请为 ${platform.label} 选择安装包`);
  }
 }
 if(stagedStatus!==requestedStatus){payload.id=saved.id;payload.status=requestedStatus;saved=await api("/admin/product","PUT",payload)}
 return saved;
}
document.addEventListener("submit",async event=>{
 const form=event.target.closest("form[data-form]");if(!form)return;event.preventDefault();
 if(form.dataset.form==="plan"&&(form.dataset.saving||form.dataset.refreshing))return;
 if(form.dataset.form==="plan"){const invalid=[...form.querySelectorAll("input,select,textarea")].find(control=>!control.disabled&&!control.checkValidity());if(invalid){const pane=invalid.closest("[data-plan-pane]");if(pane)showPlanWizardStep(form,pane.dataset.planPane);invalid.reportValidity();invalid.focus();return}}
 if(form.dataset.form==="product"){const invalid=[...form.querySelectorAll("input,select,textarea")].find(control=>!control.disabled&&!control.checkValidity());if(invalid){const pane=invalid.closest("[data-product-pane]");if(pane)showProductWizardStep(form,pane.dataset.productPane);invalid.reportValidity();invalid.focus();return}}
 const values=Object.fromEntries(new FormData(form));const button=form.querySelector('button:not([type="button"])');const errorEl=form.querySelector(".form-error");errorEl.textContent="";button.disabled=true;
 if(form.dataset.form==="plan"){form.dataset.saving="true";updatePlanPolicyForm(form)}
 try{
  switch(form.dataset.form){
   case "data-export":{const result=await api("/admin/exports","POST",values);const url=URL.createObjectURL(new Blob([result.content],{type:"text/csv;charset=utf-8"}));const link=document.createElement("a");link.href=url;link.download=result.filename;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);form.reset();toast(`已导出 ${result.rows} 条记录`);break;}
   case "staff-create":await api("/admin/staff/create","POST",values);form.reset();toast("网站人员已添加，可以使用初始密码登录");await admin("settings");break;
   case "staff-role":await api("/admin/staff","POST",{...values,reason:"在系统设置中更新网站人员角色"});toast("人员角色已更新，原登录会话已退出");await admin("settings");break;
   case "staff-status":await api("/admin/staff/"+encodeURIComponent(values.staff_id)+"/status","POST",{status:values.status,reason:values.reason});toast("网站人员状态已更新");await admin("settings");break;
   case "recovery-complete":await api("/admin/recovery/complete","POST",values);toast("恢复核对已完成，交易任务继续运行");await admin("settings");break;
   case "device-release":{await api(`/me/products/${encodeURIComponent(values.product_id)}/devices/release`,"POST",{device_id:values.device_id,environment:values.environment,idempotency_key:values.idempotency_key,expected_generation:Number(values.expected_generation)});toast(t("toast.device_queued","设备释放已排队，重试不会重复扣次数"));await devicesPage(values.product_id,values.environment);break;}
   case "sale-quote":{const quote=await api("/quotes","POST",{...values,purchase_periods:Number(values.purchase_periods||1)});await quoteConfirmation(quote,values.environment);break;}
   case "refund-request":await api("/orders/"+encodeURIComponent(values.order_id)+"/refund","POST",{reason:values.reason});toast(t("toast.refund_submitted","退款申请已提交，等待商家审核"));await orderPage(values.order_id);break;
   case "invoice-request":await api("/orders/"+encodeURIComponent(values.order_id)+"/invoice","POST",{type:values.type,title:values.title,tax_id:values.tax_id,email:values.email});toast(t("toast.invoice_requested","开票申请已提交，可在客户中心查看进度"));await account();break;
   case "invoice-decision":await api("/admin/invoices/"+encodeURIComponent(values.invoice_id),"POST",{status:values.status,url:values.url,note:values.note});toast(state.site.mail_enabled?"发票处理结果已保存，通知邮件已加入发送队列":"发票处理结果已保存，客户可在账号中心查看");await admin("orders");break;
   case "refund-approve":await api("/admin/refunds/"+encodeURIComponent(values.refund_id)+"/approve","POST",{reason:values.reason});toast("已批准，系统将向原渠道退款");await admin("refunds");break;
   case "refund-reconcile":await api("/admin/refunds/"+encodeURIComponent(values.refund_id)+"/reconcile","POST",{reason:values.reason,external_refund_id:values.external_refund_id||""});toast("已恢复原退款核对");await admin("refunds");break;
   case "order-reconcile":await api("/admin/orders/"+encodeURIComponent(values.order_id)+"/reconcile","POST",{reason:values.reason});toast("已核对原交易");await admin("orders");break;
   case "purchase-sync-retry":await api("/admin/authorizations/"+encodeURIComponent(values.task_id)+"/retry","POST",{reason:values.reason});toast("购买期限已安排重试同步");await admin("operations");break;
   case "identity-notification-retry":await api("/admin/identity-notifications/"+encodeURIComponent(values.task_id)+"/retry","POST",{reason:values.reason});toast("账号安全通知已安排重试");await admin("operations");break;
   case "user-status":await api("/admin/users/"+encodeURIComponent(values.user_id)+"/status","POST",{status:values.status,reason:values.reason});toast("账号状态已更新");await admin("users");break;
   case "initial-password":if(values.new_password!==values.confirm_password)throw new Error("The new passwords do not match.");await api("/me/password","POST",{current_password:values.current_password,new_password:values.new_password});state.user=null;resetIntegrationDraft();toast("Password changed. Sign in with your new password.");navigate("login");break;
   case "login":await api("/auth/login","POST",{...values,kind:"website"});await refresh();navigate(state.returnTo||(state.user.role==="customer"?"account":"admin"));state.returnTo=null;break;
   case "register":await api("/auth/register","POST",values);toast(state.site.email_verification_required?t("toast.registration_sent","如信息有效，验证邮件会发送至该地址"):t("toast.registration_ready","注册已完成，请使用邮箱和密码登录"));navigate("login");break;
   case "forgot":await api("/auth/email","POST",{email:values.email,kind:"reset"});toast(t("toast.reset_sent","如账号有效，重置邮件会发送至该地址"));break;
   case "complete-email":await api("/auth/complete-email","POST",{token:state.linkToken,password:values.password||""});state.linkToken=null;toast(t("toast.action_complete","操作已完成，请登录"));navigate("login");break;
   case "password":await api("/me/password","POST",values);state.user=null;resetIntegrationDraft();toast(t("toast.password_changed","密码已修改，请重新登录"));navigate("login");break;
   case "change-email":await api("/me/email","POST",values);form.reset();toast(t("toast.email_confirmation","请在新邮箱中确认变更"));break;
   case "mail-settings":await api("/admin/mail/settings","POST",{provider:values.mail_provider,require_verification:form.elements.require_verification.checked,order_notifications:form.elements.order_notifications.checked,from:values.from||"",api_key:values.api_key||"",host:values.host||"",port:values.port||"",username:values.username||"",encryption:values.encryption||""});toast(values.mail_provider==="disabled"?"已关闭邮件，客户可以直接注册和购买":"邮件设置已保存，可以发送测试邮件");await refresh();await admin("settings");break;
   case "mail-test":{const result=await api("/admin/mail/check","POST",{to:values.test_recipient});toast(`测试邮件已发送至 ${result.sent_to}，请检查收件箱`);break;}
   case "tls-settings":{const certificateFile=form.elements.certificate_file?.files?.[0];const privateKeyFile=form.elements.private_key_file?.files?.[0];const certificatePEM=certificateFile?await certificateFile.text():String(values.certificate_pem||"").trim();const privateKeyPEM=privateKeyFile?await privateKeyFile.text():String(values.private_key_pem||"").trim();const saved=await api("/admin/tls/settings","POST",{certificate_pem:certificatePEM,private_key_pem:privateKeyPEM});toast(saved.active?"SSL 证书已保存并立即用于新的 HTTPS 连接":"SSL 证书已加密保存；启用内置 HTTPS 监听并重启后生效");form.reset();await admin("settings");break;}
   case "transport-settings":{const result=await api("/admin/transport/settings","POST",{mode:values.mode,public_url:values.public_url});toast(`网站已切换为 ${result.mode.toUpperCase()}，正在打开新地址`);setTimeout(()=>location.assign(result.redirect_url),500);break;}
   case "payment-settings":await api("/admin/connections/payments","POST",values);toast("支付供应商已启用，同环境其他供应商已停用");await admin("settings");break;
	case "translation-settings":await api("/admin/translations","POST",values);toast("DeepL 已连接，管理员填写的内容已加入翻译队列");await admin("settings");break;
   case "license-settings":{const result=await api("/admin/connections/licentivo","POST",values);state.licenseCatalogSyncedAt=Date.now();state.licenseCatalogSyncError="";toast(result.sales_contract_ready?"Licentivo 已连接，可以在销售套餐中选择策略":"Licentivo 已连接；请确认管理 API Key 具有读写权限");await admin("settings");break;}
   case "checkout":{const order=await api("/orders","POST",{...values,purchase_periods:Number(values.purchase_periods||1)});history.replaceState(null,"",location.pathname+location.search+"#/order/"+order.id);if(order.checkout_url){location.assign(order.checkout_url)}else{await orderPage(order.id)};break;}
   case "brand":await api("/admin/brand","PUT",values);toast("品牌已保存");await render();break;
    case "product":await saveProductForm(form,values);toast("产品与平台安装包已保存");await admin("product");break;
   case "plan":await savePlanForm(form,values);break;
  }
 }catch(error){errorEl.textContent=error.message;errorEl.focus()}finally{if(form.dataset.form==="plan"){delete form.dataset.saving;if(form.isConnected)updatePlanPolicyForm(form)}else button.disabled=false}
});
document.addEventListener("click",async event=>{
 const target=event.target.closest("[data-action]");if(!target)return;target.disabled=true;
 try{switch(target.dataset.action){
  case "logout":await api("/auth/logout","POST",{});state.user=null;resetIntegrationDraft();navigate("login");await render();break;
  case "resend-verification":await api("/auth/email","POST",{email:state.user.email,kind:"verify"});toast(t("toast.verification_resent","如信息有效，验证邮件会重新发送"));break;
  case "revoke-session":await api("/me/sessions/"+target.dataset.id,"DELETE");toast(t("toast.session_closed","会话已退出"));await render();break;
  case "refresh-license-catalog":await connectionsForPlans(true);await admin("plans");break;
  case "refresh-plan-license-catalog":await refreshPlanLicenseCatalog(target.closest("form"));break;
  case "new-product":productEditor();break;
  case "edit-product":productEditor(state.admin.products.find(p=>p.id===target.dataset.id));break;
  case "close-product":{const dialog=document.getElementById("product-editor");dialog?.close();dialog?.replaceChildren();break;}
   case "insert-document-image":target.closest(".document-editor")?.querySelector("[data-document-image-input]")?.click();break;
   case "product-step":showProductWizardStep(target.closest("form"),target.dataset.step);break;
  case "set-default-product":await api("/admin/products/"+encodeURIComponent(target.dataset.id)+"/default","POST",{});toast("首页默认产品已更新");await admin("product");break;
  case "new-plan":planEditor();break;
  case "edit-plan":planEditor(state.admin.plans.find(p=>p.id===target.dataset.id));break;
  case "open-plan-versions":await openPlanVersions(target.dataset.id,target.dataset.name);break;
  case "close-plan-versions":target.closest("dialog")?.close();break;
  case "view-plan-version":viewPlanVersion(target.dataset.version);break;
  case "close-plan-version-detail":target.closest("dialog")?.close();break;
  case "rollback-plan-version":await api("/admin/plans/"+encodeURIComponent(target.dataset.plan)+"/rollback","POST",{version_id:target.dataset.version,environment:target.dataset.environment});toast(`已将${target.dataset.environment==="live"?"正式销售":"测试预览"}回滚到所选版本，旧订单保持不变`);document.getElementById("plan-versions-dialog")?.close();await admin("plans");break;
  case "close-plan":{const dialog=document.getElementById("plan-editor");dialog?.close();dialog?.replaceChildren();break;}
  case "plan-step":showPlanWizardStep(target.closest("form"),target.dataset.step);break;
  case "publish-plan":await api("/admin/plans/"+target.dataset.id+"/publish","POST",{version_id:target.dataset.version,environment:"sandbox"});toast("测试预览已更新");await admin("plans");break;
  case "archive-plan":await api("/admin/plans/"+target.dataset.id+"/archive","POST",{});toast("套餐已下架");await admin("plans");break;
  case "open-user-status":{const dialog=document.getElementById("user-status-dialog");const disabling=target.dataset.nextStatus==="disabled";dialog.querySelector('[name="user_id"]').value=target.dataset.id;dialog.querySelector('[name="status"]').value=target.dataset.nextStatus;dialog.querySelector('[name="reason"]').value="";dialog.querySelector("[data-user-status-title]").textContent=disabling?"停用账号":"启用账号";dialog.querySelector("[data-user-status-message]").textContent=disabling?`将停用 ${target.dataset.email}，撤销其登录并同步暂停授权。`:`将恢复 ${target.dataset.email} 的登录权限；如有人工暂停授权，仍需在“授权任务”中恢复。`;dialog.querySelector("[data-user-status-submit]").textContent=disabling?"确认停用":"确认启用";dialog.showModal();break;}
  case "close-user-status":target.closest("dialog")?.close();break;
  case "open-refund-approve":{const dialog=document.getElementById("refund-approve-dialog");dialog.querySelector('[name="refund_id"]').value=target.dataset.id;dialog.querySelector('[name="reason"]').value="";dialog.querySelector("[data-refund-approve-message]").textContent=`客户 ${target.dataset.customer} 申请退还 ${target.dataset.amount}，产品为 ${target.dataset.product}。批准后系统将通过原支付渠道执行整笔退款，并更新该订单对应的购买记录。`;dialog.showModal();break;}
  case "close-refund-approve":target.closest("dialog")?.close();break;
  case "open-staff-status":{const dialog=document.getElementById("staff-status-dialog");const disabling=target.dataset.nextStatus==="disabled";dialog.querySelector('[name="staff_id"]').value=target.dataset.id;dialog.querySelector('[name="status"]').value=target.dataset.nextStatus;dialog.querySelector('[name="reason"]').value="";dialog.querySelector("[data-staff-status-title]").textContent=disabling?"停用网站人员":"启用网站人员";dialog.querySelector("[data-staff-status-message]").textContent=disabling?`将停用 ${target.dataset.email} 并退出其所有后台会话。`:`将恢复 ${target.dataset.email} 的后台登录权限。`;dialog.querySelector("[data-staff-status-submit]").textContent=disabling?"确认停用":"确认启用";dialog.showModal();break;}
  case "close-staff-status":target.closest("dialog")?.close();break;
  case "retry-mail":await api("/admin/mail/retry","POST",{});toast("失败邮件已安排重试");await admin("settings");break;
	case "run-readiness-check":await api("/admin/readiness/check","POST",{key:target.dataset.key});toast(target.dataset.key==="mail"?"测试邮件已发送，请同时检查收件箱":"检测已完成");await admin("readiness");break;
	case "open-invoice-request":{const dialog=document.getElementById("invoice-request-dialog");dialog.querySelector('[name="order_id"]').value=target.dataset.order;dialog.showModal();break;}
	case "close-invoice-request":target.closest("dialog")?.close();break;
	case "sync-translations":await api("/admin/translations/sync","POST",{});toast("管理员填写的内容已加入翻译队列");await admin("translations");break;
	case "refresh-translations":await admin("translations");break;
  case "begin-checkout":navigate("checkout/"+encodeURIComponent(target.dataset.product)+"/"+target.dataset.version);break;
  case "reconcile-order":await api("/orders/"+target.dataset.id+"/reconcile","POST",{});await orderPage(target.dataset.id);break;
  case "purchase-unavailable":toast(t("toast.preview_only","当前为测试预览，购买功能尚未开放"));break;
 }}catch(error){toast(error.message)}finally{target.disabled=false}
});
window.addEventListener("hashchange",render);
render();

function translationSettingsSection(v){
 const languageOptions=v.supported_locales.map(item=>`<option value="${esc(item.code)}" ${item.code===v.source_locale?"selected":""}>${esc(item.name)} · ${esc(item.code)}</option>`).join("");
 const status=`<span class="pill ${v.configured?"":"orange"}">${v.configured?"已连接":"等待配置"}</span>`;
 const keyRules=`${v.configured?"":"required "}autocomplete=new-password data-lpignore=true data-1p-ignore=true placeholder=DeepL API key`;
  return `<details class="settings-group card section"><summary><span>DeepL 内容翻译设置</span>${status}</summary><div class="settings-group-body"><p>导航、按钮、页面标题和状态等固定界面文字已经内置 10 种语言，不使用 DeepL。这里只翻译管理员填写的品牌、产品、套餐、帮助文档和政策内容；API Key 会用主密钥加密保存。</p><form data-form="translation-settings" autocomplete="off"><div class="form-row"><label>DeepL 版本<select name="edition"><option value="free" ${v.edition!=="pro"?"selected":""}>API Free · api-free.deepl.com</option><option value="pro" ${v.edition==="pro"?"selected":""}>API Pro · api.deepl.com</option></select></label><label>管理员填写内容的原文语言<select name="source_locale">${languageOptions}</select></label></div>${field(v.configured?"DeepL API Key（留空继续使用已保存的 Key）":"DeepL API Key","api_key","","password",keyRules)}${formError}<button>验证并保存 DeepL</button></form><details class="nested-help"><summary>注册与配置说明</summary><div class="setup-steps"><div class="setup-step"><h3>注册 API 套餐</h3><p>打开 DeepL 官方 <a href="https://www.deepl.com/pro-api" target="_blank" rel="noopener noreferrer">API 套餐页面 ↗</a>，选择 API Free 或 API Pro。普通翻译网页账号没有 API Key。</p></div><div class="setup-step"><h3>创建 API Key</h3><p>登录 <a href="https://www.deepl.com/your-account/keys" target="_blank" rel="noopener noreferrer">DeepL API Keys &amp; Limits ↗</a>，为本站单独创建一个 Key，并按需要设置用量限制。</p></div><div class="setup-step"><h3>验证并保存</h3><p>选择与账号一致的版本，粘贴 Key。系统会使用相应的官方接口地址验证并读取字符额度。</p></div></div></details></div></details>`;
}
async function translationsView(content){
 const v=await api("/admin/translations");
 const used=v.character_limit?Math.min(100,Math.round(v.character_count*100/v.character_limit)):0;
  const status=v.configured?`<div class="notice"><strong>DeepL 已连接</strong><br>管理员内容 ${v.completed}/${v.total} 项已完成，${v.pending} 项等待处理${v.needs_attention?`，${v.needs_attention} 项需要重试`:""}。${v.character_limit?`本期已用 ${v.character_count.toLocaleString()} / ${v.character_limit.toLocaleString()} 字符（${used}%）。`:""}</div>`:`<div class="notice warning"><strong>尚未连接 DeepL</strong><br>固定界面仍会立即切换语言；管理员填写的品牌、产品和套餐内容暂时显示原文。请到<a href="#/admin/settings/translation">系统设置 → DeepL 内容翻译设置</a>完成连接。</div>`;
  content.innerHTML=`<h1>语言与翻译</h1><p class="hint">固定界面语言随程序发布；这里仅查看管理员内容的自动翻译状态。DeepL 版本、原文语言和 API Key 在“系统设置”中维护。</p>${status}<section class="card section"><h2>DeepL 翻译范围</h2><ul class="plan-features"><li>品牌名称、标语和品牌介绍</li><li>每个软件的名称、介绍、功能、各平台系统要求、安装与故障文档</li><li>每个软件的服务条款、隐私政策、退款规则、开票流程及法律与税务说明</li><li>套餐名称、套餐简介和包含内容</li></ul><p>导航、按钮、页面标题、状态、Logo、主题颜色、价格、币种、版本号、文件大小、校验值、下载地址、邮箱和 Licentivo 授权策略不会提交给 DeepL。客户可在网页顶部即时切换固定界面语言；管理员内容缺少译文时会显示原文并等待后台重试。</p><div class="actions"><button class="secondary" data-action="sync-translations" ${v.configured?"":"disabled"}>重新生成管理员内容译文</button><button class="secondary" data-action="refresh-translations">刷新处理状态</button><a href="#/admin/settings/translation">打开 DeepL 设置</a></div></section><section class="card"><h2>内置的 10 种界面语言</h2><p>${v.supported_locales.map(item=>`${esc(item.name)}（${esc(item.code)}）`).join("、")}</p><details><summary>费用和故障处理</summary><p class="hint">DeepL 只按管理员填写内容的翻译字符计量。重复保存相同原文不会重新消耗额度；原文变化时只更新相应内容。额度不足、Key 失效或网络故障时，原文照常保存，任务会自动重试；修复配置后可重新生成译文。</p></details></section>`;
}
async function checkoutPage(productSlug,versionID){
 if(!versionID){versionID=productSlug;productSlug=""}
 const checkoutSite=productSlug?await siteFor(productSlug):state.site;
 if(!state.user){state.returnTo="checkout/"+(productSlug?encodeURIComponent(productSlug)+"/":"")+versionID;authPage("login");return}
 if(!state.user.verified&&state.site.email_verification_required){main.innerHTML=empty(t("checkout.verify","先验证邮箱"),t("checkout.verify_body","验证邮箱后即可购买产品。"))+'<div class="actions"><a class="button" href="#/account">'+esc(t("common.account_center","进入账号中心"))+'</a></div>';return}
 const plan=checkoutSite.plans.find(p=>p.version_id===versionID);
 if(!plan||plan.preview_only){main.innerHTML=empty(t("checkout.unavailable","该套餐暂未开放购买"),t("checkout.unavailable_body","请返回产品页查看当前可用套餐。"));return}
 const options=enabledPurchaseOptions(plan),selected=options[0];
 if(plan.purchase_type!=="perpetual"&&!selected){main.innerHTML=empty(t("checkout.unavailable","该套餐暂未开放购买"),t("checkout.unavailable_body","请返回产品页查看当前可用套餐。"));return}
 const key=crypto.randomUUID?crypto.randomUUID():[...crypto.getRandomValues(new Uint8Array(24))].map(v=>v.toString(16).padStart(2,"0")).join("");
 const periodPicker=plan.purchase_type==="perpetual"?'<input type="hidden" name="purchase_periods" value="1">':`<fieldset class="checkout-periods"><legend>${esc(t("checkout.choose_period","选择购买期限"))}</legend><div class="checkout-period-grid">${options.map((option,index)=>`<label class="checkout-period-option"><input type="radio" name="purchase_periods" value="${option.periods}" data-amount="${option.amount_minor}" ${index===0?"checked":""}><span><strong>${esc(periodLabel(option.periods))}</strong><small>${esc(moneyMinor(plan.currency,option.amount_minor))}</small></span></label>`).join("")}</div></fieldset>`;
 const docsBase=`#/docs/${encodeURIComponent(checkoutSite.product.slug)}`;
 main.innerHTML=`<section class="panel wide checkout-panel"><p class="eyebrow">${esc(t("section.checkout","结账"))}</p><h1>${esc(t("checkout.title","确认购买"))}</h1><h2>${esc(plan.name)}</h2><p class="price" data-checkout-price>${esc(plan.purchase_type==="perpetual"?money(plan):moneyMinor(plan.currency,selected.amount_minor))}</p><p class="hint" data-checkout-duration>${esc(plan.purchase_type==="perpetual"?t("common.permanent","永久使用"):periodLabel(selected.periods))}</p><p>${esc(plan.summary||t("plans.summary_fallback","购买后立即获得对应期限的软件使用权。"))}</p><ul class="plan-features">${publicPlanBenefits(plan).map(item=>`<li>${esc(item)}</li>`).join("")}</ul><p>${plan.snapshot.max_devices} ${esc(plan.snapshot.license_model==="floating"?t("common.seat","个同时使用席位"):t("common.device","台设备"))}</p><div class="notice">${esc(plan.purchase_type==="fixed_term"?t("checkout.fixed_notice","从成功收款时起算，不会自动扣款。"):t("checkout.perpetual_notice","永久使用权益，维护与版本范围以套餐约定为准。"))}${checkoutSite.environment==="sandbox"?" "+esc(t("checkout.test_notice","当前为测试收款，不会真实扣款。")):""}</div><form data-form="checkout" data-currency="${esc(plan.currency)}"><input type="hidden" name="product_id" value="${esc(checkoutSite.product.id)}"><input type="hidden" name="plan_version_id" value="${esc(plan.version_id)}"><input type="hidden" name="environment" value="${esc(checkoutSite.environment)}"><input type="hidden" name="idempotency_key" value="${key}">${periodPicker}<label>${esc(t("checkout.provider","支付渠道"))}<select name="provider" required>${(checkoutSite.payment_providers||[]).map(p=>`<option value="${esc(p)}">${esc(paymentNames[p]||p)}</option>`).join("")}</select></label><p class="checkout-policy-note">${esc(t("checkout.policy_note","继续支付即表示你已阅读"))} <a href="${docsBase}/terms">${esc(t("product.terms","服务条款"))}</a>、<a href="${docsBase}/privacy">${esc(t("product.privacy","隐私政策"))}</a> ${esc(t("checkout.policy_and","与"))} <a href="${docsBase}/refunds">${esc(t("product.refunds","退款规则"))}</a>。</p>${formError}<button>${esc(t("checkout.pay","前往支付"))}</button></form></section>`;
}
async function orderPage(id){
 clearInterval(state.poll);
 if(!state.user){state.returnTo="order/"+id;authPage("login");return}
 const order=await api("/orders/"+encodeURIComponent(id));
 const title=order.fulfillment==="active"?t("order.active","软件权益已开通"):order.status==="paid"?t("order.processing","已付款，授权处理中"):order.status==="refunded"?t("order.refunded","订单已退款"):order.status==="reversed"?t("order.reversed","订单已撤款"):t("order.waiting","等待付款确认");
 main.innerHTML=`<section class="panel wide"><p class="eyebrow">${esc(t("section.order","订单"))}</p><h1>${esc(title)}</h1><p>${esc(t("order.amount","订单金额"))}：<strong>${money(order)}</strong>${order.environment==="sandbox"?" · "+esc(t("order.test","测试订单")):""}</p><p class="hint">${esc(t("order.created","创建于"))} ${time(order.created_at)}${order.paid_at?" · "+esc(t("order.paid_at","收款于"))+" "+time(order.paid_at):""}</p><div class="notice">${esc(order.status==="reversed"?t("order.reversed_body","支付渠道已确认撤款，该订单对应的购买权益已移除。如有疑问，请联系商家核对。"):order.status==="paid"?(order.fulfillment==="active"?t("order.active_body","请在软件中登录同一账号，即可自动取得自己的授权。"):t("order.processing_body","商家已确认收款，授权任务会继续处理。无需再次付款。")):t("order.unpaid_body","若已经付款，请查询原订单结果，避免重复下单。"))}</div><div class="actions">${order.checkout_url&&order.status==="pending"?`<a class="button" href="${esc(order.checkout_url)}" rel="noopener noreferrer">${esc(t("order.continue","继续原订单付款"))}</a>`:""}<button class="secondary" data-action="reconcile-order" data-id="${esc(order.id)}">${esc(t("order.check","查询付款结果"))}</button><a href="#/account">${esc(t("common.back_account","返回账号中心"))}</a></div></section>`;
 if(order.status==="pending"||order.fulfillment==="pending_authorization"){state.poll=setInterval(async()=>{try{const latest=await api("/orders/"+encodeURIComponent(id));if(latest.status!==order.status||latest.fulfillment!==order.fulfillment)await orderPage(id)}catch{clearInterval(state.poll)}},5000)}
 const refunds=await api("/me/refunds");const refund=refunds.find(r=>r.order_id===id);
 if(refund){main.insertAdjacentHTML("beforeend",`<section class="panel wide"><h2>${esc(t("order.refund_progress","退款进度"))}</h2><span class="pill">${esc(refundStatus(refund))}</span><p>${money(refund)} · ${esc(refund.reason)}</p><p class="hint">${esc(t("common.requested_at","申请时间"))} ${time(refund.created_at)}${refund.completed_at?" · "+esc(t("common.confirmed_at","退款确认"))+" "+time(refund.completed_at):""}</p></section>`)}
 else if(order.status==="paid"){main.insertAdjacentHTML("beforeend",`<section class="panel wide"><h2>${esc(t("order.request_refund","申请整笔退款"))}</h2><p class="hint">${esc(t("order.refund_body","商家审核后按原支付渠道退款。申请不会立即停止权益；成功退款后按剩余已付覆盖重新计算。"))}</p><form data-form="refund-request"><input type="hidden" name="order_id" value="${esc(id)}">${area(t("order.refund_reason","退款原因"),"reason","","required minlength=2 maxlength=500")}${formError}<button class="secondary">${esc(t("order.submit_refund","提交退款申请"))}</button></form></section>`)}
}

const refundStatus=r=>r.review_required?t("refund.review","需商家核实"):({requested:t("refund.requested","等待审核"),pending:t("refund.pending","原渠道退款处理中"),unknown:t("refund.unknown","退款结果核实中"),succeeded:t("refund.succeeded","已确认退款"),failed:t("refund.failed","渠道退回，需商家处理")}[r.status]||r.status);
async function accountCommerce(){
 if(!state.user)return;const products=await api("/me/products");
 if(products.length){const section=document.createElement("section");section.className="section card";section.innerHTML=`<h2>${esc(t("account.manage_title","续费、升级与设备"))}</h2><p class="hint">${esc(t("account.manage_body","续费保持原合同锚点；升级会先计算补差价，再确认付款。"))}</p><div class="actions">${products.map(p=>`<a class="button secondary" href="#/change/${esc(p.product_id)}/${esc(p.environment)}">${esc(p.product_name||t("account.software_rights","软件权益"))}${p.environment==="sandbox"?" · "+esc(t("common.test","测试")):""} · ${esc(t("account.renew_upgrade","续费或升级"))}</a><a href="#/devices/${esc(p.product_id)}/${esc(p.environment)}">${esc(p.product_name||t("account.software_rights","软件权益"))} · ${esc(t("account.manage_devices","管理设备"))}</a>`).join("")}</div>`;main.querySelector(".account-heading").after(section)}
}
async function changePage(product,environment){
 if(!state.user){state.returnTo=`change/${product}/${environment}`;authPage("login");return}
 const plans=await api(`/me/products/${encodeURIComponent(product)}/plans?environment=${encodeURIComponent(environment)}`);
 main.innerHTML=`<section class="panel wide"><p class="eyebrow">${esc(t("section.continue_plan","继续使用套餐"))}</p><h1>${esc(t("change.title","续费或升级"))}</h1><p>${esc(t("change.body","选择操作与目标套餐，系统会核对当前已付权益并给出报价。"))}</p><form data-form="sale-quote"><input type="hidden" name="product_id" value="${esc(product)}"><input type="hidden" name="environment" value="${esc(environment)}"><label>${esc(t("change.operation","操作"))}<select name="action"><option value="renew">${esc(t("change.renew","续费当前套餐"))}</option><option value="upgrade">${esc(t("change.upgrade","升级套餐"))}</option></select></label><label>${esc(t("change.target","目标套餐"))}<select name="plan_version_id" required>${plans.map(p=>`<option value="${esc(p.version_id)}" data-currency="${esc(p.currency)}" data-purchase-options="${esc(JSON.stringify(enabledPurchaseOptions(p)))}">${esc(p.name)} · ${esc(planPriceSummary(p))}</option>`).join("")}</select></label><label data-renew-periods>${esc(t("checkout.choose_period","选择续费期限"))}<select name="purchase_periods" required></select></label>${formError}<button ${plans.length?"":"disabled"}>${esc(t("change.quote","计算报价"))}</button></form><p><a href="#/account">${esc(t("common.back_account","返回账号中心"))}</a></p></section>`;
 updateSaleQuoteForm(main.querySelector('form[data-form="sale-quote"]'));
}
function updateSaleQuoteForm(form){
 if(!form)return;
 const wrapper=form.querySelector("[data-renew-periods]"),select=wrapper?.querySelector("select");
 const renewing=form.elements.namedItem("action")?.value==="renew";
 if(wrapper)wrapper.hidden=!renewing;
 if(select)select.disabled=!renewing;
 if(!renewing||!select)return;
 const target=form.elements.namedItem("plan_version_id")?.selectedOptions?.[0];
 let options=[];try{options=JSON.parse(target?.dataset.purchaseOptions||"[]")}catch{}
 select.innerHTML=options.map(option=>`<option value="${option.periods}">${esc(periodLabel(option.periods))} · ${esc(moneyMinor(target.dataset.currency,option.amount_minor))}</option>`).join("");
 select.required=true;
}
async function quoteConfirmation(q,environment){
 const site=await api(`/site?environment=${encodeURIComponent(environment)}&locale=${encodeURIComponent(state.locale)}`),key=crypto.randomUUID();
 main.innerHTML=`<section class="panel wide"><p class="eyebrow">${esc(t("section.review_change","确认变更"))}</p><h1>${esc(q.action==="renew"?t("change.review_renew","确认续费"):t("change.review_upgrade","确认升级"))}</h1><h2>${esc(q.plan.name)}</h2><p class="price">${money(q)}</p>${q.action==="renew"?`<p class="pill">${esc(periodLabel(q.purchase_periods))}</p>`:""}<p>${esc(q.action==="renew"?t("change.period_starts","新增已付周期开始"):t("change.rights_start","新权益生效"))}：${time(q.valid_from)}</p><p>${esc(t("change.contract_end","合同截止"))}：${q.valid_until?time(q.valid_until):esc(t("common.permanent","永久"))}</p><p class="hint">${esc(t("change.time_display","时间显示为"))} ${esc(Intl.DateTimeFormat().resolvedOptions().timeZone)}。${esc(t("change.quote_expires","报价有效至"))} ${time(q.expires_at)}。</p><p>${q.plan.snapshot.max_devices} ${esc(q.plan.snapshot.license_model==="floating"?t("common.seat","个同时使用席位"):t("common.device","台设备"))}</p><div class="notice">${esc(q.action==="upgrade"?t("change.upgrade_notice","升级保留当前已付周期截止及原使用记录；后续已付周期按各订单约定履行。"):t("change.renew_notice","有效期内续费衔接原期末；若原周期在收款前已到期，则从实际收款时重新起算。"))}${environment==="sandbox"?" "+esc(t("checkout.test_notice","当前为测试收款，不会真实扣款。")):""}</div><form data-form="checkout"><input type="hidden" name="product_id" value="${esc(q.plan.product_id)}"><input type="hidden" name="plan_version_id" value="${esc(q.plan.version_id)}"><input type="hidden" name="purchase_periods" value="${q.purchase_periods||1}"><input type="hidden" name="environment" value="${esc(environment)}"><input type="hidden" name="action" value="${esc(q.action)}"><input type="hidden" name="quote_token" value="${esc(q.quote_token)}"><input type="hidden" name="idempotency_key" value="${key}"><label>${esc(t("checkout.provider","支付渠道"))}<select name="provider" required>${site.payment_providers.map(p=>`<option value="${esc(p)}">${esc(p)}</option>`).join("")}</select></label>${formError}<button>${esc(t("change.confirm_pay","确认并前往支付"))}</button></form></section>`;
}
async function commerceAdmin(content,tab){
 if(tab==="orders"){
  const [orders,invoices]=await Promise.all([api("/admin/orders"),api("/admin/invoices")]);
  const invoiceRows=invoices.map(invoice=>`<tr><td>${time(invoice.created_at)}</td><td><strong>${esc(invoice.customer_email||invoice.user_id)}</strong><br><span class="hint">${esc(invoice.email)}</span></td><td><code>${esc(String(invoice.order_id).slice(0,8))}</code></td><td>${esc(invoice.type==="company"?"企业":"个人")} · ${esc(invoice.title)}${invoice.tax_id?`<br><span class="hint">${esc(invoice.tax_id)}</span>`:""}</td><td><span class="pill ${invoice.status==="requested"?"orange":""}">${esc({requested:"等待开票",issued:"已开具",rejected:"未通过"}[invoice.status]||invoice.status)}</span></td><td>${invoice.status==="requested"?`<form data-form="invoice-decision" class="inline-decision"><input type="hidden" name="invoice_id" value="${esc(invoice.id)}"><label>处理结果<select name="status"><option value="issued">已开具</option><option value="rejected">拒绝</option></select></label>${field("发票下载地址（开具时填写）","url","","url","placeholder=https://…")}${field("处理说明","note","","text","maxlength=500")}${formError}<button class="small">${state.site.mail_enabled?"保存并通知客户":"保存处理结果"}</button></form>`:invoice.invoice_url?`<a href="${esc(invoice.invoice_url)}" target="_blank" rel="noopener">查看发票</a>`:esc(invoice.admin_note||"—")}</td></tr>`);
  content.innerHTML=`<h1>订单与发票</h1><p class="hint">订单固定关联购买时的产品和套餐版本；后续改价或回滚不会修改旧订单。</p>${table(["产品与套餐","创建时间","金额","类型","环境","状态"],orders.map(o=>`<tr><td><strong>${esc(o.product_name||"—")}</strong><br><span class="hint">${esc(o.plan_name||"—")} · v${esc(o.plan_version||"—")}</span></td><td>${time(o.created_at)}</td><td>${money(o)}</td><td>${esc(o.action)}${o.purchase_periods>1?` · ${esc(periodLabel(o.purchase_periods))}`:""}</td><td>${esc(o.environment)}</td><td>${esc(o.status)}</td></tr>`))}<section class="card section"><h2>主动核对付款</h2><form data-form="order-reconcile"><label>订单<select name="order_id">${orders.map(o=>`<option value="${esc(o.id)}">${time(o.created_at)} · ${esc(o.product_name)} · ${money(o)} · ${esc(o.status)}</option>`).join("")}</select></label>${field("核对原因","reason","","text","required minlength=2 maxlength=500")}${formError}<button>查询原交易</button></form></section><section class="section"><h2>发票申请</h2><p class="hint">开具后填写客户可访问的 HTTPS 发票地址。客户可在账号中心查看处理结果；启用邮件服务后也会收到通知邮件。</p>${invoiceRows.length?table(["申请时间","客户","订单","抬头","状态","处理"],invoiceRows):empty("暂无发票申请","客户从客户中心提交后会显示在这里。")}</section>`;return
 }
 if(tab==="refunds"){const rows=await api("/admin/refunds");const action=r=>r.status==="requested"?`<button type="button" class="small danger" data-action="open-refund-approve" data-id="${esc(r.id)}" data-customer="${esc(r.customer_email||r.user_id)}" data-product="${esc(r.product_name||"未命名产品")}" data-amount="${esc(money(r))}">审核退款</button>`:r.review_required||r.status==="unknown"?`<form data-form="refund-reconcile" class="inline-decision"><input type="hidden" name="refund_id" value="${esc(r.id)}">${field("核对说明","reason","","text","required minlength=2 maxlength=500")}${field("原渠道退款编号（可选）","external_refund_id","","text","maxlength=128")}${formError}<button class="small secondary">核对并恢复原退款</button></form>`:"—";const refundRows=rows.map(r=>`<tr><td>${time(r.created_at)}</td><td><strong>${esc(r.customer_email||r.user_id)}</strong><br><span class="hint">${esc(r.product_name||"—")}</span></td><td><code title="${esc(r.order_id)}">${esc(String(r.order_id).slice(0,8))}</code></td><td>${money(r)}</td><td>${esc(r.environment==="live"?"正式":"测试")}</td><td>${esc(r.reason)}</td><td><span class="pill ${r.status==="requested"?"orange":r.status==="succeeded"?"":"gray"}">${esc(refundStatus(r))}</span></td><td>${action(r)}</td></tr>`);content.innerHTML=`<h1>退款审核</h1><p class="hint">每条申请对应一笔已成功付款。批准后由系统通过原支付渠道整笔退款，并按实际退款结果更新购买记录。结果未知时可恢复原退款核对；超出安全重试期限需填写原渠道退款编号。</p>${table(["申请时间","客户与产品","订单","金额","环境","申请原因","状态","操作"],refundRows.length?refundRows:['<tr><td colspan="8"><strong>暂无退款申请</strong><br><span class="hint">客户提交退款申请后，会显示在这里等待审核。</span></td></tr>'])}<dialog id="refund-approve-dialog" class="action-dialog"><form data-form="refund-approve"><input type="hidden" name="refund_id"><div class="section-head"><h2>审核退款</h2><button type="button" class="text-link" data-action="close-refund-approve">关闭</button></div><p class="hint" data-refund-approve-message></p>${field("审核说明","reason","","text","required minlength=2 maxlength=500 autofocus")}${formError}<div class="actions"><button class="danger">批准整笔原路退款</button><button type="button" class="secondary" data-action="close-refund-approve">取消</button></div></form></dialog>`;return}
 const v=await api("/admin/operations");
 const statusLabel=status=>({active:"购买有效",pending:"同步中",expired:"购买期限已到期",suspended:"已暂停",revoked:"购买已终止"}[status]||status);
 const rows=v.entitlements.map(e=>`<tr><td>${esc(e.customer_email)}</td><td>${esc(e.product_name)}</td><td>${esc(e.environment==="live"?"正式":"测试")}</td><td>${esc(statusLabel(e.status))}${e.synchronized?"":' <span class="pill orange">同步中</span>'}</td><td>${e.valid_until?time(e.valid_until):"永久"}</td></tr>`);
 const tasks=v.tasks.map(task=>`<tr><td>${time(task.created_at)}</td><td>${esc(task.status)}</td><td>${task.attempts}</td><td>${esc(task.error_code||"—")}</td><td>${task.status==="manual_review"&&state.user.role==="admin"?`<form data-form="purchase-sync-retry"><input type="hidden" name="task_id" value="${esc(task.id)}">${field("核对说明","reason","","text","required minlength=2 maxlength=500")}${formError}<button class="small secondary">重试同步</button></form>`:"—"}</td></tr>`);
 const notifications=(v.identity_notifications||[]).map(task=>`<tr><td>${time(task.created_at)}</td><td>${esc(task.kind==="identity.session.revoked"?"退出登录会话":"账号安全变更")}</td><td>${esc({pending:"待同步",retrying:"等待重试",running:"同步中",succeeded:"已确认",not_required:"无需远端通知",manual_review:"需要核对"}[task.status]||task.status)}</td><td>${esc(task.error_code||"—")}</td><td>${["manual_review","retrying"].includes(task.status)&&state.user.role==="admin"?`<form data-form="identity-notification-retry"><input type="hidden" name="task_id" value="${esc(task.id)}">${field("核对说明","reason","","text","required minlength=2 maxlength=500")}${formError}<button class="small secondary">重试通知</button></form>`:"—"}</td></tr>`);
 content.innerHTML=`<h1>购买期限同步</h1><p class="hint">Launch 根据订单计算购买期限，并将结果同步给所选授权服务。许可证、功能权限和设备操作由对应服务管理。</p><p><a href="https://www.licentivo.com/dashboard.html#licenses" target="_blank" rel="noopener noreferrer">使用 Licentivo 时前往管理授权</a></p>${table(["客户","产品","环境","同步状态","购买期限至"],rows.length?rows:['<tr><td colspan="5">暂无购买记录</td></tr>'])}<h2>最近同步任务</h2>${table(["时间","状态","失败尝试","处理提示","操作"],tasks.length?tasks:['<tr><td colspan="5">暂无同步任务</td></tr>'])}${state.user.role==="admin"?`<h2>账号安全通知</h2><p class="hint">强制退出、改密和停用账号后，由 Licentivo 确认撤销结果。账号范围的通知完成后，客户才能重新登录桌面软件。</p>${table(["时间","操作","通知状态","处理提示","操作"],notifications.length?notifications:['<tr><td colspan="5">暂无账号安全通知</td></tr>'])}`:""}`;

}

async function devicesPage(product,environment){
 clearTimeout(state.deviceRefreshTimer); const devicePageHash=location.hash;
 if(!state.user){state.returnTo=`devices/${product}/${environment}`;authPage("login");return}
 const v=await api(`/me/products/${encodeURIComponent(product)}/devices?environment=${encodeURIComponent(environment)}`);
 const operationStatus={pending:t("devices.waiting","正在等待处理"),running:t("devices.releasing","正在释放"),succeeded:t("devices.done","已释放"),manual_review:t("devices.review","需联系商家核实"),stale:t("devices.stale","原绑定已变化")};
 main.innerHTML=`<section class="section"><p class="eyebrow">${esc(t("section.devices","你的设备"))}</p><h1>${esc(t("devices.title","设备与席位"))}</h1><p>${esc(t("devices.remaining_a","最近 30 天剩余"))} ${v.remaining} ${esc(t("devices.remaining_b","次自助释放。重试同一操作只占用一次额度。"))}</p><p class="hint">${esc(t("devices.body","释放后，设备下一次成功联机将按最新状态验证；已签发的离线许可仍受原离线期限约束。"))}</p>${v.devices.length?`<div class="grid two">${v.devices.map(d=>`<article class="card"><h2>${esc(d.device_name||t("devices.unnamed","未命名设备"))}</h2><span class="pill">${esc(d.status==="active"?t("devices.active","使用中"):t("devices.released","已释放"))}</span><p class="hint">${esc(t("devices.last_seen","最近联机"))} ${time(d.last_seen_at)}${d.lease_expires_at?" · "+esc(t("devices.lease_end","席位截止"))+" "+time(d.lease_expires_at):""}</p>${d.status==="active"&&v.can_release&&!v.operations.some(o=>o.activation_id===d.id&&o.expected_generation===d.generation&&["pending","running"].includes(o.status))?`<form data-form="device-release"><input type="hidden" name="product_id" value="${esc(product)}"><input type="hidden" name="environment" value="${esc(environment)}"><input type="hidden" name="device_id" value="${esc(d.id)}"><input type="hidden" name="expected_generation" value="${esc(d.generation)}"><input type="hidden" name="idempotency_key" value="${crypto.randomUUID()}">${formError}<button class="secondary">${esc(t("devices.release","释放这台设备"))}</button></form>`:""}</article>`).join("")}</div>`:empty(t("devices.none","还没有设备"),t("devices.none_body","在软件中登录并激活后，会在这里显示。"))}${v.operations.length?`<section class="card section"><h2>${esc(t("devices.recent","近期释放操作"))}</h2>${v.operations.map(o=>`<p>${esc(operationStatus[o.status]||o.status)}</p>`).join("")}</section>`:""}<p><a href="#/account">${esc(t("common.back_account","返回账号中心"))}</a></p></section>`;
 if(v.operations.some(o=>["pending","running"].includes(o.status)))state.deviceRefreshTimer=setTimeout(()=>{if(location.hash===devicePageHash)devicesPage(product,environment).catch(error=>toast(error.message));},3000);
}

function staffSettingsSection(staff){
 const roleLabel=role=>({admin:"管理员",operations:"运营",support:"客服"}[role]||role);
 const statusLabel=status=>({active:"正常",disabled:"已停用",closed:"已关闭"}[status]||status);
 const roleOptions=current=>[["admin","管理员 · 完整权限"],["operations","运营 · 产品和订单"],["support","客服 · 客户服务"]].map(([value,label])=>`<option value="${value}" ${current===value?"selected":""}>${label}</option>`).join("");
 const action=user=>{if(user.status==="closed")return '<span class="hint">不可操作</span>';const next=user.status==="active"?"disabled":"active";return `<button type="button" class="small ${next==="disabled"?"danger":"secondary"}" data-action="open-staff-status" data-id="${esc(user.id)}" data-email="${esc(user.email)}" data-next-status="${next}">${next==="disabled"?"停用":"启用"}</button>`};
 const rows=staff.map(user=>`<tr><td><strong>${esc(user.email)}</strong>${user.id===state.user.id?' <span class="pill gray">当前账号</span>':""}</td><td><form class="staff-role-form" data-form="staff-role"><input type="hidden" name="user_id" value="${esc(user.id)}"><select name="role" aria-label="${esc(user.email)} 的角色" ${user.status==="active"?"":"disabled"}>${roleOptions(user.role)}</select><button class="small secondary" ${user.status==="active"?"":"disabled"}>保存角色</button>${formError}</form></td><td><span class="pill ${user.status==="active"?"":"gray"}">${esc(statusLabel(user.status))}</span></td><td>${time(user.created_at)}</td><td>${action(user)}</td></tr>`);
 return `<details class="settings-group card section" open><summary><span>网站人员管理</span><span class="pill">${staff.length} 人</span></summary><div class="settings-group-body"><p>网站人员与软件客户分开管理。管理员拥有全部后台权限；运营负责产品、套餐和订单；客服处理客户相关工作。</p>${staff.length?table(["人员邮箱","角色权限","状态","添加时间","操作"],rows):empty("还没有网站人员","")}<h3>添加网站人员</h3><p class="hint">创建后即可使用下面的邮箱和初始密码登录管理后台。邮箱由管理员确认，无需再注册客户账号。</p><form data-form="staff-create" autocomplete="off"><div class="form-row">${field("人员邮箱","email","","email","required maxlength=254 autocomplete=off")}${field("初始密码","password","","password","required minlength=12 maxlength=128 autocomplete=new-password")}</div><label>角色<select name="role">${roleOptions("support")}</select></label>${formError}<button>添加网站人员</button></form><dialog id="staff-status-dialog" class="action-dialog"><form data-form="staff-status"><input type="hidden" name="staff_id"><input type="hidden" name="status"><div class="section-head"><h2 data-staff-status-title>人员操作</h2><button type="button" class="text-link" data-action="close-staff-status">关闭</button></div><p class="hint" data-staff-status-message></p>${field("操作原因","reason","","text","required minlength=2 maxlength=500 autofocus")}${formError}<div class="actions"><button data-staff-status-submit>确认</button><button type="button" class="secondary" data-action="close-staff-status">取消</button></div></form></dialog></div></details>`;
}
async function settingsForms(content,dashboard){
 if(state.user.role!=="admin"){content.insertAdjacentHTML("beforeend",`<div class="notice warning">只有管理员可以修改系统连接、密钥和网站人员。</div>`);return}const [recovery,mail,connections,staff,translation,tlsSettings,transport]=await Promise.all([api("/admin/recovery"),api("/admin/mail/settings"),api("/admin/connections"),api("/admin/staff"),api("/admin/translations"),api("/admin/tls/settings"),api("/admin/transport/settings")]);



 const tlsState=tlsSettings.expired?'<span class="pill orange">证书已过期</span>':tlsSettings.expires_within_30_days?'<span class="pill orange">即将到期</span>':tlsSettings.active?'<span class="pill">HTTPS 使用中</span>':tlsSettings.configured?'<span class="pill gray">已保存</span>':'<span class="pill orange">等待配置</span>';
 const tlsDetails=tlsSettings.configured?`<div class="notice ${tlsSettings.expired||tlsSettings.expires_within_30_days?"warning":""}"><strong>${tlsSettings.expired?"当前证书已经过期":tlsSettings.expires_within_30_days?"当前证书将在 30 天内到期":"当前证书有效"}</strong><br>域名：${esc((tlsSettings.names||[]).join("、")||tlsSettings.common_name||"未提供")}<br>签发者：${esc(tlsSettings.issuer||"—")}<br>有效期：${time(tlsSettings.not_before)} 至 ${time(tlsSettings.not_after)}<br>SHA-256 指纹：<code>${esc(tlsSettings.fingerprint_sha256||"—")}</code></div>`:'<div class="notice warning">尚未保存 SSL/TLS 证书。请上传证书链和对应私钥。</div>';
 const tlsRuntime=tlsSettings.listener_enabled?'<p class="hint">内置 HTTPS 监听已启用。保存或更换后，新建立的 HTTPS 连接会立即使用新证书。</p>':'<p class="hint">当前进程未启用内置 HTTPS 监听。证书会安全保存，但需要配置 <code>LAUNCH_HTTPS_ADDR</code> 并重启后才由 Launch 使用；如果由 Caddy、Nginx 或云平台终止 HTTPS，应在对应代理中更换证书。</p>';
 const tlsHost=tlsSettings.expected_hostname?`<p class="hint">证书必须包含当前公开地址的域名：<strong>${esc(tlsSettings.expected_hostname)}</strong>。</p>`:'<p class="hint">当前使用 HTTP。上传证书后，在下面填写 HTTPS 公开地址并切换；切换时会再次校验证书域名。</p>';
 const httpsReady=tlsSettings.listener_enabled&&tlsSettings.configured&&!tlsSettings.expired;
 const transportState=transport.mode==="https"?'<span class="pill">当前 HTTPS</span>':'<span class="pill orange">当前 HTTP</span>';
 const httpsDisabled=!httpsReady&&transport.mode!=="https"?'disabled':'';
 const transportNotice=transport.mode==="http"?'<div class="notice warning"><strong>当前网站使用 HTTP。</strong>登录密码、支付跳转和管理操作在公网传输时不会受到 TLS 保护，正式上线前应切换到 HTTPS。</div>':'<div class="notice"><strong>当前网站使用 HTTPS。</strong>HTTP 入口会自动跳转到公开 HTTPS 地址。</div>';
 const tlsSection=`<details class="settings-group card section" ${tlsSettings.configured&&!tlsSettings.expired&&transport.mode==="https"?"":"open"}><summary><span>SSL 证书与访问协议</span>${tlsState}</summary><div class="settings-group-body"><p>管理员可以上传或更换 PEM 格式的服务器证书。系统会验证证书、私钥、有效期和正式域名，私钥使用实例主密钥加密保存，页面不会回显。</p>${tlsDetails}${tlsRuntime}${tlsHost}<form data-form="tls-settings" autocomplete="off"><div class="form-row"><label>证书链文件（fullchain.pem / .crt）<input name="certificate_file" type="file" accept=".pem,.crt,.cer,application/x-pem-file" required></label><label>私钥文件（privkey.pem / .key）<input name="private_key_file" type="file" accept=".pem,.key,application/x-pem-file" required></label></div>${formError}<button>${tlsSettings.configured?"验证并更换证书":"验证并保存证书"}</button></form><p class="hint">证书文件应先放站点证书，再附中间证书链；私钥不能设置交互式密码。替换前请保留旧证书和私钥的安全备份。</p><hr class="divider"><div class="section-head"><div><h3>网站访问协议</h3><p class="hint">协议和公开地址保存在数据库中，切换后全站链接、Cookie、支付回跳和 Webhook 地址会立即使用新地址。</p></div>${transportState}</div>${transportNotice}<form data-form="transport-settings" data-current-mode="${esc(transport.mode)}" autocomplete="off"><label>访问协议<select name="mode"><option value="http" ${transport.mode==="http"?"selected":""}>HTTP</option><option value="https" ${transport.mode==="https"?"selected":""} ${httpsDisabled}>HTTPS（正式环境推荐）</option></select></label>${field("网站公开访问地址","public_url",transport.public_url,"url","required maxlength=512 placeholder=https://example.com autocomplete=off")}${formError}<button type="submit">保存并应用 ${transport.mode.toUpperCase()}</button></form><p class="hint">启用 HTTPS 前必须先保存与公开地址域名匹配的有效证书，并启用内置 HTTPS 监听。公开地址只能填写协议、域名和可选端口，不能带路径。切换成功后页面会自动打开新地址。</p></div></details>`;
 const mailSection=mailSettingsPanel(mail,dashboard,state.user.email);
 const dataSection=`<details class="settings-group card section"><summary><span>数据导出</span><span class="pill gray">按需使用</span></summary><div class="settings-group-body"><p class="hint">CSV 包含业务关联 ID，不包含密码摘要、会话、支付密钥或完整许可证密钥。每次最多导出 20000 条记录。</p><form data-form="data-export"><label>数据<select name="kind">${[["users","账号"],["orders","订单"],["refunds","退款"],["plans","套餐版本"],["entitlements","当前权益"],["bindings","授权关联"],["devices","设备操作摘要"],["audit","审计记录"]].map(([id,label])=>`<option value="${id}">${label}</option>`).join("")}</select></label>${field("导出原因","reason","","text","required minlength=2 maxlength=500")}${formError}<button class="secondary">下载 CSV</button></form></div></details>`;
 const backupSection=`<details class="settings-group card section"><summary><span>加密备份与恢复</span><span class="pill gray">服务器操作</span></summary><div class="settings-group-body"><p>服务器命令 launch backup PATH 生成独立加密备份。先通过安全环境配置设置 LAUNCH_BACKUP_PASSPHRASE（至少 16 字符），并将备份与口令分开保存。</p><p>launch restore PATH 只恢复到空数据库，同时生成私有配置文件 PATH.secrets.yaml。首次启动可用 LAUNCH_CONFIG_FILE 指向该文件。恢复后先核对交易，再在此页面恢复任务。</p></div></details>`;
 const recoverySection=`${recovery.review_required?`<section class="card section"><h2>恢复后的交易核对</h2><div class="notice warning">自动交易任务和新付款已暂停。请核对备份时刻之后的渠道付款、退款及授权操作，再恢复运行。</div><form data-form="recovery-complete">${area("核对结果与处理说明","reason","","required minlength=10 maxlength=500")}${field("当前管理员密码","password","","password","required autocomplete=current-password")}${formError}<button>确认核对完成并恢复任务</button></form></section>`:""}`;
 state.paymentConnections=connections;
 const panels={payments:paymentSettingsPanel(connections,state.paymentEnvironment,state.paymentProvider),mail:mailSection,licensing:'<div data-external-licensing-panel></div>',website:tlsSection,translation:translationSettingsSection(translation),staff:staffSettingsSection(staff),data:recoverySection+dataSection+backupSection};
 content.insertAdjacentHTML("beforeend",settingsWorkspace(panels,location.hash.slice(2).split("/")[2]||"payments"));
 for(const detail of content.querySelectorAll(".settings-content > details.settings-group")){detail.open=true;const summary=detail.querySelector(":scope > summary");const body=detail.querySelector(":scope > .settings-group-body");if(summary&&body){const title=document.createElement("h2");title.textContent=summary.querySelector("span")?.textContent||summary.textContent;body.prepend(title)}}
 initializeMailForm(content);
	await mountExternalLicensing(content.querySelector('[data-external-licensing-panel]'),{api,connections,confirmDiscardChanges});
}

function installWorkspaceInteractions(){
 captureFormBaselines();
 new MutationObserver(()=>{captureFormBaselines();refreshDirtyState()}).observe(main,{childList:true,subtree:true});
 const trackEdit=event=>{const form=event.target.closest('form[data-form]');if(form){editedForms.add(form);refreshDirtyState()}};
 document.addEventListener('input',trackEdit);
 document.addEventListener('change',trackEdit);
 window.addEventListener('beforeunload',event=>{if(refreshDirtyState()){event.preventDefault();event.returnValue=''}});
 document.addEventListener('click',async event=>{
  const action=event.target.closest('[data-action]');
  if(action?.dataset.action==='copy-webhook'){const input=action.closest('.webhook-block').querySelector('[data-webhook-url]');try{await navigator.clipboard.writeText(input.value);toast('回调地址已复制')}catch{input.focus();input.select();toast('请选择并复制回调地址')}return}
  if(action&&['payment-environment','payment-provider'].includes(action.dataset.action)){
   if(!confirmDiscardChanges())return;
   if(action.dataset.provider)state.paymentProvider=action.dataset.provider;
   if(action.dataset.environment)state.paymentEnvironment=action.dataset.environment;
   main.querySelector('[data-settings-current="payments"]').innerHTML=paymentSettingsPanel(state.paymentConnections,state.paymentEnvironment,state.paymentProvider);
   return;
  }
  const link=event.target.closest('a[href^="#/"]');
  if(link&&!confirmDiscardChanges()){event.preventDefault();event.stopImmediatePropagation()}
 },true);
}
installWorkspaceInteractions();
