#!/usr/bin/env python3
"""Package only native-tested binaries and their exact corresponding source."""
from __future__ import annotations
import argparse
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
SOURCE_REQUIRED = ('LICENSE', 'THIRD_PARTY_NOTICES.md', 'vendor/github.com/flyssh/flyssh/LICENSE',
                   'third_party/hans-1.7.0/LICENSE', 'third_party/hans-1.7.0/third_party/lwip/COPYING',
                   'third_party/hans-1.7.0/src/sha1_license.txt', 'third_party/hans-1.7.0/src/main.cpp',
                   'third_party/hans-1.7.0/src/client.cpp', 'third_party/hans-1.7.0/src/server.cpp')


def sha256(path: Path) -> str:
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def successful(directory: Path, phases: tuple[str, ...]) -> dict:
    summaries = {}
    for phase in phases:
        report = json.loads((directory / (phase + '.json')).read_text(encoding='utf-8'))
        if report.get('success') is not True:
            raise RuntimeError(f'Native acceptance did not pass: {directory.name}/{phase}')
        # Only these named fields enter the distribution. Never archive logs,
        # terminal tails, config drafts, screenshots or arbitrary report data.
        summaries[phase] = dict(success=True, checks=report.get('checks', []))
    return summaries


def validated(directory: Path, stages: tuple[str, ...], files: tuple[str, ...], identity: dict) -> dict:
    receipt = json.loads((directory / 'ARTIFACT_RECEIPT.json').read_text(encoding='utf-8'))
    if (receipt.get('schema') != 1 or receipt.get('success') is not True
            or any(receipt.get(key) != value for key, value in identity.items())
            or not set(stages).issubset(receipt.get('stages', []))):
        raise RuntimeError(f'Incomplete or unrelated evidence receipt: {directory.name}')
    for name in files:
        if receipt.get('files', {}).get(name) != sha256(directory / name):
            raise RuntimeError(f'Evidence file checksum mismatch: {directory.name}/{name}')
    return {**identity, 'stages': list(stages), 'files': {name: receipt['files'][name] for name in files}}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--platform', choices=('all', 'darwin-arm64'), default='all',
                        help='Default requires all six platforms; explicit Mac mode uses the full native suite.')
    args = parser.parse_args()
    candidate = args.platform == 'darwin-arm64'
    root = Path(__file__).resolve().parents[2]
    artifacts, output = root / 'release-input', root / 'release-bundle'
    output.mkdir(exist_ok=False)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if revision != os.environ['GITHUB_SHA']:
        raise RuntimeError('Packaging checkout is not the validated event commit')
    identity = dict(commit=revision, event_commit=os.environ['GITHUB_SHA'],
                    tree=subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], text=True).strip(),
                    candidate_head=os.environ.get('CANDIDATE_SHA', revision),
                    run=os.environ['GITHUB_RUN_ID'], attempt=os.environ['GITHUB_RUN_ATTEMPT'])
    receipts = {}
    source_dir = artifacts / 'dragfm-gui-source'
    receipts[source_dir.name] = validated(source_dir, ('source',),
        ('COMMIT.txt', 'SHA256SUMS', 'dragfm-gui-source.tar.gz'), identity)
    if (source_dir / 'COMMIT.txt').read_text().strip() != revision:
        raise RuntimeError('Source archive provenance mismatch')
    expected_source = (source_dir / 'SHA256SUMS').read_text().split()[0]
    if sha256(source_dir / 'dragfm-gui-source.tar.gz') != expected_source:
        raise RuntimeError('Source archive checksum mismatch')
    with tarfile.open(source_dir / 'dragfm-gui-source.tar.gz') as archive:
        for name in SOURCE_REQUIRED:
            if not archive.getmember('dragfm-gui/' + name).isfile():
                raise RuntimeError('Required corresponding source or license is absent: ' + name)
    test_dir = artifacts / 'dragfm-gui-test-results'
    receipts[test_dir.name] = validated(test_dir,
        ('frontend', 'frontend-production', 'go-unit', 'go-race', 'sudo-race', 'vet'),
        ('frontend.log', 'go-unit.jsonl', 'go-race.jsonl', 'sudo-race-repeat.jsonl', 'npm-audit.json'), identity)
    counts = {}
    test_summaries = {}
    for name in ('go-unit.jsonl', 'go-race.jsonl'):
        events = [json.loads(line) for line in (test_dir / name).read_text().splitlines()]
        if any(event['Action'] == 'fail' for event in events):
            raise RuntimeError(f'{name} contains failed tests')
        started = {event['Package'] for event in events if event['Action'] == 'start' and not event.get('Test')}
        ended = {event['Package'] for event in events if event['Action'] in ('pass', 'skip') and not event.get('Test')}
        if not started or started != ended:
            raise RuntimeError(f'{name} has no complete package results')
        passed = sum(event['Action'] == 'pass' and bool(event.get('Test')) for event in events)
        if passed < 100: raise RuntimeError(f'{name} is not a full regression run')
        counts[name] = passed
        test_summaries[name] = dict(passed_tests=passed, completed_packages=sorted(ended))
    if json.loads((test_dir / 'npm-audit.json').read_text())['metadata']['vulnerabilities']['total']:
        raise RuntimeError('Reported frontend vulnerabilities remain')
    # These are the three independent jobs in ssh-integration.yml, not three
    # interchangeable copies of one report. Never accept a partial download or
    # a green shard belonging to another revision.
    ssh_required = {
        'core': ('TestHostedSSHCommandCancellationLatency', 'TestHostedSSHQueueAndHistory'),
        'protected': ('TestHostedDualProtectedQueuedTransfer', 'TestHostedExplicitRootVaultKeyQueuedTransfer'),
        'transports': ('TestHostedReviewedTransportMatrix', 'TestHostedReviewedHansRolesAndMethods',
                       'TestHostedReviewedRouteFailureRecovery', 'TestHostedReviewedFailedRelayCacheReprobes'),
    }
    ssh_summaries = {}
    for group, required in ssh_required.items():
        directory = artifacts / f'dragfm-gui-ssh-evidence-{group}'
        receipts[directory.name] = validated(directory, ('ssh-' + group,),
            ('COMMIT.txt', 'ssh-integration.jsonl'), identity)
        if (directory / 'COMMIT.txt').read_text().strip() != revision:
            raise RuntimeError(f'SSH {group} evidence belongs to a different revision')
        events = [json.loads(line) for line in (directory / 'ssh-integration.jsonl').read_text().splitlines() if line.startswith('{')]
        if any(event.get('Action') == 'fail' for event in events):
            raise RuntimeError(f'SSH {group} evidence contains failures')
        if not any(event.get('Action') == 'pass' and not event.get('Test') and
                   event.get('Package') == 'github.com/lovitus/dragfm-gui/internal/webgui' for event in events):
            raise RuntimeError(f'SSH {group} evidence has no successful package completion')
        for name in required:
            if not any(event.get('Action') == 'pass' and event.get('Test') == name for event in events):
                raise RuntimeError(f'Missing successful real SSH test in {group}: {name}')
        ssh_summaries[group] = dict(success=True, passed_tests=sorted({
            event['Test'] for event in events if event.get('Action') == 'pass' and event.get('Test')}))
    native = artifacts / 'dragfm-gui-native-macos-evidence'
    native_phases = ('exercise', 'restore', 'changed-key', 'FILESYSTEM_VERIFIED', 'NORMAL_STARTUP_VAULT')
    receipts[native.name] = validated(native, ('macos-native',),
        ('COMMIT.txt', 'BINARY_SHA256.txt', *(phase + '.json' for phase in native_phases)), identity)
    native_summaries = successful(native, native_phases)
    binary_dir = artifacts / 'dragfm-gui-macos-arm64'
    receipts[binary_dir.name] = validated(binary_dir, ('macos-arm64-build', 'macos-native'),
        ('COMMIT.txt', 'dragfm-gui-wails-darwin-arm64', 'SHA256SUMS.wails'), identity)
    full_native_binary = binary_dir / 'dragfm-gui-wails-darwin-arm64'
    full_native_hash = (native / 'BINARY_SHA256.txt').read_text().split()[0]
    binary_sums = (binary_dir / 'SHA256SUMS.wails').read_text().splitlines()
    if (sha256(full_native_binary) != full_native_hash
            or binary_sums != [full_native_hash + '  ' + full_native_binary.name]):
        raise RuntimeError('Full native Mac binary/SHA256SUMS mismatch')
    notices = root / 'package-notices'
    notices.mkdir(exist_ok=False)
    license_name = re.compile(r'(^|[._-])(licen[cs]e|copying|notice)([._-]|$)', re.IGNORECASE)
    required_licenses = [root / name for name in SOURCE_REQUIRED if license_name.search(Path(name).name)
                         or name == 'THIRD_PARTY_NOTICES.md']
    for module in json.loads((root / 'frontend/package.json').read_text())['dependencies']:
        directory = root / 'frontend/node_modules' / module
        found = [path for path in directory.glob('*') if path.is_file()
                 and license_name.search(path.name)]
        if not found:
            raise RuntimeError('Required frontend license is absent: ' + module)
        required_licenses.extend(found)
    if any(not path.is_file() or not 0 < path.stat().st_size <= 2*1024*1024 for path in required_licenses):
        raise RuntimeError('Required GPL/FlySSH/Hans/frontend license is absent or invalid')
    for name in ('LICENSE', 'THIRD_PARTY_NOTICES.md'):
        shutil.copyfile(root / name, notices / name)
    shutil.copyfile(root / 'docs/RELEASE_NOTES.md', notices / 'README.md')
    shutil.copyfile(root / 'docs/UNFINISHED.md', notices / 'UNFINISHED.md')
    licenses = []
    for directory in ('vendor', 'frontend/node_modules', 'third_party', 'licenses'):
        for path in (root / directory).rglob('*'):
            if path.is_file() and license_name.search(path.name) and path.stat().st_size <= 2*1024*1024:
                licenses.append(path)
    with (notices / 'THIRD_PARTY_LICENSES.txt').open('w', encoding='utf-8') as document:
        for path in sorted(licenses):
            document.write(f'\n=== {path.relative_to(root)} ===\n')
            document.write(path.read_text(encoding='utf-8', errors='replace') + '\n')
    platforms = []
    platform_summaries = {}
    for osname, arch in ([('darwin', 'arm64')] if candidate else PLATFORMS):
        binary_name = 'dragfm-gui.exe' if osname == 'windows' else 'dragfm-gui'
        if not candidate:
            platform = artifacts / f'dragfm-gui-platform-{osname}-{arch}'
            if (platform / 'COMMIT.txt').read_text().strip() != revision:
                raise RuntimeError(f'{osname}/{arch}: wrong source revision')
            platform_summaries[osname + '-' + arch] = successful(platform / 'evidence', ('exercise', 'restore', 'FILESYSTEM_VERIFIED'))
            tested = platform / binary_name
            expected = (platform / 'evidence/BINARY_SHA256.txt').read_text().split()[0]
            if sha256(tested) != expected: raise RuntimeError(f'{osname}/{arch}: platform-tested bytes differ')
        # The arm64 Mac distribution is the executable that also passed the
        # larger SSH/sudo/restart/key-change native suite, not a substitute.
        if (osname, arch) == ('darwin', 'arm64'):
            tested, expected = full_native_binary, full_native_hash
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
        summary = dict(receipts=receipts, regression=test_summaries, ssh=ssh_summaries,
                       full_native=native_summaries, platforms=platform_summaries, npm_vulnerabilities=0,
                       boundary='Named completion fields and hashes only; no raw logs or private material.')
        archive.writestr('VALIDATION.json', json.dumps(summary, indent=2, sort_keys=True) + '\n')
    provenance = dict(repository=os.environ['GITHUB_REPOSITORY'], commit=revision,
                      workflow_run=os.environ['GITHUB_RUN_ID'], source_sha256=expected_source,
                      regression_passes=counts, platforms=platforms,
                      signing='macOS ad-hoc; no Developer ID/notarization or Windows publisher certificate',
                      event_commit=identity['event_commit'], candidate_head=identity['candidate_head'],
                      tree=identity['tree'], workflow_attempt=identity['attempt'],
                      scope='macos-arm64-candidate' if candidate else 'six-platform-release-candidate',
                      status='unpublished; awaiting extracted-package verification and full requirements acceptance')
    (output / 'PROVENANCE.json').write_text(json.dumps(provenance, indent=2) + '\n')
    shutil.copyfile(root / 'docs/RELEASE_NOTES.md', output / 'RELEASE_NOTES.md')
    files = sorted(output.iterdir())
    (output / 'SHA256SUMS').write_text(''.join(f'{sha256(path)}  {path.name}\n' for path in files))
    print(json.dumps(provenance, indent=2))


if __name__ == '__main__':
    main()
