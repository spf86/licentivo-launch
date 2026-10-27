import {mountStorePolicies} from '../internal/httpapi/web/store-policies.js';
import {formFingerprint} from '../internal/httpapi/web/form-state.js';

const results=document.querySelector('#results'),fixtures=document.querySelector('#fixtures');
const failures=[];
const tick=()=>new Promise(resolve=>setTimeout(resolve,30));
const assert=(condition,message)=>{if(!condition)throw new Error(message)};
const deferred=()=>{let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no});return {promise,resolve,reject}};
const unhandled=[];
window.addEventListener('unhandledrejection',event=>{unhandled.push(String(event.reason));event.preventDefault()});

async function fixture(options={}){
 const host=document.createElement('section');fixtures.append(host);
 const policies=['terms','refunds','privacy'].map(key=>({key,version:1,draft:{title:`${key} title`,body:'Original body'},published:null,has_draft_changes:true}));
 const requests=[],saved=[];let baseline='';
 await mountStorePolicies(host,{
  api:async(path,method='GET',payload)=>{
   if(method==='GET')return structuredClone(policies);
   const response=deferred();requests.push({path,payload:structuredClone(payload),response});return response.promise;
  },
  onSaved:(form,content)=>{saved.push({form,content:structuredClone(content)});baseline=formFingerprint(form,content)},
  ...options,
 });
 const form=()=>host.querySelector('form[data-form="store-policy"]');
 baseline=formFingerprint(form());
 const edit=(title,body)=>{
  form().elements.title.value=title;
  const editor=host.querySelector('[data-policy-editor]');editor.textContent=body;
  editor.dispatchEvent(new InputEvent('input',{bubbles:true}));
 };
 const complete=(request,publish=false)=>request.response.resolve({
  ...policies[0],version:request.payload.version+1,
  draft:{title:request.payload.title.trim(),body:request.payload.body.trim()},
  ...(publish?{published:{title:request.payload.title.trim(),body:request.payload.body.trim()},published_at:'2026-10-27T00:00:00Z',has_draft_changes:false}:{}),
 });
 return {host,form,edit,requests,saved,complete,dirty:()=>formFingerprint(form())!==baseline};
}

const tests=[
 ['typing during a save survives its response and the next save uses the new version',async()=>{
  const f=await fixture();f.edit('Submitted title','Submitted body');f.form().requestSubmit();
  f.edit('Newer title','Newer body');f.complete(f.requests[0]);await tick();
  assert(f.form().elements.title.value==='Newer title','A save response discarded the newer title');
  assert(f.host.querySelector('[data-policy-editor]').textContent==='Newer body','A save response discarded the newer body');
  assert(f.form().elements.version.value==='2','The next save retained a stale version');
  assert(f.saved[0]?.content.title==='Submitted title'&&f.saved[0]?.content.body==='Submitted body','The saved baseline included unsaved typing');
  assert(f.dirty(),'Newer edits were marked as saved');
  f.edit('Submitted title','Submitted body');assert(!f.dirty(),'Reverting to the submitted content stayed dirty');
  f.edit('terms title','Original body');assert(f.dirty(),'Reverting to the old content silently discarded the acknowledged save');
  f.edit('Newer title','Newer body');
  f.form().requestSubmit();assert(f.requests[1]?.payload.version===2&&f.requests[1]?.payload.body==='Newer body','The next request did not save the retained edits');
  f.complete(f.requests[1]);await tick();f.host.remove();
 }],
 ['a completed save does not rewrite a panel that was removed during navigation',async()=>{
  const f=await fixture();f.edit('Submitted title','Submitted body');f.form().requestSubmit();
  const html=f.host.innerHTML;f.host.remove();const before=unhandled.length;
  f.complete(f.requests[0]);await tick();
  assert(f.host.innerHTML===html,'A removed editor was rewritten by a stale response');
  assert(unhandled.length===before,'A stale successful save caused an unhandled rejection');
 }],
 ['a rejected save does not show errors in a panel removed during navigation',async()=>{
  const f=await fixture();f.form().requestSubmit();const error=f.host.querySelector('[data-policy-error]');
  f.host.remove();const before=unhandled.length;f.requests[0].response.reject(new Error('Save failed'));await tick();
  assert(error.textContent==='','A stale failure modified the removed editor');
  assert(unhandled.length===before,'A stale failed save caused an unhandled rejection');
 }],
 ['a publication remains successful when refreshing the storefront fails',async()=>{
  const f=await fixture({onPublished:async()=>{throw new Error('Refresh failed')}});
  f.host.querySelector('[data-policy-publish]').click();f.complete(f.requests[0],true);await tick();
  assert(f.host.querySelector('[data-policy-error]').textContent==='','A refresh failure was reported as a publication failure');
  const status=f.host.querySelector('[data-policy-status]').textContent;
  assert(status.includes('发布')&&status.includes('刷新'),'The status did not distinguish publication from refresh');
  assert(!f.host.querySelector('[data-policy-publish]').disabled,'Publication controls stayed disabled');f.host.remove();
 }],
 ['typing during publication stays unpublished and keeps the published metadata',async()=>{
  const f=await fixture();f.edit('Published title','Published body');f.host.querySelector('[data-policy-publish]').click();
  f.edit('Unpublished title','Unpublished body');f.complete(f.requests[0],true);await tick();
  assert(f.form().elements.title.value==='Unpublished title','Publishing discarded the newer draft title');
  assert(f.host.querySelector('[data-policy-editor]').textContent==='Unpublished body','Publishing discarded the newer draft body');
  assert(f.host.querySelector('[data-policy-status]').textContent.includes('未发布'),'Newer edits were incorrectly presented as published');
  assert(f.host.querySelector('[data-policy-select="terms"] small').textContent==='已发布','The selector lost the successful publication state');f.host.remove();
 }],
 ['a failed save preserves edits and permits retrying the same version',async()=>{
  const f=await fixture();f.edit('Submitted title','Submitted body');f.form().requestSubmit();
  f.edit('Retry title','Retry body');f.requests[0].response.reject(new Error('Temporary failure'));await tick();
  assert(f.form().elements.title.value==='Retry title'&&f.form().elements.version.value==='1','A failed save lost edits or advanced the version');
  assert(f.host.querySelector('[data-policy-error]').textContent==='Temporary failure','A current save failure was hidden');
  f.form().requestSubmit();assert(f.requests[1]?.payload.body==='Retry body'&&f.requests[1]?.payload.version===1,'The retry did not use current edits');
  f.complete(f.requests[1]);await tick();f.host.remove();
 }],
];

for(const [name,run] of tests){
 const row=document.createElement('li');results.append(row);
 try{await run();row.textContent=`PASS: ${name}`}
 catch(error){row.textContent=`FAIL: ${name}: ${error.message}`;failures.push(name)}
 fixtures.replaceChildren();
}
document.querySelector('#summary').textContent=`${tests.length-failures.length}/${tests.length} passed`;
document.documentElement.dataset.status=failures.length?'failed':'passed';
