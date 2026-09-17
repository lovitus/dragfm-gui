"""Authenticated SOCKS5 fixture for an isolated, disposable Docker network.

The public fixture credentials are deliberately not user secrets. They must not
be reused outside tests. No proxy or listener is installed on the user's hosts.
"""
import hmac
import select
import socket
import socketserver
import struct


def exact(sock, count):
    result = bytearray()
    while len(result) < count:
        chunk = sock.recv(count - len(result))
        if not chunk:
            raise EOFError("closed SOCKS fixture connection")
        result.extend(chunk)
    return bytes(result)


class Handler(socketserver.BaseRequestHandler):
    def handle(self):
        client = self.request
        client.settimeout(10)
        try:
            version, count = exact(client, 2)
            methods = exact(client, count)
            if version != 5 or 2 not in methods:
                client.sendall(b"\x05\xff")
                return
            client.sendall(b"\x05\x02")
            version, count = exact(client, 2)
            username = exact(client, count)
            password = exact(client, exact(client, 1)[0])
            valid = (version == 1 and hmac.compare_digest(username, b"dragfm-ci")
                     and hmac.compare_digest(password, b"fixture-only"))
            client.sendall(b"\x01" + (b"\x00" if valid else b"\x01"))
            if not valid:
                return
            version, command, reserved, kind = exact(client, 4)
            if version != 5 or command != 1 or reserved != 0:
                client.sendall(b"\x05\x07\x00\x01" + b"\x00" * 6)
                return
            if kind == 1:
                host = socket.inet_ntop(socket.AF_INET, exact(client, 4))
            elif kind == 4:
                host = socket.inet_ntop(socket.AF_INET6, exact(client, 16))
            elif kind == 3:
                host = exact(client, exact(client, 1)[0]).decode("ascii")
            else:
                return
            port = struct.unpack("!H", exact(client, 2))[0]
            with socket.create_connection((host, port), 10) as upstream:
                upstream.settimeout(30)
                client.settimeout(30)
                client.sendall(b"\x05\x00\x00\x01" + b"\x00" * 6)
                readers = [client, upstream]
                while readers:
                    ready, _, _ = select.select(readers, [], [], 30)
                    if not ready:
                        return
                    for source in ready:
                        destination = upstream if source is client else client
                        data = source.recv(65536)
                        if data:
                            destination.sendall(data)
                        else:
                            readers.remove(source)
                            destination.shutdown(socket.SHUT_WR)
        except (OSError, EOFError, ValueError, UnicodeError):
            # Test failures include the client error. Never log authentication bytes.
            return


class Server(socketserver.ThreadingTCPServer):
    daemon_threads = True
    allow_reuse_address = True


if __name__ == "__main__":
    with Server(("0.0.0.0", 1080), Handler) as server:
        server.serve_forever()
