#!/usr/bin/env python3
"""Package only native-tested binaries and their exact corresponding source."""
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

PLATFORMS = [(osname, arch) for osname in ('darwin', 'linux', 'windows') for arch in ('amd64', 'arm64')]


def sha256(path: Path) -> str:
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def successful(directory: Path, phases: tuple[str, ...]) -> None:
    for phase in phases:
        report = json.loads((directory / (phase + '.json')).read_text(encoding='utf-8'))
        if report.get('success') is not True:
            raise RuntimeError(f'Native acceptance did not pass: {directory.name}/{phase}')


def main() -> None:
    root = Path(__file__).resolve().parents[2]
    artifacts, output = root / 'release-input', root / 'release-bundle'
    output.mkdir(exist_ok=False)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if revision != os.environ['GITHUB_SHA']:
        raise RuntimeError('Packaging checkout is not the validated event commit')
    source_dir = artifacts / 'dragfm-gui-source'
    if (source_dir / 'COMMIT.txt').read_text().strip() != revision:
        raise RuntimeError('Source archive provenance mismatch')
    expected_source = (source_dir / 'SHA256SUMS').read_text().split()[0]
    if sha256(source_dir / 'dragfm-gui-source.tar.gz') != expected_source:
        raise RuntimeError('Source archive checksum mismatch')
    test_dir = artifacts / 'dragfm-gui-test-results'
    counts = {}
    for name in ('go-unit.jsonl', 'go-race.jsonl'):
        events = [json.loads(line) for line in (test_dir / name).read_text().splitlines()]
        if any(event['Action'] == 'fail' for event in events):
            raise RuntimeError(f'{name} contains failed tests')
        passed = sum(event['Action'] == 'pass' and bool(event.get('Test')) for event in events)
        if passed < 100: raise RuntimeError(f'{name} is not a full regression run')
        counts[name] = passed
    if json.loads((test_dir / 'npm-audit.json').read_text())['metadata']['vulnerabilities']['total']:
        raise RuntimeError('Reported frontend vulnerabilities remain')
    ssh_dir = artifacts / 'dragfm-gui-ssh-evidence'
    if (ssh_dir / 'COMMIT.txt').read_text().strip() != revision:
        raise RuntimeError('SSH evidence belongs to a different revision')
    ssh_events = [json.loads(line) for line in (ssh_dir / 'ssh-integration.jsonl').read_text().splitlines() if line.startswith('{')]
    if any(event.get('Action') == 'fail' for event in ssh_events): raise RuntimeError('SSH evidence contains failures')
    for name in ('TestHostedReviewedTransportMatrix', 'TestHostedReviewedHansRolesAndMethods', 'TestHostedReviewedRouteFailureRecovery', 'TestHostedReviewedFailedRelayCacheReprobes', 'TestHostedSSHCommandCancellationLatency'):
        if not any(e.get('Action') == 'pass' and e.get('Test') == name for e in ssh_events):
            raise RuntimeError(f'Missing successful real SSH test: {name}')
    native = artifacts / 'dragfm-gui-native-macos-evidence'
    successful(native, ('exercise', 'restore', 'changed-key', 'FILESYSTEM_VERIFIED'))
    notices = root / 'package-notices'
    notices.mkdir(exist_ok=False)
    for name in ('LICENSE', 'THIRD_PARTY_NOTICES.md'):
        shutil.copyfile(root / name, notices / name)
    shutil.copyfile(root / 'docs/RELEASE_NOTES.md', notices / 'README.md')
    shutil.copyfile(root / 'docs/UNFINISHED.md', notices / 'UNFINISHED.md')
    licenses = []
    for directory in ('vendor', 'frontend/node_modules', 'third_party', 'licenses'):
        for path in (root / directory).rglob('*'):
            if path.is_file() and re.match(r'^(license|copying|notice)', path.name, re.IGNORECASE) and path.stat().st_size <= 2*1024*1024:
                licenses.append(path)
    with (notices / 'THIRD_PARTY_LICENSES.txt').open('w', encoding='utf-8') as document:
        for path in sorted(licenses):
            document.write(f'\n=== {path.relative_to(root)} ===\n')
            document.write(path.read_text(encoding='utf-8', errors='replace') + '\n')
    platforms = []
    evidence_dirs = [test_dir, ssh_dir, native]
    for osname, arch in PLATFORMS:
        platform = artifacts / f'dragfm-gui-platform-{osname}-{arch}'
        if (platform / 'COMMIT.txt').read_text().strip() != revision:
            raise RuntimeError(f'{osname}/{arch}: wrong source revision')
        successful(platform / 'evidence', ('exercise', 'restore', 'FILESYSTEM_VERIFIED'))
        binary_name = 'dragfm-gui.exe' if osname == 'windows' else 'dragfm-gui'
        tested = platform / binary_name
        expected = (platform / 'evidence/BINARY_SHA256.txt').read_text().split()[0]
        if sha256(tested) != expected: raise RuntimeError(f'{osname}/{arch}: platform-tested bytes differ')
        evidence_dirs.append(platform / 'evidence')
        # The arm64 Mac distribution is the executable that also passed the
        # larger SSH/sudo/restart/key-change native suite, not a substitute.
        if (osname, arch) == ('darwin', 'arm64'):
            tested = artifacts / 'dragfm-gui-macos-arm64/dragfm-gui-wails-darwin-arm64'
            expected = (native / 'BINARY_SHA256.txt').read_text().split()[0]
            if sha256(tested) != expected: raise RuntimeError('Full native SSH binary mismatch')
        stage = root / 'package-stage' / (osname + '-' + arch) / 'dragfm-gui'
        shutil.copytree(notices, stage)
        shutil.copyfile(tested, stage / binary_name)
        os.chmod(stage / binary_name, 0o755)
        basename = f'dragfm-gui-{osname}-{arch}'
        if osname == 'windows':
            archive_path = output / (basename + '.zip')
            with zipfile.ZipFile(archive_path, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
                for path in sorted(stage.iterdir()): archive.write(path, 'dragfm-gui/' + path.name)
        else:
            archive_path = output / (basename + '.tar.gz')
            with tarfile.open(archive_path, 'w:gz') as archive: archive.add(stage, arcname='dragfm-gui')
        platforms.append(dict(os=osname, arch=arch, archive=archive_path.name, binary_sha256=expected))
    shutil.copyfile(source_dir / 'dragfm-gui-source.tar.gz', output / 'dragfm-gui-source.tar.gz')
    with zipfile.ZipFile(output / 'TEST_EVIDENCE.zip', 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for directory in evidence_dirs:
            for path in sorted(directory.rglob('*')):
                if path.is_file(): archive.write(path, path.relative_to(artifacts))
    provenance = dict(repository=os.environ['GITHUB_REPOSITORY'], commit=revision,
                      workflow_run=os.environ['GITHUB_RUN_ID'], source_sha256=expected_source,
                      regression_passes=counts, platforms=platforms,
                      signing='macOS ad-hoc; no Developer ID/notarization or Windows publisher certificate',
                      status='public release candidate; see release notes for runtime and acceptance boundaries')
    (output / 'PROVENANCE.json').write_text(json.dumps(provenance, indent=2) + '\n')
    shutil.copyfile(root / 'docs/RELEASE_NOTES.md', output / 'RELEASE_NOTES.md')
    files = sorted(output.iterdir())
    (output / 'SHA256SUMS').write_text(''.join(f'{sha256(path)}  {path.name}\n' for path in files))
    print(json.dumps(provenance, indent=2))


if __name__ == '__main__':
    main()
