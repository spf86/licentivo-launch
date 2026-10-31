from common import *
from licentivo_launch import Application,MemoryStore
import sys

name=sys.argv[1]
public=json.loads((BASE/(name+'-public.json')).read_text())
cfg=public['config'];product=public['product_config']|{'allow_http':True}
store=MemoryStore()
def create():return Application(cfg['merchant_origin'],cfg['launch_product_id'],product,'sandbox',store=store,cache_dir=BASE/('python-lifecycle-'+name),allow_http=True)
calls=[]
def action():
    calls.append(True)
    (BASE/('python-'+name+'-business.txt')).write_text('Verified Launch SDK business\n')
app=None
try:
    app=create();app.login('alice@launch-joint.example',SECRETS['customer_password'])
    if name=='fixed':
        import uuid
        operation='nfix-python-'+str(uuid.uuid4());meter_calls=[]
        def metered():
            meter_calls.append(True);(BASE/'metered-python.txt').write_text(operation)
        app.run_metered_feature('export',1,operation,metered)
        app.run_metered_feature('export',1,operation,metered)
        app.resolve_metered_feature(operation,True)
        assert len(meter_calls)==1

    app.run_feature('export',action)
    denied=False
    try:app.run_feature('not_entitled',action)
    except Exception:denied=True
    assert denied and len(calls)==1
    app.close();app=create();assert app.restore();app.run_feature('export',action)
    assert app.logout();app.close();app=create();blocked=not app.restore();app.close()
    app=create();code=''
    try:app.login('bob@launch-joint.example',SECRETS['customer_password'])
    except Exception as error:code=getattr(error,'code',type(error).__name__)
    print(json.dumps({'ok':len(calls)==2 and blocked and code=='LAUNCH_ENTITLEMENT_REQUIRED','language':'Python','product':name,'callbacks':len(calls),'startup_denial':code,'logout_rejects_restore':blocked}))
except Exception as error:
    print(json.dumps({'ok':False,'language':'Python','product':name,'code':getattr(error,'code',type(error).__name__)}))
    raise SystemExit(1)
finally:
    if app:app.close()
