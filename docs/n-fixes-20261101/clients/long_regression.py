from common import *
from joint_client import connect, write, callback_count
from datetime import datetime
import csv,hashlib,uuid

operation=str(uuid.uuid4())
path=BASE/'long-result.json'
result={'complete':False,'operation_id':operation,'server_clock_changed':False,'reservation_ttl_overridden':False,'started_at_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'receipts':[]}
def persist():
    temporary=path.with_suffix('.tmp');temporary.write_text(json.dumps(result,indent=2));os.replace(temporary,path)
persist()
app=connect('meter','n09-long')
api=Client(V_ORIGIN,token=state()['api_key'])
def used(license_id):
    value=api.expect('read N09 actual metered usage','GET','/api/v1/licenses/'+license_id+'/capabilities')
    return sum(item['quantity'] for item in value['usage'] if item['feature']=='export')
try:
    app.login('alice@launch-joint.example',SECRETS['customer_password'])
    license_id=app.runtime._check()['license_id'];result['used_before']=used(license_id)
    original=app.runtime.reserve
    def observe(*args,**kwargs):
        receipt=original(*args,**kwargs)
        result['receipts'].append({key:receipt[key] for key in ['reservation_id','expires_at','status']})
        persist();return receipt
    app.runtime.reserve=observe
    started=time.monotonic()
    def business():
        first=datetime.fromisoformat(result['receipts'][0]['expires_at'].replace('Z','+00:00')).timestamp()
        result['initial_remaining_seconds']=round(first-time.time(),2)
        target=first+5
        artifact=BASE/'long-business.csv'
        with artifact.open('w',newline='') as stream:
            writer=csv.writer(stream);writer.writerow(['sequence','checksum'])
            sequence=0
            while time.time()<target:
                writer.writerow([sequence,hashlib.sha256(str(sequence).encode()).hexdigest()]);stream.flush()
                sequence+=1;result['rows']=sequence;result['elapsed_seconds']=round(time.monotonic()-started,2);persist()
                time.sleep(min(1,max(.01,target-time.time())))
            os.fsync(stream.fileno())
        result['artifact_sha256']=hashlib.sha256(artifact.read_bytes()).hexdigest()
        return write(operation,'n09-long','export')
    app.run_metered_feature('export',1,operation,business)
    result['used_after']=used(license_id)
    app.close();app=connect('meter','n09-long');assert app.restore()
    app.run_metered_feature('export',1,operation,lambda:write(operation,'n09-replayed','export'))
    app.resolve_metered_feature(operation,True)
    journal=next(json.loads(p.read_text()) for p in Path(app.runtime.config['cache_path']).parent.glob('*.task-*.json') if json.loads(p.read_text()).get('operation_id')==operation)
    result.update(complete=True,callback_count=callback_count(operation),journal_state=journal['state'],finished_at_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()))
    ok=result['used_after']==result['used_before']+1 and result['callback_count']==1 and journal['state']=='committed' and result['elapsed_seconds']>900 and len(result['receipts'])>1 and len({r['reservation_id'] for r in result['receipts']})==1
    result['passed']=ok;persist()
    record('N09 real default-TTL long business renews, commits once and survives fresh identity',ok,elapsed_seconds=result['elapsed_seconds'],csv_rows=result['rows'],callback_count=result['callback_count'],reserve_receipts=len(result['receipts']),usage_delta=result['used_after']-result['used_before'],journal_state=journal['state'])
    if not ok:raise SystemExit(1)
except Exception as error:
    result['error_code']=getattr(error,'code',type(error).__name__);persist();raise
finally:
    try:app.logout()
    except Exception:pass
    app.close()
