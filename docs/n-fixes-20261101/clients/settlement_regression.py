from common import *
from joint_client import connect,write,callback_count
import multiprocessing,uuid

def interrupted(operation):
    app=connect('fixed','n06-interrupted')
    app.login('alice@launch-joint.example',SECRETS['customer_password'])
    def business():
        write(operation,'n06-crash-before-return','export')
        os._exit(73)
    app.run_metered_feature('export',1,operation,business)

if __name__=='__main__':
    operation=str(uuid.uuid4())
    process=multiprocessing.get_context('fork').Process(target=interrupted,args=(operation,))
    process.start();process.join(30)
    if process.is_alive():process.terminate();raise RuntimeError('worker did not stop')
    assert process.exitcode==73 and callback_count(operation)==1
    with connect('fixed','n06-interrupted') as app:
        assert app.restore()
        blocked=False
        try:app.run_metered_feature('export',1,operation,lambda:write(operation,'incorrect-reexecution','export'))
        except RuntimeError:blocked=True
        record('N06 real process death retains unknown outcome and blocks business replay',blocked and callback_count(operation)==1,worker_exit=process.exitcode,callback_count=callback_count(operation))
        app.resolve_metered_feature(operation,True)
        app.run_metered_feature('export',1,operation,lambda:write(operation,'incorrect-resolved-replay','export'))
        journal=next(json.loads(p.read_text()) for p in Path(app.runtime.config['cache_path']).parent.glob('*.task-*.json') if json.loads(p.read_text()).get('operation_id')==operation)
        receipt=app.runtime.commit(journal['reservation_id'],operation)
        record('N06 Application settles verified real output after restart and keeps one receipt',callback_count(operation)==1 and journal['state']=='committed' and receipt['consumption']['quantity']==1,callback_count=callback_count(operation),journal_state=journal['state'],consumption_quantity=receipt['consumption']['quantity'])
        failure_id=str(uuid.uuid4());failure=ValueError('test business failed')
        def failed():raise failure
        caught=None
        try:app.run_metered_feature('export',1,failure_id,failed)
        except Exception as error:caught=error
        app.resolve_metered_feature(failure_id,False)
        failed_journal=next(json.loads(p.read_text()) for p in Path(app.runtime.config['cache_path']).parent.glob('*.task-*.json') if json.loads(p.read_text()).get('operation_id')==failure_id)
        record('N06 callback failure cancels reservation and preserves original business error',caught is failure and failed_journal['state']=='canceled',journal_state=failed_journal['state'])
        app.logout()
    with connect('quota','n06-exhausted') as app:
        app.login('alice@launch-joint.example',SECRETS['customer_password'])
        denied_id=str(uuid.uuid4());error_code=''
        try:app.run_metered_feature('export',1,denied_id,lambda:write(denied_id,'incorrect-quota-bypass','export'))
        except Exception as error:error_code=getattr(error,'code',type(error).__name__)
        record('N06 exhausted real quota rejects Application business callback',error_code=='FEATURE_QUOTA_EXCEEDED' and callback_count(denied_id)==0,code=error_code,callback_count=callback_count(denied_id))
        app.logout()
