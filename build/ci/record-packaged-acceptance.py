#!/usr/bin/env python3
"""Record actual extracted Mac acceptance without modifying the product archive."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def main():
    if os.environ.get('GITHUB_ACTIONS') != 'true':
        raise RuntimeError('Packaged acceptance requires the disposable hosted runner')
    root = Path(__file__).resolve().parents[2]
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if revision != os.environ['GITHUB_SHA']:
        raise RuntimeError('Extracted acceptance checkout differs from event source')
    bundle = root / 'release-bundle'
    provenance = json.loads((bundle / 'PROVENANCE.json').read_text())
    if (provenance['commit'] != revision or provenance.get('scope') != 'macos-arm64-candidate'
            or provenance.get('candidate_head') != os.environ.get('CANDIDATE_SHA', revision)
            or provenance.get('tree') != subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], text=True).strip()):
        raise RuntimeError('Unexpected candidate scope or revision')
    platform, = provenance['platforms']
    if (platform['os'], platform['arch']) != ('darwin', 'arm64'):
        raise RuntimeError('Extracted acceptance requires the single selected Mac target')
    verified_manifest = os.environ.get('VERIFIED_MANIFEST_SHA256', '')
    if not re.fullmatch(r'[0-9a-f]{64}', verified_manifest):
        raise RuntimeError('Missing fixed verified distribution manifest')
    manifest_bytes = (bundle / 'SHA256SUMS').read_bytes()
    if hashlib.sha256(manifest_bytes).hexdigest() != verified_manifest:
        raise RuntimeError('Distribution manifest changed during native acceptance')
    lines = manifest_bytes.decode('utf-8').splitlines()
    sums = dict((name, expected) for expected, name in (line.split('  ', 1) for line in lines))
    actual_assets = {path.name for path in bundle.iterdir() if path.name != 'SHA256SUMS'}
    if len(sums) != len(lines) or set(sums) != actual_assets:
        raise RuntimeError('Distribution asset set changed during native acceptance')
    # Check all assets again after native execution; no re-signing, chmod or
    # repackaging is performed here or by the extraction job.
    if any(digest(bundle / name) != expected for name, expected in sums.items()):
        raise RuntimeError('Distribution bytes changed during native acceptance')
    binary = root / 'unpacked/dragfm-gui/dragfm-gui'
    if digest(binary) != platform['binary_sha256'] or not stat.S_IMODE(binary.stat().st_mode) & 0o111:
        raise RuntimeError('Extracted executable changed or lost executable permission')
    native = root / 'test-results/native-macos-arm64'
    if (native / 'BINARY_SHA256.txt').read_text().split()[0] != platform['binary_sha256']:
        raise RuntimeError('Native acceptance ran another executable')
    reports = {}
    for phase in ('exercise', 'restore', 'changed-key', 'FILESYSTEM_VERIFIED', 'NORMAL_STARTUP_VAULT'):
        report = json.loads((native / (phase + '.json')).read_text())
        if report.get('success') is not True:
            raise RuntimeError('Extracted native acceptance failed: ' + phase)
        reports[phase] = dict(success=True, checks=report.get('checks', []))
    output = root / 'test-results/packaged-acceptance'
    output.mkdir(parents=True, exist_ok=False)
    receipt = dict(schema=1, success=True, commit=revision, candidate_head=provenance['candidate_head'],
                   tree=provenance['tree'], run=os.environ['GITHUB_RUN_ID'], attempt=os.environ['GITHUB_RUN_ATTEMPT'],
                   archive=platform['archive'], archive_sha256=sums[platform['archive']],
                   binary_sha256=platform['binary_sha256'], source_sha256=provenance['source_sha256'],
                   verified_manifest_sha256=verified_manifest,
                   native=reports, scope='extracted macOS arm64 technical acceptance only; not user acceptance or publication')
    (output / 'PACKAGED_ACCEPTANCE.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
