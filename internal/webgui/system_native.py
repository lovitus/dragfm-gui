"""Definitions prepended to the existing pinned Paramiko route carrier.

No custom SCP/rsync protocol, executable upload, credential file or listener.
The rsync rsh subprocess inherits one anonymous socketpair endpoint; it never
receives credentials. Native processes inherit the owning workspace's fd9.
"""
import shlex
import subprocess
import threading


NATIVE_RSH = r'''
import json, os, socket, sys, threading
link = socket.socket(fileno=int(sys.argv[1]))
link.settimeout(15)
link.sendall(json.dumps(sys.argv[2:]).encode("utf-8") + b"\n")
if link.recv(1) != b"\x01":
    raise RuntimeError("native rsh handshake rejected")
link.settimeout(None)
def outgoing():
    try:
        while True:
            block = os.read(0, 65536)
            if not block:
                link.shutdown(socket.SHUT_WR)
                return
            link.sendall(block)
    except OSError:
        pass
threading.Thread(target=outgoing, daemon=True).start()
while True:
    block = link.recv(65536)
    if not block:
        break
    view = memoryview(block)
    while view:
        view = view[os.write(1, view):]
link.close()
# Do not join stdin: rsync waits for rsh exit before closing that pipe.
'''


def run_native(transport, task):
    method, pull = task["method"], task["pull"]
    source, target = task["source"], task["target"]
    if method not in ("rsync", "scp") or not isinstance(pull, bool):
        raise ValueError("invalid native method/direction")
    if any(not isinstance(p, str) or not p.startswith("/") or "\x00" in p for p in (source, target)):
        raise ValueError("native paths must be absolute")
    if not stat.S_ISDIR(os.fstat(9).st_mode):
        raise ValueError("native transfer has no inherited workspace lease")
    child = channel = bridge = child_bridge = None
    peer_requested, peer_confirmed = False, False
    failure = None
    threads, errors = [], []
    local_error, peer_error = bytearray(), bytearray()
    wire_lock = threading.Lock()
    wire_bytes, last_report = 0, 0

    def measured(size):
        nonlocal wire_bytes, last_report
        with wire_lock:
            wire_bytes += size
            now = time.monotonic()
            if now - last_report >= 0.5:
                print("wire " + str(wire_bytes), flush=True)
                last_report = now

    def background(function):
        def guarded():
            try:
                function()
            except Exception as error:
                errors.append(error)
        thread = threading.Thread(target=guarded, daemon=True)
        threads.append(thread)
        thread.start()
        return thread

    def drain(read, retained):
        while True:
            block = read(65536)
            if not block:
                return
            retained.extend(block)
            if len(retained) > 16384:
                del retained[:-16384]

    try:
        environment = {key: value for key, value in os.environ.items()
                       if not key.startswith("RSYNC_") and key not in ("BASH_ENV", "ENV")}
        if method == "rsync":
            if task["directory"]:
                # Target is an exact private staging root. Request directory
                # contents explicitly instead of nesting its basename again.
                source = source.rstrip("/") + "/"
            bridge, child_bridge = socket.socketpair()
            # rsync -e has its own quote parser: doubled quotes, not POSIX
            # backslash escapes. Only static source/fd numbers are in argv.
            words = [sys.executable, "-I", "-B", "-c", NATIVE_RSH, str(child_bridge.fileno())]
            rsh = " ".join("'" + value.replace("'", "''") + "'" for value in words)
            arguments = ["/usr/bin/rsync", "-a", "--no-owner", "--no-group",
                         "--no-devices", "--no-specials", "--rsync-path=/usr/bin/rsync", "-e", rsh, "--"]
            arguments += ["dragfm:" + source, target] if pull else [source, "dragfm:" + target]
            child = subprocess.Popen(arguments, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                     stderr=subprocess.PIPE, bufsize=0, env=environment,
                                     pass_fds=(9, child_bridge.fileno()))
            child_bridge.close()
            child_bridge = None
            background(lambda: drain(child.stderr.read, local_error))
            bridge.settimeout(15)
            frame = bytearray()
            while len(frame) <= 65536:
                part = bridge.recv(1)
                if part == b"\n":
                    break
                if not part:
                    raise RuntimeError("rsync exited before rsh handshake")
                frame.extend(part)
            else:
                raise ValueError("rsync rsh arguments exceed limit")
            remote_args = json.loads(frame)
            if (not isinstance(remote_args, list) or len(remote_args) < 6
                    or not all(isinstance(arg, str) and "\x00" not in arg for arg in remote_args)
                    or remote_args[:3] != ["dragfm", "/usr/bin/rsync", "--server"]
                    or remote_args[-2] != "."
                    or (("--sender" in remote_args) != pull)):
                raise ValueError("unexpected native rsync server invocation")
            remote_args = remote_args[1:]
            # Match rsyncbridge: use the operation's exact single path, not
            # rsync's pre-escaped rsh spelling. SSH quoting below prevents
            # expansion. -s is not used: rsync itself still globs its paths.
            remote_args[-1] = source if pull else target
        else:
            common = ["/usr/bin/scp", "-p"] + (["-r"] if task["directory"] else [])
            local_args = common + (["-t", "--", target] if pull else ["-f", "--", source])
            remote_args = common + (["-f", "--", source] if pull else ["-t", "--", target])
            child = subprocess.Popen(local_args, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE, bufsize=0, env=environment, pass_fds=(9,))
            background(lambda: drain(child.stderr.read, local_error))
        channel = transport.open_session(timeout=5)
        channel.settimeout(60)
        # Host pin and authentication have already succeeded. Every argument
        # is quoted exactly once; paths are never interpreted as shell syntax.
        command = "unset BASH_ENV ENV; exec " + " ".join(shlex.quote(arg) for arg in task["peer_prefix"] + remote_args)
        peer_requested = True
        channel.exec_command(command)
        background(lambda: drain(channel.recv_stderr, peer_error))
        if bridge is not None:
            bridge.sendall(b"\x01")
            bridge.settimeout(None)

        def outgoing():
            while True:
                block = bridge.recv(65536) if bridge is not None else child.stdout.read(65536)
                if not block:
                    channel.shutdown_write()
                    return
                channel.sendall(block)
                measured(len(block))
        background(outgoing)
        while True:
            block = channel.recv(65536)
            if not block:
                break
            if bridge is not None:
                bridge.sendall(block)
            else:
                view = memoryview(block)
                while view:
                    view = view[child.stdin.write(view):]
            measured(len(block))
        if bridge is not None:
            bridge.shutdown(socket.SHUT_WR)
        else:
            child.stdin.close()
        # Drain first: Paramiko documents a window deadlock if exit status is
        # requested before stdout/stderr are consumed. Wait only after EOF.
        status_ready = threading.Event()
        peer_status = []

        def wait_peer():
            try:
                peer_status.append(channel.recv_exit_status())
            finally:
                status_ready.set()
        background(wait_peer)
        if not status_ready.wait(10) or not peer_status or peer_status[0] < 0:
            raise ExitUnconfirmed("peer native process supplied no exit status")
        if peer_status[0] == 77:
            raise ExitUnconfirmed("peer transfer monitor could not confirm native exit")
        peer_confirmed = True
        local_status = child.wait(timeout=10)
        if local_status < 0:
            raise ExitUnconfirmed("initiating native process exited by signal")
        for thread in threads:
            thread.join(timeout=3)
            if thread.is_alive():
                raise ExitUnconfirmed("native pipe has not closed after process exit")
        if errors or local_status != 0 or peer_status[0] != 0:
            detail = (local_error + peer_error).decode("utf-8", errors="replace")
            raise RuntimeError("native exit local=%s peer=%s: %s; %s" %
                               (local_status, peer_status[0], detail, "; ".join(str(e) for e in errors)))
        print("wire " + str(wire_bytes), flush=True)
    except Exception as error:
        failure = error
    finally:
        if channel is not None:
            channel.close()
        for link in (bridge, child_bridge):
            if link is not None:
                link.close()
        if child is not None:
            try:
                # EOF can precede waitpid readiness. Let an ordinary option/
                # peer error exit normally so SCP fallback is not mistaken for
                # an unconfirmed signal termination by a race with child.kill.
                try:
                    child.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait(timeout=3)
                if child.returncode < 0:
                    failure = ExitUnconfirmed("native child signalled; dependent leases retained: " + str(failure))
            except subprocess.TimeoutExpired:
                failure = ExitUnconfirmed("native child did not exit: " + str(failure))
            for stream in (child.stdin, child.stdout, child.stderr):
                if stream is not None:
                    stream.close()
        if peer_requested and not peer_confirmed:
            failure = ExitUnconfirmed("peer exit unconfirmed; workspace retained: " + str(failure))
    if failure is not None:
        raise failure
