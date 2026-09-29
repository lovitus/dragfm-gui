#!/usr/bin/env python3
"""Packaging E2E on real same-run artifacts; never build or execute the product.

The optional fixed baseline changes only the final recorder in same-event
checkouts. Its validation cases reuse genuine native reports; they do not run
the extracted Mac application on Linux or count as new GUI acceptance.
"""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import zipfile


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--record-baseline')
    args = parser.parse_args()
    if os.environ.get('GITHUB_ACTIONS') != 'true' or not os.environ.get('RUNNER_TEMP'):
        raise RuntimeError('Packaging E2E is restricted to disposable hosted fixtures')
    if args.record_baseline and not re.fullmatch(r'[0-9a-f]{40}', args.record_baseline):
        raise RuntimeError('A fixed full baseline SHA is required')
    root = Path(__file__).resolve().parents[2]
    event = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
    if event != os.environ['GITHUB_SHA']:
        raise RuntimeError('Packaging test checkout is not the actual event source')
    output = root / 'test-results/package-candidate'
    output.mkdir(parents=True, exist_ok=False)
    report = dict(success=False, event_commit=event, record_baseline=args.record_baseline, checks=[], contrasts=[],
                  scope='Real packaging/extraction and final-recorder validation inputs only; extracted GUI execution is a separate native job.')
    baseline = None
    if args.record_baseline:
        baseline = subprocess.check_output(['git', 'show', args.record_baseline + ':build/ci/record-packaged-acceptance.py'], cwd=root)
        report['baseline_script_sha256'] = hashlib.sha256(baseline).hexdigest()
    environment = dict(os.environ, TARGET_OS='darwin', TARGET_ARCH='arm64')
    environment.pop('VERIFIED_MANIFEST_SHA256', None)
    licenses = [path for path in (root / 'frontend/node_modules').rglob('*')
                if path.is_file() and re.search(r'(^|[._-])(licen[cs]e|copying|notice)([._-]|$)', path.name, re.IGNORECASE)]
    if not licenses:
        raise RuntimeError('The real locked frontend license inputs are missing')
    try:
        with tempfile.TemporaryDirectory(prefix='dragfm-package-check-', dir=os.environ['RUNNER_TEMP']) as fixture:
            fixture = Path(fixture).resolve()

            @contextmanager
            def checkout(name, inputs=True):
                directory = fixture / name
                subprocess.run(['git', 'worktree', 'add', '--detach', str(directory), event], cwd=root,
                               check=True, capture_output=True, text=True, timeout=60)
                try:
                    if inputs:
                        shutil.copytree(root / 'release-input', directory / 'release-input')
                        # Copy actual installed license files, not fabricated
                        # dependency or platform evidence. No npm/build rerun.
                        for path in licenses:
                            target = directory / path.relative_to(root)
                            target.parent.mkdir(parents=True, exist_ok=True)
                            shutil.copyfile(path, target)
                    yield directory
                finally:
                    # This checkout was just created below this owned fixture;
                    # never remove the caller's checkout or original artifacts.
                    if directory.parent != fixture or not (directory / '.git').is_file():
                        raise RuntimeError('Packaging fixture ownership is ambiguous')
                    subprocess.run(['git', 'worktree', 'remove', '--force', str(directory)], cwd=root,
                                   check=True, capture_output=True, text=True, timeout=60)

            def run(directory, name, *options, extra_env=None):
                child_environment = dict(environment, GITHUB_OUTPUT=str(directory / 'fixture-github-output'))
                child_environment.update(extra_env or {})
                return subprocess.run([sys.executable, str(directory / 'build/ci' / name), *options],
                                      cwd=directory, env=child_environment, capture_output=True, text=True, timeout=120)

            with checkout('new-packer') as current:
                result = run(current, 'package-release.py', '--platform', 'darwin-arm64')
                if result.returncode:
                    raise RuntimeError('Real Mac-only packaging failed: ' + result.stderr[-1000:])
                bundle = current / 'release-bundle'
                provenance = json.loads((bundle / 'PROVENANCE.json').read_text())
                platform, = provenance['platforms']
                if provenance['scope'] != 'macos-arm64-candidate' or (platform['os'], platform['arch']) != ('darwin', 'arm64'):
                    raise RuntimeError('Selected package contains an unrequested platform')
                actual = current / 'release-input/dragfm-gui-macos-arm64/dragfm-gui-wails-darwin-arm64'
                notices = {'LICENSE', 'THIRD_PARTY_NOTICES.md', 'THIRD_PARTY_LICENSES.txt', 'README.md', 'UNFINISHED.md'}
                with tarfile.open(bundle / platform['archive']) as archive:
                    members = archive.getmembers()
                    files = {member.name for member in members if member.isfile()}
                    if files != {'dragfm-gui/' + name for name in notices | {'dragfm-gui'}}:
                        raise RuntimeError('Actual Mac archive member contract is incomplete')
                    member = archive.getmember('dragfm-gui/dragfm-gui')
                    if not member.mode & 0o111 or hashlib.sha256(archive.extractfile(member).read()).hexdigest() != digest(actual):
                        raise RuntimeError('Archive does not carry the original executable bytes and permissions')
                    licenses_text = archive.extractfile('dragfm-gui/THIRD_PARTY_LICENSES.txt').read()
                    for license_path in ('LICENSE', 'vendor/github.com/flyssh/flyssh/LICENSE',
                                         'third_party/hans-1.7.0/LICENSE', 'frontend/node_modules/react/LICENSE'):
                        # GPL itself is a separate member; other upstream text
                        # must really occur in the aggregated distribution.
                        document = archive.extractfile('dragfm-gui/LICENSE').read() if license_path == 'LICENSE' else licenses_text
                        if (current / license_path).read_bytes() not in document:
                            raise RuntimeError('Actual distribution omitted required license text: ' + license_path)
                if digest(bundle / 'dragfm-gui-source.tar.gz') != provenance['source_sha256']:
                    raise RuntimeError('Distribution source differs from validated source')
                with zipfile.ZipFile(bundle / 'TEST_EVIDENCE.zip') as evidence:
                    if evidence.namelist() != ['VALIDATION.json']:
                        raise RuntimeError('Candidate evidence includes unapproved raw files')
                    summary = json.loads(evidence.read('VALIDATION.json'))
                    if set(summary['ssh']) != {'core', 'protected', 'transports'} or summary['npm_vulnerabilities'] != 0:
                        raise RuntimeError('Candidate evidence omitted a required producer')
                result = run(current, 'extract-release.py')
                extracted = current / 'unpacked/dragfm-gui/dragfm-gui'
                if result.returncode or digest(extracted) != digest(actual) or not stat.S_IMODE(extracted.stat().st_mode) & 0o111:
                    raise RuntimeError('Genuine selected-archive extraction failed')
                exported = (current / 'fixture-github-output').read_text().splitlines()
                verified_manifest = digest(bundle / 'SHA256SUMS')
                if exported != ['manifest_sha256=' + verified_manifest]:
                    raise RuntimeError('Extraction did not export the exact verified manifest')
                report['checks'].append('one real Mac archive; exact source/executable; executable bit; GPL/FlySSH/Hans/frontend notices; bounded evidence; genuine extraction')

                def remove_source_checksum(target):
                    sums = target / 'release-bundle/SHA256SUMS'
                    lines = sums.read_text().splitlines()
                    removed = [line for line in lines if line.split('  ', 1)[1] == 'dragfm-gui-source.tar.gz']
                    if len(removed) != 1:
                        raise RuntimeError('Missing-manifest fixture did not establish its prerequisite')
                    sums.write_text('\n'.join(line for line in lines if line not in removed) + '\n')

                with checkout('incomplete-extraction', inputs=False) as target:
                    shutil.copytree(bundle, target / 'release-bundle')
                    remove_source_checksum(target)
                    result = run(target, 'extract-release.py')
                    if result.returncode == 0 or 'Distribution checksum manifest is incomplete' not in result.stderr:
                        raise RuntimeError('Extractor did not reject the missing source checksum')
                report['checks'].append('missing corresponding-source checksum is rejected, not silently repaired')

                def recorder_inputs(target):
                    shutil.copytree(bundle, target / 'release-bundle')
                    shutil.copytree(current / 'unpacked', target / 'unpacked')
                    native = target / 'test-results/native-macos-arm64'
                    native.mkdir(parents=True)
                    for name in ('BINARY_SHA256.txt', 'exercise.json', 'restore.json', 'changed-key.json',
                                 'FILESYSTEM_VERIFIED.json', 'NORMAL_STARTUP_VAULT.json'):
                        shutil.copyfile(current / 'release-input/dragfm-gui-native-macos-evidence' / name, native / name)

                def recorded(target):
                    receipt = json.loads((target / 'test-results/packaged-acceptance/PACKAGED_ACCEPTANCE.json').read_text())
                    if (receipt.get('success') is not True or receipt.get('commit') != event
                            or receipt.get('binary_sha256') != digest(actual)):
                        raise RuntimeError('Final recorder did not preserve the genuine event and native binary')
                    return receipt

                with checkout('intact-recording', inputs=False) as target:
                    recorder_inputs(target)
                    result = run(target, 'record-packaged-acceptance.py',
                                 extra_env={'VERIFIED_MANIFEST_SHA256': verified_manifest})
                    if result.returncode:
                        raise RuntimeError('Intact final recorder failed: ' + result.stderr[-1000:])
                    if recorded(target).get('verified_manifest_sha256') != verified_manifest:
                        raise RuntimeError('Successful final receipt did not bind the verified manifest')
                report['checks'].append('intact final receipt binds the complete pre-native manifest; genuine native report prerequisites preserved')

                def remove_source_asset(target):
                    remove_source_checksum(target)
                    (target / 'release-bundle/dragfm-gui-source.tar.gz').unlink()

                def rewrite_source_and_manifest(target):
                    changed = target / 'release-bundle'
                    source = changed / 'dragfm-gui-source.tar.gz'
                    with source.open('ab') as stream:
                        stream.write(b'changed-after-native-acceptance')
                    path = changed / 'PROVENANCE.json'
                    changed_provenance = json.loads(path.read_text())
                    changed_provenance['source_sha256'] = digest(source)
                    path.write_text(json.dumps(changed_provenance, indent=2, sort_keys=True) + '\n')
                    sums = changed / 'SHA256SUMS'
                    entries = [line.split('  ', 1) for line in sums.read_text().splitlines()]
                    sums.write_text(''.join(digest(changed / name) + '  ' + name + '\n' for _, name in entries))

                # These are table-driven recorder integrity checks using real
                # same-event inputs, not new product/GUI execution evidence.
                cases = (
                    ('removed-source-checksum', remove_source_checksum, 'Distribution manifest changed during native acceptance'),
                    ('removed-source-asset', remove_source_asset, 'Distribution manifest changed during native acceptance'),
                    ('rewritten-source-and-manifest', rewrite_source_and_manifest, 'Distribution manifest changed during native acceptance'),
                    ('extra-unlisted-asset', lambda target: (target / 'release-bundle/extra.txt').write_text('unlisted\n'),
                     'Distribution asset set changed during native acceptance'),
                )
                for name, mutate, expected in cases:
                    for side in (('old', 'new') if baseline is not None else ('new',)):
                        with checkout(name + '-' + side, inputs=False) as target:
                            recorder_inputs(target)
                            mutate(target)
                            if side == 'old':
                                (target / 'build/ci/record-packaged-acceptance.py').write_bytes(baseline)
                            result = run(target, 'record-packaged-acceptance.py',
                                         extra_env={'VERIFIED_MANIFEST_SHA256': verified_manifest})
                            if side == 'old':
                                if result.returncode:
                                    raise RuntimeError('Old recorder did not establish the designated acceptance flaw: ' + name)
                                recorded(target)
                                report['contrasts'].append(dict(behavior=name,
                                    baseline='red: issued a success receipt after the original distribution set or manifest changed'))
                            elif result.returncode == 0 or expected not in result.stderr:
                                raise RuntimeError('Final recorder did not refuse changed distribution: ' + name)
                            elif (target / 'test-results/packaged-acceptance/PACKAGED_ACCEPTANCE.json').exists():
                                raise RuntimeError('Rejected distribution still received a final acceptance receipt')
                            elif baseline is not None:
                                report['contrasts'][-1]['candidate'] = 'green: refuses with ' + expected
                    report['checks'].append(name + ': final receipt refused')
                with checkout('missing-fixed-manifest', inputs=False) as target:
                    recorder_inputs(target)
                    result = run(target, 'record-packaged-acceptance.py')
                    if result.returncode == 0 or 'Missing fixed verified distribution manifest' not in result.stderr:
                        raise RuntimeError('Recorder accepted an absent extraction baseline')
                report['checks'].append('missing extraction baseline is refused')

            def failure_control(name, mutate, expected):
                with checkout(name) as target:
                    mutate(target)
                    result = run(target, 'package-release.py', *(() if name == 'default-six' else ('--platform', 'darwin-arm64')))
                    if result.returncode == 0 or expected not in result.stderr:
                        raise RuntimeError('Packaging refusal control failed: ' + name)
                    report['checks'].append(name + ': rejected at ' + expected)

            def mix_commit(target):
                (target / 'release-input/dragfm-gui-source/COMMIT.txt').write_text('0' * 40 + '\n')

            def corrupt_binary(target):
                path = target / 'release-input/dragfm-gui-macos-arm64/dragfm-gui-wails-darwin-arm64'
                with path.open('r+b') as binary:
                    first = binary.read(1)
                    if not first: raise RuntimeError('Real binary fixture is empty')
                    binary.seek(0); binary.write(bytes([first[0] ^ 1]))

            def failed_native(target):
                path = target / 'release-input/dragfm-gui-native-macos-evidence/ARTIFACT_RECEIPT.json'
                receipt = json.loads(path.read_text())
                receipt['success'] = False
                path.write_text(json.dumps(receipt) + '\n')

            controls = (
                ('mixed-commit', mix_commit, 'Evidence file checksum mismatch'),
                ('changed-binary', corrupt_binary, 'Evidence file checksum mismatch'),
                ('missing-ssh-shard', lambda target: shutil.rmtree(target / 'release-input/dragfm-gui-ssh-evidence-protected'), 'dragfm-gui-ssh-evidence-protected'),
                ('missing-normal-startup', lambda target: (target / 'release-input/dragfm-gui-native-macos-evidence/NORMAL_STARTUP_VAULT.json').unlink(), 'NORMAL_STARTUP_VAULT.json'),
                ('failed-native-producer', failed_native, 'Incomplete or unrelated evidence receipt'),
                ('missing-gpl-license', lambda target: (target / 'LICENSE').unlink(), 'Required GPL/FlySSH/Hans/frontend license is absent or invalid'),
                ('missing-frontend-license', lambda target: (target / 'frontend/node_modules/react/LICENSE').unlink(), 'Required frontend license is absent: react'),
                ('default-six', lambda target: None, 'dragfm-gui-platform-darwin-amd64/COMMIT.txt'),
            )
            for name, mutate, expected in controls:
                failure_control(name, mutate, expected)
            report['success'] = True
    except Exception as error:
        report['error'] = str(error)
        raise
    finally:
        (output / 'PACKAGING_CHECKS.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
