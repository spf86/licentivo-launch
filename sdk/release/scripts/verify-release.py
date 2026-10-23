#!/usr/bin/env python3
"""Verify an immutable, self-contained nine-language release before publication."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import tarfile


def verify(directory):
    directory = directory.resolve()
    inventory = json.loads((directory / 'integration-packages.json').read_text(encoding='utf-8-sig'))
    if (inventory.get('schema_version') != 2 or len(inventory.get('languages', [])) != 9 or
            inventory.get('requires_separate_official_sdk') is not False or
            inventory.get('initialization') != 'direct_parameters' or
            inventory.get('configuration_files_required') is not False):
        raise ValueError('Expected the managed nine-language release contract.')
    version = inventory['wrapper_version']
    if not re.fullmatch(r'\d+\.\d+\.\d+', version):
        raise ValueError('Invalid release version.')
    records = {}
    for line in (directory / 'SHA256SUMS').read_text(encoding='ascii').splitlines():
        match = re.fullmatch(r'([a-f0-9]{64})  ([A-Za-z0-9_.-]+)', line)
        if not match or match[2] in records or match[2] in {'.', '..'}:
            raise ValueError('Invalid or duplicate artifact checksum entry.')
        digest, name = match.groups()
        path = directory / name
        if path.is_symlink() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError('Artifact checksum mismatch: ' + name)
        records[name] = digest
    for language in inventory['languages']:
        if (not language.get('sdk_managed') or language.get('requires_separate_official_sdk') is not False or
                language.get('initialization') != 'direct_parameters' or
                language.get('configuration_files_required') is not False or
                language.get('version') != version or language.get('artifact') not in records):
            raise ValueError('Invalid language artifact contract.')
    if inventory.get('source_bundle') not in records or 'integration-packages.json' not in records:
        raise ValueError('Missing checksummed source bundle or inventory.')
    npm_package = next(item for item in inventory['languages'] if item['language'] == 'JavaScript/Node.js')
    with tarfile.open(directory / npm_package['artifact'], 'r:gz') as archive:
        names = set(archive.getnames())
        manifest = json.load(archive.extractfile('package/package.json'))
        if manifest['version'] != version:
            raise ValueError('The npm package version does not match the release.')
        def check_entrypoints(value):
            if isinstance(value, dict):
                for target in value.values():
                    check_entrypoints(target)
            elif isinstance(value, str) and value.startswith('./'):
                if 'package/' + value[2:] not in names:
                    raise ValueError('Missing npm export: ' + value)
        check_entrypoints(manifest.get('exports', {}))
        if 'package/' + manifest['types'].removeprefix('./') not in names:
            raise ValueError('Missing npm TypeScript declarations.')
    return {'version': version, 'languages': 9, 'verified_artifacts': len(records)}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--packages', required=True, type=Path)
    print(json.dumps(verify(parser.parse_args().packages)))
