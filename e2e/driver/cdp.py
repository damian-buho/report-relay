# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT
"""Minimal CDP client over a raw stdlib websocket: permission grants and page evaluates."""
import base64
import hashlib
import json
import os
import socket
import struct
import sys
import urllib.request


def ws_exchange(url, payload):
    key = base64.b64encode(os.urandom(16)).decode()
    host, _, path = url.replace("ws://", "").partition("/")
    host_part, _, port = host.partition(":")
    sock = socket.create_connection((host_part, int(port or 80)), timeout=10)
    req = "GET /%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n" % (path, host, key)
    sock.sendall(req.encode())
    head = b""
    while b"\r\n\r\n" not in head:
        chunk = sock.recv(4096)
        if not chunk:
            raise RuntimeError("handshake closed")
        head += chunk
    if b"101" not in head.split(b"\r\n")[0]:
        raise RuntimeError("handshake refused: %r" % head[:120])
    body = json.dumps(payload).encode()
    mask = os.urandom(4)
    head_out = bytes([0x81])
    if len(body) < 126:
        head_out += bytes([0x80 | len(body)])
    elif len(body) < 65536:
        head_out += bytes([0x80 | 126]) + struct.pack(">H", len(body))
    else:
        head_out += bytes([0x80 | 127]) + struct.pack(">Q", len(body))
    sock.sendall(head_out + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(body)))
    want_id = payload.get("id")
    for _ in range(50):
        resp = b""
        hdr = b""
        while len(hdr) < 2:
            chunk = sock.recv(2 - len(hdr))
            if not chunk:
                raise RuntimeError("frame closed waiting header")
            hdr += chunk
        length = hdr[1] & 0x7F
        if length == 126:
            (length,) = struct.unpack(">H", sock.recv(2))
        elif length == 127:
            (length,) = struct.unpack(">Q", sock.recv(8))
        while len(resp) < length:
            chunk = sock.recv(length - len(resp))
            if not chunk:
                raise RuntimeError("frame truncated")
            resp += chunk
        msg = json.loads(resp.decode())
        if msg.get("id") == want_id:
            sock.close()
            return msg
    raise RuntimeError("no response frame for id %r" % (want_id,))


def main():
    debug_port, origin = sys.argv[1], sys.argv[2]
    perms = sys.argv[3:] or ["backgroundSync"]
    with urllib.request.urlopen("http://localhost:%s/json/version" % debug_port, timeout=10) as res:
        browser_ws = json.load(res)["webSocketDebuggerUrl"]
    out = ws_exchange(browser_ws, {"id": 1, "method": "Browser.grantPermissions", "params": {"origin": origin, "permissions": perms}})
    print(json.dumps(out))
    if "error" in out:
        sys.exit(1)


def evaluate(debug_port, tab_id, expression):
    with urllib.request.urlopen("http://localhost:%s/json/list" % debug_port, timeout=10) as res:
        tabs = json.load(res)
    ws = [t["webSocketDebuggerUrl"] for t in tabs if t["id"] == tab_id][0]
    out = ws_exchange(ws, {"id": 2, "method": "Runtime.evaluate", "params": {"expression": expression, "awaitPromise": True}})
    print(json.dumps(out))


if __name__ == "__main__":
    if sys.argv[1] == "eval":
        evaluate(sys.argv[2], sys.argv[3], sys.argv[4])
    else:
        main()
