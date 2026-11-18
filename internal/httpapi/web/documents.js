import {icon} from './workspace.js';
import {escapeHTML as esc,renderDocument,editorMarkdown,documentURL} from './document-content.js';
import {publicRouteURL} from './public-routing.js';

const copy=value=>JSON.parse(JSON.stringify(value));
const field=(label,name,value='',extra='')=>`<label>${label}<input name="${name}" value="${esc(value)}" ${extra}></label>`;
const date=value=>value?new Date(value).toLocaleDateString():'';
const changed=a=>a.published&&JSON.stringify(a.draft)!==JSON.stringify(a.published);

export async function mountDocuments(root,{api,uploadAPI,locale,toast}){
 const catalog=await api('/admin/catalog'),products=catalog.products||[];
 let product=products[0],library={version:0,groups:[]},selected='',draft=null,saved='',timer,queue=Promise.resolve(),disposed=false,seoTab=false,search='',filter='',collapsed=new Set(),drag='';
 const find=id=>library.groups.flatMap(g=>g.articles).find(a=>a.id===id);
 const groupOf=id=>library.groups.find(g=>g.articles.some(a=>a.id===id));
 const articleURL=slug=>publicRouteURL(['docs',product.slug,slug],locale)||publicRouteURL(['docs',product.slug,slug],'zh-CN');
 const dirty=()=>draft&&JSON.stringify(draft)!==saved;
 const status=(message,error=false)=>{const el=root.querySelector('[data-doc-status]');if(el){el.textContent=message;el.classList.toggle('error',error)}};
 const command=incoming=>{
  const productID=product.id;
  const task=queue.catch(()=>{}).then(async()=>{const next=await api(`/admin/documents/${encodeURIComponent(productID)}`,'POST',{...incoming,version:library.version});library=next;return next});
  queue=task;return task;
 };
 async function saveDraft(){
  clearTimeout(timer);if(!draft||!dirty())return;
  const snapshot=copy(draft),id=selected;status('正在保存草稿…');
  try{await command({action:'save_article',id,content:snapshot});saved=JSON.stringify(snapshot);status('草稿已自动保存');if(dirty())scheduleSave()}
  catch(error){status(`保存失败：${error.message}。当前内容仍保留在编辑器中。`,true);throw error}
 }
 function scheduleSave(){status('草稿有修改');clearTimeout(timer);timer=setTimeout(()=>saveDraft().catch(()=>{}),1000)}
 async function flush(){clearTimeout(timer);await queue.catch(()=>{});while(dirty())await saveDraft()}
 function errorMessage(error){status(error.message,true);toast(error.message)}
 async function load(id){await flush();product=products.find(p=>p.id===id)||products[0];library=await api(`/admin/documents/${product.id}`);selected='';draft=null;search='';filter='';renderManager()}
 function productPicker(){return `<label class="document-product-picker">当前产品<select data-user-content data-doc-product>${products.map(p=>`<option value="${esc(p.id)}" ${p.id===product.id?'selected':''}>${esc(p.name)}${p.status==='published'?'':' · '+(p.status==='draft'?'草稿':'已下架')}</option>`).join('')}</select></label>`}
 function publicLink(){return product.status==='published'?`<a class="text-link" href="${articleURL('')}" target="_blank" rel="noopener">查看公开文档 ${icon('external-link')}</a>`:''}
 function badge(a){return `<span class="document-state ${a.published?'published':'draft'}">${a.published?'已发布':'草稿'}</span>${changed(a)?'<small class="document-pending">有未发布修改</small>':''}`}
 const action=(key,text,id='',group='')=>`<button type="button" data-doc-action="${key}" ${key==='edit'?'data-user-content':''} data-id="${esc(id)}" data-group="${esc(group)}">${text}</button>`;
 function menu(items){return `<details class="document-row-menu"><summary aria-label="更多操作">${icon('sliders')}</summary><div>${items}</div></details>`}
 function rows(){return library.groups.map(g=>{
  const articles=g.articles.filter(a=>(!search||a.draft.title.toLowerCase().includes(search.toLowerCase()))&&(!filter||(filter==='published'?a.published:filter==='changes'?changed(a):!a.published)));
  if((search||filter)&&!articles.length)return '';
  return `<tr class="document-group-row" draggable="true" data-drag="group:${esc(g.id)}" data-group="${esc(g.id)}"><th colspan="4"><div><button class="document-disclosure" type="button" data-doc-action="collapse" data-group="${esc(g.id)}" aria-expanded="${!collapsed.has(g.id)}" aria-label="展开或收起${esc(g.title)}">${icon('chevron-down')}</button><span class="document-drag" aria-label="拖动分组">${icon('list')}</span>${icon('book-open')}<strong data-user-content>${esc(g.title)}</strong><small>${g.articles.length} 篇</small><span class="document-group-actions">${action('new_article','新建文章','',g.id)}${menu(action('rename_group','重命名','',g.id)+action('group_up','上移','',g.id)+action('group_down','下移','',g.id)+action('delete_group','删除空分组','',g.id))}</span></div></th></tr>${collapsed.has(g.id)&&!search?'':articles.map(a=>`<tr draggable="true" data-drag="article:${esc(a.id)}" data-article="${esc(a.id)}" data-group="${esc(g.id)}"><td><span class="document-drag" aria-label="拖动文章">${icon('list')}</span>${icon('file-text')}${action('edit',esc(a.draft.title),a.id)}</td><td>${badge(a)}</td><td>${date(a.updated_at)}</td><td>${action('edit','编辑',a.id)}${menu(action('move_article','移动到分组',a.id)+action('article_up','上移',a.id)+action('article_down','下移',a.id)+(a.published?action('unpublish_article','取消发布',a.id):'')+action('delete_article','删除文章',a.id))}</td></tr>`).join('')}`;
 }).join('')}
 function renderManager(){
  document.body.classList.remove('document-editing');root.classList.remove('document-editor-workspace');
  const articles=library.groups.flatMap(g=>g.articles),published=articles.filter(a=>a.published).length;
  root.innerHTML=`<section class="document-manager"><div class="section-head"><div><h1>文档管理</h1><p class="hint">选择产品，独立维护文档分组、文章和发布状态。</p></div>${publicLink()}</div><div class="document-manager-toolbar">${productPicker()}<span class="hint">${library.groups.length} 个分组 · ${articles.length} 篇文章</span><div class="actions"><button type="button" class="secondary" data-doc-action="new_group">新建分组</button><button type="button" data-doc-action="new_article">新建文章</button></div></div><div class="document-visibility ${product.status==='published'?'is-public':''}"><strong>${product.status==='published'?`产品已发布 · ${published} 篇文章前台可见`:'产品未发布 · 全部文档仅供后台预览'}</strong><span>草稿和未发布的修改仅后台可见。</span></div><div class="document-filters"><label class="sr-only" for="document-search">搜索文章标题</label><input id="document-search" type="search" placeholder="搜索文章标题" data-doc-search><select data-doc-filter aria-label="文章状态"><option value="">全部文章状态</option><option value="published">已发布</option><option value="draft">草稿</option><option value="changes">有未发布修改</option></select></div>${articles.length||library.groups.length?`<div class="table-wrap document-table"><table><thead><tr><th>文章标题</th><th>文章状态</th><th>最近编辑</th><th>操作</th></tr></thead><tbody data-document-rows>${rows()}</tbody></table></div>`:'<div class="empty document-empty">'+icon('book-open')+'<h2>开始编写产品文档</h2><p>先新建文章或分组；目录、标题和内容均可自由调整。</p></div>'}<footer class="document-manager-footer"><span>拖动可调整顺序或跨分组移动文章，也可使用菜单移动。</span><span role="status" data-doc-status>目录已保存</span></footer></section>`;
 }
 function renderSettings(){
  const a=find(selected),g=groupOf(selected);
  if(!seoTab)return `<h2>文章设置</h2><label>所属分组<select data-user-content data-doc-group>${library.groups.map(group=>`<option value="${esc(group.id)}" ${group.id===g.id?'selected':''}>${esc(group.title)}</option>`).join('')}</select></label><div class="document-copy-link"><span class="hint">文章链接由系统自动生成</span><button type="button" class="secondary" data-doc-action="copy_link">复制链接</button></div><hr><h3>产品状态</h3><p>${product.status==='published'?'已发布':'未发布 · 文档仅后台预览'}</p><h3>文章状态</h3>${badge({...a,draft})}<h3>最近发布</h3><p>${a.published_at?date(a.published_at):'尚未发布'}</p><p class="hint">自动保存只更新草稿。产品已发布时，发布文章后客户才能看到内容。</p>${a.published?`<a href="${articleURL(a.published.slug)}" target="_blank" rel="noopener">查看公开版本</a>`:''}`;
  return `<h2>文章 SEO 与分享</h2><p class="hint" data-user-content>仅用于「${esc(draft.title)}」这一篇文章。留空时自动使用文章标题和正文摘要。</p>${field('搜索标题（可选）','seo_title',draft.seo.title,'maxlength="160" data-doc-field') }<label>搜索描述（可选）<textarea name="seo_description" rows="4" maxlength="500" data-doc-field>${esc(draft.seo.description)}</textarea></label><label class="check-option"><input type="checkbox" name="seo_index" data-doc-field ${draft.seo.no_index?'':'checked'}><span>允许搜索引擎收录</span></label><p class="hint">仅公开的已发布版本可被收录，草稿和预览不参与收录。</p>${field('分享图（可选）','seo_image',draft.seo.share_image_url,'type="url" data-doc-field placeholder="https://…"')}<button type="button" class="secondary small" data-doc-action="share_image">上传分享图</button><h3>搜索结果预览</h3><div class="document-serp"><small>${esc(location.origin+articleURL(draft.slug))}</small><strong data-serp-title>${esc(draft.seo.title||draft.title+' · '+product.name)}</strong><p data-serp-description>${esc(draft.seo.description||draft.body.replace(/[#*\x60|>\[\]]/g,'').slice(0,160)||'正文摘要将显示在这里。')}</p></div><details class="document-advanced"><summary>高级设置</summary>${field('自定义链接','article_slug',draft.slug,'pattern="[a-z0-9][a-z0-9-]{0,79}" maxlength="80" data-doc-field')}<p class="hint">默认无需修改。发布新链接后，原链接自动跳转到新地址。</p><button type="button" class="secondary small" data-doc-action="copy_link">复制文章链接</button></details>`;
 }
 function renderEditor(){
  const a=find(selected);if(!a)return renderManager();draft=copy(a.draft);saved=JSON.stringify(draft);
  document.body.classList.add('document-editing');root.classList.add('document-editor-workspace');
  root.innerHTML=`<section class="document-editor"><header class="document-editor-header"><button type="button" class="text-link" data-doc-action="back">返回文档管理</button><span class="document-editor-context">${esc(product.name)} / 产品文档</span><span class="document-save-state" role="status" data-doc-status>草稿已自动保存</span><button type="button" class="secondary" data-doc-action="preview">预览草稿</button><button type="button" data-doc-action="publish_article">${a.published?'发布更新':'发布文章'}</button></header><div class="document-editor-layout"><aside class="document-editor-tree"><h2>文档目录</h2>${library.groups.map(g=>`<details open><summary>${icon('book-open')}${esc(g.title)}</summary>${g.articles.map(row=>`<button type="button" data-user-content data-doc-action="edit" data-id="${esc(row.id)}" class="${row.id===selected?'selected':''}">${icon('file-text')}<span>${esc(row.draft.title)}</span>${row.published?'':'<small class="document-draft-dot" aria-label="草稿"></small>'}</button>`).join('')}</details>`).join('')}<button type="button" class="text-link" data-doc-action="new_article">新建文章</button></aside><section class="document-writing"><p class="hint">${esc(groupOf(selected).title)}</p><label class="sr-only" for="document-title">文章标题</label><input id="document-title" class="document-title" name="article_title" value="${esc(draft.title)}" maxlength="160" data-doc-field><div class="document-formatting" role="toolbar" aria-label="正文格式"><select data-doc-block aria-label="段落格式"><option value="p">正文</option><option value="h1">一级标题</option><option value="h2">二级标题</option><option value="h3">三级标题</option></select><button type="button" data-doc-command="bold" aria-label="加粗"><strong>B</strong></button><button type="button" data-doc-command="italic" aria-label="斜体"><em>I</em></button><button type="button" data-doc-command="insertUnorderedList" aria-label="无序列表">${icon('list')}</button><button type="button" data-doc-command="insertOrderedList" aria-label="编号列表">编号</button><button type="button" data-doc-action="link" aria-label="插入链接" title="插入链接">${icon('link')}</button><button type="button" data-doc-action="image">图片</button><button type="button" data-doc-action="code">代码块</button><button type="button" data-doc-action="table">表格</button><button type="button" data-doc-command="undo" aria-label="撤销">${icon('rotate-ccw')}</button></div><div class="document-rich-body document-prose" contenteditable="true" role="textbox" aria-label="文章正文" aria-multiline="true" data-user-content data-doc-editor spellcheck="false">${renderDocument(draft.body)||'<p><br></p>'}</div></section><aside class="document-editor-settings"><div class="document-settings-tabs" role="tablist" aria-label="文章设置"><button type="button" role="tab" aria-selected="${!seoTab}" data-doc-action="tab_article">文章</button><button type="button" role="tab" aria-selected="${seoTab}" data-doc-action="tab_seo">SEO 与分享</button></div><div data-doc-settings role="tabpanel" aria-label="当前文章设置">${renderSettings()}</div></aside></div></section>`;
 }
 async function ask(title,fields=[],confirm='保存'){
  const dialog=document.createElement('dialog');dialog.className='action-dialog document-dialog';dialog.innerHTML=`<form><h2>${esc(title)}</h2>${fields.map(f=>f.options?`<label>${esc(f.label)}<select name="${f.name}">${f.options.map(o=>`<option value="${esc(o.value)}" ${o.value===f.value?'selected':''}>${esc(o.label)}</option>`).join('')}</select></label>`:f.type==='textarea'?`<label>${esc(f.label)}<textarea name="${f.name}" rows="6">${esc(f.value||'')}</textarea></label>`:field(esc(f.label),f.name,f.value||'',`${f.optional?'':'required'} maxlength="${f.max||160}"`)).join('')}<div class="actions"><button type="button" class="secondary" data-close>取消</button><button>${esc(confirm)}</button></div></form>`;root.append(dialog);dialog.showModal();return new Promise(resolve=>{let value=null;dialog.querySelector('[data-close]').onclick=()=>dialog.close();dialog.querySelector('form').onsubmit=e=>{e.preventDefault();value=Object.fromEntries(new FormData(e.target));dialog.close()};dialog.onclose=()=>{dialog.remove();resolve(value)}});
 }
 function editor(){return root.querySelector('[data-doc-editor]')}
 let selection;
 const remember=()=>{const s=window.getSelection();if(s.rangeCount&&editor()?.contains(s.anchorNode))selection=s.getRangeAt(0).cloneRange()};
 function restore(){editor()?.focus();if(selection&&editor()?.contains(selection.startContainer)){const s=window.getSelection();s.removeAllRanges();s.addRange(selection)}}
 function insert(html){
  const surface=editor();restore();
  const template=document.createElement('template');template.innerHTML=html;
  const fragment=template.content,last=fragment.lastChild;
  if(html.startsWith('<a')&&selection&&surface.contains(selection.startContainer)){
   selection.deleteContents();selection.insertNode(fragment);
  }else{
   let block=selection?.startContainer;
   while(block&&block!==surface&&block.parentNode!==surface)block=block.parentNode;
   if(block&&block!==surface&&block.parentNode===surface)block.after(fragment);else surface.append(fragment);
  }
  if(last){const range=document.createRange();range.selectNodeContents(last);range.collapse(false);const current=window.getSelection();current.removeAllRanges();current.addRange(range);selection=range.cloneRange()}
  surface.focus();draft.body=editorMarkdown(surface);scheduleSave();
 }
 async function imageUpload(share=false){
  const input=document.createElement('input');input.type='file';input.accept='image/png,image/jpeg,image/webp,image/gif';input.onchange=async()=>{if(!input.files[0])return;try{const body=new FormData();body.append('file',input.files[0]);status('正在上传图片…');const result=await uploadAPI(`/admin/products/${product.id}/document-images`,body);if(share){draft.seo.share_image_url=result.url;root.querySelector('[name=seo_image]').value=result.url;scheduleSave()}else insert(`<p><img src="${esc(result.url)}" alt=""></p>`)}catch(e){errorMessage(e)}};input.click();
 }
 async function handle(key,target){
  const id=target.dataset.id,gid=target.dataset.group;
  switch(key){
  case 'edit':await flush();selected=id;seoTab=false;renderEditor();return;
  case 'back':await flush();selected='';draft=null;renderManager();return;
  case 'tab_article':case 'tab_seo':seoTab=key==='tab_seo';root.querySelectorAll('[role=tab]').forEach(t=>t.setAttribute('aria-selected',String((t.dataset.docAction==='tab_seo')===seoTab)));root.querySelector('[data-doc-settings]').innerHTML=renderSettings();return;
  case 'collapse':collapsed.has(gid)?collapsed.delete(gid):collapsed.add(gid);root.querySelector('[data-document-rows]').innerHTML=rows();return;
  case 'copy_link':await navigator.clipboard.writeText(location.origin+articleURL(draft.slug));toast('文章链接已复制');return;
  case 'preview':{await flush();const dialog=document.createElement('dialog');dialog.className='document-preview';dialog.innerHTML=`<div class="section-head"><h2>草稿预览</h2><button type="button" class="secondary">关闭</button></div><p class="hint">仅后台可见，不参与搜索引擎收录。</p><article class="document-prose"><h1>${esc(draft.title)}</h1>${renderDocument(draft.body)}</article>`;dialog.querySelector('button').onclick=()=>dialog.close();dialog.onclose=()=>dialog.remove();root.append(dialog);dialog.showModal();return}
  case 'new_group':case 'rename_group':{await flush();const value=await ask(key==='new_group'?'新建分组':'重命名分组',[{name:'title',label:'分组名称',value:library.groups.find(g=>g.id===gid)?.title||''}]);if(!value)return;await command({action:key==='new_group'?'create_group':'rename_group',group_id:gid,title:value.title});renderManager();return}
  case 'new_article':{
   await flush();const value=await ask('新建文章',[{name:'title',label:'文章标题'},...(library.groups.length?[{name:'group',label:'所属分组',value:gid||library.groups[0].id,options:library.groups.map(g=>({value:g.id,label:g.title}))}]:[])] ,'创建文章');if(!value)return;
   if(!library.groups.length)await command({action:'create_group',title:'未分组'});
   const before=new Set(library.groups.flatMap(g=>g.articles.map(a=>a.id)));await command({action:'create_article',group_id:value.group||library.groups[0].id,title:value.title});selected=library.groups.flatMap(g=>g.articles).find(a=>!before.has(a.id)).id;seoTab=false;renderEditor();return;
  }
  case 'publish_article':await flush();await command({action:key,id:selected});toast(product.status==='published'?'文章已发布':'文章版本已保存；产品发布后才会对外可见');renderEditor();return;
  case 'delete_group':case 'delete_article':case 'unpublish_article':{
   await flush();const value=await ask(key==='delete_group'?'删除空分组？':key==='delete_article'?'永久删除这篇文章？':'取消发布这篇文章？',[],'确认');if(!value)return;await command({action:key,id,group_id:gid});selected='';draft=null;renderManager();return;
  }
  case 'group_up':case 'group_down':{const at=library.groups.findIndex(g=>g.id===gid),next=key==='group_up'?at-1:at+2;if(at<0||next<0||key==='group_down'&&at===library.groups.length-1)return;await command({action:'move_group',group_id:gid,before_id:library.groups[next]?.id||''});renderManager();return}
  case 'article_up':case 'article_down':{const g=groupOf(id),at=g.articles.findIndex(a=>a.id===id),next=key==='article_up'?at-1:at+2;if(next<0||key==='article_down'&&at===g.articles.length-1)return;await command({action:'move_article',id,group_id:g.id,before_id:g.articles[next]?.id||''});renderManager();return}
  case 'move_article':{const v=await ask('移动文章',[{name:'group',label:'目标分组',value:groupOf(id).id,options:library.groups.map(g=>({value:g.id,label:g.title}))}],'移动到分组末尾');if(v){await command({action:'move_article',id,group_id:v.group});renderManager()}return}
  case 'link':{remember();const v=await ask('插入链接',[{name:'text',label:'链接文字'},{name:'url',label:'链接地址',max:2048}]);if(v){if(!documentURL(v.url))throw Error('请输入有效的 HTTP/HTTPS 链接');insert(`<a href="${esc(v.url)}">${esc(v.text)}</a>`)}return}
  case 'image':remember();await imageUpload();return;
  case 'share_image':await imageUpload(true);return;
  case 'code':{remember();const v=await ask('插入代码块',[{name:'code',label:'代码内容',type:'textarea'}],'插入');if(v)insert(`<pre><code>${esc(v.code)}</code></pre><p><br></p>`);return}
  case 'table':remember();insert('<table><thead><tr><th>列标题</th><th>列标题</th></tr></thead><tbody><tr><td>内容</td><td>内容</td></tr><tr><td>内容</td><td>内容</td></tr></tbody></table><p><br></p>');return;
  }
 }
 root.addEventListener('click',event=>{const target=event.target.closest('[data-doc-action]');if(target){event.preventDefault();handle(target.dataset.docAction,target).catch(errorMessage)}const cmd=event.target.closest('[data-doc-command]');if(cmd){restore();document.execCommand(cmd.dataset.docCommand);draft.body=editorMarkdown(editor());scheduleSave()}});
 root.addEventListener('pointerdown',event=>{if(event.target.closest('.document-formatting'))remember()});
 root.addEventListener('keyup',remember);root.addEventListener('mouseup',remember);
 root.addEventListener('paste',event=>{if(event.target.closest('[data-doc-editor]')){event.preventDefault();document.execCommand('insertText',false,event.clipboardData.getData('text/plain'));draft.body=editorMarkdown(editor());scheduleSave()}});
 root.addEventListener('input',event=>{
  const el=event.target;
  if(el.matches('[data-doc-search]')){search=el.value;root.querySelector('[data-document-rows]')?.replaceChildren();if(root.querySelector('[data-document-rows]'))root.querySelector('[data-document-rows]').innerHTML=rows();return}
  if(!draft)return;
  if(el.closest('[data-doc-editor]'))draft.body=editorMarkdown(editor());
  else if(el.matches('[data-doc-field]')){const name=el.name;if(name==='article_title')draft.title=el.value;else if(name==='article_slug')draft.slug=el.value;else if(name==='seo_title')draft.seo.title=el.value;else if(name==='seo_description')draft.seo.description=el.value;else if(name==='seo_image')draft.seo.share_image_url=el.value;else if(name==='seo_index')draft.seo.no_index=!el.checked;}
  else return;
  const title=root.querySelector('[data-serp-title]'),desc=root.querySelector('[data-serp-description]');if(title)title.textContent=draft.seo.title||draft.title+' · '+product.name;if(desc)desc.textContent=draft.seo.description||draft.body.slice(0,160);scheduleSave();
 });
 root.addEventListener('change',event=>{const el=event.target;if(el.matches('[data-doc-product]'))load(el.value).catch(errorMessage);if(el.matches('[data-doc-filter]')){filter=el.value;root.querySelector('[data-document-rows]').innerHTML=rows()}if(el.matches('[data-doc-block]')){restore();document.execCommand('formatBlock',false,el.value);draft.body=editorMarkdown(editor());scheduleSave()}if(el.matches('[data-doc-group]')){flush().then(()=>command({action:'move_article',id:selected,group_id:el.value})).then(()=>renderEditor()).catch(errorMessage)}});
 root.addEventListener('dragstart',event=>{const row=event.target.closest('[data-drag]');if(!row)return;drag=row.dataset.drag;event.dataTransfer.setData('text/plain',drag);event.dataTransfer.effectAllowed='move'});
 root.addEventListener('dragover',event=>{if(drag&&event.target.closest('[data-group]')){event.preventDefault();event.dataTransfer.dropEffect='move'}});
 root.addEventListener('dragend',()=>{drag=''});
 root.addEventListener('drop',event=>{const row=event.target.closest('[data-group]');if(!row||!drag)return;event.preventDefault();const [kind,id]=drag.split(':');drag='';if(kind==='group'&&id===row.dataset.group||kind==='article'&&id===row.dataset.article)return;command(kind==='group'?{action:'move_group',group_id:id,before_id:row.dataset.group}:{action:'move_article',id,group_id:row.dataset.group,before_id:row.dataset.article||''}).then(renderManager).catch(errorMessage)});
 const unload=event=>{if(dirty()){event.preventDefault();event.returnValue=''}};window.addEventListener('beforeunload',unload);
 if(!products.length){root.innerHTML='<h1>文档管理</h1><div class="empty"><h2>请先创建产品</h2><p>文档按产品独立管理。</p><a class="button" href="#/admin/product">前往品牌与产品</a></div>'}else await load(product.id);
 return async()=>{if(disposed)return;await flush();disposed=true;clearTimeout(timer);window.removeEventListener('beforeunload',unload);document.body.classList.remove('document-editing')};
}
