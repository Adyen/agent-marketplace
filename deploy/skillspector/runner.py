import argparse
import base64
import json
import os
import re
import select
import signal
import socket
import socketserver
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unicodedata
from pathlib import Path

VERSION = "2.13.6"
SCANNER_BINARY = "/opt/scanner/bin/skillspector"
SCANNER_EXEC = (
    "import os, resource, sys; "
    "limit = int(sys.argv[1]); "
    "resource.setrlimit(resource.RLIMIT_FSIZE, (limit, limit)); "
    "os.execv(sys.argv[2], sys.argv[2:])"
)
TMP_ROOT = "/tmp"
MAX_BODY_BYTES = 6 * 1024 * 1024
MAX_FILE_BYTES = 1024 * 1024
MAX_BUNDLE_BYTES = 4 * 1024 * 1024
MAX_FILES = 128
MAX_FINDINGS = 128
MAX_RESPONSE_BYTES = 128 * 1024
MAX_SCANNER_OUTPUT_BYTES = 8 * 1024 * 1024
MAX_HEADER_BYTES = 16 * 1024
MAX_PATH_BYTES = 512
REQUEST_TIMEOUT = 14.0
READ_TIMEOUT = 2.0
SCAN_TIMEOUT = 12.0
RULE_ID = re.compile(r"^(AE|AR|AS|DE|DS|EA|HC|MP|OH|PE|PI|RA|SC|SL|TM|TR|SS)[0-9]{1,3}$")
SEVERITIES = {"CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO", "INFORMATIONAL"}


class RequestError(Exception):
    def __init__(self, status):
        self.status = status


class ScanFailure(Exception):
    pass


class ScanTimeout(Exception):
    pass


class PeerDisconnected(Exception):
    pass


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result


def valid_path(value):
    if not isinstance(value, str) or not value:
        return False
    try:
        if len(value.encode("utf-8")) > MAX_PATH_BYTES:
            return False
    except UnicodeEncodeError:
        return False
    if value.startswith("/") or "\\" in value:
        return False
    parts = value.split("/")
    return all(part not in {"", ".", ".."} for part in parts) and not any(
        unicodedata.category(char) == "Cc" for char in value
    )


def decode_bundle(body):
    request = json.loads(body, object_pairs_hook=unique_object)
    if type(request) is not dict or set(request) != {"files"}:
        raise ValueError("invalid request shape")
    files = request["files"]
    if type(files) is not dict or not 1 <= len(files) <= MAX_FILES or "SKILL.md" not in files:
        raise ValueError("invalid bundle")
    names = list(files)
    for index, name in enumerate(names):
        if not valid_path(name):
            raise ValueError("invalid path")
        if any(other.startswith(name + "/") or name.startswith(other + "/") for other in names[index + 1:]):
            raise ValueError("path collision")
    decoded = {}
    total = 0
    for name, encoded in files.items():
        if not isinstance(encoded, str):
            raise ValueError("invalid file data")
        try:
            contents = base64.b64decode(encoded, validate=True)
        except (ValueError, UnicodeEncodeError):
            raise ValueError("invalid file data") from None
        if len(contents) > MAX_FILE_BYTES:
            raise ValueError("file too large")
        total += len(contents)
        if total > MAX_BUNDLE_BYTES:
            raise ValueError("bundle too large")
        decoded[name] = contents
    return decoded


def read_ready(connection, deadline):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise RequestError(408)
    readable, _, _ = select.select([connection], [], [], min(READ_TIMEOUT, remaining))
    return bool(readable)


def receive(connection, buffer, required, deadline):
    while len(buffer) < required:
        if not read_ready(connection, deadline):
            continue
        chunk = connection.recv(min(65536, required - len(buffer)))
        if not chunk:
            raise RequestError(400)
        buffer.extend(chunk)
    return bytes(buffer)


def read_request(connection, deadline):
    header = bytearray()
    while b"\r\n\r\n" not in header:
        if len(header) >= MAX_HEADER_BYTES:
            raise RequestError(431)
        if not read_ready(connection, deadline):
            continue
        chunk = connection.recv(min(4096, MAX_HEADER_BYTES + 1 - len(header)))
        if not chunk:
            raise RequestError(400)
        header.extend(chunk)
    raw_header, pending = bytes(header).split(b"\r\n\r\n", 1)
    try:
        lines = raw_header.decode("ascii").split("\r\n")
        method, target, version = lines[0].split(" ")
    except (UnicodeDecodeError, ValueError):
        raise RequestError(400) from None
    if version not in {"HTTP/1.0", "HTTP/1.1"}:
        raise RequestError(400)
    headers = {}
    for line in lines[1:]:
        if ":" not in line or line[:1].isspace():
            raise RequestError(400)
        key, value = line.split(":", 1)
        key = key.lower()
        if not key or key in headers:
            raise RequestError(400)
        headers[key] = value.strip()
    if "transfer-encoding" in headers or "expect" in headers:
        raise RequestError(400)
    if headers.get("content-type", "").split(";", 1)[0].strip().lower() != "application/json":
        raise RequestError(400)
    length = headers.get("content-length", "")
    if not length.isdecimal():
        raise RequestError(411)
    size = int(length)
    if size > MAX_BODY_BYTES:
        raise RequestError(413)
    body = bytearray(pending)
    if len(body) > size:
        raise RequestError(400)
    return method, target, receive(connection, body, size, deadline), headers


def write_response(connection, status, body, deadline):
    encoded = json.dumps(body, ensure_ascii=True, separators=(",", ":")).encode("ascii")
    if len(encoded) > MAX_RESPONSE_BYTES:
        status = 502
        encoded = b'{"error":"scan unavailable"}'
    reasons = {200: "OK", 400: "Bad Request", 404: "Not Found", 405: "Method Not Allowed",
               408: "Request Timeout", 411: "Length Required", 413: "Payload Too Large",
               431: "Request Header Fields Too Large", 502: "Bad Gateway",
               503: "Service Unavailable", 504: "Gateway Timeout"}
    response = (
        f"HTTP/1.1 {status} {reasons[status]}\r\n"
        f"Content-Type: application/json\r\nContent-Length: {len(encoded)}\r\n"
        "Connection: close\r\nCache-Control: no-store\r\n\r\n"
    ).encode("ascii") + encoded
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        return
    connection.settimeout(min(READ_TIMEOUT, remaining))
    try:
        connection.sendall(response)
    except OSError:
        pass


def peer_closed(connection):
    try:
        readable, _, _ = select.select([connection], [], [], 0)
        return bool(readable and connection.recv(1, socket.MSG_PEEK) == b"")
    except OSError:
        return True


def write_bundle(root, files):
    bundle = root / "bundle"
    bundle.mkdir(mode=0o700)
    for name, contents in files.items():
        destination = bundle.joinpath(*name.split("/"))
        destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        descriptor = os.open(
            destination,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0),
            0o600,
        )
        with os.fdopen(descriptor, "wb") as output:
            output.write(contents)
    return bundle


def kill_scanner(process):
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=1)
    except subprocess.TimeoutExpired:
        pass


def normalize_result(raw, input_paths):
    if type(raw) is not dict or raw.get("execution_successful") is not True:
        raise ScanFailure()
    metadata = raw.get("metadata")
    completeness = raw.get("analysis_completeness")
    issues = raw.get("issues")
    if type(metadata) is not dict or metadata.get("skillspector_version") != VERSION:
        raise ScanFailure()
    if type(completeness) is not dict or type(issues) is not list or len(issues) > MAX_FINDINGS:
        raise ScanFailure()
    coverage = completeness.get("status")
    if coverage == "complete" and completeness.get("is_complete", True) is True:
        status = "complete_with_caveats" if completeness.get("limitations") else "complete"
    elif coverage == "complete_with_caveats":
        status = "complete_with_caveats"
    elif coverage == "partial" and completeness.get("is_complete") is False:
        status = "partial"
    else:
        raise ScanFailure()
    findings = []
    for issue in issues:
        if type(issue) is not dict or not isinstance(issue.get("id"), str):
            raise ScanFailure()
        severity = issue.get("severity")
        if not isinstance(severity, str) or severity.upper() not in SEVERITIES:
            raise ScanFailure()
        code = issue["id"] if RULE_ID.fullmatch(issue["id"]) else "SS_OTHER"
        finding = {"code": code, "severity": severity.upper()}
        location = issue.get("location")
        if type(location) is dict:
            path = location.get("file")
            if valid_path(path) and path in input_paths:
                finding["path"] = path
        findings.append(finding)
    return {"version": VERSION, "status": status, "findings": findings}


def scan(connection, files, request_deadline, stopping):
    try:
        with tempfile.TemporaryDirectory(prefix="skillspector-", dir=TMP_ROOT) as temporary:
            process = None
            try:
                root = Path(temporary)
                os.chmod(root, 0o700)
                bundle = write_bundle(root, files)
                output_path = root / "scanner-output.json"
                descriptor = os.open(output_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                env = {
                    "PATH": "/opt/scanner/bin:/usr/bin:/bin",
                    "HOME": str(root),
                    "TMPDIR": str(root),
                    "SKILLSPECTOR_OSV_TIMEOUT": "0",
                    "PYTHONDONTWRITEBYTECODE": "1",
                }
                argv = [
                    sys.executable,
                    "-I",
                    "-c",
                    SCANNER_EXEC,
                    str(MAX_SCANNER_OUTPUT_BYTES),
                    SCANNER_BINARY,
                    "scan",
                    str(bundle),
                    "--no-llm",
                    "--format",
                    "json",
                ]
                with os.fdopen(descriptor, "wb") as output:
                    process = subprocess.Popen(
                        argv,
                        cwd=root,
                        env=env,
                        stdin=subprocess.DEVNULL,
                        stdout=output,
                        stderr=subprocess.DEVNULL,
                        start_new_session=True,
                    )
                    scan_deadline = min(time.monotonic() + SCAN_TIMEOUT, request_deadline - 0.2)
                    while process.poll() is None:
                        if stopping.is_set() or peer_closed(connection):
                            raise PeerDisconnected()
                        remaining = scan_deadline - time.monotonic()
                        if remaining <= 0:
                            raise ScanTimeout()
                        try:
                            process.wait(timeout=min(0.1, remaining))
                        except subprocess.TimeoutExpired:
                            pass
                if process.returncode not in (0, 1):
                    raise ScanFailure()
                if output_path.stat().st_size > MAX_SCANNER_OUTPUT_BYTES:
                    raise ScanFailure()
                output = output_path.read_bytes()
                raw = json.loads(output, object_pairs_hook=unique_object)
                return normalize_result(raw, set(files))
            finally:
                if process is not None and process.poll() is None:
                    kill_scanner(process)
    except PeerDisconnected:
        raise
    except ScanTimeout:
        raise
    except Exception:
        raise ScanFailure() from None


class RequestHandler(socketserver.BaseRequestHandler):
    def handle(self):
        deadline = time.monotonic() + REQUEST_TIMEOUT
        status = 502
        response = {"error": "scan unavailable"}
        try:
            method, target, body, _ = read_request(self.request, deadline)
            if method != "POST":
                status = 405
                response = {"error": "invalid request"}
            elif target != "/v1/scan":
                status = 404
                response = {"error": "invalid request"}
            else:
                try:
                    files = decode_bundle(body)
                except (ValueError, UnicodeError):
                    status = 400
                    response = {"error": "invalid request"}
                else:
                    response = scan(self.request, files, deadline, self.server.stopping)
                    status = 200
        except RequestError as error:
            status = error.status
            response = {"error": "invalid request"}
        except PeerDisconnected:
            return
        except ScanTimeout:
            status = 504
        except Exception:
            status = 502
        write_response(self.request, status, response, deadline)


class BoundedThreadingMixIn(socketserver.ThreadingMixIn):
    daemon_threads = False
    block_on_close = True
    request_queue_size = 1

    def __init__(self, *args, **kwargs):
        self.scan_slot = threading.BoundedSemaphore(1)
        self.stopping = threading.Event()
        super().__init__(*args, **kwargs)

    def process_request(self, request, client_address):
        if not self.scan_slot.acquire(blocking=False):
            try:
                request.settimeout(0.5)
                request.sendall(b'HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n')
            except OSError:
                pass
            self.shutdown_request(request)
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self.scan_slot.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.scan_slot.release()


class TCPScanServer(BoundedThreadingMixIn, socketserver.TCPServer):
    allow_reuse_address = True


class UnixScanServer(BoundedThreadingMixIn, socketserver.UnixStreamServer):
    def server_close(self):
        super().server_close()
        try:
            current = os.lstat(self.server_address)
            if stat.S_ISSOCK(current.st_mode) and (current.st_dev, current.st_ino) == self.socket_identity:
                os.unlink(self.server_address)
        except FileNotFoundError:
            pass


def make_tcp_server(address):
    return TCPScanServer(address, RequestHandler)


def make_unix_server(path):
    if not os.path.isabs(path):
        raise ValueError("Unix socket path must be absolute")
    directory = os.path.dirname(path)
    os.makedirs(directory, mode=0o700, exist_ok=True)
    directory_stat = os.stat(directory, follow_symlinks=False)
    if not stat.S_ISDIR(directory_stat.st_mode) or directory_stat.st_uid != os.geteuid():
        raise ValueError("Unix socket directory is not private")
    os.chmod(directory, 0o700)
    try:
        existing = os.lstat(path)
    except FileNotFoundError:
        existing = None
    if existing is not None:
        if not stat.S_ISSOCK(existing.st_mode) or existing.st_uid != os.geteuid():
            raise ValueError("Unix socket path is occupied")
        os.unlink(path)
    server = UnixScanServer(path, RequestHandler)
    os.chmod(path, 0o600)
    created = os.lstat(path)
    server.socket_identity = (created.st_dev, created.st_ino)
    return server


def main():
    parser = argparse.ArgumentParser()
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--unix", default="/run/skillspector/scan.sock")
    group.add_argument("--port", type=int)
    args = parser.parse_args()
    if args.port is not None:
        if not 1 <= args.port <= 65535:
            parser.error("--port must be between 1 and 65535")
        server = make_tcp_server(("0.0.0.0", args.port))
    else:
        server = make_unix_server(args.unix)

    def stop(*_):
        server.stopping.set()
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        server.serve_forever(poll_interval=0.2)
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
