#!/usr/bin/env python3
"""Run unchanged, pinned FlySSH default-transfer tests on the actual GUI vendor.

The public upstream tests are existing compatibility contracts, not newly
invented red/green tests or proof that an external CLI executable was run.
Only test/support files and test-package metadata enter the disposable tree;
the SDK implementation and the application's locked dependencies stay exact.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
from urllib.request import urlopen


UPSTREAM = 'f61415b692c4f27ea774059f1404b806073c489d'
TEST_FILES = {
    'pkg/transfer/spec_test.go': '15b139f595153fef10af15652b73f2cd653b9750ecd3859d13db30016538de90',
    'pkg/transfer/scp_integration_test.go': 'b09426791b316d80efb7e36737705d0718256df5ccbbb9ed2f3e46224ab23f08',
    'internal/testkit/netkit.go': '006c0efb0901aeda24ca1fc1a2983747474bc084a619cdc7502e3cae8257190b',
}


def implementation(directory):
    return {str(path.relative_to(directory)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted((directory / 'pkg').rglob('*.go')) if not path.name.endswith('_test.go')}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--directory-contrast', action='store_true')
    args = parser.parse_args()
    if os.environ.get('GITHUB_ACTIONS') != 'true' or not os.environ.get('RUNNER_TEMP'):
        raise RuntimeError('FlySSH compatibility is restricted to disposable hosted runners')
    root = Path(__file__).resolve().parents[2]
    event = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
    if event != os.environ['GITHUB_SHA']:
        raise RuntimeError('Compatibility checkout is not the actual workflow event')
    output = root / 'test-results'
    report = dict(success=False, event_commit=event, candidate_sha=os.environ['CANDIDATE_SHA'],
                  upstream_commit=UPSTREAM, upstream_test_sha256=TEST_FILES,
                  scope='Existing pinned FromOptions and public Run/SCP contracts on exact current vendor, including the no-p file umask fix; no GUI prefix/timeout and no external CLI executable claim.')
    original = implementation(root / 'vendor/github.com/flyssh/flyssh')
    report['vendor_implementation_sha256'] = original
    try:
        with tempfile.TemporaryDirectory(prefix='dragfm-flyssh-compat-', dir=os.environ['RUNNER_TEMP']) as fixture:
            owned = Path(fixture).resolve()
            checkout = owned / 'checkout'
            subprocess.run(['git', 'worktree', 'add', '--detach', str(checkout), event], cwd=root,
                           check=True, capture_output=True, timeout=60)
            try:
                sdk = checkout / 'vendor/github.com/flyssh/flyssh'
                for name, expected in TEST_FILES.items():
                    # Fixed full commit plus exact test bytes. No latest-version
                    # lookup, retries, fixture substitution or edited expected values.
                    with urlopen(f'https://raw.githubusercontent.com/lovitus/flyssh/{UPSTREAM}/{name}', timeout=25) as response:
                        data = response.read(256 * 1024 + 1)
                    if len(data) > 256 * 1024 or hashlib.sha256(data).hexdigest() != expected:
                        raise RuntimeError('Pinned upstream contract file differs: ' + name)
                    destination = sdk / name
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    with destination.open('xb') as stream:
                        stream.write(data)
                driver = (root / 'build/ci/flyssh-default-directory_test.go.txt').read_bytes()
                report['default_directory_driver_sha256'] = hashlib.sha256(driver).hexdigest()
                with (sdk / 'pkg/transfer/default_directory_test.go').open('xb') as stream:
                    stream.write(driver)
                # Go normally omits dependency test/support packages when
                # vendoring. Add only this test support package's metadata,
                # never regenerate/upgrade or substitute the dependency set.
                modules = checkout / 'vendor/modules.txt'
                text = modules.read_text()
                anchor = 'github.com/flyssh/flyssh/pkg/auth\n'
                if text.count(anchor) != 1:
                    raise RuntimeError('Pinned vendor metadata is ambiguous')
                modules.write_text(text.replace(anchor, 'github.com/flyssh/flyssh/internal/testkit\n' + anchor))
                if implementation(sdk) != original:
                    raise RuntimeError('Compatibility setup changed the SDK implementation')
                if args.directory_contrast:
                    # One regression-sensitivity check: withdraw only the
                    # known temporary owner-write mkdir fix. Keep current
                    # framing/error propagation and the exact same driver.
                    scp = sdk / 'pkg/transfer/scp.go'
                    present = scp.read_bytes()
                    patch = root / 'build/ci/scp-owner-write-red.patch'
                    try:
                        subprocess.run(['git', 'apply', '--unidiff-zero', str(patch)], cwd=checkout, check=True, capture_output=True, timeout=10)
                        old_hash = hashlib.sha256(scp.read_bytes()).hexdigest()
                        with (owned / 'old-directory.jsonl').open('wb') as log:
                            old = subprocess.run(['go', 'test', '-mod=vendor', '-count=1', '-timeout=90s', '-json',
                                                  'github.com/flyssh/flyssh/pkg/transfer',
                                                  '-run', '^TestSCPDownloadDefaultDirectoryPermissions$'],
                                                 cwd=checkout, stdout=log, stderr=subprocess.STDOUT, timeout=120)
                        old_events = [json.loads(line) for line in (owned / 'old-directory.jsonl').read_text().splitlines() if line.startswith('{')]
                        old_output = ''.join(item.get('Output', '') for item in old_events)
                        old_failures = {item['Test'] for item in old_events if item.get('Action') == 'fail' and item.get('Test')}
                        if (old.returncode == 0 or old_failures != {'TestSCPDownloadDefaultDirectoryPermissions'}
                                or 'DEFAULT_SCP_NEW_PERMISSION_FAILURE' not in old_output
                                or 'DEFAULT_SCP_EXISTING_OK' not in old_output
                                or 'DEFAULT_SCP_NEW_OK' in old_output
                                or any(item.get('Action') == 'skip' for item in old_events)):
                            report['contrast_failure_output'] = (owned / 'old-directory.jsonl').read_text(errors='replace')[-2000:].replace(str(owned), '[owned-fixture]')
                            raise RuntimeError('Default SCP rollback did not produce the designated real permission red and existing-directory control')
                        report['directory_contrast'] = dict(old_red=True, existing_directory_control=True,
                            rollback_scp_sha256=old_hash, rollback_patch_sha256=hashlib.sha256(patch.read_bytes()).hexdigest(),
                            scope='Only known owner-write mkdir behavior withdrawn; not an unmodified historical SDK or CLI executable.')
                    finally:
                        scp.write_bytes(present)
                    if implementation(sdk) != original:
                        raise RuntimeError('Compatibility setup did not restore the exact current SDK')
                    patch = root / 'build/ci/scp-file-umask-red.patch'
                    try:
                        # This restores the actual pre-fix unconditional chmod
                        # behavior without changing source readability, transfer
                        # completion, directory modes, umask or driver assertions.
                        subprocess.run(['git', 'apply', '--unidiff-zero', str(patch)], cwd=checkout, check=True, capture_output=True, timeout=10)
                        old_hash = hashlib.sha256(scp.read_bytes()).hexdigest()
                        with (owned / 'old-file-umask.jsonl').open('wb') as log:
                            old = subprocess.run(['go', 'test', '-mod=vendor', '-count=1', '-timeout=90s', '-json',
                                                  'github.com/flyssh/flyssh/pkg/transfer',
                                                  '-run', '^TestSCPDownloadDefaultDirectoryPermissions$'],
                                                 cwd=checkout, stdout=log, stderr=subprocess.STDOUT, timeout=120)
                        old_events = [json.loads(line) for line in (owned / 'old-file-umask.jsonl').read_text().splitlines() if line.startswith('{')]
                        old_output = ''.join(item.get('Output', '') for item in old_events)
                        old_failures = {item['Test'] for item in old_events if item.get('Action') == 'fail' and item.get('Test')}
                        if (old.returncode == 0 or old_failures != {'TestSCPDownloadDefaultDirectoryPermissions'}
                                or 'DEFAULT_SCP_FILE_UMASK_FAILURE_new' not in old_output
                                or 'DEFAULT_SCP_FILE_UMASK_FAILURE_existing' not in old_output
                                or 'DEFAULT_SCP_NEW_PERMISSION_FAILURE' in old_output
                                or any(item.get('Action') == 'skip' for item in old_events)):
                            report['umask_contrast_failure_output'] = (owned / 'old-file-umask.jsonl').read_text(errors='replace')[-2000:].replace(str(owned), '[owned-fixture]')
                            raise RuntimeError('Default SCP file rollback did not produce both designated actual permission-broadening failures')
                        report['file_umask_contrast'] = dict(old_red=True, copied_content_and_directory_modes_verified=True,
                            rollback_scp_sha256=old_hash, rollback_patch_sha256=hashlib.sha256(patch.read_bytes()).hexdigest(),
                            scope='Actual pre-fix unconditional chmod behavior restored; not an unmodified historical SDK or CLI executable.')
                    finally:
                        scp.write_bytes(present)
                    if implementation(sdk) != original:
                        raise RuntimeError('File umask contrast did not restore the exact current SDK')
                with (owned / 'go.jsonl').open('wb') as log:
                    result = subprocess.run(['go', 'test', '-mod=vendor', '-count=1', '-timeout=90s', '-json',
                                             'github.com/flyssh/flyssh/pkg/transfer',
                                             '-run', '^Test(FromOptions_|SCP|IsTransferSuccess|SanitizeTransferStatusPath)'],
                                            cwd=checkout, stdout=log, stderr=subprocess.STDOUT, timeout=240)
                events = [json.loads(line) for line in (owned / 'go.jsonl').read_text().splitlines() if line.startswith('{')]
                passed = sorted({item['Test'] for item in events if item.get('Action') == 'pass' and item.get('Test')})
                report['passed_tests'] = passed
                report['failed_tests'] = sorted({item['Test'] for item in events if item.get('Action') == 'fail' and item.get('Test')})
                required = {'TestFromOptions_RsyncUploadParsesFlagsAndOperands', 'TestFromOptions_SCPMultiSourceAndDoubleDash',
                            'TestSCPUploadFile', 'TestSCPDownloadFileToDirectory', 'TestSCPUploadRecursive',
                            'TestSCPDownloadRecursivePreserveMode', 'TestSCPPreserveMode',
                            'TestSCPDownloadDefaultDirectoryPermissions'}
                if (result.returncode or any(item.get('Action') in ('fail', 'skip') for item in events)
                        or not required.issubset(passed)
                        or not any(item.get('Action') == 'pass' and not item.get('Test') for item in events)):
                    report['failure_output'] = (owned / 'go.jsonl').read_text(errors='replace')[-2000:].replace(str(owned), '[owned-fixture]')
                    raise RuntimeError('Pinned default CLI/library contract did not pass on the current vendor')
                if implementation(sdk) != original:
                    raise RuntimeError('Compatibility execution changed the SDK implementation')
                if args.directory_contrast:
                    report['directory_contrast']['current_green'] = True
                    report['file_umask_contrast']['current_green'] = True
                report['success'] = True
            finally:
                if checkout.parent != owned or not (checkout / '.git').is_file():
                    raise RuntimeError('Compatibility worktree ownership is ambiguous')
                subprocess.run(['git', 'worktree', 'remove', '--force', str(checkout)], cwd=root,
                               check=True, capture_output=True, timeout=60)
    finally:
        (output / 'FLYSSH_COMPATIBILITY.json').write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    main()
