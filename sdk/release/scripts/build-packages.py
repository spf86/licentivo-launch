#!/usr/bin/env python3
"""Build Launch distributions from the client-only repository, without server access."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
MODULE = 'github.com/spf86/licentivo-launch-sdk'
SKIP = {'.git', '.cache', 'artifacts', 'dist', 'build', 'target', 'bin', 'obj', 'node_modules', '__pycache__'}


def run(*arguments, cwd=ROOT, capture=False):
    result = subprocess.run([str(value) for value in arguments], cwd=cwd, check=True,
                            text=True, stdout=subprocess.PIPE if capture else None)
    return result.stdout.strip() if capture else None


def public_files(directory):
    for source in sorted(directory.rglob('*')):
        rel = source.relative_to(directory)
        if any(part in SKIP or part.endswith('.egg-info') for part in rel.parts):
            continue
        if source.is_symlink():
            raise ValueError('Symlinks are not permitted in distributions.')
        if source.is_file() and source.suffix not in {'.pyc', '.log', '.so', '.dll', '.dylib', '.a', '.lib', '.exe', '.o', '.obj'}:
            yield rel, source


def copy_tree(source, destination):
    for relative, path in public_files(source):
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, target)


def archive(destination, source):
    # Preserve installed native bin/ folders; source filtering is performed earlier.
    with zipfile.ZipFile(destination, 'w', compression=zipfile.ZIP_DEFLATED) as output:
        for path in sorted(source.rglob('*')):
            if path.is_symlink():
                raise ValueError('Symlinks are not permitted in release archives.')
            if path.is_file():
                output.write(path, path.relative_to(source).as_posix())


def copy_curl(source, destination):
    if not source:
        return
    for part in ('include', 'lib', 'bin'):
        target = destination / part
        target.mkdir(parents=True, exist_ok=True)
        if part == 'include':
            shutil.copytree(source / part, target, dirs_exist_ok=True)
        else:
            patterns = ('*.dll',) if part == 'bin' else ('libcurl.dll.a',)
            for pattern in patterns:
                for path in (source / part).glob(pattern):
                    shutil.copyfile(path, target / path.name)
    for path in source.iterdir():
        if path.is_file() and any(word in path.name.upper() for word in ('LICEN', 'COPYING', 'NOTICE')):
            shutil.copyfile(path, destination / path.name)
    for path in (source / 'dep').rglob('*'):
        if path.is_file() and any(word in path.name.upper() for word in ('LICEN', 'COPYING', 'NOTICE')):
            target = destination / 'licenses' / path.relative_to(source / 'dep')
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--curl-root', type=Path)
    parser.add_argument('--cc')
    parser.add_argument('--dotnet', default='dotnet')
    parser.add_argument('--ruby', default='ruby')
    parser.add_argument('--cargo', default='cargo')
    parser.add_argument('--java-home', type=Path)
    args = parser.parse_args()
    output = args.out.resolve()
    if output.exists():
        raise ValueError('Choose a new output directory; release assets are immutable.')
    version = (ROOT / 'VERSION').read_text().strip()
    if 'module ' + MODULE not in (ROOT / 'go.mod').read_text():
        raise ValueError('Build from the exported public client-only repository.')
    output.mkdir(parents=True)
    windows = os.name == 'nt'
    suffix = '.exe' if windows else ''
    def java(name):
        return args.java_home / 'bin' / (name + suffix) if args.java_home else name
    languages = []
    with tempfile.TemporaryDirectory(prefix='launch-sdk-build-') as temporary:
        work = Path(temporary)
        source = work / 'source'
        copy_tree(ROOT, source)
        for language in ('rust', 'ruby', 'native'):
            copy_curl(args.curl_root, source / 'sdk' / language / 'vendor/curl')
        def record(language, artifact, source_archive, kind):
            languages.append({'language': language, 'version': version, 'upstream_version': '0.11.0',
                              'artifact': artifact, 'source_archive': source_archive, 'artifact_type': kind,
                              'sdk_managed': True, 'requires_separate_official_sdk': False,
                              'initialization': 'direct_parameters', 'configuration_files_required': False})
        go_source = work / 'go'
        copy_tree(source / 'pkg', go_source / 'pkg')
        for name in ('go.mod', 'go.sum', 'LICENSE'):
            shutil.copyfile(source / name, go_source / name)
        shutil.copyfile(source / 'pkg/launchclient/README.md', go_source / 'README.md')
        go_zip = f'licentivo-launch-go-{version}.zip'
        archive(output / go_zip, go_source)
        record('Go', go_zip, go_zip, 'self_contained_go_module')
        for name in ('python', 'javascript', 'csharp', 'java', 'rust', 'ruby', 'native'):
            archive(output / f'licentivo-launch-{name}-{version}-source.zip', source / 'sdk' / name)
        run(sys.executable, '-m', 'build', '--sdist', '--wheel', '--no-isolation', '--outdir', output, source / 'sdk/python')
        record('Python', f'licentivo_launch-{version}-py3-none-any.whl', f'licentivo-launch-python-{version}-source.zip', 'self_contained_wheel')
        run(args.dotnet, 'pack', source / 'sdk/csharp/Licentivo.Launch.csproj', '-c', 'Release', '-o', output, '-p:UseSharedCompilation=false')
        record('C#', f'Licentivo.Launch.{version}.nupkg', f'licentivo-launch-csharp-{version}-source.zip', 'self_contained_nuget')
        run('npm.cmd' if windows else 'npm', 'pack', source / 'sdk/javascript', '--ignore-scripts', '--pack-destination', output)
        record('JavaScript/Node.js', f'licentivo-launch-{version}.tgz', f'licentivo-launch-javascript-{version}-source.zip', 'self_contained_npm')
        classes = work / 'classes'
        classes.mkdir()
        run(java('javac'), '--release', '17', '-encoding', 'UTF-8', '-d', classes, *sorted((source / 'sdk/java/src').rglob('*.java')))
        copy_tree(source / 'sdk/java/vendor', classes / 'META-INF/licenses/licentivo')
        shutil.copyfile(source / 'LICENSE', classes / 'META-INF/LICENSE')
        run(java('jar'), '--create', '--file', output / f'launch-client-{version}.jar', '-C', classes, '.')
        shutil.copyfile(source / 'sdk/java/pom.xml', output / f'launch-client-{version}.pom')
        record('Java', f'launch-client-{version}.jar', f'licentivo-launch-java-{version}-source.zip', 'self_contained_jar')
        ruby_source = source / 'sdk/ruby'
        gem_script = "require 'rubygems/gem_runner'; Gem::GemRunner.new.run(ARGV)"
        run(args.ruby, '-e', gem_script, '--', 'build', 'licentivo-launch.gemspec', '--output', output / f'licentivo-launch-{version}.gem', cwd=ruby_source)
        record('Ruby', f'licentivo-launch-{version}.gem', f'licentivo-launch-ruby-{version}-source.zip', 'self_contained_gem')
        if windows:
            if not args.curl_root or not args.cc:
                raise ValueError('Windows native packages need --curl-root and --cc.')
            native = ruby_source / 'lib/licentivo_launch/vendor/licentivo/native'
            native.mkdir(parents=True)
            core = ruby_source / 'vendor/c'
            files = [core / name for name in ('licentivo.c', 'vendor/cJSON.c', 'vendor/monocypher.c', 'vendor/monocypher-ed25519.c')]
            run(args.cc, '-std=c11', '-O2', '-shared', '-Wl,--export-all-symbols', '-I' + str(core),
                '-I' + str(args.curl_root / 'include'), *files, '-L' + str(args.curl_root / 'lib'),
                '-lcurl', '-lbcrypt', '-ladvapi32', '-o', native / 'licentivo_native.dll')
            for path in (args.curl_root / 'bin').glob('*.dll'):
                shutil.copyfile(path, native / path.name)
            ruby_platform = run(args.ruby, '-e', 'print Gem::Platform.local', capture=True)
            env_previous = os.environ.get('LAUNCH_GEM_PRECOMPILED')
            try:
                os.environ['LAUNCH_GEM_PRECOMPILED'] = '1'
                gem = f'licentivo-launch-{version}-{ruby_platform}.gem'
                run(args.ruby, '-e', gem_script, '--', 'build', 'licentivo-launch.gemspec', '--output', output / gem, cwd=ruby_source)
                languages[-1]['artifact'] = gem
            finally:
                if env_previous is None:
                    os.environ.pop('LAUNCH_GEM_PRECOMPILED', None)
                else:
                    os.environ['LAUNCH_GEM_PRECOMPILED'] = env_previous
        run(args.cargo, 'generate-lockfile', '--manifest-path', source / 'sdk/rust/Cargo.toml', '--offline')
        run(args.cargo, 'package', '--manifest-path', source / 'sdk/rust/Cargo.toml', '--allow-dirty', '--no-verify', '--locked', '--offline', '--target-dir', work / 'rust-target')
        crate = f'licentivo-launch-{version}.crate'
        shutil.copyfile(work / 'rust-target/package' / crate, output / crate)
        record('Rust', crate, f'licentivo-launch-rust-{version}-source.zip', 'self_contained_crate')
        native_build = work / 'native-build'
        prefix = work / 'native-install'
        cmake = ['cmake', '-S', str(source / 'sdk/native'), '-B', str(native_build), '-DCMAKE_BUILD_TYPE=Release', '-DCMAKE_INSTALL_PREFIX=' + str(prefix)]
        if windows:
            compiler = Path(args.cc)
            cmake += ['-G', 'MinGW Makefiles', '-DCMAKE_MAKE_PROGRAM=' + str(compiler.parent / 'mingw32-make.exe'),
                      '-DCMAKE_C_COMPILER=' + str(compiler), '-DCMAKE_CXX_COMPILER=' + str(compiler.parent / 'g++.exe')]
        run(*cmake)
        run('cmake', '--build', native_build, '--config', 'Release')
        run('cmake', '--install', native_build, '--config', 'Release')
        native_zip = f'licentivo-launch-native-{version}-' + ('windows-amd64-gnu.zip' if windows else 'linux-amd64.zip')
        archive(output / native_zip, prefix)
        for name in ('C', 'C++'):
            record(name, native_zip, f'licentivo-launch-native-{version}-source.zip', 'self_contained_cmake')
        # Source distributions include no generated test/cache files.
        clean_source = work / 'clean-source'
        copy_tree(source, clean_source)
        for language in ('rust', 'ruby', 'native'):
            copy_curl(args.curl_root, clean_source / 'sdk' / language / 'vendor/curl')
        archive(output / f'licentivo-launch-{version}-source.zip', clean_source)
    inventory = {'schema_version': 2, 'wrapper_version': version, 'upstream_sdk_version': '0.11.0',
                 'sdk_dependency_management': 'embedded', 'requires_separate_official_sdk': False,
                 'initialization': 'direct_parameters', 'configuration_files_required': False,
                 'published': False, 'languages': languages, 'source_bundle': f'licentivo-launch-{version}-source.zip'}
    (output / 'integration-packages.json').write_text(json.dumps(inventory, indent=2) + '\n')
    checksums = [hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + path.name
                 for path in sorted(output.iterdir()) if path.is_file()]
    (output / 'SHA256SUMS').write_text('\n'.join(checksums) + '\n', encoding='ascii')
    print('Built nine managed Launch distributions: ' + str(output))


if __name__ == '__main__':
    main()
