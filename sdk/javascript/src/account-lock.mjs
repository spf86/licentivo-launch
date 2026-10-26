import {mkdir, readFile, writeFile, rename, readdir, rm, stat, open} from 'node:fs/promises';
import {join} from 'node:path';
import {homedir} from 'node:os';
import {createHash, randomUUID} from 'node:crypto';
import {setTimeout as delay} from 'node:timers/promises';

function intentPath(scope,token){const hash=value=>createHash('sha256').update(value).digest('hex');const root=join(homedir(),'.cache','LicentivoLaunch','account-intents','javascript');return {root,path:join(root,hash(scope)+'.'+hash(token)+'.signedout')};}
export async function rememberIntent(scope,token){if(!token)return;const {root,path}=intentPath(scope,token);await mkdir(root,{recursive:true,mode:0o700});const file=await open(path,'w',0o600);try{await file.writeFile('1');await file.sync();}finally{await file.close();}}
export async function hasIntent(scope,token){try{await stat(intentPath(scope,token).path);return true;}catch(error){if(error.code==='ENOENT')return false;throw error;}}
export async function clearIntent(scope,token){if(token)await rm(intentPath(scope,token).path,{force:true});}

// File-backed bakery lock. Every participant removes only its own unique entry;
// dead participants can be removed without racing a newly acquired lock. There
// are no expiring leases that could let a paused process write without a lock.
export async function accountLock(scope) {
  const directory=join(homedir(),'.cache','LicentivoLaunch','account-locks','javascript',createHash('sha256').update(scope).digest('hex'));
  await mkdir(directory,{recursive:true,mode:0o700});
  const name=`${process.pid}-${randomUUID()}.json`, path=join(directory,name);
  await writeFile(path,JSON.stringify({choosing:true,ticket:0}),{flag:'wx',mode:0o600});
  const participants=async()=>{
    const result=[];
    for(const entry of await readdir(directory)) {
      const match=/^(\d+)-[a-f0-9-]{36}\.json$/.exec(entry);if(!match||entry===name)continue;
      try { process.kill(Number(match[1]),0); }
      catch(error){if(error.code==='ESRCH'){await rm(join(directory,entry),{force:true});continue;}if(error.code!=='EPERM')throw error;}
      try { let value;try{value=JSON.parse(await readFile(join(directory,entry),'utf8'));}catch(error){if(error instanceof SyntaxError){result.push({name:entry,choosing:true,ticket:0});continue;}throw error;}if(!Number.isSafeInteger(value.ticket)||value.ticket<0||typeof value.choosing!=='boolean')throw Error('ACCOUNT_LOCK_FAILED');result.push({name:entry,...value}); }
      catch(error){if(error.code!=='ENOENT')throw error;}
    }
    return result;
  };
  try {
    const ticket=Math.max(0,...(await participants()).map(value=>value.ticket))+1;
    if(!Number.isSafeInteger(ticket))throw Error('ACCOUNT_LOCK_FAILED');
    const temporary=path+'.tmp';
    try{await writeFile(temporary,JSON.stringify({choosing:false,ticket}),{flag:'wx',mode:0o600});await rename(temporary,path);}finally{await rm(temporary,{force:true});}
    const deadline=Date.now()+5000;
    while((await participants()).some(other=>other.choosing||other.ticket>0&&(other.ticket<ticket||other.ticket===ticket&&other.name<name))){if(Date.now()>=deadline)throw Error('ACCOUNT_LOCK_BUSY');await delay(20);}
    return async()=>{await rm(path,{force:true});};
  } catch(error) { await rm(path,{force:true});throw error; }
}
