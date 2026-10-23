#!/usr/bin/env python3
"""Prepare and verify a signed Maven Central upload; no network or publication."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import xml.etree.ElementTree as ET
import zipfile

from pgpy import PGPKey
from pgpy.constants import HashAlgorithm

ROOT = Path(__file__).resolve().parents[1]


def zip_tree(target, directory):
    with zipfile.ZipFile(target, 'w', compression=zipfile.ZIP_DEFLATED) as output:
        for path in sorted(directory.rglob('*')):
            if path.is_symlink():
                raise ValueError('Public Java archives cannot contain symlinks.')
            if path.is_file():
                output.write(path, path.relative_to(directory).as_posix())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--packages', required=True, type=Path)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--java-home', required=True, type=Path)
    parser.add_argument('--private-key', required=True, type=Path)
    parser.add_argument('--passphrase-file', required=True, type=Path)
    parser.add_argument('--fingerprint', required=True)
    args = parser.parse_args()
    subprocess.run([__import__('sys').executable, str(ROOT / 'scripts/verify-release.py'),
                    '--packages', str(args.packages)], check=True)
    version = (ROOT / 'VERSION').read_text().strip()
    pom_path = ROOT / 'sdk/java/pom.xml'
    pom = ET.parse(pom_path).getroot()
    ns = {'m': 'http://maven.apache.org/POM/4.0.0'}
    group = pom.findtext('m:groupId', namespaces=ns)
    name = pom.findtext('m:artifactId', namespaces=ns)
    if pom.findtext('m:version', namespaces=ns) != version:
        raise ValueError('Maven and SDK versions do not match.')
    for field in ('name', 'description', 'url', 'licenses', 'developers', 'scm'):
        if pom.find('m:' + field, ns) is None:
            raise ValueError('Missing Central metadata: ' + field)
    output = args.out.resolve()
    if output.exists():
        raise ValueError('Choose a new Central staging directory.')
    content = output / 'content'
    target = content / Path(*group.split('.')) / name / version
    target.mkdir(parents=True)
    jar_name = f'{name}-{version}.jar'
    shutil.copyfile(args.packages / jar_name, target / jar_name)
    shutil.copyfile(pom_path, target / f'{name}-{version}.pom')
    with tempfile.TemporaryDirectory(prefix='launch-central-') as temporary:
        work = Path(temporary)
        sources = work / 'sources'
        shutil.copytree(ROOT / 'sdk/java/src', sources)
        shutil.copytree(ROOT / 'sdk/java/vendor', sources / 'META-INF/licenses/licentivo')
        shutil.copyfile(ROOT / 'LICENSE', sources / 'META-INF/LICENSE')
        zip_tree(target / f'{name}-{version}-sources.jar', sources)
        javadoc = work / 'javadoc'
        tool = args.java_home / 'bin' / ('javadoc.exe' if os.name == 'nt' else 'javadoc')
        subprocess.run([str(tool), '-quiet', '-encoding', 'UTF-8', '--release', '17', '-Xdoclint:none',
                        '-d', str(javadoc), *map(str, sorted((ROOT / 'sdk/java/src').rglob('*.java')))], check=True)
        shutil.copyfile(ROOT / 'LICENSE', javadoc / 'LICENSE')
        shutil.copyfile(ROOT / 'sdk/java/vendor/NOTICE', javadoc / 'NOTICE')
        zip_tree(target / f'{name}-{version}-javadoc.jar', javadoc)
    key, _ = PGPKey.from_file(str(args.private_key))
    if key.is_public or not key.is_protected or str(key.fingerprint) != args.fingerprint:
        raise ValueError('Expected the protected Launch release signing key.')
    artifacts = sorted(target.iterdir())
    if len(artifacts) != 4:
        raise ValueError('Expected JAR, POM, sources and Javadoc.')
    for artifact in artifacts:
        data = artifact.read_bytes()
        with key.unlock(args.passphrase_file.read_text().strip()):
            signature = key.sign(data, hash=HashAlgorithm.SHA256)
        if not bool(key.pubkey.verify(data, signature)):
            raise ValueError('Detached signature did not verify.')
        Path(str(artifact) + '.asc').write_text(str(signature), encoding='ascii')
        for algorithm in ('md5', 'sha1', 'sha256', 'sha512'):
            Path(str(artifact) + '.' + algorithm).write_text(hashlib.new(algorithm, data).hexdigest(), encoding='ascii')
    bundle = output / f'{name}-{version}-central.zip'
    zip_tree(bundle, content)
    receipt = {'coordinates': f'{group}:{name}:{version}', 'bundle': bundle.name,
               'sha256': hashlib.sha256(bundle.read_bytes()).hexdigest(), 'signed_artifacts': 4,
               'signing_fingerprint': args.fingerprint, 'network_actions': 0}
    (output / 'build.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt))


if __name__ == '__main__':
    main()
