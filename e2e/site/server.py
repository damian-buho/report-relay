# SPDX-FileCopyrightText: 2026 Damián Búho <damian.buho@proton.me>
#
# SPDX-License-Identifier: MIT
"""Throwaway site that makes a real browser emit security reports."""
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SITE_PORT = int(os.environ.get("E2E_SITE_PORT", "8901"))
RELAY_URL = os.environ.get("E2E_RELAY_URL", "http://localhost:8080")
PAGE_RE = re.compile(r"^/m/([A-Za-z0-9_.-]{1,64})/?$")
LIB_RE = re.compile(r"^/m/([A-Za-z0-9_.-]{1,64})/lib\.js$")
PAGE_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>report marker __MARKER__</title>
</head>
<body>
<h1>report marker __MARKER__</h1>
<script>window.__inlineBlocked = "marker __MARKER__";</script>
<script src="https://evil.example/x.js?marker=__MARKER__"></script>
<script src="/m/__MARKER__/lib.js" integrity="sha384-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" crossorigin="anonymous"></script>
<img src="http://127.0.0.1:9/marker-__MARKER__.png" alt="">
<script src="/m/__MARKER__/lib.js"></script>
<script>
try { navigator.geolocation.getCurrentPosition(function(){}, function(){}); } catch (e) {}
try { document.write("<!-- marker __MARKER__ -->"); } catch (e) {}
</script>
</body>
</html>
"""
LIB_BODY = 'console.log("marker lib __MARKER__");\n'


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return

    def _report_headers(self):
        self.send_header("Reporting-Endpoints", 'default="%s/"' % RELAY_URL)
        self.send_header("Report-To", '{"group":"default","max_age":60,"endpoints":[{"url":"%s/"}]}' % RELAY_URL)
        # report-uri without report-to: Chrome 154 uploads legacy reports at once, while a report-to directive silences them and its V1 batch never arrives.
        self.send_header("Content-Security-Policy", "default-src 'self'; script-src 'self'; img-src 'self'; connect-src 'self'; report-uri %s/" % RELAY_URL)
        self.send_header("NEL", '{"report_to":"default","max_age":60,"success_fraction":1.0,"failure_fraction":1.0}')
        self.send_header("Cross-Origin-Opener-Policy", 'same-origin; report-to="default"')
        self.send_header("Cross-Origin-Embedder-Policy", 'require-corp; report-to="default"')
        self.send_header("Permissions-Policy", 'geolocation=(), camera=(), microphone=()')
        self.send_header("Document-Policy", "document-write=?0")

    def do_GET(self):
        if self.path == "/healthz":
            body = b"ok\n"
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        match = PAGE_RE.match(self.path.split("?")[0])
        if match:
            marker = match.group(1)
            body = PAGE_TEMPLATE.replace("__MARKER__", marker).encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self._report_headers()
            self.end_headers()
            self.wfile.write(body)
            return
        lib = LIB_RE.match(self.path.split("?")[0])
        if lib:
            body = LIB_BODY.replace("__MARKER__", lib.group(1)).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/javascript")
            self.send_header("Content-Length", str(len(body)))
            self._report_headers()
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(404)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_HEAD(self):
        self.send_response(200)
        self.send_header("Content-Length", "0")
        self.end_headers()


if __name__ == "__main__":
    server = ThreadingHTTPServer(("127.0.0.1", SITE_PORT), Handler)
    print("e2e site on 127.0.0.1:%d reporting to %s" % (SITE_PORT, RELAY_URL))
    server.serve_forever()
