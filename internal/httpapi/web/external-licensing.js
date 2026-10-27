const escape=value=>String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const input=(label,name,value="",extra="",type="text")=>`<label>${escape(label)}<input type="${type}" name="${name}" value="${escape(value)}" ${extra}></label>`;
const providerNames={licentivo:"Licentivo",custom_http:"自定义授权服务"};
const options=(values,current)=>values.map(([value,label])=>`<option value="${escape(value)}" ${value===current?"selected":""}>${escape(label)}</option>`).join("");

export function licensingSettingsPanel(connections,data,catalog,selection={},editing=null){
 const active=data.default_provider||connections.licensing_provider||"licentivo",selected=providerNames[selection.provider]?selection.provider:active;
 const license=connections.licensing?.[0],products=license?.catalog?.products?.items||[],policies=license?.catalog?.policies?.items||[];
 const licenseReady=license?.account_protocol_ready===true;
 const licenseStatus=license?.last_check_error?`<p class="notice warning" role="alert">${escape(license.last_check_error)}</p>`:license?`<p class="notice ${licenseReady?"":"warning"}">${licenseReady?"账号授权连接已建立。":"目录已读取，账号授权连接尚未建立，请重新连接。"}</p>`:"";
 const licentivo=`<h3>Licentivo 授权连接</h3><p class="hint">连接 <a href="${escape(connections.licentivo_url)}" target="_blank" rel="noopener noreferrer">Licentivo 官网 ↗</a>，保存读写管理 API Key 后，在销售套餐中选择授权策略。</p>${license?`<div class="notice">已读取 ${products.length} 个产品和 ${policies.length} 个授权策略。</div>${licenseStatus}`:'<p class="notice warning">尚未连接，请填写 Licentivo API Key。</p>'}<form data-form="license-settings" autocomplete="off">${input(license?"Licentivo API Key（留空刷新策略）":"Licentivo API Key","api_key","",`${license?'placeholder="已加密保存；留空刷新策略"':"required"} autocomplete="new-password" data-lpignore="true" data-1p-ignore="true"`,"password")}<p class="form-error" role="alert"></p><div class="form-actions"><button type="submit">${license?(licenseReady?"刷新授权策略":"重新连接并读取策略"):"连接并读取策略"}</button></div></form>`;
 return `<section class="licensing-settings"><div class="section-head"><div><h2>授权系统</h2><p class="hint">全站只能启用一个授权系统，所有商品及测试、正式环境统一使用。选择后点击启用生效。</p></div><span class="pill">当前启用：${escape(providerNames[active])}</span></div><p class="form-error" role="alert" data-external-error></p><p role="status" aria-live="polite" data-external-status></p><div class="payment-layout"><div class="provider-list" aria-label="授权提供方">${Object.entries(providerNames).map(([provider,name])=>{const configured=provider==="licentivo"?Boolean(license):Boolean(data.connections.length),connected=active===provider&&(provider!=="licentivo"||licenseReady),status=provider==="licentivo"&&active===provider&&!licenseReady?(license?.last_check_error?"连接失败":license?"连接未完成":"未配置"):active===provider?"已启用":configured?"已配置 · 未启用":"未配置";return `<button type="button" class="provider-row ${selected===provider?"selected":""}" aria-pressed="${selected===provider}" data-licensing-provider="${provider}"><span class="provider-wordmark">${escape(name)}</span><span class="provider-description">${provider==="licentivo"?"Licentivo 授权策略、SDK 与设备管理":"使用商户自己的授权服务和 SDK"}</span><span class="connection-state ${connected?"connected":""}">${status}</span><span class="provider-configure">${selected===provider?"正在配置":"配置"}</span></button>`}).join("")}</div><section class="provider-editor" aria-label="授权系统配置">${active!==selected?`<form data-form="licensing-provider" data-external-form="provider"><input type="hidden" name="provider" value="${selected}"><p class="notice warning">当前启用 ${escape(providerNames[active])}。启用 ${escape(providerNames[selected])} 后，另一授权系统将停用；已有其他系统订单时需要先迁移。</p><div class="form-actions"><button type="submit">启用 ${escape(providerNames[selected])}</button></div></form><hr class="divider">`:""}${selected==="licentivo"?licentivo:externalLicensingForms(data,catalog,selection,editing)}</section></div></section>`;
}

export function externalLicensingForms(data,catalog,selection={},editing=null){
 const environment=selection.environment||"sandbox",productID=selection.productID||catalog.products?.[0]?.id||"";
 const binding=data.bindings.find(row=>row.product_id===productID&&row.environment===environment);
 const provider="custom_http",active=data.default_provider==="custom_http";
 const connectionID=selection.connectionID??binding?.connection_id??"";
 const connections=data.connections.filter(row=>row.environment===environment);
 const plans=catalog.plans.filter(row=>row.product_id===productID);
 const connection=editing||{};
 return `<section><h3>自定义授权服务</h3><p class="hint">使用你自己的授权服务和 SDK，按商品与环境配置连接及套餐映射。</p><p><a href="/api/v1/licensing/documentation" target="_blank" rel="noopener">API 说明与完整对接流程</a> · <a href="/api/v1/licensing/protocol" target="_blank" rel="noopener">下载自定义服务 OpenAPI</a></p>
 ${active?`<form data-form="external-binding" data-external-form="binding"><input type="hidden" name="provider" value="${provider}"><div class="form-row"><label>软件商品<select name="product_id" data-licensing-selection="productID" required>${options((catalog.products||[]).map(p=>[p.id,p.name]),productID)}</select></label><label>环境<select name="environment" data-licensing-selection="environment">${options([["sandbox","测试环境"],["live","正式环境"]],environment)}</select></label></div>
 <label>自定义连接<select name="connection_id" data-licensing-selection="connectionID" required><option value="">请选择对应环境的连接</option>${options(connections.map(c=>[c.id,c.name]),connectionID)}</select></label>${connections.length?"":'<p class="notice warning">先在下方创建并验证此环境的连接。</p>'}<p class="hint">填写外部系统已有的商品、套餐引用。授权规则由该系统管理。编辑销售中的映射需要先创建套餐新版本。</p>${plans.map(plan=>{const mapping=data.mappings.find(m=>m.version_id===plan.version_id&&m.environment===environment&&m.connection_id===connectionID);return `<fieldset data-external-plan="${escape(plan.version_id)}"><legend>${escape(plan.name)} · v${escape(plan.version)}</legend><div class="form-row">${input("外部商品引用","product_ref_"+plan.version_id,mapping?.product_ref||"",'maxlength="128" pattern="[A-Za-z0-9_.\\-]+"')}${input("外部套餐引用","plan_ref_"+plan.version_id,mapping?.plan_ref||"",'maxlength="128" pattern="[A-Za-z0-9_.\\-]+"')}</div><p class="hint">两项留空则保留原映射，可先只配置准备销售的套餐。</p></fieldset>`}).join("")}
 <p class="hint">已有订单不能直接更换连接。切换授权系统后需要重新发布对应环境的套餐。</p><div class="form-actions"><button type="submit" ${!productID?"disabled":""}>保存商品授权映射</button></div></form>`:'<p class="hint">可以先配置连接，启用自定义授权后再保存商品映射。</p>'}</section>
 <section class="card section"><div class="section-head"><h2>自定义授权连接</h2><button type="button" class="secondary" data-external-new>新增连接</button></div>${data.connections.length?`<div class="table-wrap"><table><thead><tr><th>名称</th><th>环境</th><th>服务地址</th><th>状态</th><th>操作</th></tr></thead><tbody>${data.connections.map(row=>`<tr><td>${escape(row.name)}</td><td>${escape(row.environment)}</td><td>${escape(row.base_url)}</td><td>${row.last_check_error?escape(row.last_check_error):"检查通过"}</td><td><button type="button" class="small secondary" data-external-edit="${escape(row.id)}">编辑</button> <button type="button" class="small secondary" data-external-check="${escape(row.id)}">检查连接</button></td></tr>`).join("")}</tbody></table></div>`:'<p class="hint">尚未配置自定义服务。</p>'}
 <form data-form="external-connection" data-external-form="connection" autocomplete="off"><h3>${editing?"编辑连接与轮换签名密钥":"新增自定义连接"}</h3><input type="hidden" name="id" value="${escape(connection.id||"")}">${input("连接名称","name",connection.name||"",'required minlength="2" maxlength="160"')}<label>环境<select name="environment">${options([["sandbox","测试环境"],["live","正式环境"]],connection.environment||environment)}</select></label>${input("服务地址（可含固定路径前缀）","base_url",connection.base_url||"",'required maxlength="512" placeholder="https://licenses.example.com"',"url")}${input("双方约定的连接标识","connection_ref",connection.connection_ref||"",'required maxlength="128"')}${input("签名密钥编号","key_id",connection.key_id||"key-1",'required maxlength="64"')}${input("签名密钥（标准 Base64）","signing_key","",`${editing?'placeholder="留空保留现有密钥"':"required"} maxlength="180" autocomplete="new-password"`,"password")}<button type="button" class="small secondary" data-external-key>生成随机密钥并填入</button><p class="hint">将同一密钥安全配置到你的授权服务；Launch 保存后不会回显。已保存连接的地址、环境和网络策略不能编辑，请新增连接。</p><label>允许的激活入口来源（每行一个 HTTPS origin）<textarea name="activation_origins" rows="3" placeholder="https://activate.example.com">${escape((connection.activation_origins||[]).join("\n"))}</textarea></label><label class="check-option"><input type="checkbox" name="allow_private_network" ${connection.allow_private_network?"checked":""}> 允许访问管理员指定的内网 HTTPS 服务</label><label class="check-option"><input type="checkbox" name="allow_local_http" ${connection.allow_local_http?"checked":""}> 允许测试环境使用本机 HTTP 服务</label><div class="actions"><button type="submit">验证并保存连接</button></div></form></section>`;
}

export async function mountExternalLicensing(host,{api,connections:initialConnections,confirmDiscardChanges=()=>true}){
 if(!host)return;
 let data,catalog,connections=initialConnections,selection={},editing=null;
 const status=(message,error=false)=>{const element=host.querySelector(error?"[data-external-error]":"[data-external-status]");if(element)element.textContent=message};
 const render=()=>{if(host.isConnected)host.innerHTML=licensingSettingsPanel(connections,data,catalog,selection,editing)};
 const load=async(refresh=false)=>{[data,catalog,connections]=await Promise.all([api("/admin/licensing/connections"),api("/admin/catalog"),refresh||!connections?api("/admin/connections"):connections]);render()};
 try{await load()}catch(error){host.textContent=error.message;return}
 host.addEventListener("change",event=>{
  const key=event.target.dataset.licensingSelection;if(!key)return;selection[key]=event.target.value;
  if(key==="productID"||key==="environment"){delete selection.provider;delete selection.connectionID}
  render();
 });
 host.addEventListener("click",async event=>{
  const button=event.target.closest("button");if(!button)return;
  if(button.dataset.licensingProvider){if(button.dataset.licensingProvider===(selection.provider||data.default_provider))return;if(!confirmDiscardChanges())return;selection.provider=button.dataset.licensingProvider;editing=null;render();return}
  if(button.hasAttribute("data-external-new")){editing=null;render()}
  if(button.hasAttribute("data-external-edit")){editing=data.connections.find(row=>row.id===button.dataset.externalEdit)||null;render()}
  if(button.hasAttribute("data-external-key")){const key=crypto.getRandomValues(new Uint8Array(32));host.querySelector('[name="signing_key"]').value=btoa(String.fromCharCode(...key));status("已生成随机密钥。保存前请安全复制到授权服务。")}
  if(button.hasAttribute("data-external-check")){button.disabled=true;try{const result=await api(`/admin/licensing/connections/${encodeURIComponent(button.dataset.externalCheck)}/check`,"POST",{});await load();status(result.last_check_error||"连接检查通过",Boolean(result.last_check_error))}catch(error){status(error.message,true)}finally{button.disabled=false}}
 });
 host.addEventListener("submit",async event=>{
  const form=event.target.closest("form[data-external-form]");if(!form)return;event.preventDefault();event.stopPropagation();if(!form.reportValidity())return;
  const values=Object.fromEntries(new FormData(form)),submit=form.querySelector('[type="submit"]');submit.disabled=true;status("");status("",true);
  try{
   if(form.dataset.externalForm==="provider"){
    await api("/admin/licensing/provider","PUT",{provider:values.provider});delete selection.connectionID;
   }else if(form.dataset.externalForm==="connection"){
    const payload={provider:"custom_http",name:values.name,base_url:values.base_url,environment:values.environment,connection_ref:values.connection_ref,key_id:values.key_id,signing_key:values.signing_key,allow_private_network:values.allow_private_network==="on",allow_local_http:values.allow_local_http==="on",activation_origins:(values.activation_origins||"").split("\n").map(v=>v.trim()).filter(Boolean)};
    const result=await api("/admin/licensing/connections"+(values.id?"/"+encodeURIComponent(values.id):""),values.id?"PUT":"POST",payload);editing=null;selection.environment=result.environment;selection.provider="custom_http";selection.connectionID=result.id;
   }else{
    const mappings=[];if(values.provider==="custom_http")for(const fieldset of form.querySelectorAll("[data-external-plan]")){const version=fieldset.dataset.externalPlan,productRef=(values["product_ref_"+version]||"").trim(),planRef=(values["plan_ref_"+version]||"").trim();if(!productRef&&!planRef)continue;if(!productRef||!planRef)throw new Error("外部商品与套餐引用需要成对填写");mappings.push({version_id:version,product_ref:productRef,plan_ref:planRef})}
    await api(`/admin/products/${encodeURIComponent(values.product_id)}/licensing`,"PUT",{provider:values.provider,environment:values.environment,connection_id:values.provider==="custom_http"?values.connection_id:"",mappings});
   }
   await load(true);status(form.dataset.externalForm==="provider"?`已启用 ${providerNames[values.provider]}，另一授权系统已停用。请配置并重新发布对应环境的套餐。`:"已保存。商品配置完成后，请到销售套餐页发布对应环境。")
  }catch(error){status(error.message,true)}finally{submit.disabled=false}
 });
}

export function mountExternalDeliveries(root,products,{api}){
 for(const product of products.filter(row=>row.licensing_provider==="custom_http")){
  const card=[...root.querySelectorAll("[data-account-product]")].find(node=>node.dataset.accountProduct===product.product_id&&node.dataset.environment===product.environment);if(!card)continue;
  const section=document.createElement("section"),button=document.createElement("button"),output=document.createElement("div");button.type="button";button.className="secondary";button.textContent="查看授权交付";button.disabled=product.status!=="active"||!product.synchronized;output.setAttribute("aria-live","polite");section.append(button,output);card.append(section);
  button.addEventListener("click",async()=>{button.disabled=true;output.replaceChildren();try{
   const result=await api(`/me/products/${encodeURIComponent(product.product_id)}/delivery?environment=${encodeURIComponent(product.environment)}`);if(result.status!=="ready"||!result.delivery){output.textContent=result.status==="pending"?"授权服务仍在准备交付，请稍后重试。":"授权交付暂不可用，请联系商家。";return}
   const value=result.delivery;if(value.type==="activation_url"){const link=document.createElement("a"),url=new URL(value.value);if(url.protocol!=="https:")throw new Error("激活入口无效");link.href=url.href;link.target="_blank";link.rel="noopener noreferrer";link.textContent="打开商家激活页面";output.append(link)}else{const text=document.createElement(value.type==="license_key"?"code":"p");text.textContent=value.value;text.style.whiteSpace="pre-wrap";output.append(text)}
  }catch(error){output.textContent=error.message}finally{button.disabled=false}});
 }
}
