#!/usr/bin/env python3
"""Exercise a hub and agent using temporary keys, TLS, and localhost ports."""

import argparse
import base64
import json
import pathlib
import re
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=pathlib.Path)
    binary = parser.parse_args().binary.resolve()
    web_port, ingest_port = free_port(), free_port()
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory)
        cert, key = root / "cert.pem", root / "key.pem"
        subprocess.run(
            ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN=localhost", "-keyout", str(key), "-out", str(cert), "-days", "1"],
            check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        agent_key = subprocess.check_output([str(binary), "gen-key"], text=True).strip()
        password = "local-integration-test-password-123"
        hashed = subprocess.check_output([str(binary), "hash-password"], input=password + "\n", text=True).strip()
        hub = {
            "mode": "hub", "id": "hub", "name": "Hub", "listen_web": f"127.0.0.1:{web_port}",
            "listen_ingest": f"127.0.0.1:{ingest_port}", "tls_cert": str(cert), "tls_key": str(key),
            "web_user": "tester", "web_password_hash": hashed,
            "nodes": [{"id": "edge", "name": "Edge", "source_ip": "127.0.0.1", "key": agent_key}],
            "links": [{"id": "local-web", "name": "Web", "target": "hub", "address": "127.0.0.1", "port": web_port, "protocol": "tcp", "mtr": False}],
            "interval_seconds": 30, "storage_path": str(root / "store.sqlite3"), "storage_max_mb": 32, "min_free_mb": 128,
        }
        agent = {
            "mode": "agent", "id": "edge", "name": "Edge", "hub_url": f"http://127.0.0.1:{ingest_port}/report",
            "agent_key": agent_key,
            "links": [{"id": "hub-web", "name": "Hub Web", "target": "hub", "address": "127.0.0.1", "port": web_port, "protocol": "tcp", "mtr": False}],
            "interval_seconds": 30,
        }
        (root / "hub.json").write_text(json.dumps(hub))
        (root / "agent.json").write_text(json.dumps(agent))
        hub_proc = subprocess.Popen([str(binary), "--config", str(root / "hub.json")], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)
        try:
            time.sleep(1)
            agent_proc = subprocess.Popen([str(binary), "--config", str(root / "agent.json")], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)
            try:
                context = ssl._create_unverified_context()
                base = f"https://127.0.0.1:{web_port}"
                try:
                    urllib.request.urlopen(base + "/api/snapshot", context=context, timeout=2)
                    raise AssertionError("unauthenticated API request succeeded")
                except urllib.error.HTTPError as error:
                    assert error.code == 401
                auth = "Basic " + base64.b64encode(("tester:" + password).encode()).decode()
                snapshot = None
                for _ in range(30):
                    try:
                        request = urllib.request.Request(base + "/api/snapshot", headers={"Authorization": auth})
                        snapshot = json.load(urllib.request.urlopen(request, context=context, timeout=2))
                        if len(snapshot["nodes"]) == 2 and len(snapshot["links"]) == 2:
                            break
                    except (OSError, ValueError):
                        pass
                    time.sleep(0.5)
                assert snapshot and len(snapshot["nodes"]) == 2 and len(snapshot["links"]) == 2, snapshot
                request = urllib.request.Request(base + "/", headers={"Authorization": auth})
                html = urllib.request.urlopen(request, context=context, timeout=2).read().decode()
                assert "Chainwatch" in html
                asset = re.search(r'(?:src|href)="(/_next/static/[^"]+\.js)"', html)
                assert asset, "Next.js bundle URL missing"
                request = urllib.request.Request(base + asset.group(1), headers={"Authorization": auth})
                assert urllib.request.urlopen(request, context=context, timeout=2).status == 200
                print("smoke OK: authenticated HTTPS, two reports, two links, embedded Next.js assets")
            finally:
                agent_proc.terminate()
                agent_proc.wait(timeout=5)
        finally:
            hub_proc.terminate()
            hub_proc.wait(timeout=5)


if __name__ == "__main__":
    main()
