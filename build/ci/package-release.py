#!/usr/bin/env python3
"""Package only the binary and source revision whose retained evidence passed."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import zipfile


def sha256(path: Path) -> str:
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def main() -> None:
    root = Path(__file__).resolve().parents[2]
    artifacts = root / 'release-input'
    output = root / 'release-bundle'
    output.mkdir(exist_ok=False)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if revision != os.environ['GITHUB_SHA']:
        raise RuntimeError('Packaging checkout is not the validated event commit')
    source_dir = artifacts / 'dragfm-gui-source'
    if (source_dir / 'COMMIT.txt').read_text().strip() != revision:
        raise RuntimeError('Source archive provenance does not match the tested binary')
    expected_source = (source_dir / 'SHA256SUMS').read_text().split()[0]
    if sha256(source_dir / 'dragfm-gui-source.tar.gz') != expected_source:
        raise RuntimeError('Source archive checksum mismatch')
    test_dir = artifacts / 'dragfm-gui-test-results'
    counts: dict[str, int] = {}
    for name in ('go-unit.jsonl', 'go-race.jsonl'):
        events = [json.loads(line) for line in (test_dir / name).read_text().splitlines()]
        if any(event['Action'] == 'fail' for event in events):
            raise RuntimeError(f'{name} contains failed tests')
        passed = sum(event['Action'] == 'pass' and bool(event.get('Test')) for event in events)
        if passed < 100:
            raise RuntimeError(f'{name} is missing the full regression suite')
        counts[name] = passed
    if json.loads((test_dir / 'npm-audit.json').read_text())['metadata']['vulnerabilities']['total'] != 0:
        raise RuntimeError('Release dependencies still have reported vulnerabilities')
    ssh_dir = artifacts / 'dragfm-gui-ssh-evidence'
    if (ssh_dir / 'COMMIT.txt').read_text().strip() != revision:
        raise RuntimeError('Real SSH test evidence belongs to a different revision')
    native = artifacts / 'dragfm-gui-native-macos-evidence'
    for phase in ('exercise', 'restore', 'changed-key', 'FILESYSTEM_VERIFIED'):
        report = json.loads((native / f'{phase}.json').read_text())
        if report.get('success') is not True:
            raise RuntimeError(f'Native release acceptance failed: {phase}')
    binary = artifacts / 'dragfm-gui-macos-arm64' / 'dragfm-gui-wails-darwin-arm64'
    expected_binary = (native / 'BINARY_SHA256.txt').read_text().split()[0]
    if sha256(binary) != expected_binary:
        raise RuntimeError('Artifact bytes differ from the native-tested executable')
    package = root / 'package-stage' / 'dragfm-gui'
    package.mkdir(parents=True, exist_ok=False)
    shutil.copyfile(binary, package / 'dragfm-gui')
    os.chmod(package / 'dragfm-gui', 0o755)
    for name in ('LICENSE', 'THIRD_PARTY_NOTICES.md'):
        shutil.copyfile(root / name, package / name)
    shutil.copyfile(root / 'docs' / 'RELEASE_NOTES.md', package / 'README.md')
    shutil.copyfile(root / 'docs' / 'UNFINISHED.md', package / 'UNFINISHED.md')
    license_files = []
    for directory in ('vendor', 'frontend/node_modules', 'third_party', 'licenses'):
        for path in (root / directory).rglob('*'):
            if path.is_file() and re.match(r'^(license|copying|notice)', path.name, re.IGNORECASE):
                if path.stat().st_size <= 2 * 1024 * 1024:
                    license_files.append(path)
    with (package / 'THIRD_PARTY_LICENSES.txt').open('w') as document:
        for path in sorted(license_files):
            document.write(f'\n=== {path.relative_to(root)} ===\n')
            document.write(path.read_text(errors='replace'))
            document.write('\n')
    with tarfile.open(output / 'dragfm-gui-darwin-arm64.tar.gz', 'w:gz') as archive:
        archive.add(package, arcname=package.name)
    shutil.copyfile(source_dir / 'dragfm-gui-source.tar.gz', output / 'dragfm-gui-source.tar.gz')
    with zipfile.ZipFile(output / 'TEST_EVIDENCE.zip', 'w', compression=zipfile.ZIP_DEFLATED) as evidence:
        for dirname in ('dragfm-gui-test-results', 'dragfm-gui-ssh-evidence', 'dragfm-gui-native-macos-evidence'):
            for path in sorted((artifacts / dirname).rglob('*')):
                if path.is_file():
                    evidence.write(path, path.relative_to(artifacts))
    provenance = dict(repository=os.environ['GITHUB_REPOSITORY'], commit=revision,
                      workflow_run=os.environ['GITHUB_RUN_ID'],
                      platform='darwin/arm64', binary_sha256=expected_binary,
                      source_sha256=expected_source, regression_passes=counts,
                      native_phases=['exercise', 'restore', 'changed-key'],
                      signing='ad-hoc; not Developer ID signed or notarized',
                      status='development preview; see README.md and UNFINISHED.md')
    (output / 'PROVENANCE.json').write_text(json.dumps(provenance, indent=2) + '\n')
    shutil.copyfile(root / 'docs' / 'RELEASE_NOTES.md', output / 'RELEASE_NOTES.md')
    files = sorted(output.iterdir())
    (output / 'SHA256SUMS').write_text(''.join(f'{sha256(path)}  {path.name}\n' for path in files))
    print(json.dumps(provenance, indent=2))


if __name__ == '__main__':
    main()
