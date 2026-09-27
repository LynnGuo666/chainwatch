#!/usr/bin/env python3
"""Exercise a hub and agent using temporary keys, TLS, and localhost ports."""

import argparse
import base64
import hashlib
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
                login_page = urllib.request.urlopen(base + "/login", context=context, timeout=2)
                login_html = login_page.read().decode()
                policy = login_page.headers["Content-Security-Policy"]
                scripts = [body for attrs, body in re.findall(r"<script\b([^>]*)>(.*?)</script>", login_html, re.S) if "src=" not in attrs and body]
                assert scripts and all("'sha256-" + base64.b64encode(hashlib.sha256(script.encode()).digest()).decode() + "'" in policy for script in scripts)
                login = urllib.request.Request(
                    base + "/api/login",
                    data=json.dumps({"username": "tester", "password": password}).encode(),
                    headers={"Content-Type": "application/json", "Origin": base},
                )
                login_response = urllib.request.urlopen(login, context=context, timeout=2)
                assert login_response.status == 204
                cookie = login_response.headers["Set-Cookie"].split(";", 1)[0]
                assert "Secure" in login_response.headers["Set-Cookie"]
                topology_request = urllib.request.Request(base + "/api/topology", headers={"Cookie": cookie})
                topology_body = urllib.request.urlopen(topology_request, context=context, timeout=2).read()
                assert agent_key.encode() not in topology_body, "agent key leaked through topology"
                assert len(json.loads(topology_body)) == 2
                edit_request = urllib.request.Request(
                    base + "/api/nodes/edge", data=json.dumps({"name": "Edited Edge", "note": "Test node"}).encode(),
                    headers={"Cookie": cookie, "Origin": base, "Content-Type": "application/json"}, method="PUT",
                )
                assert urllib.request.urlopen(edit_request, context=context, timeout=2).status == 200
                topology = json.load(urllib.request.urlopen(topology_request, context=context, timeout=2))
                assert topology[1]["name"] == "Edited Edge" and topology[1]["note"] == "Test node"
                forbidden_request = urllib.request.Request(
                    base + "/api/nodes/edge", data=b'{}', headers={"Cookie": cookie, "Origin": "https://evil.example"}, method="PUT",
                )
                try:
                    urllib.request.urlopen(forbidden_request, context=context, timeout=2)
                    raise AssertionError("cross-origin edit succeeded")
                except urllib.error.HTTPError as error:
                    assert error.code == 403
                snapshot = None
                for _ in range(30):
                    try:
                        request = urllib.request.Request(base + "/api/snapshot", headers={"Cookie": cookie})
                        snapshot = json.load(urllib.request.urlopen(request, context=context, timeout=2))
                        if len(snapshot["nodes"]) == 2 and len(snapshot["links"]) == 2:
                            break
                    except (OSError, ValueError):
                        pass
                    time.sleep(0.5)
                assert snapshot and len(snapshot["nodes"]) == 2 and len(snapshot["links"]) == 2, snapshot
                assert all(node["system"]["version"] == "0.4.0" for node in snapshot["nodes"]), snapshot
                request = urllib.request.Request(base + "/", headers={"Cookie": cookie})
                html = urllib.request.urlopen(request, context=context, timeout=2).read().decode()
                assert "Chainwatch" in html
                asset = re.search(r'(?:src|href)="(/_next/static/[^"]+\.js)"', html)
                assert asset, "Next.js bundle URL missing"
                request = urllib.request.Request(base + asset.group(1))
                assert urllib.request.urlopen(request, context=context, timeout=2).status == 200
                for path in ("/api/diagnosis", "/api/version"):
                    request = urllib.request.Request(base + path, headers={"Cookie": cookie})
                    response = urllib.request.urlopen(request, context=context, timeout=2)
                    assert response.status == 200
                    if path == "/api/version":
                        versions = json.load(response)
                        assert versions["hub"] == "0.4.0" and versions["frontend"] == "0.4.0"
                print("smoke OK: HTTPS login, two reports, two links, diagnosis, versions, embedded Next.js assets")
            finally:
                agent_proc.terminate()
                agent_proc.wait(timeout=5)
        finally:
            hub_proc.terminate()
            hub_proc.wait(timeout=5)


if __name__ == "__main__":
    main()
