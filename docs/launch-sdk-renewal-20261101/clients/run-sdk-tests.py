from pathlib import Path
import subprocess,os,json,time
base=Path('/home/spf86/launch-sdk-renewal-20261101');source=base/'sdk-source';old=Path('/home/spf86/joint-acceptance-20261031');python='/home/spf86/launch-acceptance-20261031/venv/bin/python'
env=os.environ.copy();env.update(XDG_CACHE_HOME=str(base/'unit-cache'),PYTHONPATH=str(source/'sdk/python/src'),DOTNET_CLI_HOME=str(old/'dotnet-home'),NUGET_PACKAGES=str(old/'nuget'),DOTNET_CLI_TELEMETRY_OPTOUT='1',CARGO_HOME=str(old/'cargo-home'),CARGO_TARGET_DIR=str(base/'sdk-build/rust-unit'))
steps=[
('go',['./launchclient.test','-test.v'],base),
('python',[python,'-m','unittest','discover','-s','sdk/python/tests','-v'],source),

('javascript',['node','--test',*[str(p) for p in (source/'sdk/javascript/test').glob('*.test.mjs')]],source),
('java-build',['javac','--release','17','-encoding','UTF-8','-d',str(base/'sdk-build/java'),*[str(p) for folder in ['sdk/java/src','sdk/java/tests'] for p in (source/folder).rglob('*.java')]],source),
('java',['java','-ea','-cp',str(base/'sdk-build/java'),'LaunchTests'],source),
('java-runtime',['java','-ea','-cp',str(base/'sdk-build/java'),'OfficialSDKTests'],source),
('csharp-build',[str(old/'dotnet/dotnet'),'build','sdk/csharp-tests/Launch.Tests.csproj','-m:1','-p:UseSharedCompilation=false'],source),
('csharp',[str(old/'dotnet/dotnet'),'sdk/csharp-tests/bin/Debug/net8.0/Launch.Tests.dll'],source),
('csharp-runtime-build',[str(old/'dotnet/dotnet'),'build','sdk/csharp-runtime-tests/Launch.RuntimeTests.csproj','-m:1','-p:UseSharedCompilation=false'],source),
('csharp-runtime',[str(old/'dotnet/dotnet'),'sdk/csharp-runtime-tests/bin/Debug/net8.0/Launch.RuntimeTests.dll'],source),
('ruby-build',['bash','-c','core="$PWD/sdk/ruby/vendor/c"; mkdir -p sdk/ruby/lib/licentivo_launch/vendor/licentivo/native; gcc -std=c11 -fPIC -shared -O2 -I "$core" "$core/licentivo.c" "$core/vendor/cJSON.c" "$core/vendor/monocypher.c" "$core/vendor/monocypher-ed25519.c" -o sdk/ruby/lib/licentivo_launch/vendor/licentivo/native/licentivo_native.so $(pkg-config --cflags --libs libcurl openssl) -lpthread -lm'],source),
('ruby',['ruby','sdk/ruby/test/test_client.rb'],source),('ruby-runtime',['ruby','sdk/ruby/test/test_runtime.rb'],source),
('rust',['cargo','test','--manifest-path','sdk/rust/Cargo.toml','--locked','--jobs','2'],source),
('native-configure',['cmake','-S','sdk/native','-B',str(base/'sdk-build/native-unit'),'-DCMAKE_BUILD_TYPE=Release','-DLICENTIVO_LAUNCH_BUILD_TESTS=ON'],source),
('native-build',['cmake','--build',str(base/'sdk-build/native-unit'),'--parallel','2'],source),
('native',[python,'sdk/native/tests/run.py','--c',str(base/'sdk-build/native-unit/launch_native_test'),'--cpp',str(base/'sdk-build/native-unit/launch_cpp_test')],source),
]
(base/'launchclient.test').chmod(0o700);(base/'sdk-build/java').mkdir(parents=True,exist_ok=True)
results=[]
for name,command,cwd in steps:
    started=time.monotonic()
    with (base/('sdk-'+name+'-private.log')).open('w') as stream:
        process=subprocess.run(command,cwd=cwd,env=env,stdout=stream,stderr=subprocess.STDOUT,timeout=600)
    row={'name':name,'passed':process.returncode==0,'exit_code':process.returncode,'seconds':round(time.monotonic()-started,2)};results.append(row)
    (base/'sdk-unit-results.json').write_text(json.dumps(results,indent=2));print(json.dumps(row),flush=True)
raise SystemExit(0 if all(r['passed'] for r in results) else 1)
