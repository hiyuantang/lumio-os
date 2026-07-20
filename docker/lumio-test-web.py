# SPDX-License-Identifier: AGPL-3.0-only
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"lumio phase 5 web service\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        return


with open("/etc/lumio-test-web.json", encoding="utf-8") as config_file:
    config = json.load(config_file)

port = int(config["port"])
if port < 1024 or port > 65535:
    print("lumio-test-web: port must be between 1024 and 65535", flush=True)
    raise ValueError("port must be between 1024 and 65535")

ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
