"""One real callback produces three report files after sustained work."""
from pathlib import Path
import csv,json,os,time,hashlib
root=Path(__file__).resolve().parent
language=os.environ['LAUNCH_LANGUAGE'];scope=os.environ['LAUNCH_SCOPE'];phase=os.environ['LAUNCH_PHASE'];operation=os.environ['LAUNCH_OPERATION']
assert language in ['python','go','javascript','java','csharp','ruby','c','cpp','rust'] and scope in ['short','long']
out=root/'cases'/scope/language;out.mkdir(parents=True,exist_ok=True)
with (out/'callbacks.jsonl').open('a') as stream:
    stream.write(json.dumps({'phase':phase,'operation_id':operation,'time':time.time()})+'\n');stream.flush();os.fsync(stream.fileno())
if phase!='run':
    (out/'incorrect-replay.txt').write_text('Business callback was repeated')
    raise RuntimeError('Completed business must not run again')
(out/'ready.json').write_text(json.dumps({'operation_id':operation,'callback_started':time.time()}))
if scope=='long':
    limit=time.monotonic()+180
    while not (root/'start-at.txt').exists():
        if time.monotonic()>limit:raise TimeoutError('No shared start boundary')
        time.sleep(.1)
    start_at=float((root/'start-at.txt').read_text())
    while time.time()<start_at:time.sleep(min(.1,start_at-time.time()))
started=time.monotonic();seconds=int(os.environ['LAUNCH_SECONDS'])
with (out/'progress.csv').open('w',newline='') as stream:
    writer=csv.writer(stream);writer.writerow(['sequence','elapsed_seconds','checksum']);sequence=0
    while time.monotonic()-started<seconds:
        value=f'{operation}:{sequence}'.encode();writer.writerow([sequence,round(time.monotonic()-started,3),hashlib.sha256(value).hexdigest()]);stream.flush()
        sequence+=1;time.sleep(min(1,max(.01,seconds-(time.monotonic()-started))))
    os.fsync(stream.fileno())
artifacts=[]
for unit in range(1,4):
    target=out/f'report-{unit}.csv'
    with target.open('x',newline='') as stream:
        writer=csv.writer(stream);writer.writerow(['operation_id','unit','item','amount_minor'])
        for item in range(1,101):writer.writerow([operation,unit,item,125])
        stream.flush();os.fsync(stream.fileno())
    artifacts.append({'file':target.name,'sha256':hashlib.sha256(target.read_bytes()).hexdigest(),'rows':100})
result={'operation_id':operation,'callback_count':1,'completed_units':len(artifacts),'business_seconds':round(time.monotonic()-started,3),'progress_rows':sequence,'artifacts':artifacts}
(out/'business-result.json').write_text(json.dumps(result,indent=2))
