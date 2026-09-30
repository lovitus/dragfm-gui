#!/usr/bin/env python3
"""Exercise the production macOS Wails binary against disposable real SSH.

No user SSH configuration, login files, keychain, or personal vault is changed.
The fixture is intentionally restricted to GitHub-hosted runners. Credentials
stay in a private temporary directory, never in arguments or retained artifacts.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time

from byte_rate import ByteRatePacer
from native_input import NativeInput


class ThrottledSSH:
    def __init__(self, destination: int) -> None:
        self.destination = destination
        self.listener = socket.socket()
        self.listener.bind(('127.0.0.1', 0))
        self.port = self.listener.getsockname()[1]
        self.listener.listen()
        self.stopped = threading.Event()
        self.connections: list[socket.socket] = []
        self.transfers: list[dict] = []
        self.lock = threading.Lock()
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()

    def accept(self) -> None:
        while not self.stopped.is_set():
            try:
                downstream, _ = self.listener.accept()
                upstream = socket.create_connection(('127.0.0.1', self.destination), timeout=10)
                upstream.settimeout(None)
                downstream.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
                upstream.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
            except OSError:
                return
            with self.lock:
                self.connections.extend((downstream, upstream))
            threading.Thread(target=self.relay, args=(downstream, upstream, 'to-server'), daemon=True).start()
            threading.Thread(target=self.relay, args=(upstream, downstream, 'to-client'), daemon=True).start()

    def relay(self, source: socket.socket, target: socket.socket, direction: str) -> None:
        state = dict(direction=direction, bytes=0, started=time.monotonic(), last_io=None, closed=False)
        pacer = ByteRatePacer(4 * 1024 * 1024)
        with self.lock:
            self.transfers.append(state)
        try:
            while not self.stopped.is_set():
                data = source.recv(65536)
                if not data:
                    break
                target.sendall(data)
                with self.lock:
                    state['bytes'] += len(data)
                    state['last_io'] = time.monotonic()
                # Keep genuine encrypted SSH traffic capped at 4 MiB/s. Use
                # elapsed time, not one relative sleep per network fragment:
                # scheduler coalescing must not multiply protocol latency.
                delay = pacer.delay(len(data))
                if delay:
                    self.stopped.wait(delay)
        except OSError:
            pass
        finally:
            with self.lock:
                state['closed'] = True
            try:
                target.shutdown(socket.SHUT_WR)
            except OSError:
                pass

    def snapshot(self) -> list[dict]:
        now = time.monotonic()
        with self.lock:
            return [dict(direction=s['direction'], wire_bytes=s['bytes'],
                         elapsed_seconds=round(now-s['started'], 3),
                         idle_seconds=round(now-s['last_io'], 3) if s['last_io'] else None,
                         closed=s['closed']) for s in self.transfers]

    def close(self) -> None:
        self.stopped.set()
        self.listener.close()
        with self.lock:
            for connection in self.connections:
                connection.close()


def command(*args: str, **kwargs: object) -> subprocess.CompletedProcess:
    return subprocess.run(args, check=True, timeout=30, **kwargs)


def digest(path: Path) -> str:
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def main() -> None:
    if os.environ.get('GITHUB_ACTIONS') != 'true' or not os.environ.get('RUNNER_TEMP'):
        raise RuntimeError('This native fixture is restricted to disposable GitHub-hosted runners')
    if sys.platform != 'darwin':
        raise RuntimeError('This acceptance entrypoint currently targets macOS arm64')
    binary = Path(sys.argv[1]).resolve(strict=True)
    project = Path(__file__).resolve().parents[2]
    command(sys.executable, str(project / 'build/ci/test_byte_rate.py'))
    evidence = project / 'test-results' / 'native-macos-arm64'
    evidence.mkdir(parents=True, exist_ok=True)
    (evidence / 'BINARY_SHA256.txt').write_text(digest(binary) + '  ' + binary.name + '\n')
    try:
        native_input = NativeInput(evidence)
    except Exception as error:
        (evidence / 'OS_INPUT_BLOCKER.json').write_text(json.dumps(dict(success=False, error=str(error))) + '\n')
        raise
    fixture = Path(tempfile.mkdtemp(prefix='.dragfm-native-', dir=os.environ['RUNNER_TEMP'])).resolve()
    os.chmod(fixture, 0o700)
    server: subprocess.Popen | None = None
    proxy: ThrottledSSH | None = None
    server_log = (evidence / 'sshd.log').open('wb')
    app: subprocess.Popen | None = None
    server_readers = []
    try:
        native_input.prepare_display()
        (fixture / 'SMOKE_ONLY').write_text('dragfm-native-smoke-v1\n')
        user_key, host_key = fixture / 'client-key', fixture / 'host-key'
        command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(user_key))
        command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(host_key))
        authorized = fixture / 'authorized_keys'
        authorized.write_bytes(user_key.with_suffix('.pub').read_bytes())
        os.chmod(authorized, 0o600)
        with socket.socket() as reservation:
            reservation.bind(('127.0.0.1', 0))
            server_port = reservation.getsockname()[1]
        user = command('id', '-un', capture_output=True, text=True).stdout.strip()
        config = fixture / 'sshd_config'
        config.write_text(f'''Port {server_port}
ListenAddress 127.0.0.1
HostKey {host_key}
PidFile {fixture / 'sshd.pid'}
AuthorizedKeysFile {authorized}
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
PubkeyAuthentication yes
UsePAM no
AllowUsers {user}
AllowTcpForwarding yes
X11Forwarding no
PrintMotd no
LogLevel DEBUG2
Subsystem sftp internal-sftp
''')

        def start_server() -> subprocess.Popen:
            child = subprocess.Popen(['sudo', '-n', '/usr/sbin/sshd', '-D', '-e', '-f', str(config)], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            ready, ended = threading.Event(), threading.Event()
            def read_server():
                try:
                    for line in child.stdout:
                        server_log.write(line); server_log.flush()
                        if b'Server listening on 127.0.0.1 port ' in line:
                            ready.set()
                finally:
                    ended.set(); ready.set()
                    child.stdout.close()
            reader = threading.Thread(target=read_server, daemon=True)
            server_readers.append(reader); reader.start()
            if not ready.wait(30) or ended.is_set():
                stop_server(child)
                raise RuntimeError('Disposable sshd did not report readiness before exit/timeout')
            return child

        def stop_server(child: subprocess.Popen) -> None:
            pid_file = fixture / 'sshd.pid'
            pid = int(pid_file.read_text().strip()) if pid_file.exists() else child.pid
            subprocess.run(['sudo', '-n', 'kill', '-TERM', str(pid)], check=False, timeout=10)
            child.wait(timeout=10)

        server = start_server()
        known = fixture / 'known_hosts'
        known.write_text(f'[127.0.0.1]:{server_port} ' + host_key.with_suffix('.pub').read_text())
        ssh = ['ssh', '-F', '/dev/null', '-i', str(user_key), '-p', str(server_port), '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes', '-o', 'StrictHostKeyChecking=yes', '-o', f'UserKnownHostsFile={known}', f'{user}@127.0.0.1']
        home = command(*ssh, 'printf %s "$HOME"', capture_output=True, text=True).stdout
        fingerprint = command('ssh-keygen', '-lf', str(host_key.with_suffix('.pub')), capture_output=True, text=True).stdout.split()[1]
        proxy = ThrottledSSH(server_port)
        source, target = fixture / 'source', fixture / 'target'
        source.mkdir(); target.mkdir(); (target / 'archive').mkdir()
        protected = fixture / 'protected-local'
        protected.mkdir(mode=0o755)
        command('sudo', '-n', 'chown', 'root:wheel', str(protected))
        command('sudo', '-n', 'chmod', '0755', str(protected))
        with (source / 'first.bin').open('wb') as output:
            for _ in range(64):
                output.write(os.urandom(1024 * 1024))
        (source / 'second.txt').write_bytes(b'Native Wails real-SSH move fixture\n')
        (source / 'hash.txt').write_bytes(b'Native hash fixture\n')
        (source / 'delete.txt').write_bytes(b'Native confirmed deletion fixture\n')
        (source / 'tree').mkdir()
        (source / 'tree' / 'nested').mkdir(mode=0o750)
        leaf = source / 'tree' / 'nested' / 'leaf.txt'
        leaf.write_bytes(b'Native directory merge fixture\n')
        os.chmod(leaf, 0o640)
        os.utime(leaf, (1700000000, 1700000000))
        (source / 'tree' / 'link').symlink_to('nested/leaf.txt')
        (target / 'archive' / 'tree').mkdir()
        (target / 'archive' / 'tree' / 'keep.txt').write_text('existing target retained')
        # A separate, genuine multi-screen directory leaves the transfer
        # fixture's initial rows and ordering unchanged. Both browser panes
        # can reach it through their existing local/SSH endpoints.
        browse = fixture / 'browse-many'
        browse.mkdir(mode=0o750)
        for index in range(192):
            entry = browse / (f'entry-{index:04d}' if index == 64 else f'entry-{index:04d}.bin')
            if index == 64:
                entry.mkdir(mode=0o750)
                (entry / 'inside.txt').write_bytes(b'Native deep directory fixture\n')
                os.chmod(entry, 0o750)
            else:
                entry.write_bytes(b'x' * 8192)
                os.chmod(entry, 0o640)
            os.utime(entry, (1700000000 - index * 60, 1700000000 - index * 60))
        expected = {name: digest(source / name) for name in ('first.bin', 'second.txt')}
        password = os.urandom(24).hex()
        markdown = f'#主机\n##Native SSH\n{user}@127.0.0.1:{proxy.port} --keys "ci-key" ,/bin/bash\n#私钥\n##ci-key\n{user_key.read_text().strip()}\n#socks池\n##mask-test\nuser:fixture-secret@127.0.0.1:1081\n'
        script = (project / 'build/ci/native-smoke.js').read_text()
        # Only this process tree gets an isolated login home. No runner/user
        # profile is overwritten. Prove actual zprofile + zshrc execution,
        # rather than mistaking an inherited HOME variable for login loading.
        login_home = fixture / 'login-home'
        login_home.mkdir(mode=0o700)
        (login_home / '.zprofile').write_text("export DRAGFM_NATIVE_PROFILE=profile-loaded\n")
        (login_home / '.zshrc').write_text("native_profile_probe() { printf 'PROFILE=%s\\n' \"$DRAGFM_NATIVE_PROFILE\"; }\nPROMPT='NATIVE> '\n")
        from native_vault import normal_vault_startup
        normal_vault_startup(binary, fixture, evidence, native_input, login_home)
        app_environment = dict(os.environ, HOME=str(login_home), ZDOTDIR=str(login_home), SHELL='/bin/zsh')
        history: list[str] = []
        for phase in ('exercise', 'restore', 'changed-key'):
            if phase == 'changed-key':
                stop_server(server); server = None
                host_key.unlink(); host_key.with_suffix('.pub').unlink()
                command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(host_key))
                server = start_server()
            plan = dict(phase=phase, password=password, markdown=markdown, fingerprint=fingerprint,
                        source=str(source), target=str(target), parent=str(fixture), home=home, protected=str(protected),
                        browse=str(browse), browseCount=192, browseFile='entry-0063.bin', browseDirectory='entry-0064',
                        browseFileTime=1700000000 - 63 * 60,
                        hash=digest(source / 'hash.txt'), firstBytes=64*1024*1024, historyIDs=history)
            (fixture / 'smoke.js').write_text('window.__dragfmSmokePlan = ' + json.dumps(plan, ensure_ascii=True) + ';\n' + script)
            report_path = fixture / 'report.json'
            report_path.unlink(missing_ok=True)
            with (evidence / f'{phase}-native.log').open('wb') as log:
                app = subprocess.Popen([str(binary), '--native-smoke-dir', str(fixture)], stdin=subprocess.PIPE,
                                       stdout=subprocess.PIPE, stderr=log, env=app_environment)
                input_errors = []
                def read_application():
                    try:
                        for line in app.stdout:
                            if line.startswith(b'DRAGFM_NATIVE_INPUT '):
                                request = json.loads(line[len(b'DRAGFM_NATIVE_INPUT '):])
                                response = dict(id=request['id'])
                                try:
                                    native_input.perform(app, request)
                                except Exception as error:
                                    response['error'] = str(error)
                                    input_errors.append(str(error))
                                    try:
                                        native_input.close()
                                    except Exception as cleanup_error:
                                        # Keep the original action response even
                                        # when its separate owned release fails.
                                        response['cleanupError'] = str(cleanup_error)
                                        input_errors.append('owned release: ' + str(cleanup_error))
                                app.stdin.write((json.dumps(response) + '\n').encode()); app.stdin.flush()
                            elif line.strip() == b'DRAGFM_NATIVE_REPORT':
                                (evidence / f'{phase}-wire.json').write_text(json.dumps(proxy.snapshot(), indent=2) + '\n')
                                # A failure may leave keys visible: never capture it.
                                if json.loads(report_path.read_text()).get('success'):
                                    subprocess.run(['screencapture', '-x', str(evidence / f'{phase}-window.png')], check=False, timeout=5,
                                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                            else:
                                log.write(line); log.flush()
                    except Exception as error:
                        input_errors.append(str(error))
                    finally:
                        app.stdout.close()
                reader = threading.Thread(target=read_application, daemon=True)
                reader.start()
                try:
                    app.wait(timeout=260)  # One bounded process wait, no state/file polling.
                except subprocess.TimeoutExpired:
                    app.terminate(); app.wait(timeout=10)
                    raise RuntimeError(f'{phase}: native window exceeded acceptance timeout')
                finally:
                    reader.join(timeout=10)
                    app.stdin.close()
                    try:
                        native_input.close()
                    except Exception as cleanup_error:
                        input_errors.append('phase owned release: ' + str(cleanup_error))
                # A renderer may already have reported the original stage and
                # successful preceding checks when an OS adapter action fails.
                # Retain that evidence before raising the separate bridge error.
                if report_path.is_file():
                    shutil.copyfile(report_path, evidence / f'{phase}.json')
                if reader.is_alive() or input_errors:
                    (evidence / f'{phase}-input-errors.json').write_text(json.dumps(input_errors) + '\n')
                    raise RuntimeError(f'{phase}: OS input bridge failed; this is not an input PASS')
            if not report_path.exists():
                raise RuntimeError(f'{phase}: native renderer returned no report (exit {app.returncode})')
            report = json.loads(report_path.read_text())
            print(json.dumps(report, ensure_ascii=False, indent=2), flush=True)
            if app.returncode != 0 or not report.get('success'):
                with (evidence / f'{phase}-fixture-processes.log').open('wb') as processes:
                    subprocess.run(['ps', '-axo', 'pid,ppid,pgid,state,etime,command'], stdout=processes, stderr=subprocess.STDOUT, timeout=5, check=False)
                raise RuntimeError(f'{phase}: native acceptance failed')
            if phase == 'exercise':
                history = report['historyIDs']
                assert len(history) >= 9, 'Expected transfers, cancellation, failure, hash, move and deletion history'
                for name, sha in expected.items():
                    assert digest(target / 'archive' / name) == sha, f'Native transfer hash mismatch: {name}'
                assert (source / 'first.bin').exists(), 'Copy unexpectedly removed source'
                assert not (source / 'second.txt').exists(), 'Verified move left source behind'
                assert not (source / 'delete.txt').exists(), 'Confirmed deletion was not performed'
                copied = target / 'archive' / 'tree' / 'nested' / 'leaf.txt'
                assert digest(copied) == digest(leaf), 'Directory merge content differs'
                assert (target / 'archive' / 'tree' / 'keep.txt').read_text() == 'existing target retained', 'Directory merge discarded unrelated destination'
                assert os.readlink(target / 'archive' / 'tree' / 'link') == 'nested/leaf.txt', 'Directory merge changed symlink semantics'
                assert copied.stat().st_mode & 0o777 == 0o640 and int(copied.stat().st_mtime) == 1700000000, 'Directory copy lost mode/mtime'
                assert digest(protected / 'second.txt') == expected['second.txt'], 'Protected local download hash mismatch'
                assert (protected / 'second.txt').stat().st_uid == os.getuid(), 'Protected download not owned by launching user'
                assert not list(protected.glob('.dragfm-partial-*')), 'Protected download left staging files'
                vault = (fixture / 'vault.json').read_bytes()
                assert password.encode() not in vault and user_key.read_bytes() not in vault, 'Vault stored credentials in plaintext'
        (evidence / 'FILESYSTEM_VERIFIED.json').write_text(json.dumps(dict(success=True, checks=['SHA-256 of both destination files', 'copy source retained', 'move source removed only after verification', 'confirmed deletion', 'directory merge preserves existing files/symlink/mode/mtime', 'vault ciphertext excludes plaintext credentials', 'real sudo protected local download SHA-256 and ownership', 'declined sudo move retains source']), indent=2) + '\n')
    finally:
        original_failure = sys.exc_info()[1]
        release_failure = None
        try:
            if app is not None and app.poll() is None:
                app.terminate()
                try: app.wait(timeout=10)
                except subprocess.TimeoutExpired: app.kill(); app.wait(timeout=10)
            if proxy is not None: proxy.close()
            if server is not None:
                try: stop_server(server)
                except (OSError, ValueError, subprocess.SubprocessError):
                    subprocess.run(['sudo', '-n', 'kill', '-TERM', str(server.pid)], check=False, timeout=10)
            try:
                native_input.close()
            except Exception as cleanup_error:
                release_failure = cleanup_error
                (evidence / 'OWNED_RELEASE_ERROR.json').write_text(json.dumps(dict(
                    error=str(cleanup_error)[:1000],
                    originalFailure=type(original_failure).__name__ if original_failure else None)) + '\n')
            for reader in server_readers:
                reader.join(timeout=3)
            server_log.close()
            protected = fixture / 'protected-local'
            if protected.exists():
                subprocess.run(['sudo', '-n', 'rm', '-rf', str(protected)], check=False, timeout=10)
            shutil.rmtree(fixture)
        finally:
            native_input.restore_display()
            # Preserve guard/action counts on failure too, without requests,
            # characters, field values or other applications' identities.
            (evidence / 'OS_INPUT_ACTIONS.json').write_text(json.dumps(native_input.counts, indent=2) + '\n')
        if release_failure is not None and original_failure is None:
            raise RuntimeError('final owned mouse release failed; native acceptance is not a PASS') from release_failure


if __name__ == '__main__':
    main()
