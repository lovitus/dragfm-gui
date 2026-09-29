"""Optional remote system-tool carrier; supplied as static python -c source.

Only fd6 carries task configuration. No secrets are in argv, environment, a
key file, or this program. PySocks and Paramiko must already be installed.
Archive bytes use fd0/fd1 and a bounded buffer, never the controller connection.
"""
import base64
import hashlib
import hmac
import io
import json
import logging
import os
import pwd
import resource
import socket
import stat
import sys
import time

resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
logging.getLogger("paramiko").addHandler(logging.NullHandler())
logging.getLogger("paramiko").propagate = False
configuration = {}
transports = []
agents = []
connection = None
stage = "configuration"


class HostKeyChanged(Exception):
    pass


class ExitUnconfirmed(Exception):
    pass


def key_from_memory(module, pem, passphrase):
    for name in ("Ed25519Key", "RSAKey", "ECDSAKey"):
        key_type = getattr(module, name, None)
        if key_type is None:
            continue
        try:
            return key_type.from_private_key(io.StringIO(pem), password=passphrase)
        except (module.SSHException, ValueError):
            pass
    raise ValueError("unsupported or incorrectly decrypted private key")


def authenticate(module, transport, hop):
    # Passwordless credentials belong to the actual remote initiating UID.
    # sudo invokes this interpreter as root; it does not borrow the original
    # user's private keys. Vault keys remain explicitly bound to each hop.
    keys = []
    if hop.get("allow_local_identity") is True:
        agent_path = os.environ.get("SSH_AUTH_SOCK", "")
        try:
            agent_metadata = os.stat(agent_path)
            if (agent_metadata.st_uid == os.geteuid() and stat.S_ISSOCK(agent_metadata.st_mode)
                    and os.environ.get("SUDO_UID", str(os.geteuid())) == str(os.geteuid())):
                # This is a dedicated, noninteractive carrier process. Bound
                # the standard Agent constructor's socket operations before
                # it enumerates keys; no private Paramiko API is patched.
                previous_timeout = socket.getdefaulttimeout()
                try:
                    socket.setdefaulttimeout(5)
                    agent = module.Agent()
                finally:
                    socket.setdefaulttimeout(previous_timeout)
                agents.append(agent)
                keys.extend(agent.get_keys())
        except (OSError, module.SSHException):
            pass
        directory = None
        try:
            # Resolve the actual account, not an inherited HOME after sudo.
            home = pwd.getpwuid(os.geteuid()).pw_dir
            directory = os.open(os.path.join(home, ".ssh"), os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            metadata = os.fstat(directory)
            if metadata.st_uid != os.geteuid() or metadata.st_mode & 0o022:
                os.close(directory)
                directory = None
            if directory is not None:
                for name in ("id_ed25519", "id_ecdsa", "id_rsa"):
                    try:
                        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
                        with os.fdopen(descriptor, "r", encoding="utf-8") as source:
                            metadata = os.fstat(source.fileno())
                            if (metadata.st_uid != os.geteuid() or metadata.st_mode & 0o077
                                    or not stat.S_ISREG(metadata.st_mode) or metadata.st_size > 65536):
                                continue
                            material = source.read(65537)
                            if len(material) <= 65536:
                                keys.append(key_from_memory(module, material, None))
                    except (OSError, ValueError, module.SSHException):
                        continue
        except (KeyError, OSError):
            pass
        finally:
            if directory is not None:
                os.close(directory)
    last = None
    for item in hop.get("private_keys", []):
        try:
            pem = base64.b64decode(item["pem"], validate=True).decode("utf-8")
            phrase = base64.b64decode(item.get("passphrase", ""), validate=True).decode("utf-8") or None
            keys.append(key_from_memory(module, pem, phrase))
        except (ValueError, module.SSHException) as error:
            last = error
    for key in keys:
        try:
            transport.auth_publickey(hop["user"], key)
            if transport.is_authenticated():
                return
        except module.AuthenticationException as error:
            last = error
    if hop.get("password"):
        # Paramiko handles servers offering keyboard-interactive instead of
        # password; no untrusted remote prompt is evaluated as an instruction.
        transport.auth_password(hop["user"], hop["password"], fallback=True)
    if not transport.is_authenticated():
        raise last or module.AuthenticationException("no accepted hop credential")


def safe_error(error):
    text = stage + ": " + type(error).__name__ + ": " + str(error)
    secrets = []
    proxy = configuration.get("route", {}).get("socks") or {}
    if proxy.get("password"):
        secrets.append(proxy["password"])
    for hop in configuration.get("route", {}).get("hops") or []:
        if hop.get("password"):
            secrets.append(hop["password"])
        for key in hop.get("private_keys", []):
            for field in ("pem", "passphrase"):
                if key.get(field):
                    secrets.append(key[field])
                    try:
                        secrets.append(base64.b64decode(key[field]).decode("utf-8"))
                    except (ValueError, UnicodeError):
                        pass
    for secret in sorted(set(secrets), key=len, reverse=True):
        if secret:
            text = text.replace(secret, "[redacted]")
    return text


try:
    with os.fdopen(6, "rb") as control:
        payload = control.read(1048577)
    if len(payload) > 1048576:
        raise ValueError("route configuration exceeds limit")
    configuration = json.loads(payload)
    del payload
    if configuration["version"] != 1:
        raise ValueError("unsupported route protocol version")
    # Enforce before any socket/authentication or identity discovery. Ordinary
    # controller credentials can never rescue a remote-root-only request.
    # Go's zero-value Route encodes its nil slice as JSON null for a plain
    # TCP probe. A route without SSH hops needs no identity checks.
    identity_hops = configuration.get("route", {}).get("hops") or []
    for index, hop in enumerate(identity_hops):
        root_only = hop.get("root_identity_only", False)
        if not isinstance(root_only, bool):
            raise ValueError("invalid root identity policy")
        if root_only and (os.geteuid() != 0 or index != len(identity_hops) - 1
                          or hop.get("allow_local_identity") is not True
                          or hop.get("password") or hop.get("private_keys")):
            raise ValueError("remote root identity requires actual UID 0, final hop and no borrowed credentials")
    destination = (configuration["host"], int(configuration["port"]))
    mode = configuration["mode"]
    if mode == "probe":
        stage = "tcp-probe"
        started = time.monotonic()
        with socket.create_connection(destination, timeout=3):
            elapsed = int((time.monotonic() - started) * 1000000000)
        print(elapsed)
    elif mode in ("send", "receive", "native"):
        route = configuration["route"]
        hops, proxy = route.get("hops") or [], route.get("socks")
        first = (hops[0]["host"], int(hops[0]["port"])) if hops else destination
        stage = "proxy-connect" if proxy else "tcp-connect"
        if proxy:
            import socks
            # SOCKS addresses are canonical host:port, with optional brackets.
            host, port = proxy["address"].rsplit(":", 1)
            connection = socks.create_connection(first, timeout=5,
                proxy_type=socks.SOCKS5, proxy_addr=host.strip("[]"), proxy_port=int(port),
                proxy_rdns=True, proxy_username=proxy.get("username"), proxy_password=proxy.get("password"))
        else:
            connection = socket.create_connection(first, timeout=5)
        if hops:
            import paramiko
            for index, hop in enumerate(hops):
                stage = "ssh-hop-" + str(index + 1)
                if index:
                    connection = transports[-1].open_channel("direct-tcpip",
                        (hop["host"], int(hop["port"])), ("127.0.0.1", 0), timeout=5)
                transport = paramiko.Transport(connection)
                transports.append(transport)
                transport.auth_timeout = 5
                transport.start_client(timeout=5)
                actual = base64.b64encode(hashlib.sha256(transport.get_remote_server_key().asbytes()).digest()).decode("ascii").rstrip("=")
                expected = hop["fingerprint"]
                if expected.startswith("SHA256:"):
                    expected = expected[len("SHA256:"):]
                expected = expected.rstrip("=")
                if not hmac.compare_digest(actual, expected):
                    raise HostKeyChanged("SSH host fingerprint changed; authentication blocked")
                authenticate(paramiko, transport, hop)
            if mode != "native":
                connection = transports[-1].open_channel("direct-tcpip", destination, ("127.0.0.1", 0), timeout=5)
        if mode == "native":
            if not transports:
                raise ValueError("native tools require an authenticated SSH peer")
            stage = "native-" + configuration["native"]["method"]
            run_native(transports[-1], configuration["native"])
        else:
            stage = "archive-stream"
            connection.settimeout(60)
            print("Connected to routed archive", file=sys.stderr, flush=True)
            if mode == "send":
                while True:
                    block = sys.stdin.buffer.read(65536)
                    if not block:
                        break
                    connection.sendall(block)
                connection.shutdown(socket.SHUT_WR)
                while connection.recv(65536):
                    pass
            else:
                while True:
                    block = connection.recv(65536)
                    if not block:
                        break
                    sys.stdout.buffer.write(block)
                sys.stdout.buffer.flush()
    else:
        raise ValueError("unsupported route operation")
except Exception as error:
    print("system-route: " + safe_error(error), file=sys.stderr, flush=True)
    sys.exit(78 if isinstance(error, HostKeyChanged) else 77 if isinstance(error, ExitUnconfirmed) else 1)
finally:
    if connection is not None:
        connection.close()
    for transport in reversed(transports):
        transport.close()
    for agent in agents:
        agent.close()
