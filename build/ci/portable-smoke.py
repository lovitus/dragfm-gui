#!/usr/bin/env python3
"""Run the exact production executable on its native hosted platform.

Local filesystem acceptance is independent of the full macOS SSH suite and the
Linux remote-transport matrix. Each evidence directory records its binary hash.
"""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time


def digest(path: Path) -> str:
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()


def main() -> None:
    if os.environ.get('GITHUB_ACTIONS') != 'true' or not os.environ.get('RUNNER_TEMP'):
        raise RuntimeError('Only disposable GitHub-hosted runner fixtures are supported')
    binary = Path(sys.argv[1]).resolve(strict=True)
    project = Path(__file__).resolve().parents[2]
    evidence = project / 'platform-output' / 'evidence'
    evidence.mkdir(parents=True, exist_ok=True)
    (evidence / 'BINARY_SHA256.txt').write_text(digest(binary) + '  ' + binary.name + '\n')
    root = Path(tempfile.mkdtemp(prefix='.dragfm-native-', dir=os.environ['RUNNER_TEMP'])).resolve()
    os.chmod(root, 0o700)
    if os.name == 'nt':
        user = subprocess.check_output(['whoami'], text=True).strip()
        subprocess.run(['icacls', str(root), '/inheritance:r', '/grant:r', user + ':(OI)(CI)F'], check=True, timeout=15)
    child = None
    try:
        (root / 'SMOKE_ONLY').write_text('dragfm-native-smoke-v1\n')
        source, target = root / 'source', root / 'target'
        source.mkdir(); target.mkdir(); (target / 'archive').mkdir()
        space = source / "space [brackets] it's $literal"
        space.mkdir()
        (source / 'source.txt').write_bytes(os.urandom(128*1024))
        (source / 'hash.txt').write_bytes(b'Platform hash fixture\n')
        (source / 'delete.txt').write_bytes(b'Confirmed delete fixture\n')
        expected = digest(source / 'source.txt')
        password = os.urandom(24).hex()
        script = (project / 'build/ci/portable-smoke.js').read_text()
        history = []
        for phase in ('exercise', 'restore'):
            plan = dict(phase=phase, platform='windows' if os.name == 'nt' else sys.platform,
                        password=password, source=str(source), target=str(target),
                        spacePath=str(space), hash=digest(source / 'hash.txt'), historyIDs=history)
            (root / 'smoke.js').write_text('window.__dragfmSmokePlan=' + json.dumps(plan) + ';\n' + script)
            report = root / 'report.json'
            report.unlink(missing_ok=True)
            with (evidence / (phase + '.log')).open('wb') as log:
                child = subprocess.Popen([str(binary), '--native-smoke-dir', str(root)], stdout=log, stderr=subprocess.STDOUT)
                deadline = time.monotonic() + 260
                captured = False
                while child.poll() is None and time.monotonic() < deadline:
                    if report.exists() and not captured:
                        captured = True
                        result = json.loads(report.read_text())
                        # Never capture the unlock screen or credential editor.
                        if result.get('success') and sys.platform == 'darwin':
                            subprocess.run(['screencapture', '-x', str(evidence / (phase + '.png'))], check=False, timeout=5)
                    time.sleep(.1)
                if child.poll() is None:
                    child.terminate()
                    try: child.wait(timeout=10)
                    except subprocess.TimeoutExpired: child.kill(); child.wait(timeout=10)
                    raise RuntimeError('Native platform acceptance timed out')
            if not report.exists():
                raise RuntimeError(f'Native executable exited {child.returncode} without a report; inspect {phase}.log')
            result = json.loads(report.read_text())
            shutil.copyfile(report, evidence / (phase + '.json'))
            print(json.dumps(result, ensure_ascii=True, indent=2), flush=True)
            if child.returncode != 0 or result.get('success') is not True:
                raise RuntimeError('Native platform acceptance failed')
            if phase == 'exercise':
                history = result['historyIDs']
                assert len(history) >= 5
                assert digest(target / 'archive' / 'source.txt') == expected
                assert not (source / 'source.txt').exists()
                assert not (source / 'delete.txt').exists()
                assert password.encode() not in (root / 'vault.json').read_bytes()
        (evidence / 'FILESYSTEM_VERIFIED.json').write_text(json.dumps(dict(success=True, checks=['copied/moved destination hash', 'move source removed', 'confirmed deletion', 'vault excludes plaintext master password'])) + '\n')
    finally:
        if child is not None and child.poll() is None:
            child.kill(); child.wait(timeout=10)
        shutil.rmtree(root)


if __name__ == '__main__':
    main()
