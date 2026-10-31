from pathlib import Path
import json,os,subprocess
from licentivo_launch import Application,MemoryStore
root=Path(__file__).resolve().parent
report=json.loads((root/'meter-public.json').read_text());secret=json.loads((root/'deployment-private.json').read_text())
phase=os.environ['LAUNCH_PHASE'];calls=0
app=Application(report['config']['merchant_origin'],report['config']['launch_product_id'],report['product_config']|{'allow_http':True},'sandbox',store=MemoryStore(),cache_dir=root/'runtime/python',allow_http=True)
try:
    app.login(os.environ['LAUNCH_EMAIL'],secret['customer_password'])
    def business():
        global calls
        calls+=1;subprocess.run(['python3',str(root/'long_business.py')],check=True)
    app.run_metered_feature('export',3,os.environ['LAUNCH_OPERATION'],business)
    if phase=='replay':app.resolve_metered_feature(os.environ['LAUNCH_OPERATION'],True)
    assert calls==(1 if phase=='run' else 0)
    print(json.dumps({'ok':True,'language':'python','phase':phase,'callbacks':calls}))
finally:app.close()
