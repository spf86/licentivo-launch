import {Application, EncryptedFileStore, LaunchError} from '../../sdk/javascript/src/index.mjs';
import {createInterface} from 'node:readline/promises';
import {homedir} from 'node:os';
import {join} from 'node:path';

const args = process.argv.slice(2);
if (args.includes('--help') || args.length < 2) {
  console.log('node examples/javascript/main.mjs launch-config.json product.json [--store-key-env NAME] [--logout|--release-device] [--feature NAME]');
  process.exit(args.includes('--help') ? 0 : 2);
}
const [launchPath, productPath, ...flags] = args;
let keyName, mode, feature;
for(let i=0;i<flags.length;i++) {
  const flag=flags[i];
  if(flag==='--store-key-env' && flags[i+1]) keyName=flags[++i];
  else if(flag==='--feature' && flags[i+1]) feature=flags[++i];
  else if(['--logout','--release-device'].includes(flag) && !mode) mode=flag;
  else {console.error('Unknown or conflicting option. Use --help.');process.exit(2);}
}
const controller = new AbortController();
process.on('SIGINT',()=>controller.abort());
function passwordPrompt() {
  if(!process.stdin.isTTY || !process.stdin.setRawMode) throw new LaunchError('TTY_REQUIRED');
  process.stdout.write('Password: ');
  return new Promise((resolve,reject)=>{
    let value=''; process.stdin.setRawMode(true);process.stdin.resume();
    const done=(error)=>{process.stdin.off('data',read);process.stdin.setRawMode(false);process.stdin.pause();process.stdout.write('\n');error?reject(error):resolve(value);};
    const read=data=>{for(const char of data.toString('utf8')){if(char==='\u0003'){controller.abort();done(Error('CANCELLED'));return;} if(char==='\r'||char==='\n'){done();return;} if(char==='\b'||char==='\u007f')value=value.slice(0,-1);else if(char>=' ')value+=char;}};
    process.stdin.on('data',read);
  });
}
let app;
try {
  let store;
  if(keyName) {
    const encoded=process.env[keyName]??'', key=Buffer.from(encoded,'base64');
    if(key.length!==32 || key.toString('base64')!==encoded) throw new LaunchError('STORE_KEY_REQUIRED');
    store=new EncryptedFileStore(join(homedir(),'.local','share','LicentivoLaunch','sessions'),key);key.fill(0);
  } else console.log('Memory session only. Restart requires login; no plaintext session file is written.');
  // Prepared repository examples run without npm install. Distributed consumers
  // install @licentivo/launch + its exact @licentivo/sdk peer dependency instead.
  const {Client}=await import('../vendor/sdk/javascript/licentivo.mjs');
  app=await Application.fromFiles(launchPath,productPath,{store,runtimeFactory:config=>new Client(config)});
  if(mode==='--logout') {console.log(await app.logout({signal:controller.signal})?'Signed out.':'Local session removed; remote logout was not confirmed.');}
  else {
    let state=await app.restore({signal:controller.signal});
    if(!state) {
      if(!process.stdin.isTTY) throw new LaunchError('TTY_REQUIRED');
      const input=createInterface({input:process.stdin,output:process.stdout});
      let email;try{email=await input.question('Email: ',{signal:controller.signal});}finally{input.close();}
      let password=await passwordPrompt();
      try{state=await app.login(email,password,{signal:controller.signal});}finally{password='';}
    }
    if(mode==='--release-device'){await app.releaseDevice({signal:controller.signal});console.log('Device released and local session removed.');}
    else {console.log('Signed in; official SDK runtime started.');if(feature)await app.runFeature(feature,()=>console.log('Protected business callback executed.'));}
  }
} catch(error) {console.error(error.code??'OPERATION_FAILED');process.exitCode=1;}
finally {await app?.close();}
