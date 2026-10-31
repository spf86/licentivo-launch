from pathlib import Path
import json,os,subprocess,time,tempfile,uuid,signal,hashlib,math
root=Path(__file__).resolve().parent
secret=json.loads((root/'deployment-private.json').read_text());fixtures=json.loads((root/'fixtures.json').read_text())
run_id='launch-renewal-'+uuid.uuid4().hex[:16]
langs=['python','go','javascript','java','csharp','ruby','c','cpp','rust']
python='/home/spf86/launch-acceptance-20261031/venv/bin/python'
old=Path('/home/spf86/joint-acceptance-20261031')
commands={'python':[python,str(root/'long_python.py')],'go':[str(root/'long-go')],'javascript':['node',str(root/'long_javascript.mjs')],'java':['java','-cp',str(root/'sdk-build/java'),'LongJava'],'csharp':[str(old/'dotnet/dotnet'),str(root/'csharp-client/bin/Release/net8.0/LongSharp.dll')],'ruby':['ruby',str(root/'long_ruby.rb')],'c':[str(root/'sdk-build/native/long-c')],'cpp':[str(root/'sdk-build/native/long-cpp')],'rust':[str(root/'sdk-build/rust/debug/launch-long-client')]}
summary={'run_id':run_id,'started_at_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'status':'starting','quantity_per_job':3,'long_duration_seconds':920,'commands_use_launch_application':True,'server_clock_changed':False,'reservation_ttl_overridden':False,'checks':[],'operations':{scope:{lang:run_id+'-'+scope+'-'+lang for lang in langs} for scope in ['short','long']}}
started_procs=[]
def save():
    temporary=root/'matrix-result.tmp';temporary.write_text(json.dumps(summary,indent=2));os.replace(temporary,root/'matrix-result.json')
def record(name,passed,**detail):
    row={'name':name,'passed':bool(passed),**detail};summary['checks'].append(row);save();print(json.dumps(row),flush=True)
    if not passed:raise RuntimeError(name)
def sql(query):
    fd,name=tempfile.mkstemp(dir=root,suffix='.cnf')
    try:
        with os.fdopen(fd,'w') as stream:stream.write('[client]\nhost=127.0.0.1\nprotocol=tcp\nuser=lv_joint1031\npassword='+secret['licentivo_database_password']+'\ndatabase=licentivo_joint_20261031\n')
        data=subprocess.check_output(['mysql','--defaults-extra-file='+name,'--batch','--raw','--skip-column-names'],input=query,text=True)
        return [line.split('\t') for line in data.splitlines()]
    finally:Path(name).unlink(missing_ok=True)
def holds(scope):
    ids=','.join("'"+summary['operations'][scope][lang]+"'" for lang in langs)
    rows=sql("SELECT operation_id,license_id,quantity,status,TIMESTAMPDIFF(MICROSECOND,created_at,expires_at)/1000000,(expires_at>UTC_TIMESTAMP(6)),TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),expires_at)/1000000 FROM feature_reservations WHERE operation_id IN ("+ids+") ORDER BY operation_id;")
    return [{'operation_id':r[0],'license_id':r[1],'quantity':int(r[2]),'status':r[3],'expiry_extension_from_created_seconds':float(r[4]),'unexpired':r[5]=='1','remaining_seconds':float(r[6])} for r in rows]
def usage(license_id):
    return int(sql("SELECT COALESCE(SUM(quantity),0) FROM feature_usages WHERE license_id='"+license_id+"' AND feature='export';")[0][0])
def launch_group(scope,phase):
    group={}
    for lang in langs:
        env=os.environ.copy();(root/'client-caches'/lang).mkdir(parents=True,exist_ok=True)
        env.update(LAUNCH_EMAIL=fixtures['customers'][lang]['email'],LAUNCH_LANGUAGE=lang,LAUNCH_SCOPE=scope,LAUNCH_PHASE=phase,LAUNCH_SECONDS='920' if scope=='long' else '2',LAUNCH_OPERATION=summary['operations'][scope][lang],XDG_CACHE_HOME=str(root/'client-caches'/lang),DOTNET_CLI_HOME=str(old/'dotnet-home'),DOTNET_CLI_TELEMETRY_OPTOUT='1')
        (root/'logs').mkdir(exist_ok=True);stream=(root/'logs'/f'{scope}-{phase}-{lang}-private.log').open('w')
        process=subprocess.Popen(commands[lang],cwd=root,env=env,stdout=stream,stderr=subprocess.STDOUT,start_new_session=True);started_procs.append(process);group[lang]=(process,stream,time.monotonic())
        # Account lifecycle transitions are serialized by the SDK across processes.
        # Once the callback is entered, metered business may run concurrently.
        deadline=time.monotonic()+40
        if phase=='run':
            ready=root/'cases'/scope/lang/'ready.json'
            while not ready.exists():
                if process.poll() is not None:raise RuntimeError(lang+' ended before its callback')
                if time.monotonic()>deadline:raise TimeoutError(lang+' login/callback startup')
                time.sleep(.1)
        else:
            while process.poll() is None:
                if time.monotonic()>deadline:raise TimeoutError(lang+' replay startup')
                time.sleep(.1)

    return group
def wait_group(group,scope,phase,timeout):
    end=time.monotonic()+timeout
    while any(p.poll() is None for p,_,_ in group.values()):
        for lang,(p,_,_) in group.items():
            if p.poll() is not None and p.returncode!=0:
                record(scope+' '+phase+' '+lang+' process succeeded',False,exit_code=p.returncode)
        if time.monotonic()>end:raise TimeoutError(scope+' '+phase+' timed out')
        time.sleep(.5)
    for lang,(p,stream,began) in group.items():
        stream.close();log=(root/'logs'/f'{scope}-{phase}-{lang}-private.log').read_text(errors='replace').splitlines()
        payload=json.loads(log[-1]) if log else {}
        record(scope+' '+phase+' '+lang+' Application process',p.returncode==0 and payload.get('ok') is True and payload.get('callbacks')==(1 if phase=='run' else 0),exit_code=p.returncode,process_seconds=round(time.monotonic()-began,3),result=payload)
def verify_outputs(scope):
    for lang in langs:
        folder=root/'cases'/scope/lang
        business=json.loads((folder/'business-result.json').read_text());callbacks=[json.loads(line) for line in (folder/'callbacks.jsonl').read_text().splitlines()]
        ok=len(callbacks)==1 and callbacks[0]['phase']=='run' and business['completed_units']==3 and len(business['artifacts'])==3 and not (folder/'incorrect-replay.txt').exists()
        for artifact in business['artifacts']:ok=ok and hashlib.sha256((folder/artifact['file']).read_bytes()).hexdigest()==artifact['sha256']
        if scope=='long':ok=ok and business['business_seconds']>=920
        record(scope+' '+lang+' durable business output and replay',ok,callback_count=len(callbacks),completed_units=business['completed_units'],business_seconds=business['business_seconds'],progress_rows=business['progress_rows'])
def assert_settled(scope):
    rows=holds(scope);summary[scope+'_final_holds']=rows
    record(scope+' nine original reservations settled exactly',len(rows)==9 and len({r['operation_id'] for r in rows})==9 and all(r['status']=='committed' and r['quantity']==3 for r in rows),count=len(rows),committed_units=sum(r['quantity'] for r in rows if r['status']=='committed'))
save()
try:
    record('Launch and Licentivo database configuration is accessible',sql('SELECT 1;')==[['1']])
    summary['status']='short tasks';save();group=launch_group('short','run');wait_group(group,'short','run',100);assert_settled('short')
    group=launch_group('short','replay');wait_group(group,'short','replay',100);verify_outputs('short')
    license_ids={r['license_id'] for r in summary['short_final_holds']};record('Nine languages use nine independent real Launch entitlements',len(license_ids)==9)
    summary['license_ids']=sorted(license_ids);summary['used_before_by_license']={identifier:usage(identifier) for identifier in license_ids};summary['used_before_long']=sum(summary['used_before_by_license'].values())
    def all_usage():return sum(usage(identifier) for identifier in license_ids)
    summary['status']='long tasks starting';save();group=launch_group('long','run');limit=time.monotonic()+120
    while not all((root/'cases/long'/lang/'ready.json').exists() for lang in langs):
        for lang,(p,_,_) in group.items():
            if p.poll() is not None:record('long '+lang+' reached real business callback',False,exit_code=p.returncode)
        if time.monotonic()>limit:raise TimeoutError('Long callback start boundary')
        time.sleep(.25)
    rows=holds('long');summary['long_initial_holds']=rows
    record('Initial long reservations retain default 900-second TTL',len(rows)==9 and all(r['status']=='pending' and r['quantity']==3 and 899<=r['expiry_extension_from_created_seconds']<=901 for r in rows),count=len(rows))
    go_at=math.ceil(time.time()+2);(root/'start-at.txt').write_text(str(go_at));summary['business_start_epoch']=go_at;summary['status']='long tasks running';save()
    next_sample=go_at+60
    while time.time()<go_at+905:
        for lang,(p,_,_) in group.items():
            if p.poll() is not None:record('long '+lang+' remains running before observation boundary',False,exit_code=p.returncode)
        if time.time()>=next_sample:
            rows=holds('long');summary['latest_progress']={'elapsed_seconds':round(time.time()-go_at,2),'pending_count':sum(r['status']=='pending' for r in rows),'pending_units':sum(r['quantity'] for r in rows if r['status']=='pending' and r['unexpired']),'minimum_remaining_seconds':min(r['remaining_seconds'] for r in rows),'used_delta':all_usage()-summary['used_before_long']};save()
            print(json.dumps({'progress':summary['latest_progress']}),flush=True);next_sample+=60
        time.sleep(1)
    rows=holds('long');summary['after_original_expiry_holds']=rows
    record('After original TTL all nine preoccupations still hold their 27 units',len(rows)==9 and all(r['status']=='pending' and r['unexpired'] and r['quantity']==3 and r['expiry_extension_from_created_seconds']>1500 for r in rows) and all_usage()==summary['used_before_long'],held_units=sum(r['quantity'] for r in rows if r['status']=='pending' and r['unexpired']),used_delta=all_usage()-summary['used_before_long'])
    wait_group(group,'long','run',90);assert_settled('long');summary['used_after_long']=all_usage()
    summary['used_after_by_license']={identifier:usage(identifier) for identifier in license_ids}
    record('Long tasks debit neither more nor less than 27 completed units',summary['used_after_long']-summary['used_before_long']==27 and all(summary['used_after_by_license'][identifier]-summary['used_before_by_license'][identifier]==3 for identifier in license_ids),usage_delta=summary['used_after_long']-summary['used_before_long'])
    group=launch_group('long','replay');wait_group(group,'long','replay',100);verify_outputs('long');assert_settled('long')
    record('Fresh-process replays do not increase metered usage',all_usage()==summary['used_after_long'])
    summary.update(status='complete',passed=True,finished_at_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()));save()
except Exception as error:
    summary.update(status='failed',passed=False,error_type=type(error).__name__,error_message=str(error));save();raise
finally:
    for process in started_procs:
        if process.poll() is None:
            os.killpg(process.pid,signal.SIGTERM)
            try:process.wait(timeout=8)
            except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
