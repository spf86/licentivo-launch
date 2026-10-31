const escapeHTML=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const statuses={pending:['amber','等待付款'],paid:['green','已付款'],refunding:['amber','退款中'],refunded:['blue','已退款'],reversed:['red','已撤款'],closed:['gray','已关闭'],failed:['red','付款失败'],expired:['gray','已过期']};

export function orderStatusBadge(status,label){
 const [tone,fallback]=statuses[status]||['gray',status||'未知状态'];
 return `<span class="order-status order-status-${tone}">${escapeHTML(label||fallback)}</span>`;
}

export async function reconcileSelection(api,ids,reason,onProgress=()=>{}){
 const unique=[...new Set(ids)];
 if(!unique.length||unique.length>25)throw new Error('请选择 1–25 笔订单');
 const results=[];
 for(let start=0;start<unique.length;start+=5){
  const batch=unique.slice(start,start+5);
  try{results.push(...await api('/admin/orders/reconcile','POST',{order_ids:batch,reason}))}
  catch(error){
   // A lost response is ambiguous: do not report success or retry automatically.
   results.push(...batch.map(order_id=>({order_id,checked:false,code:error.code||'RESULT_UNKNOWN',message:error.message||'结果未能确认，请刷新订单状态后核对'})));
   onProgress([...results]);
   break;
  }
  onProgress([...results]);
 }
 return results;
}

export async function mountOrderManagement(root,{api,time,money,canManage}){
 const selected=new Map();
 let page={items:[],total:0,limit:25,offset:0},filter={q:'',status:'',environment:'',needs_review:false},busy=false,sequence=0;
 root.innerHTML=`<h1>订单</h1><p class="hint">可搜索全部历史订单。订单固定关联购买时的产品和套餐版本。</p>
  <form class="order-filters" aria-label="筛选订单">
   <label class="order-search">搜索订单<input name="q" type="search" maxlength="254" placeholder="订单编号、客户邮箱或产品名称"></label>
   <label>状态<select name="status"><option value="">全部状态</option>${Object.entries(statuses).filter(([key])=>!['failed','expired'].includes(key)).map(([key,[,label]])=>`<option value="${key}">${label}</option>`).join('')}</select></label>
   <label>环境<select name="environment"><option value="">全部环境</option><option value="live">正式</option><option value="sandbox">测试</option></select></label>
   <label class="order-checkbox"><input name="needs_review" type="checkbox">仅待核对</label>
   <button>搜索</button><button type="button" class="secondary" data-order-command="reset">重置</button>
  </form>
  <div class="section-head order-toolbar"><p data-order-count role="status" aria-live="polite"></p>${canManage?'<div class="actions"><span data-order-selection>已选 0 / 25</span><button type="button" class="secondary" data-order-command="clear">清空选择</button><button type="button" data-order-command="bulk" disabled>核对所选付款</button></div>':''}</div>
  <p class="form-error" data-order-error role="alert"></p><div data-order-table></div>
  <nav class="actions order-pagination" aria-label="订单分页"><button type="button" class="secondary" data-order-command="prev">上一页</button><span data-order-page></span><button type="button" class="secondary" data-order-command="next">下一页</button></nav>
  <section class="card section" data-order-results hidden><h2>付款核对结果</h2><p data-order-progress role="status" aria-live="polite"></p><div data-order-result-rows></div></section>
  <dialog class="action-dialog" data-order-dialog><form><div class="section-head"><h2>主动核对付款</h2><button type="button" class="text-link" data-order-command="close">关闭</button></div><p data-order-dialog-summary></p><ul class="order-preview" data-order-preview></ul><p class="hint">将核实所选订单的原交易，并按支付渠道返回的实际结果更新状态。不会直接将订单标记为已付款。</p><label>核对原因<input name="reason" required minlength="2" maxlength="500" autocomplete="off"></label><p class="form-error" data-order-dialog-error role="alert"></p><div class="actions"><button>开始核对</button><button type="button" class="secondary" data-order-command="close">取消</button></div></form></dialog>`;
 const find=selector=>root.querySelector(selector),form=find('.order-filters'),dialog=find('[data-order-dialog]');
 let pending=[];
 function selectionState(){
  const counter=find('[data-order-selection]');if(counter)counter.textContent=`已选 ${selected.size} / 25`;
  const bulk=find('[data-order-command="bulk"]');if(bulk)bulk.disabled=busy||!selected.size;
  const boxes=[...root.querySelectorAll('[data-order-select]')];
  for(const box of boxes)box.checked=selected.has(box.dataset.orderSelect);
  const all=find('[data-order-select-page]');if(all){all.checked=boxes.length>0&&boxes.every(box=>box.checked);all.indeterminate=boxes.some(box=>box.checked)&&!all.checked}
 }
 function render(){
  const rows=page.items.map(order=>`<tr>${canManage?`<td><input type="checkbox" data-order-select="${escapeHTML(order.id)}" aria-label="选择订单 ${escapeHTML(order.id)}"></td>`:''}<td><strong>${escapeHTML(order.product_name)}</strong><br><span class="hint">${escapeHTML(order.plan_name)} · v${escapeHTML(order.plan_version)}</span><br><code class="order-id">${escapeHTML(order.id)}</code></td><td>${escapeHTML(order.customer_email)}</td><td>${escapeHTML(time(order.created_at))}</td><td>${escapeHTML(money(order))}</td><td>${escapeHTML({purchase:'购买',renew:'续费',upgrade:'升级'}[order.action]||order.action)}</td><td>${order.environment==='live'?'正式':'测试'}</td><td>${orderStatusBadge(order.status)}${order.fulfillment_review?'<br><span class="order-status order-status-amber">权益需核对</span>':''}</td>${canManage?`<td><button type="button" class="small secondary" data-order-reconcile="${escapeHTML(order.id)}">核对付款</button></td>`:''}</tr>`);
  const widths=canManage?[4,22,18,14,8,6,6,10,12]:[27,22,17,10,7,7,10];
  find('[data-order-table]').innerHTML=`<div class="table-wrap"><table class="order-table"><colgroup>${widths.map(width=>`<col class="order-col-${width}">`).join('')}</colgroup><thead><tr>${canManage?'<th scope="col"><input type="checkbox" data-order-select-page aria-label="选择本页订单"></th>':''}${['产品与套餐 / 订单编号','客户','创建时间','金额','类型','环境','状态',...(canManage?['操作']:[])].map(label=>`<th scope="col">${label}</th>`).join('')}</tr></thead><tbody>${rows.length?rows.join(''):`<tr><td colspan="${canManage?9:7}">没有符合条件的订单，可调整搜索条件。</td></tr>`}</tbody></table></div>`;
  find('[data-order-count]').textContent=`共 ${page.total} 笔订单`;
  find('[data-order-page]').textContent=page.total?`第 ${Math.floor(page.offset/page.limit)+1} / ${Math.ceil(page.total/page.limit)} 页`:'第 0 / 0 页';
  find('[data-order-command="prev"]').disabled=busy||page.offset===0;
  find('[data-order-command="next"]').disabled=busy||page.offset+page.limit>=page.total;
  selectionState();
 }
 async function load(offset=0){
  const request=++sequence;find('[data-order-error]').textContent='';find('[data-order-table]').setAttribute('aria-busy','true');
  const params=new URLSearchParams({...filter,offset:String(offset),limit:'25'});
  try{const result=await api('/admin/orders/search?'+params);if(request!==sequence||!root.isConnected)return;page=result;render()}
  catch(error){if(request===sequence)find('[data-order-error]').textContent=error.message}
  finally{if(request===sequence)find('[data-order-table]').removeAttribute('aria-busy')}
 }
 form.addEventListener('submit',event=>{event.preventDefault();event.stopPropagation();if(busy)return;const values=new FormData(form);filter={q:String(values.get('q')||'').trim(),status:String(values.get('status')||''),environment:String(values.get('environment')||''),needs_review:values.has('needs_review')};load(0)});
 root.addEventListener('change',event=>{
  const box=event.target;
  if(box.matches('[data-order-select-page]')){for(const order of page.items){if(box.checked&&selected.size<25)selected.set(order.id,order);else if(!box.checked)selected.delete(order.id)}}
  if(box.matches('[data-order-select]')){const order=page.items.find(item=>item.id===box.dataset.orderSelect);if(box.checked&&selected.size<25)selected.set(order.id,order);else if(!box.checked)selected.delete(order.id);else find('[data-order-error]').textContent='每次最多选择 25 笔订单，请先核对或清空选择。'}
  selectionState();
 });
 root.addEventListener('click',async event=>{
  const button=event.target.closest('[data-order-command],[data-order-reconcile]');if(!button)return;event.preventDefault();event.stopPropagation();if(busy)return;
  const command=button.dataset.orderCommand;
  if(command==='close'){dialog.close();return}
  if(command==='clear'){selected.clear();selectionState();return}
  if(command==='prev'){await load(Math.max(0,page.offset-page.limit));return}
  if(command==='next'){await load(page.offset+page.limit);return}
  if(command==='reset'){form.reset();filter={q:'',status:'',environment:'',needs_review:false};await load(0);return}
  if(command==='bulk'||button.dataset.orderReconcile){
   pending=command==='bulk'?[...selected.keys()]:[button.dataset.orderReconcile];
   find('[data-order-dialog-summary]').textContent=pending.length===1?`核对订单 ${pending[0]}`:`核对已选择的 ${pending.length} 笔订单（含其他页选择）。`;
   find('[data-order-preview]').innerHTML=pending.map(id=>{const order=selected.get(id)||page.items.find(item=>item.id===id);return `<li>${order?`<strong>${escapeHTML(order.product_name)}</strong> · ${escapeHTML(money(order))}<br>${escapeHTML(order.customer_email)}<br>`:''}<code class="order-id">${escapeHTML(id)}</code></li>`}).join('');
   find('[data-order-dialog-error]').textContent='';dialog.querySelector('[name="reason"]').value='';dialog.showModal();dialog.querySelector('[name="reason"]').focus();
  }
 });
 dialog.addEventListener('cancel',event=>{if(busy)event.preventDefault()});
 dialog.querySelector('form').addEventListener('submit',async event=>{
  event.preventDefault();event.stopPropagation();if(busy)return;
  const reason=dialog.querySelector('[name="reason"]').value.trim();if(reason.length<2){find('[data-order-dialog-error]').textContent='请填写至少 2 个字符的核对原因';return}
  busy=true;const ids=[...pending];dialog.close();find('[data-order-results]').hidden=false;
  for(const control of root.querySelectorAll('button,input,select'))control.disabled=true;
  const progress=results=>{
   find('[data-order-progress]').textContent=`已完成 ${results.length} / ${ids.length} 笔，核对成功 ${results.filter(item=>item.checked).length} 笔`;
   find('[data-order-result-rows]').innerHTML=results.map(result=>`<p><code class="order-id">${escapeHTML(result.order_id)}</code> ${orderStatusBadge(result.checked?'paid':'failed',result.checked?'核对完成':'需继续核实')} ${escapeHTML(result.message)}${result.code?` <span class="hint">${escapeHTML(result.code)}</span>`:''}</p>`).join('');
  };
  progress([]);
  try{const results=await reconcileSelection(api,ids,reason,progress);for(const result of results)if(result.checked)selected.delete(result.order_id);if(results.length<ids.length)find('[data-order-progress]').textContent+=`；其余 ${ids.length-results.length} 笔尚未执行，请查看错误后重新选择核对。`}
  finally{busy=false;for(const control of root.querySelectorAll('button,input,select'))control.disabled=false;await load(page.offset)}
 });
 await load();
}
