import {icon} from "./workspace.js";
import {policyTypes,policyURL,policyEditorValue,renderPolicyBody} from "./policy-content.js";
const escape=value=>String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const date=value=>value?new Date(value).toLocaleDateString("zh-CN"):"尚未发布";

export function policySettingsPanel(policies,selected="terms"){
 const current=policies.find(row=>row.key===selected)||policies[0];
 if(!current)return '<p class="hint">暂时无法读取政策设置，请刷新页面。</p>';
 return `<section class="policy-settings"><h2>条款与政策</h2><p class="hint policy-intro">管理商店展示的服务与交易政策，发布后更新页脚和结账页入口。</p><div class="policy-layout"><nav class="policy-list" aria-label="选择政策文档">${policyTypes.map(type=>{const row=policies.find(item=>item.key===type.key);return `<button type="button" class="policy-option ${type.key===current.key?"selected":""}" data-policy-select="${type.key}" aria-pressed="${type.key===current.key}">${icon("file-text")}<span><strong>${type.title}</strong><small class="${row?.published?"published":""}">${row?.published?"已发布":row?.version?"草稿":"未设置"}</small></span></button>`}).join("")}</nav><div class="policy-workspace"><div class="policy-heading"><h2>${escape(policyTypes.find(type=>type.key===current.key)?.title)}</h2><p class="hint">最近发布：${date(current.published_at)}${current.has_draft_changes?'<span class="policy-draft-indicator">草稿有修改</span>':""}</p></div><form data-form="store-policy"><input type="hidden" name="key" value="${escape(current.key)}"><input type="hidden" name="version" value="${current.version}"><label>页面标题<input name="title" value="${escape(current.draft.title)}" required maxlength="160" autocomplete="off"></label><label id="policy-body-label">正文内容</label><div class="policy-rich-editor"><div class="policy-toolbar" role="toolbar" aria-label="正文格式"><select aria-label="段落格式" data-policy-block><option value="p">段落</option><option value="h1">一级标题</option><option value="h2">二级标题</option><option value="h3">三级标题</option></select><button type="button" class="format-bold" data-policy-command="bold" aria-label="加粗" title="加粗">B</button><button type="button" data-policy-command="insertUnorderedList" aria-label="无序列表" title="无序列表">${icon("list")}</button><button type="button" data-policy-command="insertOrderedList" aria-label="有序列表" title="有序列表">编号</button><button type="button" data-policy-link aria-label="插入链接" title="插入链接">${icon("link")}</button><button type="button" data-policy-command="undo" aria-label="撤销" title="撤销">${icon("rotate-ccw")}</button></div><div class="policy-document" contenteditable="true" role="textbox" aria-labelledby="policy-body-label" aria-multiline="true" data-policy-editor spellcheck="false">${renderPolicyBody(current.draft.body)||"<p><br></p>"}</div></div><textarea name="body" hidden>${escape(current.draft.body)}</textarea><p class="form-error" role="alert" tabindex="-1" data-policy-error></p><p class="policy-save-status" role="status" aria-live="polite" data-policy-status></p><div class="policy-actions"><p class="hint">草稿仅在后台可见，发布后客户才会看到更新。</p><div class="actions"><button type="submit" class="secondary" data-policy-save>保存草稿</button><button type="button" class="text-link" data-policy-preview>预览</button><button type="button" data-policy-publish>发布更新</button></div></div></form><div class="policy-footer-preview"><h3>商店页脚入口</h3><p class="hint">发布后将在商店页脚和结账页显示以下链接： <span>${policyTypes.map(type=>`<button type="button" class="text-link" data-policy-select="${type.key}">${escape(policies.find(row=>row.key===type.key)?.draft.title||type.title)}</button>`).join('<span aria-hidden="true"> · </span>')}</span></p></div></div></div><dialog class="policy-preview-dialog" data-policy-preview-dialog aria-labelledby="policy-preview-title"><div class="section-head"><h2 id="policy-preview-title">草稿预览</h2><button type="button" class="text-link" data-policy-close-preview>关闭</button></div><p class="hint">这是当前编辑内容的预览，客户仍会看到已发布的版本。</p><article class="policy-public-document" data-policy-preview-content></article></dialog><dialog class="action-dialog" data-policy-link-dialog aria-labelledby="policy-link-title"><form data-policy-link-form><h2 id="policy-link-title">插入链接</h2><label>链接文字<input name="text" required maxlength="200"></label><label>链接地址<input name="href" required placeholder="https://example.com/help" maxlength="2000"></label><p class="form-error" data-policy-link-error></p><div class="actions"><button type="submit">插入链接</button><button type="button" class="secondary" data-policy-close-link>取消</button></div></form></dialog></section>`;
}

export async function mountStorePolicies(host,{api,confirmDiscardChanges=()=>true,onSaved=()=>{},onPublished=async()=>{}}){
 if(!host)return;
 let policies=[],selected="terms",selection=null,busy=false;
 const render=()=>{if(host.isConnected)host.innerHTML=policySettingsPanel(policies,selected)};
 try{policies=await api("/admin/policies");render()}catch(error){host.textContent=error.message;return}
 const editor=()=>host.querySelector("[data-policy-editor]");
 const sync=()=>{const form=host.querySelector('form[data-form="store-policy"]');form.elements.body.value=policyEditorValue(editor());return form};
 const remember=()=>{const value=window.getSelection();if(value.rangeCount&&editor()?.contains(value.anchorNode))selection=value.getRangeAt(0).cloneRange()};
 const restore=()=>{editor().focus();if(selection&&editor().contains(selection.commonAncestorContainer)){const value=window.getSelection();value.removeAllRanges();value.addRange(selection)}};
 const changed=()=>{sync();editor().dispatchEvent(new Event("input",{bubbles:true}))};
 const command=(name,value=null)=>{restore();if(name==="insertUnorderedList"||name==="insertOrderedList")document.execCommand("formatBlock",false,"p");document.execCommand(name,false,value);remember();changed()};
 const save=async publish=>{
  if(busy||!host.isConnected)return;
  let form=sync();if(!form.reportValidity())return;
  const ownsForm=()=>host.isConnected&&host.querySelector('form[data-form="store-policy"]')===form;
  const payload={title:form.elements.title.value,body:form.elements.body.value,version:Number(form.elements.version.value)};
  if([...payload.body].length>16000){host.querySelector("[data-policy-error]").textContent="正文内容不能超过 16000 字";return}
  busy=true;host.querySelector("[data-policy-error]").textContent="";
  for(const button of host.querySelectorAll("button"))button.disabled=true;
  try{
   const result=await api(`/admin/policies/${selected}${publish?"/publish":""}`,publish?"POST":"PUT",payload);
   policies=policies.map(row=>row.key===result.key?result:row);
   if(ownsForm()){
    sync();const newerEdits=form.elements.title.value!==payload.title||form.elements.body.value!==payload.body;
    if(newerEdits){
     form.elements.version.value=result.version;
     host.querySelector('.policy-heading .hint').innerHTML=`最近发布：${date(result.published_at)}<span class="policy-draft-indicator">草稿有修改</span>`;
     const label=host.querySelector(`[data-policy-select="${selected}"] small`);label.textContent=result.published?"已发布":"草稿";label.classList.toggle("published",Boolean(result.published));
     host.querySelector(`.policy-footer-preview [data-policy-select="${selected}"]`).textContent=result.draft.title;
    }else{render();selection=null;form=host.querySelector('form[data-form="store-policy"]')}
    onSaved(form,{title:result.draft.title,body:result.draft.body,version:result.version});
    host.querySelector("[data-policy-status]").textContent=newerEdits?(publish?"已发布提交时的内容，当前修改尚未发布。":"已保存提交时的草稿，当前修改尚未保存。"):(publish?"已发布，商店页脚、结账页和政策文档已更新。":"草稿已保存，商店展示的内容保持不变。");
   }
   if(publish){
    try{await onPublished()}
    catch{if(ownsForm())host.querySelector("[data-policy-status]").textContent+=" 商店信息刷新失败，请刷新页面。"}
   }
  }catch(error){if(ownsForm()){const message=host.querySelector("[data-policy-error]");message.textContent=error.message;message.focus()}}
  finally{busy=false;if(ownsForm())for(const button of host.querySelectorAll("button"))button.disabled=false}
 };
 host.addEventListener("input",event=>{if(event.target.closest("[data-policy-editor]"))sync()});
 host.addEventListener("keyup",remember);host.addEventListener("mouseup",remember);
 host.addEventListener("mousedown",event=>{if(event.target.closest("[data-policy-command],[data-policy-link]")){remember();event.preventDefault()}});
 host.addEventListener("change",event=>{if(event.target.matches("[data-policy-block]"))command("formatBlock",event.target.value)});
 host.addEventListener("paste",event=>{if(event.target.closest("[data-policy-editor]")){event.preventDefault();document.execCommand("insertText",false,event.clipboardData.getData("text/plain"));sync()}});
 host.addEventListener("drop",event=>{if(event.target.closest("[data-policy-editor]")){event.preventDefault();restore();document.execCommand("insertText",false,event.dataTransfer.getData("text/plain"));changed()}});
 host.addEventListener("submit",event=>{
  if(event.target.matches('form[data-form="store-policy"]')){event.preventDefault();event.stopPropagation();save(false)}
  if(event.target.matches("[data-policy-link-form]")){
   event.preventDefault();event.stopPropagation();const form=event.target,href=policyURL(form.elements.href.value);
   if(!href){form.querySelector("[data-policy-link-error]").textContent="请填写有效的 HTTP、HTTPS 或站内链接";return}
   const label=form.elements.text.value;host.querySelector("[data-policy-link-dialog]").close();restore();document.execCommand("insertText",false,label);const range=window.getSelection().getRangeAt(0);range.setStart(range.endContainer,Math.max(0,range.endOffset-label.length));window.getSelection().removeAllRanges();window.getSelection().addRange(range);document.execCommand("createLink",false,href);remember();changed();
  }
 });
 host.addEventListener("click",event=>{
  const target=event.target.closest("button");if(!target||busy)return;
  if(target.dataset.policySelect&&target.dataset.policySelect!==selected){if(!confirmDiscardChanges())return;selected=target.dataset.policySelect;selection=null;render()}
  if(target.dataset.policyCommand)command(target.dataset.policyCommand);
  if(target.hasAttribute("data-policy-publish"))save(true);
  if(target.hasAttribute("data-policy-preview")){const form=sync(),preview=host.querySelector("[data-policy-preview-content]");preview.innerHTML=`<h1>${escape(form.elements.title.value)}</h1>${renderPolicyBody(form.elements.body.value,form.elements.title.value)}`;host.querySelector("[data-policy-preview-dialog]").showModal()}
  if(target.hasAttribute("data-policy-close-preview"))host.querySelector("[data-policy-preview-dialog]").close();
  if(target.hasAttribute("data-policy-link")){remember();const dialog=host.querySelector("[data-policy-link-dialog]");dialog.querySelector("[name=text]").value=window.getSelection().toString();dialog.querySelector("[name=href]").value="";dialog.querySelector("[data-policy-link-error]").textContent="";dialog.showModal()}
  if(target.hasAttribute("data-policy-close-link"))host.querySelector("[data-policy-link-dialog]").close();
 });
}
