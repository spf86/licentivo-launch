#!/usr/bin/env python3
"""Run all nine local client suites without a merchant or licensing service."""
import json
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
WORK = ROOT / '.cache/source-check'


def run(*args, cwd=ROOT):
    subprocess.run([str(value) for value in args], cwd=cwd, check=True)


def main():
    WORK.mkdir(parents=True, exist_ok=True)
    manifest = json.loads((ROOT / 'sdk/managed-sdk-manifest.json').read_text())
    for item in manifest['files']:
        source = (ROOT / item['destination']).resolve()
        if ROOT not in source.parents or hashlib.sha256(source.read_bytes()).hexdigest() != item['sha256']:
            raise ValueError('Embedded SDK source checksum mismatch: ' + item['destination'])
    windows = os.name == 'nt'
    java_home = os.environ.get('JAVA_HOME')
    def java(name):
        return Path(java_home) / 'bin' / (name + ('.exe' if windows else '')) if java_home else name
    run('go', 'test', './pkg/launchclient', '-count=1')
    for example in ('go', 'web-backend'):
        run('go', 'test', './...', '-count=1', cwd=ROOT / 'examples' / example)
    os.environ['PYTHONPATH'] = str(ROOT / 'sdk/python/src')
    run(sys.executable, '-m', 'unittest', 'discover', '-s', 'sdk/python/tests', '-v')
    run(sys.executable, 'scripts/test_acceptance.py')
    run('node', '--test', *sorted((ROOT / 'sdk/javascript/test').glob('*.test.mjs')))
    run('node', '--test', 'scripts/integration-prompts.test.mjs', 'scripts/integration-state.test.mjs')
    for suite in ('csharp-tests', 'csharp-runtime-tests'):
        project = next((ROOT / 'sdk' / suite).glob('*.csproj'))
        run('dotnet', 'run', '--project', project, '-p:UseSharedCompilation=false')
    classes = WORK / 'java'
    classes.mkdir(exist_ok=True)
    sources = sorted((ROOT / 'sdk/java/src').rglob('*.java')) + sorted((ROOT / 'sdk/java/tests').glob('*.java'))
    run(java('javac'), '--release', '17', '-encoding', 'UTF-8', '-d', classes, *sources)
    for suite in ('LaunchTests', 'OfficialSDKTests'):
        run(java('java'), '-ea', '-cp', classes, suite)
    lock = ROOT / 'sdk/rust/Cargo.lock'
    if not lock.exists():
        shutil.copyfile(ROOT / 'sdk/rust/Cargo.package.lock', lock)
    run('cargo', 'test', '--manifest-path', ROOT / 'sdk/rust/Cargo.toml', '--locked')
    ruby_native = ROOT / 'sdk/ruby/lib/licentivo_launch/vendor/licentivo/native'
    ruby_native.mkdir(parents=True, exist_ok=True)
    core = ROOT / 'sdk/ruby/vendor/c'
    files = [core / name for name in ('licentivo.c', 'vendor/cJSON.c', 'vendor/monocypher.c', 'vendor/monocypher-ed25519.c')]
    if windows:
        curl = Path(os.environ['LICENTIVO_CURL_ROOT'])
        run('gcc', '-std=c11', '-O2', '-shared', '-Wl,--export-all-symbols', '-I' + str(core),
            '-I' + str(curl / 'include'), *files, '-L' + str(curl / 'lib'), '-lcurl', '-lbcrypt', '-ladvapi32',
            '-o', ruby_native / 'licentivo_native.dll')
        for path in (curl / 'bin').glob('*.dll'):
            shutil.copyfile(path, ruby_native / path.name)
    else:
        run('cc', '-std=c11', '-O2', '-fPIC', '-shared', '-I' + str(core), *files,
            '-lcurl', '-lcrypto', '-lpthread', '-lm', '-o', ruby_native / 'licentivo_native.so')
    run('ruby', 'sdk/ruby/test/test_client.rb')
    run('ruby', 'sdk/ruby/test/test_runtime.rb')
    native = WORK / 'native'
    command = ['cmake', '-S', 'examples/native', '-B', native, '-DCMAKE_BUILD_TYPE=Release', '-DLICENTIVO_LAUNCH_BUILD_TESTS=ON']
    if windows:
        command += ['-G', 'MinGW Makefiles', '-DCMAKE_C_COMPILER=gcc', '-DCMAKE_CXX_COMPILER=g++']
    run(*command)
    run('cmake', '--build', native, '--config', 'Release')
    suffix = '.exe' if windows else ''
    run(sys.executable, 'sdk/native/tests/run.py', '--c', native / 'launch-account' / ('launch_native_test' + suffix),
        '--cpp', native / 'launch-account' / ('launch_cpp_test' + suffix))
    (WORK / 'report.json').write_text(json.dumps({'languages': 9, 'all_nine_passed': True,
        'scope': 'local_signed_runtime_fixtures', 'external_acceptance_complete': False}, indent=2) + '\n')


if __name__ == '__main__':
    main()
