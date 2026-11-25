import base64
import http.client
import json
import os
import socket
import stat
import tempfile
import threading
import time
import unittest
from pathlib import Path
from urllib.parse import urlsplit

import runner


class RunnerProtocolTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.tmp_root = self.root / "tmp"
        self.tmp_root.mkdir()
        self.capture = self.root / "capture.json"
        self.old_binary = runner.SCANNER_BINARY
        self.old_tmp_root = runner.TMP_ROOT
        self.old_timeout = runner.SCAN_TIMEOUT
        runner.SCANNER_BINARY = str(self.root / "scanner")
        runner.TMP_ROOT = self.tmp_root
        runner.SCAN_TIMEOUT = 0.5
        self.write_scanner({"metadata": {"skillspector_version": "2.13.6"},
                            "execution_successful": True,
                            "analysis_completeness": {"status": "partial"},
                            "issues": []})
        self.server = runner.make_tcp_server(("127.0.0.1", 0))
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.base = f"http://127.0.0.1:{self.server.server_address[1]}"

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)
        runner.SCANNER_BINARY = self.old_binary
        runner.TMP_ROOT = self.old_tmp_root
        runner.SCAN_TIMEOUT = self.old_timeout
        self.temp.cleanup()

    def write_scanner(self, result, exit_code=0, delay=0):
        script = "\n".join([
            f"#!{os.sys.executable}",
            "import json, pathlib, sys, time",
            f"time.sleep({delay!r})",
            "root = pathlib.Path(sys.argv[2])",
            f"capture = pathlib.Path({str(self.capture)!r})",
            "files = {path.relative_to(root).as_posix(): path.read_text() for path in root.rglob('*') if path.is_file()}",
            "capture.write_text(json.dumps({'argv': sys.argv[1:], 'files': files}))",
            "sys.stdout.write(" + repr(json.dumps(result)) + ")",
            f"sys.exit({exit_code})",
        ]) + "\n"
        path = Path(runner.SCANNER_BINARY)
        path.write_text(script)
        path.chmod(0o755)

    def post(self, body):
        target = urlsplit(self.base)
        connection = http.client.HTTPConnection(target.hostname, target.port, timeout=3)
        connection.request("POST", "/v1/scan", body=body, headers={"Content-Type": "application/json"})
        response = connection.getresponse()
        result = response.status, response.read()
        connection.close()
        return result

    def encoded_bundle(self):
        return json.dumps({"files": {
            "SKILL.md": base64.b64encode(b"---\nname: inert\n---\nInstructions only.\n").decode(),
            "requirements.txt": base64.b64encode(b"requests==2.30.0\n").decode(),
        }}).encode()

    def test_partial_report_exposes_only_typed_issue_fields_and_cleans_input(self):
        self.write_scanner({
            "metadata": {"skillspector_version": "2.13.6", "private": "ignored"},
            "execution_successful": True,
            "analysis_completeness": {"status": "partial", "is_complete": False},
            "issues": [
                {"id": "SC4", "severity": "HIGH",
                 "location": {"file": "requirements.txt"},
                 "explanation": "untrusted explanation", "code_snippet": "private bytes"},
                {"id": "AE1", "severity": "MEDIUM",
                 "location": {"file": "/tmp/secret.txt"}, "finding": "private"},
            ],
        }, exit_code=1)
        status, body = self.post(self.encoded_bundle())
        self.assertEqual(status, 200, body)
        report = json.loads(body)
        self.assertEqual(report, {
            "version": "2.13.6",
            "status": "partial",
            "findings": [
                {"code": "SC4", "severity": "HIGH", "path": "requirements.txt"},
                {"code": "AE1", "severity": "MEDIUM"},
            ],
        })
        captured = json.loads(self.capture.read_text())
        argv = captured["argv"]
        self.assertEqual(argv[0], "scan")
        self.assertEqual(argv[2:], ["--no-llm", "--format", "json"])
        scanned_dir = Path(argv[1])
        self.assertEqual(captured["files"], {
            "SKILL.md": "---\nname: inert\n---\nInstructions only.\n",
            "requirements.txt": "requests==2.30.0\n",
        })
        self.assertFalse(scanned_dir.exists())
        self.assertEqual(list(self.tmp_root.iterdir()), [])

    def test_complete_report_and_unrecognized_coverage(self):
        self.write_scanner({
            "metadata": {"skillspector_version": "2.13.6"},
            "execution_successful": True,
            "analysis_completeness": {"status": "complete", "is_complete": True},
            "issues": [],
        })
        status, body = self.post(self.encoded_bundle())
        self.assertEqual(status, 200)
        self.assertEqual(json.loads(body), {"version": "2.13.6", "status": "complete", "findings": []})

        self.write_scanner({
            "metadata": {"skillspector_version": "2.13.6"},
            "execution_successful": True,
            "analysis_completeness": {"status": "unknown"},
            "issues": [],
        })
        status, body = self.post(self.encoded_bundle())
        self.assertEqual(status, 502)
        self.assertNotIn(b"unknown", body)

    def test_failed_execution_and_duplicate_json_keys_are_not_evidence(self):
        self.write_scanner({
            "metadata": {"skillspector_version": "2.13.6"},
            "execution_successful": False,
            "analysis_completeness": {"status": "failed"},
            "issues": [],
        }, exit_code=1)
        status, _ = self.post(self.encoded_bundle())
        self.assertEqual(status, 502)

        self.write_scanner({
            "metadata": {"skillspector_version": "2.13.6"},
            "execution_successful": True,
            "analysis_completeness": {"status": "partial"},
            "issues": [],
        })
        body = self.encoded_bundle().replace(b'"files":', b'"files":')
        body = body[:-1] + b',"files":{}}'
        status, _ = self.post(body)
        self.assertEqual(status, 400)

    def test_oversized_and_invalid_bundle_requests_are_rejected(self):
        target = urlsplit(self.base)
        connection = http.client.HTTPConnection(target.hostname, target.port, timeout=3)
        connection.putrequest("POST", "/v1/scan")
        connection.putheader("Content-Type", "application/json")
        connection.putheader("Content-Length", str(runner.MAX_BODY_BYTES + 1))
        connection.endheaders()
        response = connection.getresponse()
        self.assertEqual(response.status, 413)
        response.read()
        connection.close()

        invalid = [
            b'{"files":{"SKILL.md":"a","SKILL.md":"b"}}',
            json.dumps({"files": {"../outside": "eA=="}}).encode(),
            json.dumps({"files": {"SKILL.md": "eA==", "file": "eA==", "file/child": "eA=="}}).encode(),
            json.dumps({"files": {"other.txt": "eA=="}}).encode(),
            json.dumps({"files": {"SKILL.md": "not-base64!"}}).encode(),
        ]
        for body in invalid:
            with self.subTest(body=body[:48]):
                status, _ = self.post(body)
                self.assertEqual(status, 400)
        self.assertFalse(self.capture.exists())

    def test_scanner_timeout_is_bounded_and_generic(self):
        self.write_scanner({
            "metadata": {"skillspector_version": "2.13.6"},
            "execution_successful": True,
            "analysis_completeness": {"status": "complete"},
            "issues": [],
        }, delay=2)
        started = time.monotonic()
        status, body = self.post(self.encoded_bundle())
        self.assertLess(time.monotonic() - started, 2)
        self.assertEqual(status, 504)
        self.assertNotIn(b"Traceback", body)

    def test_unix_listener_is_private(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)
        socket_path = self.root / "scan.sock"
        server = runner.make_unix_server(str(socket_path))
        try:
            self.assertEqual(stat.S_IMODE(socket_path.stat().st_mode), 0o600)
        finally:
            server.server_close()


if __name__ == "__main__":
    unittest.main()
