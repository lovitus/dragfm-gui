"""Normal startup coverage inside the existing disposable macOS native fixture.

The product receives no arguments, test hook, vault override or replacement
HOME. AX reads its ordinary UI; the existing Quartz adapter types/clicks it.
This verifies existing functionality, not an invented old-product regression.
"""
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import stat
import struct
import subprocess
import sys


def normal_vault_startup(binary, fixture, evidence, native_input, login_home):
    if os.environ.get('GITHUB_ACTIONS') != 'true' or not os.environ.get('RUNNER_TEMP') or os.geteuid() == 0:
        raise RuntimeError('Normal-startup acceptance requires a non-root disposable hosted runner')
    project = Path(__file__).resolve().parents[2]
    observer = fixture / 'vault-ui-observer'
    subprocess.run(['swiftc', str(project / 'build/ci/native-vault-accessibility.swift'),
                    '-module-cache-path', str(fixture / 'swift-module-cache'), '-o', str(observer)],
                   check=True, timeout=60)
    cwd = fixture / 'ordinary-launch-cwd'
    cwd.mkdir(mode=0o700)
    portable, fallback = fixture / 'ordinary-portable', fixture / 'ordinary-fallback'
    for directory in (portable, fallback):
        directory.mkdir(mode=0o700)
        shutil.copy2(binary, directory / binary.name)
    adjacent = portable / 'dragfm-gui.vault'
    system_dir = Path.home() / 'Library' / 'Application Support' / 'dragfm-gui'
    system = system_dir / 'dragfm-gui.vault'
    # Never replace or clean up any pre-existing application directory.
    if system_dir.exists() or system_dir.is_symlink():
        raise RuntimeError('BLOCKER: disposable runner already has application configuration; refusing to replace it')
    report = dict(success=False, checks=[], launches=[], observations=[], permission='existing OS accessibility only')
    portable_password, fallback_password = secrets.token_hex(16), secrets.token_hex(16)
    portable_hint, fallback_hint = 'plain-portable-' + secrets.token_hex(6), 'plain-fallback-' + secrets.token_hex(6)
    # Preserve HOME so the product actually chooses the standard OS directory.
    environment = dict(os.environ, ZDOTDIR=str(login_home), SHELL='/bin/zsh')
    app = None

    def header(path, expected_hint, password):
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) & 0o077:
            raise AssertionError('Normal startup vault is not an owned private regular file')
        data = path.read_bytes()
        if data[:8] != b'DFGUIV01' or len(data) < 12:
            raise AssertionError('Normal startup did not write an actual encrypted vault')
        length = struct.unpack('>I', data[8:12])[0]
        if not 0 < length <= 1 << 20 or len(data) < 12 + length + 16:
            raise AssertionError('Invalid vault framing')
        parsed = json.loads(data[12:12 + length])
        report['observations'].append(dict(kind='vault-header', hintMatched=parsed['hint'] == expected_hint,
                                           hintLength=len(parsed['hint']), expectedHintLength=len(expected_hint),
                                           privateFile=True, passwordAbsent=password.encode() not in data))
        if parsed['hint'] != expected_hint or password.encode() in data:
            raise AssertionError('Wrong vault selected or master password present in plaintext')
        return hashlib.sha256(data).hexdigest()

    def observe(state, hint, fit=False):
        completed = subprocess.run([str(observer), str(app.pid), str(app.args[0]), state, hint, 'fit' if fit else 'keep'],
                                   capture_output=True, text=True, timeout=150)
        if completed.returncode:
            raise RuntimeError('Normal startup observation: ' + completed.stderr.strip())
        snapshot = json.loads(completed.stdout)
        # No field values, raw accessibility text, password or public hint is
        # logged. Keep enough facts to distinguish observer vs product failure.
        report['observations'].append(dict(kind='ui', expected=state, state=snapshot.get('state'),
                                           fields=len(snapshot.get('fields', [])), hintMatched=snapshot.get('hintMatched'),
                                           hintSources=snapshot.get('hintSources'), nodes=snapshot.get('nodes'),
                                           wrongPassword=snapshot.get('wrongPassword'), staleSnapshots=snapshot.get('staleSnapshots', 0)))
        return snapshot

    def action(snapshot, kind, **details):
        native_input.perform(app, dict(action=kind, viewport=snapshot['viewport'], **details))

    def text(snapshot, value):
        for start in range(0, len(value), 32):
            action(snapshot, 'text', text=value[start:start + 32])

    def quit(snapshot):
        nonlocal app
        action(snapshot, 'keys', keys=['command', 'q'])
        app.wait(timeout=15)
        if app.returncode != 0:
            raise RuntimeError('Normal startup product did not exit successfully')
        app = None

    def launch(directory, name, hint, password, create=False, wrong_password=False, relock=False):
        nonlocal app
        executable = directory / binary.name
        with (evidence / (name + '-ordinary.log')).open('wb') as log:
            # Deliberately no --native-smoke-dir, stdin RPC, injected script or
            # vault path. The cwd is also different from the binary directory.
            app = subprocess.Popen([str(executable)], cwd=cwd, env=environment, stdin=subprocess.DEVNULL,
                                   stdout=log, stderr=subprocess.STDOUT)
            state = observe('create' if create else 'unlock', hint, fit=True)
            if len(state['fields']) != (3 if create else 1) or (not create and not state['hintMatched']):
                raise AssertionError('Normal startup did not show the expected locked/create form and hint')
            if wrong_password:
                action(state, 'click', point=state['fields'][0], compare_focus=True)
                text(state, 'intentionally-wrong-' + secrets.token_hex(4))
                action(state, 'click', point=state['submit'])
                state = observe('wrong-password', hint)
                if not state['hintMatched']:
                    raise AssertionError('Wrong password changed the selected vault')
                action(state, 'click', point=state['fields'][0])
                action(state, 'keys', keys=['command', 'a'])
                action(state, 'keys', keys=['backspace'])
                report['checks'].append(name + ': wrong password stays locked')
            values = [hint, password, password] if create else [password]
            for index, (point, value) in enumerate(zip(state['fields'], values)):
                action(state, 'click', point=point)
                text(state, value)
                if create and index == 0:
                    # The only entered value is the random public hint; no
                    # master password has been typed yet. Do not capture a
                    # filled form, which could expose a misdirected password.
                    action(state, 'capture', label=name + '-hint-only')
            action(state, 'click', point=state['submit'])
            state = observe('workspace', hint)
            action(state, 'capture', label=name + '-ordinary')
            header(adjacent if directory == portable else system, hint, password)
            if relock:
                action(state, 'click', point=state['lock'])
                state = observe('unlock', hint)
                if not state['hintMatched'] or len(state['fields']) != 1:
                    action(state, 'capture', label=name + '-lock-failed')
                    raise AssertionError('Manual lock did not return to the selected encrypted vault')
                report['checks'].append(name + ': manual lock requires password again')
            quit(state)
        report['launches'].append(dict(phase=name, argc=1, different_cwd=True, startup_locked=True))

    try:
        launch(portable, 'portable-create', portable_hint, portable_password, create=True, relock=True)
        report['portableSHA256'] = header(adjacent, portable_hint, portable_password)
        if system.exists() or (cwd / 'dragfm-gui.vault').exists():
            raise AssertionError('New writable install did not keep its vault beside the executable')
        report['checks'].append('new writable install creates a private encrypted adjacent vault, not cwd')
        launch(portable, 'portable-restart', portable_hint, portable_password, wrong_password=True)
        header(adjacent, portable_hint, portable_password)
        report['checks'].append('ordinary restart reuses adjacent vault and requires the master password')

        os.chmod(fallback, 0o555)
        # Test real non-root directory permissions, not just the mode bits.
        probe = fallback / '.owned-permission-check'
        try:
            probe.touch(exist_ok=False)
        except PermissionError:
            pass
        else:
            probe.unlink()
            raise AssertionError('Fixture executable directory is actually writable')
        launch(fallback, 'fallback-create', fallback_hint, fallback_password, create=True)
        report['fallbackSHA256'] = header(system, fallback_hint, fallback_password)
        if (fallback / 'dragfm-gui.vault').exists():
            raise AssertionError('Unwritable install unexpectedly wrote an adjacent vault')
        report['checks'].append('unwritable install creates the actual standard OS fallback vault')

        os.chmod(fallback, 0o700)
        launch(fallback, 'fallback-reuse', fallback_hint, fallback_password)
        header(system, fallback_hint, fallback_password)
        if (fallback / 'dragfm-gui.vault').exists():
            raise AssertionError('Existing fallback vault was abandoned when the program directory became writable')
        report['checks'].append('existing system fallback is reused before creating a new adjacent vault')
        launch(portable, 'adjacent-priority', portable_hint, portable_password)
        header(adjacent, portable_hint, portable_password)
        header(system, fallback_hint, fallback_password)
        report['checks'].append('existing adjacent vault wins over a different existing system vault')
        report['success'] = True
    finally:
        original_error = sys.exc_info()[1]
        cleanup_error = None
        try:
            if app is not None and app.poll() is None:
                app.terminate()
                try: app.wait(timeout=10)
                except subprocess.TimeoutExpired: app.kill(); app.wait(timeout=10)
            os.chmod(fallback, 0o700)
            # Only the newly created, positively identified fixture vault can
            # be removed. Unknown contents belong to runner teardown, not us.
            if system.exists():
                header(system, fallback_hint, fallback_password)
                system.unlink()
            if system_dir.exists():
                system_dir.rmdir()
        except Exception as error:
            cleanup_error = error
            report['cleanupError'] = str(error)
            report['success'] = False
        if original_error is not None:
            report['error'] = str(original_error)
        (evidence / 'NORMAL_STARTUP_VAULT.json').write_text(json.dumps(report, indent=2) + '\n')
        if cleanup_error is not None and original_error is None:
            raise cleanup_error
