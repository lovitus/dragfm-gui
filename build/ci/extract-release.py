#!/usr/bin/env python3
"""Verify every downloaded distribution checksum, then safely extract one."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tarfile
import zipfile

root = Path('release-bundle')
manifest_bytes = (root / 'SHA256SUMS').read_bytes()
listed = {}
for line in manifest_bytes.decode('utf-8').splitlines():
    expected, name = line.split('  ', 1)
    if (Path(name).name != name or name in listed or name == 'SHA256SUMS'
            or not re.fullmatch(r'[0-9a-f]{64}', expected)):
        raise RuntimeError('Unsafe or duplicate checksum entry')
    if (root / name).is_symlink(): raise RuntimeError('Symlink distribution asset')
    listed[name] = expected
    with (root / name).open('rb') as file:
        actual = hashlib.file_digest(file, 'sha256').hexdigest()
    if actual != expected: raise RuntimeError('Distribution checksum mismatch: ' + name)
provenance = json.loads((root / 'PROVENANCE.json').read_text())
if provenance['commit'] != os.environ['GITHUB_SHA']: raise RuntimeError('Source revision mismatch')
platforms = provenance['platforms']
if provenance.get('scope') not in ('macos-arm64-candidate', 'six-platform-release-candidate'):
    raise RuntimeError('Unknown distribution scope')
targets = {(p['os'], p['arch']) for p in platforms}
expected_targets = {('darwin', 'arm64')} if provenance.get('scope') == 'macos-arm64-candidate' else {
    (system, arch) for system in ('darwin', 'linux', 'windows') for arch in ('amd64', 'arm64')}
if targets != expected_targets or len(platforms) != len(targets):
    raise RuntimeError('Distribution platform set does not match its declared scope')
required = {'PROVENANCE.json', 'dragfm-gui-source.tar.gz', 'TEST_EVIDENCE.zip', 'RELEASE_NOTES.md'}
for item in platforms:
    expected_archive = f"dragfm-gui-{item['os']}-{item['arch']}" + ('.zip' if item['os'] == 'windows' else '.tar.gz')
    if item['archive'] != expected_archive:
        raise RuntimeError('Unexpected distribution archive name')
    required.add(expected_archive)
actual_assets = {path.name for path in root.iterdir() if path.name != 'SHA256SUMS'}
if not required.issubset(listed) or set(listed) != actual_assets:
    raise RuntimeError('Distribution checksum manifest is incomplete')
if listed['dragfm-gui-source.tar.gz'] != provenance['source_sha256']:
    raise RuntimeError('Corresponding source checksum differs from provenance')
platform = next(p for p in provenance['platforms'] if p['os'] == os.environ['TARGET_OS'] and p['arch'] == os.environ['TARGET_ARCH'])
destination = Path('unpacked')
destination.mkdir(exist_ok=False)
source = root / platform['archive']
if source.suffix == '.zip':
    with zipfile.ZipFile(source) as archive:
        if len(set(archive.namelist())) != len(archive.namelist()): raise RuntimeError('Duplicate archive member')
        for name in archive.namelist():
            if not name.startswith('dragfm-gui/') or '..' in Path(name).parts or Path(name).is_absolute():
                raise RuntimeError('Unsafe archive member')
        archive.extractall(destination)
else:
    with tarfile.open(source) as archive:
        members = archive.getmembers()
        if len({member.name for member in members}) != len(members): raise RuntimeError('Duplicate archive member')
        if any(not (member.isfile() or member.isdir()) for member in members):
            raise RuntimeError('Distribution contains a non-regular member')
        archive.extractall(destination, filter='data')
binary = destination / 'dragfm-gui' / ('dragfm-gui.exe' if platform['os'] == 'windows' else 'dragfm-gui')
for name in ('LICENSE', 'THIRD_PARTY_NOTICES.md', 'THIRD_PARTY_LICENSES.txt', 'README.md', 'UNFINISHED.md'):
    if not (destination / 'dragfm-gui' / name).is_file() or (destination / 'dragfm-gui' / name).stat().st_size == 0:
        raise RuntimeError('Extracted package is missing a required notice: ' + name)
if platform['os'] != 'windows' and not stat.S_IMODE(binary.stat().st_mode) & 0o111:
    raise RuntimeError('Extracted executable has no executable permission')
with binary.open('rb') as file:
    if hashlib.file_digest(file, 'sha256').hexdigest() != platform['binary_sha256']:
        raise RuntimeError('Extracted executable differs from native-tested bytes')
# Export the exact bytes whose complete asset set was verified above, not a
# later reread. The post-native step receives this fixed workflow output.
if os.environ.get('GITHUB_OUTPUT'):
    with Path(os.environ['GITHUB_OUTPUT']).open('a', encoding='utf-8') as output:
        output.write('manifest_sha256=' + hashlib.sha256(manifest_bytes).hexdigest() + '\n')
print('Verified ' + platform['archive'] + ': ' + platform['binary_sha256'])
