"""Real small client: account login, signed SDK runtime, protected CSV writes."""
import csv
import fcntl
import hashlib
import json
import os
from pathlib import Path
import sys
import uuid

BASE=Path('/home/spf86/n-fixes-20261101')
os.environ['XDG_CACHE_HOME']=str(BASE/'sdk-system-cache')
from exporter import LinuxEncryptedStore
from licentivo_launch import Application

def connect(name='fixed', state='alice-fixed', **extra):
    public=json.loads((BASE/(name+'-public.json')).read_text())
    cfg=public['config']
    product=public['product_config'] | {'allow_http':True} | extra
    location=BASE/state
    return Application(cfg['merchant_origin'],cfg['launch_product_id'],product,environment='sandbox',allow_http=True,store=LinuxEncryptedStore(location/'account'),cache_dir=location/'runtime')

def write(operation, product, feature):
    file=BASE/'protected-business.csv'
    with file.open('a+',newline='') as stream:
        fcntl.flock(stream,fcntl.LOCK_EX)
        writer=csv.writer(stream)
        writer.writerow([operation,product,feature,'Protected report',2,125,250])
        stream.flush()
        os.fsync(stream.fileno())
        fcntl.flock(stream,fcntl.LOCK_UN)
    return {'operation_id':operation,'sha256':hashlib.sha256(b'Protected report,2,125,250').hexdigest()}

def callback_count(operation):
    file=BASE/'protected-business.csv'
    if not file.exists():return 0
    with file.open(newline='') as stream:
        return sum(row[0]==operation for row in csv.reader(stream))

def main():
    request=json.load(sys.stdin)
    secrets=json.loads((BASE/'deployment-private.json').read_text())
    name=request.get('product','fixed')
    action=request.get('action','export')
    state=request.get('state','alice-'+name)
    operation=request.get('operation_id',str(uuid.uuid4()))
    feature=request.get('feature','export')
    reauthenticated=False
    extra=request.get('runtime_options',{})
    if set(extra)-{'proxy_url','app_version'}:
        raise ValueError('Unsupported test runtime option')
    with connect(name,state,**extra) as app:
        if action=='logout':
            return {'action':action,'confirmed':app.logout()}
        if action=='release':
            return {'action':action,'confirmed':app.release_device()}
        if action=='login':
            app.login(request.get('email','alice@launch-joint.example'),secrets['customer_password'])
        else:
            try:
                restored=app.restore()
            except Exception as error:
                if request.get('email')!='soak@launch-joint.example' or getattr(error,'code','')!='LAUNCH_LOGIN_REQUIRED':
                    raise
                # The test sandbox intentionally expires authorization sessions
                # after 1800 seconds. Re-enter the dedicated test account's
                # credentials when the SDK explicitly requests a fresh login.
                app.login('soak@launch-joint.example',secrets['customer_password'])
                restored=True
                reauthenticated=True
                with (BASE/'soak-reauthentication.jsonl').open('a') as log:
                    import time
                    log.write(json.dumps({'time':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'product':name,'reason':'LAUNCH_LOGIN_REQUIRED','fresh_login_succeeded':True})+'\n')
            if not restored:
                return {'action':action,'restored':False,'code':'LOGIN_REQUIRED'}
        app.runtime.refresh_online()
        if action in ('login','status','restore'):
            status=app.runtime.status()
            return {'action':action,'state':status.get('state'),'allowed':status.get('allowed'),'code':status.get('code'),'license_model':getattr(app.runtime,'_lease',{}).get('capabilities',{}).get('license_model') if isinstance(getattr(app.runtime,'_lease',{}),dict) else None}
        before=callback_count(operation)
        callback=lambda:write(operation,name,feature)
        if request.get('metered'):
            result=app.runtime.run_metered_feature(feature,request.get('units',1),operation,callback)
        else:
            result=app.run_feature(feature,callback)
        return {'action':action,'operation_id':operation,'callback_count':callback_count(operation),'new_callbacks':callback_count(operation)-before,'result_present':result is not None,'reauthenticated':reauthenticated}

if __name__=='__main__':
    try:
        print(json.dumps({'ok':True,'result':main()}))
    except Exception as error:
        print(json.dumps({'ok':False,'code':getattr(error,'code',type(error).__name__),'request_id':getattr(error,'request_id','')}))
        raise SystemExit(1)
