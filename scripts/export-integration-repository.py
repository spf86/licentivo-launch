#!/usr/bin/env python3
"""Export only public client sources; never export a Launch server checkout."""
import argparse
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
MODULE = 'github.com/spf86/licentivo-launch-sdk'
EXCLUDED = {'.git', '.cache', '.local', 'bin', 'obj', 'build', 'dist', 'target',
            'node_modules', '__pycache__', 'native'}
EXTENSIONS = {'.go', '.sum', '.mod', '.py', '.mjs', '.js', '.ts', '.json', '.toml', '.lock',
              '.cs', '.csproj', '.java', '.xml', '.c', '.h', '.cpp', '.hpp', '.rs',
              '.rb', '.gemspec', '.md', '.txt', '.cmake', '.in', '.ps1', '.sh', '.yml'}
DOCS = ('manual-integration.md', 'application-quickstart.zh-CN.md', 'software-sdk-integration.md',
        'integration-packages.md', 'ai-integration.md', 'acceptance.md', 'openapi.json')
SCRIPTS = ('test-integration-packages.ps1', 'test-managed-installations.ps1',
           'generate-integration-smoke.mjs', 'integration-prompts.test.mjs',
           'integration-state.test.mjs', 'acceptance.py', 'test_acceptance.py')


def export(destination):
    destination = destination.resolve()
    if ROOT not in destination.parents or destination.exists():
        raise ValueError('Choose a new export directory inside this workspace.')
    version = (ROOT / 'sdk/VERSION').read_text().strip()
    if not re.fullmatch(r'\d+\.\d+\.\d+', version):
        raise ValueError('Invalid SDK version.')
    written = {}

    def write(target, data):
        path = destination / target
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        written[target] = hashlib.sha256(data).hexdigest()

    def copy(source, target):
        if source.is_symlink() or ROOT not in source.resolve().parents:
            raise ValueError('Unsafe source path: ' + str(source))
        data = source.read_bytes().replace(b'\r\n', b'\n')
        if source.suffix in {'.go', '.mod', '.md', '.ps1', '.js', '.mjs'}:
            data = data.replace(b'licentivo-launch/pkg/launchclient',
                                (MODULE + '/pkg/launchclient').encode())
            if source.suffix == '.mod':
                data = data.replace(b'require licentivo-launch ', ('require ' + MODULE + ' ').encode())
                data = data.replace(b'replace licentivo-launch =>', ('replace ' + MODULE + ' =>').encode())
                data = data.replace(b'licentivo-launch v0.0.0', (MODULE + ' v0.0.0').encode())
        if re.search(rb'-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY', data):
            raise ValueError('Private key material in public source: ' + target)
        write(target, data)

    def tree(relative):
        directory = ROOT / relative
        for source in sorted(directory.rglob('*')):
            rel = source.relative_to(directory)
            if relative == 'examples' and rel.parts and rel.parts[0] == 'vendor':
                continue
            # native source folders are permitted; private compiled Ruby files are not.
            if any(part in (EXCLUDED - {'native'}) or part.endswith('.egg-info') for part in rel.parts):
                continue
            # These suites exercise the merchant server and its private MySQL
            # storage. They remain in the server repository, not client modules.
            if relative == 'pkg/launchclient' and source.name.endswith('_integration_test.go'):
                continue
            if source.is_file() and (source.suffix in EXTENSIONS or
                    source.name in {'LICENSE', 'NOTICE', 'VERSION', 'UPSTREAM_VERSION',
                                    'CMakeLists.txt', 'cJSON-LICENSE', 'Monocypher-LICENCE.md'}):
                if source.name in {'launch-config.json', 'product.json', 'licensed-report.txt'}:
                    continue
                copy(source, source.relative_to(ROOT).as_posix())

    copy(ROOT / 'LICENSE', 'LICENSE')
    copy(ROOT / 'go.sum', 'go.sum')
    write('go.mod', ('module ' + MODULE + '\n\ngo 1.26.0\n\nrequire golang.org/x/sys v0.41.0\n').encode())
    for directory in ('pkg/launchclient', 'sdk/python', 'sdk/javascript', 'sdk/csharp',
                      'sdk/csharp-tests', 'sdk/csharp-runtime-tests', 'sdk/java', 'sdk/rust',
                      'sdk/ruby', 'sdk/native', 'examples'):
        tree(directory)
    # The prepared upstream tree and private application configuration are never exported.
    for name in ('VERSION', 'UPSTREAM_VERSION', 'managed-sdk-manifest.json'):
        copy(ROOT / 'sdk' / name, 'sdk/' + name)
    for name in DOCS:
        copy(ROOT / 'docs' / name, 'docs/' + name)
    for name in SCRIPTS:
        copy(ROOT / 'scripts' / name, 'scripts/' + name)
    for name in ('integration-prompts.js', 'integration-state.js'):
        copy(ROOT / 'internal/httpapi/web' / name, 'internal/httpapi/web/' + name)
    copy(ROOT / 'internal/app/integration.go', 'scripts/fixtures/integration-contract.go.txt')
    for source in sorted((ROOT / 'sdk/release').rglob('*')):
        if source.is_file() and source.suffix not in {'.pyc'} and '__pycache__' not in source.parts:
            copy(source, source.relative_to(ROOT / 'sdk/release').as_posix())
    write('VERSION', (version + '\n').encode())
    # Record rewritten private Go imports without changing the reviewed upstream hashes.
    manifest = json.loads((destination / 'sdk/managed-sdk-manifest.json').read_text())
    manifest['changes'] += ' Public repository line endings are normalized to LF.'
    for item in manifest['files']:
        path = destination / item['destination']
        if path.is_file():
            item['sha256'] = hashlib.sha256(path.read_bytes()).hexdigest()
    write('sdk/managed-sdk-manifest.json', (json.dumps(manifest, indent=2) + '\n').encode())
    receipt = {'schema_version': 1, 'version': version, 'module': MODULE,
               'client_only': True, 'files': written}
    # Keep the export receipt outside the public repository.
    receipt_path = destination.parent / (destination.name + '-export.json')
    receipt_path.write_text(json.dumps(receipt, indent=2) + '\n')
    return {'repository': str(destination), 'version': version, 'public_files': len(written),
            'receipt': str(receipt_path)}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', required=True, type=Path)
    print(json.dumps(export(parser.parse_args().out)))
