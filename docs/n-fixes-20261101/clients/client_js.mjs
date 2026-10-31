import {readFile,writeFile} from 'node:fs/promises';
import {Application,MemoryStore} from './sdk-source/sdk/javascript/src/index.mjs';
const base='/home/spf86/n-fixes-20261101';
const name=process.argv[2];
const publicConfig=JSON.parse(await readFile(`${base}/${name}-public.json`,'utf8'));
const secret=JSON.parse(await readFile(`${base}/deployment-private.json`,'utf8'));
const store=new MemoryStore();
const create=()=>new Application(publicConfig.config,{...publicConfig.product_config,allow_http:true},{store,cacheDir:`${base}/javascript-${name}`});
let app,calls=0;
const action=async()=>{calls++;await writeFile(`${base}/javascript-${name}-business.txt`,'Verified Launch SDK business\n',{mode:0o600});};
try{
 app=create();await app.login('alice@launch-joint.example',secret.customer_password);
 if(name==='fixed'){const operation='nfix-javascript-'+process.env.NFIX_RUN;let meterCalls=0;const job=async()=>{meterCalls++;await writeFile(`${base}/metered-javascript.txt`,operation);};await app.runMeteredFeature('export',1,operation,job);await app.runMeteredFeature('export',1,operation,job);await app.resolveMeteredFeature(operation,true);if(meterCalls!==1)throw Error('METERING_REPEATED');}

 await app.runFeature('export',action);
 let denied=false;try{await app.runFeature('not_entitled',action);}catch{denied=true;}
 if(!denied||calls!==1)throw Error('FEATURE_GATE_FAILED');
 await app.close();app=create();await app.restore();await app.runFeature('export',action);
 const logout=await app.logout();await app.close();app=create();const restored=await app.restore();await app.close();
 app=create();let original;try{await app.login('bob@launch-joint.example',secret.customer_password);}catch(error){original=error.code;}
 console.log(JSON.stringify({ok:calls===2&&logout===true&&!restored&&original==='LAUNCH_ENTITLEMENT_REQUIRED',language:'JavaScript',product:name,callbacks:calls,startup_denial:original,logout_rejects_restore:!restored}));
}catch(error){console.log(JSON.stringify({ok:false,language:'JavaScript',product:name,code:error.code??error.name}));process.exitCode=1;}
finally{await app?.close();}
