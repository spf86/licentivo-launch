// Check local references, operation identities and documented route existence.
// This complements (and does not claim to replace) a full OpenAPI validator.
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const spec=JSON.parse(fs.readFileSync(path.join(root,'docs/openapi.json'),'utf8'));
const source=fs.readdirSync(path.join(root,'internal/httpapi')).filter(n=>n.endsWith('.go')&&!n.endsWith('_test.go')).map(n=>fs.readFileSync(path.join(root,'internal/httpapi',n),'utf8')).join('\n');
function walk(value){
 if(!value||typeof value!=='object')return;
 if(value.$ref){
  if(!value.$ref.startsWith('#/'))throw new Error('Unreviewed external schema reference');
  let target=spec;
  for(const key of value.$ref.slice(2).split('/').map(k=>k.replace(/~1/g,'/').replace(/~0/g,'~'))){target=target?.[key];}
  if(!target)throw new Error('Unresolved schema: '+value.$ref);
 }
 for(const child of Object.values(value))walk(child);
}
walk(spec);
const ids=new Set();let operations=0;
for(const [route,methods] of Object.entries(spec.paths))for(const [method,op] of Object.entries(methods)){
 if(!['get','post','put','patch','delete','head','options'].includes(method))continue;
 if(!op.operationId||ids.has(op.operationId))throw new Error('Missing/duplicate operationId: '+route);
 ids.add(op.operationId);
 const normalized=route.replace(/\{[^}]+\}/g,'{}');
 const registered=[...source.matchAll(/"(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) ([^"\s]+)"/g)].some(([,m,r])=>m===method.toUpperCase()&&r.replace(/\{[^}]+\}/g,'{}')===normalized);
 if(!registered)throw new Error('Documented route absent from server: '+method+' '+route);
 for(const name of [...route.matchAll(/\{([^}]+)\}/g)].map(m=>m[1])){
  if(!op.parameters?.some(p=>p.in==='path'&&p.name===name&&p.required))throw new Error('Missing path parameter: '+route);
 }
 const removedCredential=method==='post'&&route==='/api/v1/software/products/{id}/credential';
 if(removedCredential){
  if(!op.deprecated||!op.responses?.['410']||op.responses?.['200'])throw new Error('Removed credential endpoint must document only HTTP 410: '+route);
 }else if(!op.responses?.['200'])throw new Error('Missing success response: '+route);
 for(const requirement of op.security||[])for(const name of Object.keys(requirement)){
  if(!spec.components?.securitySchemes?.[name])throw new Error('Unknown security scheme: '+name+' at '+route);
 }
 operations++;
}
console.log(`OpenAPI: ${operations} documented operations, all references and routes resolved.`);
