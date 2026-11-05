import {orderStatusBadge} from './order-management.js';
import {icon} from './workspace.js';
import {installExpiryPreferenceControls,expiryPreferenceSummary} from './mail-notifications.js';

const esc=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const stateNames={active:'正常',disabled:'已停用',closed:'已关闭'};
const actions={purchase:'购买',renew:'续费',upgrade:'升级'};

export function customerPlanState(product,now=Date.now()){
 const states={pending:['blue','待开通'],scheduled:['blue','待生效'],expired:['red','已到期'],revoked:['gray','已撤销'],suspended:['amber','已暂停']};
 if(states[product.status])return states[product.status];
 if(!product.dates_known)return ['gray','期限待确认'];
 if(product.valid_until){const left=new Date(product.valid_until).getTime()-now;if(left<=0)return ['red','已到期'];if(left<=7*86400000)return ['amber','即将到期']}
 return ['green','有效'];
}
export function customerExpiry(product,time){
 if(!product.dates_known)return '待确认';
 if(product.permanent)return '永久';
 return product.valid_until?time(product.valid_until):'未记录';
}
export function renderCustomerProduct(product,time,{canManage=false}={}){
 const [tone,label]=customerPlanState(product);
 const renewal=product.purchased_permanent&&!product.permanent?'已续费为永久':product.purchased_until&&product.valid_until&&new Date(product.purchased_until)>new Date(product.valid_until)?'已购买至 '+time(product.purchased_until):'';
 return `<tr><td data-label="产品"><strong>${esc(product.product_name)}</strong></td><td data-label="套餐">${esc(product.plan_name||'待确认')}${renewal?`<small class="customer-renewal">${esc(renewal)}</small>`:''}</td><td data-label="购买时间">${esc(time(product.paid_at))}</td><td data-label="生效时间">${esc(time(product.valid_from))}</td><td data-label="到期时间">${esc(customerExpiry(product,time))}</td><td data-label="套餐状态"><span class="order-status order-status-${tone}">${esc(label)}</span>${!product.synchronized&&product.status!=='revoked'?'<small class="customer-sync">授权未同步</small>':''}${canManage?`<div class="customer-expiry-controls"><small data-expiry-summary>${esc(expiryPreferenceSummary(product))}</small><button type="button" class="small secondary" data-expiry-entitlement="${esc(product.id)}">到期提醒</button></div>`:''}</td></tr>`;
}

export function renderCustomerOrder(v,{time,money}){
 return `<tr><td><strong>${esc(v.product_name)}</strong><small class="customer-order-id">${esc(v.id)}</small></td><td>${esc(v.plan_name)}<small>版本 ${esc(v.plan_version)}</small></td><td>${esc(actions[v.action]||v.action)}</td><td>${esc(money(v))}</td><td>${esc(time(v.paid_at))}</td><td>${v.coverage_recorded?esc(time(v.valid_from))+' 至 '+esc(v.permanent?'永久':time(v.valid_until))+(v.voided?'<small>该期限已作废</small>':''):'<span class="hint">'+(v.paid_at?'待确认权益':'未生效')+'</span>'}</td><td>${orderStatusBadge(v.status)}</td></tr>`;
}

export function mountCustomerManagement(root,{api,time,money,canManage,initialPage}){
 let page=initialPage||{items:[],total:0,limit:25,offset:0},filter={q:'',status:'',environment:'live'},busy=false,sequence=0,expanded='',detailSequence=0,detail=null,tab='products',detailBusy=false,detailError='';
 root.classList.add('customer-management');
 root.innerHTML=`<h1>客户管理</h1><p class="hint customer-intro">查看客户购买的套餐与使用期限。</p><form class="customer-filters" aria-label="筛选客户"><label class="customer-search">搜索客户邮箱<input name="q" type="search" maxlength="254" placeholder="搜索全部客户邮箱"></label><label>账号状态<select name="status"><option value="">全部状态</option><option value="active">正常</option><option value="disabled">已停用</option><option value="closed">已关闭</option></select></label><label class="customer-environment">购买环境<select name="environment"><option value="live">正式</option><option value="sandbox">测试</option></select></label><button>搜索</button><button type="button" class="secondary" data-customer-command="reset">重置</button></form><p class="customer-count" role="status" aria-live="polite"></p><p class="form-error" data-customer-error role="alert"></p><div data-customer-table></div><nav class="customer-pagination" aria-label="客户分页"><button type="button" class="secondary" data-customer-command="prev">上一页</button><span data-customer-page></span><button type="button" class="secondary" data-customer-command="next">下一页</button></nav>`;
 const find=s=>root.querySelector(s),form=find('form');
 installExpiryPreferenceControls(root,{api,canManage});
 const badge=(tone,label)=>`<span class="order-status order-status-${tone}">${esc(label)}</span>`;
 const pageControls=(type,value)=>`<nav class="customer-pagination" aria-label="${type==='products'?'套餐':'购买记录'}分页"><button type="button" class="small secondary" data-customer-command="detail-prev" ${detailBusy||value.offset===0?'disabled':''}>上一页</button><span>第 ${value.total?Math.floor(value.offset/value.limit)+1:0} / ${Math.ceil(value.total/value.limit)} 页 · 共 ${value.total} 项</span><button type="button" class="small secondary" data-customer-command="detail-next" ${detailBusy||value.offset+value.limit>=value.total?'disabled':''}>下一页</button></nav>`;
 function purchaseSummary(user){const p=user.purchases;if(!p?.product_count)return '<span class="hint">未购买</span>';return p.products.slice(0,1).map(v=>`<strong>${esc(v.product_name)}</strong><small>${esc(v.plan_name||'待确认')}</small>${p.product_count>1?'<small>另有 '+(p.product_count-1)+' 个产品</small>':''}`).join('')}
 function nextExpiry(user){const p=user.purchases;if(p?.next_expiry)return time(p.next_expiry);return ({permanent:'永久',inactive:'查看套餐期限',not_purchased:'—'})[p?.expiry_state]||'—'}
 function detailBody(user){
  let body='';
  if(detailBusy)body='<p class="customer-loading" role="status">正在读取'+(tab==='products'?'套餐与期限':'购买记录')+'…</p>';
  else if(detailError)body=`<p class="form-error" role="alert">${esc(detailError)}</p><button type="button" class="small secondary" data-customer-command="detail-retry">重试</button>`;
  else if(detail){
   if(tab==='products')body=detail.items.length?`<div class="customer-inner-scroll"><table class="customer-products-table"><thead><tr>${['产品','套餐','购买时间','生效时间','到期时间','套餐状态'].map(v=>'<th scope="col">'+v+'</th>').join('')}</tr></thead><tbody>${detail.items.map(v=>renderCustomerProduct(v,time,{canManage})).join('')}</tbody></table></div>${detail.total>detail.limit?pageControls(tab,detail):''}`:'<p class="hint customer-empty">当前环境暂无已购买的套餐。可以在购买记录中查看待付款或已关闭订单。</p>';
   else body=`<form class="customer-history-search" aria-label="搜索该客户购买记录"><input type="search" name="order_q" maxlength="254" aria-label="搜索订单编号、产品或套餐" placeholder="搜索订单编号、产品或套餐" value="${esc(detail.query||'')}"><button class="small secondary">搜索记录</button></form>`+(detail.items.length?`<div class="customer-inner-scroll"><table class="customer-orders-table"><thead><tr>${['订单与产品','套餐','类型','金额','付款时间','本单有效期','状态'].map(v=>'<th scope="col">'+v+'</th>').join('')}</tr></thead><tbody>${detail.items.map(v=>renderCustomerOrder(v,{time,money})).join('')}</tbody></table></div>${pageControls(tab,detail)}`:'<p class="hint customer-empty">暂无匹配的购买记录。</p>');
  }
  return `<tr class="customer-detail-row"><td colspan="6"><section class="customer-detail" id="customer-detail-${esc(user.id)}" aria-label="${esc(user.email)}的套餐与期限"><div class="customer-detail-heading"><h2>${esc(user.email)} 的${tab==='products'?'套餐与期限':'购买记录'}</h2><button type="button" class="text-link" data-customer-command="${tab==='products'?'orders':'products'}">${tab==='products'?'查看全部购买记录':'返回套餐与期限'}</button></div>${body}<div class="customer-detail-meta"><span>${filter.environment==='live'?'正式环境':'测试环境'} · 注册于 ${esc(time(user.created_at))} · 邮箱${user.verified?'已验证':'待验证'}</span>${canManage&&user.status!=='closed'?`<button type="button" class="text-link ${user.status==='active'?'danger':''}" data-action="open-user-status" data-id="${esc(user.id)}" data-email="${esc(user.email)}" data-next-status="${user.status==='active'?'disabled':'active'}">${user.status==='active'?'停用账号':'启用账号'}</button>`:''}</div></section></td></tr>`;
 }
 function paint(){
  if(!root.isConnected)return;
  const active=document.activeElement,focused=root.contains(active)?{command:active.dataset?.customerCommand,id:active.dataset?.id,historyInput:active.matches?.('.customer-history-search input'),historySubmit:active.matches?.('.customer-history-search button'),value:active.value,start:active.selectionStart,end:active.selectionEnd}:null;
  find('.customer-count').textContent=`共 ${page.total} 位客户`;
  find('[data-customer-page]').textContent=`第 ${page.total?Math.floor(page.offset/page.limit)+1:0} / ${Math.ceil(page.total/page.limit)} 页`;
  find('[data-customer-command="prev"]').disabled=busy||page.offset===0;
  find('[data-customer-command="next"]').disabled=busy||page.offset+page.limit>=page.total;
  find('[data-customer-table]').innerHTML=page.items.length?`<div class="customer-table-wrap"><table class="customer-table"><colgroup><col class="customer-col-email"><col class="customer-col-plan"><col class="customer-col-date"><col class="customer-col-date"><col class="customer-col-status"><col class="customer-col-action"></colgroup><thead><tr>${['客户邮箱','当前套餐','最近付款','下次到期','账号状态','操作'].map(v=>'<th scope="col">'+v+'</th>').join('')}</tr></thead><tbody>${page.items.map(user=>`<tr class="customer-row ${expanded===user.id?'is-expanded':''}"><td data-label="客户邮箱"><button type="button" class="customer-toggle" data-customer-command="toggle" data-id="${esc(user.id)}" aria-expanded="${expanded===user.id}" aria-controls="customer-detail-${esc(user.id)}">${icon('chevron-down')}<span>${esc(user.email)}</span></button></td><td data-label="当前套餐" class="customer-plan-summary">${purchaseSummary(user)}</td><td data-label="最近付款">${esc(time(user.purchases?.last_paid_at))}</td><td data-label="下次到期">${esc(nextExpiry(user))}</td><td data-label="账号状态">${badge(user.status==='active'?'green':'gray',stateNames[user.status]||user.status)}</td><td data-label="操作"><button type="button" class="small secondary" data-customer-command="toggle" data-id="${esc(user.id)}" aria-expanded="${expanded===user.id}">${expanded===user.id?'收起':'查看'}</button></td></tr>${expanded===user.id?detailBody(user):''}`).join('')}</tbody></table></div>`:'<p class="hint customer-empty">暂无匹配的客户。</p>';
  if(focused&&!active.isConnected){
   let target=focused.historyInput?find('.customer-history-search input'):focused.historySubmit?find('.customer-history-search button'):null;
   if(!target&&focused.command)target=[...root.querySelectorAll('[data-customer-command]')].find(v=>v.dataset.customerCommand===focused.command&&(!focused.id||v.dataset.id===focused.id)&&!v.disabled)||find('.customer-detail-heading button');
   if(target){target.focus({preventScroll:true});if(focused.historyInput){target.value=focused.value;target.setSelectionRange(focused.start,focused.end)}}
  }
 }
 async function load(offset=0){
  const request=++sequence;detailSequence++;busy=true;expanded='';detail=null;find('[data-customer-error]').textContent='';root.setAttribute('aria-busy','true');paint();
  try{const value=await api('/admin/users/search?'+new URLSearchParams({...filter,limit:String(page.limit),offset:String(offset)}));if(request!==sequence||!root.isConnected)return;if(offset>0&&value.total>0&&offset>=value.total){page=value;return await load(Math.floor((value.total-1)/value.limit)*value.limit)}page=value.total===0?{...value,offset:0}:value}
  catch(error){if(request===sequence&&root.isConnected)find('[data-customer-error]').textContent=error.message||'客户数据读取失败，请重试'}
  finally{if(request===sequence&&root.isConnected){busy=false;root.removeAttribute('aria-busy');paint()}}
 }
 async function loadDetail(offset=0,query=''){
  const request=++detailSequence,userID=expanded,view=tab;detailBusy=true;detailError='';if(detail)find('.customer-detail')?.setAttribute('aria-busy','true');else paint();
  try{const value=await api('/admin/users/'+encodeURIComponent(userID)+'/'+(view==='orders'?'orders':'products')+'?'+new URLSearchParams({environment:filter.environment,limit:'25',offset:String(offset),...(view==='orders'?{q:query}:{})}));if(request!==detailSequence||!root.isConnected||expanded!==userID||tab!==view)return;detail={...value,query}}
  catch(error){if(request===detailSequence&&root.isConnected)detailError=error.message||'购买详情读取失败，请重试'}
  finally{if(request===detailSequence&&root.isConnected){detailBusy=false;paint()}}
 }
 form.addEventListener('submit',event=>{event.preventDefault();filter=Object.fromEntries(new FormData(form));void load(0)});
 root.addEventListener('submit',event=>{if(event.target.matches('.customer-history-search')){event.preventDefault();void loadDetail(0,new FormData(event.target).get('order_q')||'')}});
 root.addEventListener('click',event=>{
  const button=event.target.closest('[data-customer-command]');if(!button)return;const command=button.dataset.customerCommand;
  if(command==='reset'){form.reset();filter={q:'',status:'',environment:'live'};void load(0)}
  if(command==='prev'&&!busy)void load(Math.max(0,page.offset-page.limit));
  if(command==='next'&&!busy)void load(page.offset+page.limit);
  if(command==='toggle'&&!busy){if(expanded===button.dataset.id){expanded='';detailSequence++;paint()}else{expanded=button.dataset.id;tab='products';detail=null;void loadDetail()}}
  if(command==='orders'||command==='products'){tab=command;detail=null;void loadDetail()}
  if(command==='detail-retry')void loadDetail(detail?.offset||0,detail?.query||'');
  if(command==='detail-prev'&&!detailBusy)void loadDetail(Math.max(0,detail.offset-detail.limit),detail.query||'');
  if(command==='detail-next'&&!detailBusy)void loadDetail(detail.offset+detail.limit,detail.query||'');
 });
 paint();if(!initialPage)void load();
 return {reload:()=>load(page.offset),get connected(){return root.isConnected}};
}
