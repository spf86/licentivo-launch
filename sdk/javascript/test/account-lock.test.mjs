import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {randomUUID} from 'node:crypto';
import {accountLock} from '../src/account-lock.mjs';

test('account lock serializes processes and recovers after process termination',async()=>{
  const scope='lock-regression-'+randomUUID(),url=new URL('../src/account-lock.mjs',import.meta.url).href;
  const child=spawn(process.execPath,['--input-type=module','-e',`import {accountLock} from ${JSON.stringify(url)};await accountLock(${JSON.stringify(scope)});process.stdout.write('ready');setInterval(()=>{},1000);`],{stdio:['ignore','pipe','pipe'],windowsHide:true});
  try{
    await once(child.stdout,'data');let acquired=false;const waiting=accountLock(scope).then(unlock=>{acquired=true;return unlock;});
    await new Promise(resolve=>setTimeout(resolve,100));assert.equal(acquired,false);
    const exited=once(child,'exit');child.kill();await exited;
    const unlock=await waiting;assert.equal(acquired,true);await unlock();
  }finally{if(child.exitCode===null&&child.signalCode===null)child.kill();}
});
