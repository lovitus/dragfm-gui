#!/usr/bin/env python3
"""Verify every downloaded distribution checksum, then safely extract one."""
import hashlib
import json
import os
from pathlib import Path
import tarfile
import zipfile

root = Path('release-bundle')
for line in (root / 'SHA256SUMS').read_text().splitlines():
    expected, name = line.split('  ', 1)
    if Path(name).name != name: raise RuntimeError('Unsafe checksum entry')
    with (root / name).open('rb') as file:
        actual = hashlib.file_digest(file, 'sha256').hexdigest()
    if actual != expected: raise RuntimeError('Distribution checksum mismatch: ' + name)
provenance = json.loads((root / 'PROVENANCE.json').read_text())
if provenance['commit'] != os.environ['GITHUB_SHA']: raise RuntimeError('Source revision mismatch')
platform = next(p for p in provenance['platforms'] if p['os'] == os.environ['TARGET_OS'] and p['arch'] == os.environ['TARGET_ARCH'])
destination = Path('unpacked')
destination.mkdir(exist_ok=False)
source = root / platform['archive']
if source.suffix == '.zip':
    with zipfile.ZipFile(source) as archive:
        for name in archive.namelist():
            if not name.startswith('dragfm-gui/') or '..' in Path(name).parts or Path(name).is_absolute():
                raise RuntimeError('Unsafe archive member')
        archive.extractall(destination)
else:
    with tarfile.open(source) as archive: archive.extractall(destination, filter='data')
binary = destination / 'dragfm-gui' / ('dragfm-gui.exe' if platform['os'] == 'windows' else 'dragfm-gui')
with binary.open('rb') as file:
    if hashlib.file_digest(file, 'sha256').hexdigest() != platform['binary_sha256']:
        raise RuntimeError('Extracted executable differs from native-tested bytes')
print('Verified ' + platform['archive'] + ': ' + platform['binary_sha256'])
