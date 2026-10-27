// Overrides describe the values acknowledged by a save response. The current
// form can already contain newer edits and must keep a different fingerprint.
export function formFingerprint(form,overrides={}){
 return JSON.stringify([...new FormData(form)].map(([name,current])=>{
  const value=Object.hasOwn(overrides,name)?String(overrides[name]):current;
  return [name,value instanceof File?(value.name?{name:value.name,size:value.size,type:value.type,lastModified:value.lastModified}:null):value];
 }));
}
