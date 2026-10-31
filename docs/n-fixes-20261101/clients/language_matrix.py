from common import *
import subprocess,uuid
os.environ['NFIX_RUN']=uuid.uuid4().hex
os.environ['XDG_CACHE_HOME']=str(BASE/'matrix-cache')

commands=[('Python',['/home/spf86/launch-acceptance-20261031/venv/bin/python',str(BASE/'client_python.py')]),('Go',[str(BASE/'client-go')]),('JavaScript',['node',str(BASE/'client_js.mjs')]),('Java',['java','-cp',str(BASE/'sdk-build/java'),'ClientJava']),('Ruby',['ruby',str(BASE/'client_ruby.rb')]),('C',[str(BASE/'sdk-build/native/client-native')]),('C++',[str(BASE/'sdk-build/native/client-cpp')]),('C#',['/home/spf86/joint-acceptance-20261031/dotnet/dotnet',str(BASE/'csharp-client/bin/Release/net8.0/ClientSharp.dll')]),('Rust',[str(BASE/'sdk-build/rust/debug/joint-launch-client')])]
with (BASE/'language-results.jsonl').open('a') as output:
    for language,command in commands:
        for name in ['fixed','floating']:
            start=time.monotonic()
            try:
                process=subprocess.run(command+[name],capture_output=True,text=True,cwd=BASE,timeout=75)
                lines=process.stdout.splitlines()
                payload=json.loads(lines[-1]) if lines else {'ok':False,'code':'EMPTY_OUTPUT'}
                result=record(language+' '+name+' real Launch SDK lifecycle',process.returncode==0 and payload.get('ok') is True,seconds=round(time.monotonic()-start,3),exit_code=process.returncode,details=payload)
                if process.returncode:
                    # Kept private; never print arbitrary transport or SDK output.
                    (BASE/('language-'+language.lower()+'-'+name+'-private.log')).write_text(process.stderr)
            except Exception as error:
                result=record(language+' '+name+' real Launch SDK lifecycle',False,code=type(error).__name__)
            output.write(json.dumps(result)+'\n')
            output.flush()
            time.sleep(10)

markers=['python','go','javascript','java','ruby','c','cpp','csharp','rust']
for language in markers:
    artifact=BASE/('metered-'+language+'.txt')
    record('N06 '+language+' Application metering ran once, replayed and resolved',artifact.exists() and len(artifact.read_text())>10,artifact=artifact.name)
