const escape=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

// The server supplies the total and a bounded page, never a truncated history.
export function mountRecordList(root,{api,path,initialPage,head,row,table,t,placeholder,filters='',emptyMessage,onTotal=()=>{}}){
 let page=initialPage||{items:[],total:0,limit:25,offset:0},query={},sequence=0,busy=false;
 root.classList.add('record-list');
 root.innerHTML=`<form class="record-filters"><label class="record-search">${escape(t('list.search','搜索'))}<input type="search" name="q" maxlength="254" placeholder="${escape(placeholder)}"></label>${filters}<button>${escape(t('list.search','搜索'))}</button><button type="button" class="secondary" data-record-command="reset">${escape(t('list.reset','重置'))}</button></form><div class="record-summary" role="status" aria-live="polite"></div><p class="form-error" role="alert"></p><div class="record-rows"></div><nav class="record-pager"><button type="button" class="secondary" data-record-command="previous">${escape(t('list.previous','上一页'))}</button><span></span><button type="button" class="secondary" data-record-command="next">${escape(t('list.next','下一页'))}</button></nav>`;
 const form=root.querySelector('form'),rows=root.querySelector('.record-rows'),summary=root.querySelector('.record-summary'),error=root.querySelector('.form-error'),pager=root.querySelector('.record-pager');
 const label=(key,fallback,values)=>Object.entries(values).reduce((s,[key,value])=>s.replaceAll('{'+key+'}',String(value)),t(key,fallback));
 function paint(){
  rows.innerHTML=page.items.length?table(head,page.items.map(row)):`<p class="hint">${escape(emptyMessage)}</p>`;
  summary.textContent=label('list.total','共 {count} 项',{count:page.total});
  pager.querySelector('span').textContent=label('list.page','第 {current} / {total} 页',{current:page.total?Math.floor(page.offset/page.limit)+1:0,total:Math.ceil(page.total/page.limit)});
  root.querySelector('[data-record-command="previous"]').disabled=busy||page.offset===0;
  root.querySelector('[data-record-command="next"]').disabled=busy||page.offset+page.limit>=page.total;
  onTotal(page.total);
 }
 async function load(offset=0){
  const request=++sequence;busy=true;error.textContent='';root.setAttribute('aria-busy','true');paint();
  const params=new URLSearchParams({...query,limit:String(page.limit),offset:String(offset)});
  try{
   const result=await api(path+'?'+params);
   if(request!==sequence||!root.isConnected)return;
   // A deletion can make the last page disappear. Fetch its new last page once.
   if(offset>0&&result.total>0&&offset>=result.total){page={...result,offset:Math.floor((result.total-1)/result.limit)*result.limit};return await load(page.offset)}
   page=result.total===0?{...result,offset:0}:result;
  }catch(e){if(request===sequence&&root.isConnected)error.textContent=e.message||String(e)}
  finally{if(request===sequence&&root.isConnected){busy=false;root.removeAttribute('aria-busy');paint()}}
 }
 form.addEventListener('submit',event=>{event.preventDefault();query=Object.fromEntries([...new FormData(form)].filter(([,value])=>String(value).trim()!==''));void load(0)});
 root.addEventListener('click',event=>{
  const command=event.target.closest('[data-record-command]')?.dataset.recordCommand;
  if(!command)return;
  if(command==='reset'){form.reset();query={};void load(0)}
  if(command==='previous'&&!busy)void load(Math.max(0,page.offset-page.limit));
  if(command==='next'&&!busy)void load(page.offset+page.limit);
 });
 paint();if(!initialPage)void load();
 return {reload:()=>load(page.offset),get connected(){return root.isConnected}};
}
