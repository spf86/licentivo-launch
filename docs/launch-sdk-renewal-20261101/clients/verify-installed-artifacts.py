from pathlib import Path
import os,json,subprocess,tempfile,hashlib,time
root=Path(__file__).resolve().parent
matrix=json.loads((root/'matrix-result.json').read_text())
assert matrix.get('passed') is True, 'Full long-task matrix must pass before artifact replays'
fixtures=json.loads((root/'fixtures.json').read_text());secret=json.loads((root/'deployment-private.json').read_text())
python='/home/spf86/launch-acceptance-20261031/venv/bin/python'
result={'scope':'installed Linux artifact replays','checks':[],'started_at_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())}
def record(name,passed,**detail):
 result['checks'].append({'name':name,'passed':bool(passed),**detail})
 (root/'installed-artifact-results.json').write_text(json.dumps(result,indent=2))
 assert passed,name

def usage():
 fd,name=tempfile.mkstemp(dir=root,suffix='.cnf')
 try:
  with os.fdopen(fd,'w') as stream:stream.write('[client]\nhost=127.0.0.1\nprotocol=tcp\nuser=lv_joint1031\npassword='+secret['licentivo_database_password']+'\ndatabase=licentivo_joint_20261031\n')
  ids=','.join("'"+identifier+"'" for identifier in matrix['license_ids'])
  return int(subprocess.check_output(['mysql','--defaults-extra-file='+name,'--batch','--skip-column-names'],input="SELECT COALESCE(SUM(quantity),0) FROM feature_usages WHERE feature='export' AND license_id IN ("+ids+");",text=True).strip())
 finally:Path(name).unlink(missing_ok=True)
before=usage();result['used_before']=before
wheel=root/'licentivo_launch-0.1.7-py3-none-any.whl'
record('Final distribution wheel present',wheel.is_file(),sha256=hashlib.sha256(wheel.read_bytes()).hexdigest())
with (root/'final-wheel-install-private.log').open('w') as stream:
 process=subprocess.run([python,'-m','pip','install','--no-deps','--force-reinstall',str(wheel)],stdout=stream,stderr=subprocess.STDOUT)
record('Final distribution wheel installed',process.returncode==0,exit_code=process.returncode)
for lang,command in [('c',[str(root/'sdk-build/installed-consumer/installed-c')]),('cpp',[str(root/'sdk-build/installed-consumer/installed-cpp')]),('python',[python,str(root/'long_python.py')])]:
 env=os.environ.copy();env.update(LAUNCH_EMAIL=fixtures['customers'][lang]['email'],LAUNCH_LANGUAGE=lang,LAUNCH_SCOPE='long',LAUNCH_PHASE='replay',LAUNCH_SECONDS='920',LAUNCH_OPERATION=matrix['operations']['long'][lang],XDG_CACHE_HOME=str(root/'client-caches'/lang))
 process=subprocess.run(command,cwd=root,env=env,capture_output=True,text=True,timeout=40)
 (root/'logs'/('installed-replay-'+lang+'-private.log')).write_text(process.stdout+process.stderr)
 try:payload=json.loads(process.stdout.splitlines()[-1])
 except Exception:payload={}
 record('Installed '+lang+' artifact committed replay skips business',process.returncode==0 and payload.get('ok') is True and payload.get('callbacks')==0,exit_code=process.returncode,result=payload)
after=usage();record('Installed artifact replays add zero usage',after==before,used_before=before,used_after=after)
result.update(passed=True,finished_at_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()))
(root/'installed-artifact-results.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
