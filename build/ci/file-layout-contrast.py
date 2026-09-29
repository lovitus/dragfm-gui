#!/usr/bin/env python3
"""One hosted native contrast, withdrawing the known per-row positioning fix.

This is not an unmodified historical release or a synthetic input failure.
The disposable checkout retains the present fixture, navigation and OS input.
Only the translation repaired in 70c9092 is removed; compilation/startup or an
unrelated native failure cannot satisfy the designated behavior red.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def main():
    if os.environ.get('GITHUB_ACTIONS') != 'true' or not os.environ.get('RUNNER_TEMP') or sys.platform != 'darwin':
        raise RuntimeError('Native layout contrast is restricted to disposable hosted Mac runners')
    root = Path(__file__).resolve().parents[2]
    event = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
    if event != os.environ['GITHUB_SHA']:
        raise RuntimeError('Layout contrast checkout is not the actual workflow event')
    output = root / 'test-results/file-layout-contrast'
    output.mkdir(parents=True, exist_ok=False)
    report = dict(success=False, event_commit=event, candidate_sha=os.environ['CANDIDATE_SHA'],
                  workflow_run=os.environ['GITHUB_RUN_ID'], workflow_attempt=os.environ['GITHUB_RUN_ATTEMPT'],
                  scope='Known row-positioning fix withdrawn, with current real native fixture and OS input; not an unmodified old release.')
    try:
        with tempfile.TemporaryDirectory(prefix='dragfm-layout-contrast-', dir=os.environ['RUNNER_TEMP']) as fixture:
            owned = Path(fixture).resolve()
            checkout = owned / 'checkout'
            subprocess.run(['git', 'worktree', 'add', '--detach', str(checkout), event], cwd=root,
                           check=True, capture_output=True, timeout=60)
            try:
                patch = root / 'build/ci/file-row-layout-red.patch'
                report['rollback_patch_sha256'] = hashlib.sha256(patch.read_bytes()).hexdigest()
                subprocess.run(['git', 'apply', str(patch)], cwd=checkout, check=True, capture_output=True, timeout=10)
                with (owned / 'build.log').open('wb') as log:
                    built = subprocess.run(['sh', str(checkout / 'build/build-wails-macos.sh'), str(owned / 'binary')],
                                           cwd=checkout, stdout=log, stderr=subprocess.STDOUT, timeout=900)
                if built.returncode:
                    # Only this public-source build tail, not the native vault
                    # transcript, is needed to distinguish compile from red.
                    report['build_failure'] = (owned / 'build.log').read_text(errors='replace')[-2000:].replace(str(owned), '[owned-fixture]')
                    raise RuntimeError('Row-layout control did not compile; no behavioral red was established')
                binary = owned / 'binary/dragfm-gui-wails-darwin-arm64'
                with binary.open('rb') as stream:
                    report['control_binary_sha256'] = hashlib.file_digest(stream, 'sha256').hexdigest()
                with (owned / 'native.log').open('wb') as log:
                    result = subprocess.run([sys.executable, str(checkout / 'build/ci/native-smoke.py'), str(binary)],
                                            cwd=checkout, stdout=log, stderr=subprocess.STDOUT, timeout=600)
                evidence = checkout / 'test-results/native-macos-arm64'
                native = json.loads((evidence / 'exercise.json').read_text())
                actual = native.get('fileBrowsing', {}).get('left', {})
                report['native_stage'] = native.get('stage')
                report['native_error'] = native.get('error', '')[:1000]
                report['control_file_browsing'] = actual
                report['trusted_wheels'] = native.get('trustedInput', {}).get('wheel', 0)
                report['preceding_checks'] = native.get('checks', [])
                if (result.returncode == 0 or native.get('success') is not False
                        or native.get('stage') != 'left scrolled deep entries are hit-testable'
                        or 'Timeout: left scrolled deep entries are hit-testable' not in native.get('error', '')
                        or actual.get('initialScrollTop') != 0 or not 0 < actual.get('initialRenderedRows', 0) < 192
                        or actual.get('scrollActions', 0) < 1 or actual.get('scrollTop', 0) <= 0
                        or actual.get('reachedDeepBand') is not True or actual.get('deepEntriesRendered') is not True or report['trusted_wheels'] < 1
                        or (evidence / 'exercise-input-errors.json').exists()):
                    raise RuntimeError('Row-layout control did not produce the designated real OS deep-entry failure')
                report['success'] = True
            finally:
                if checkout.parent != owned or not (checkout / '.git').is_file():
                    raise RuntimeError('Layout contrast worktree ownership is ambiguous')
                subprocess.run(['git', 'worktree', 'remove', '--force', str(checkout)], cwd=root,
                               check=True, capture_output=True, timeout=60)
    finally:
        (output / 'FILE_LAYOUT_CONTRAST.json').write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    main()
