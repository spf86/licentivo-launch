from common import *
from joint_client import connect,write,callback_count
from licentivo_launch._vendor.licentivo import Client as Runtime
import multiprocessing,uuid

def worker(settings, inherited, operation, barrier, results):
    client=inherited or Runtime(settings)
    try:
        if not inherited:client.start()
        barrier.wait(timeout=20)
        def action():
            time.sleep(1.5)
            return write(operation,'n04-fork','export')
        client.run_metered_feature('export',1,operation,action)
        results.put({'ok':True})
    except Exception as error:
        message=str(error)
        results.put({'ok':False,'inherited_rejected':'after fork' in message,'busy':'another process' in message,'code':getattr(error,'code',type(error).__name__)})
    finally:
        if not inherited:client.close()

if __name__=='__main__':
    with connect('fixed','n04-fork') as app:
        app.login('alice@launch-joint.example',SECRETS['customer_password'])
        settings=dict(app.runtime.config)
        license_id=app.runtime._check()['license_id']
    context=multiprocessing.get_context('fork')
    for inherited_case in [True,False]:
        parent=Runtime(settings) if inherited_case else None
        operation=str(uuid.uuid4());barrier=context.Barrier(2);results=context.Queue()
        children=[context.Process(target=worker,args=(settings,parent,operation,barrier,results)) for _ in range(2)]
        for child in children:child.start()
        for child in children:child.join(35)
        values=[]
        for child in children:
            if child.is_alive():child.terminate();child.join()
        for _ in children:
            try:values.append(results.get(timeout=2))
            except Exception:break
        count=callback_count(operation)
        if inherited_case:
            ok=len(values)==2 and count==0 and all(v.get('inherited_rejected') for v in values)
            record('N04 inherited Client is rejected in both real Linux fork workers',ok,callback_count=count,process_results=values)
            parent.close()
        else:
            client=Runtime(settings);client.start()
            try:
                client.run_metered_feature('export',1,operation,lambda:write(operation,'n04-replayed','export'))
                hold=client.commit(next(json.loads(p.read_text())['reservation_id'] for p in Path(settings['cache_path']).parent.glob('*.task-*.json') if json.loads(p.read_text()).get('operation_id')==operation),operation)
                ok=len(values)==2 and count==1 and callback_count(operation)==1 and hold['status']=='committed' and hold.get('consumption',{}).get('quantity')==1
                record('N04 independent fork workers share one journal, one callback and one usage receipt',ok,callback_count=callback_count(operation),process_results=values,reservation_status=hold['status'],consumption_quantity=hold.get('consumption',{}).get('quantity'))
            finally:client.close()
        if not ok:raise SystemExit(1)
    with connect('fixed','n04-fork') as app:app.logout()
