#!/usr/bin/env python3
"""Synthetic, allowlisted HTTPS fixture for disposable Cursor host smokes."""

import argparse
import json
import os
import socket
import ssl
import threading
import time


TEAMS = [
    {"id": 30677936, "name": "Other smoke team", "requestQuotaPerSeat": 8},
    {"id": 30677937, "name": "Provider Usage smoke team", "requestQuotaPerSeat": 2},
]
ALLOWED_HOSTS = {"cursor.com:443", "api2.cursor.sh:443"}


def read_mode(path):
    try:
        with open(path, encoding="utf-8") as mode_file:
            return mode_file.read().strip() or "default"
    except OSError:
        return "default"


def response_for(path, body, mode):
    payload = json.loads(body or "{}")
    status = 200
    if path == "/api/auth/me":
        data = {"sub": "auth0|42424242", "email": "provider-smoke@example.test"}
    elif path == "/api/usage-summary":
        if "teamId" in payload:
            data = {
                "teamId": payload["teamId"],
                "membershipType": "team",
                "billingCycleEnd": "2026-11-01T00:00:00Z",
                "individualUsage": {"plan": {"enabled": True, "totalPercentUsed": 37, "autoPercentUsed": 12, "apiPercentUsed": 4}},
            }
        else:
            data = {
                "membershipType": "pro",
                "billingCycleStart": "2026-10-01T00:00:00Z",
                "billingCycleEnd": "2026-11-01T00:00:00Z",
                "individualUsage": {
                    "plan": {"enabled": True, "totalPercentUsed": 62.5, "autoPercentUsed": 28, "apiPercentUsed": 10},
                    "onDemand": {"enabled": True, "used": 12.34, "limit": 100, "remaining": 87.66},
                },
            }
    elif path == "/api/usage":
        data = {"gpt-4": {"numRequests": 37, "numRequestsTotal": 50, "maxRequestUsage": 750}}
    elif path == "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
        data = {
            "enabled": True,
            "billingCycleEnd": "2026-11-01T00:00:00Z",
            "planUsage": {"totalPercentUsed": 62.5, "autoPercentUsed": 28, "apiPercentUsed": 10},
            "spendLimitUsage": {"individualUsed": "1234"},
        }
    elif path == "/api/dashboard/teams":
        data = {"teams": TEAMS}
    elif path == "/api/dashboard/team":
        data = {"teamId": payload.get("teamId", 30677937), "userId": 42424242}
    elif path == "/api/dashboard/get-team-spend":
        data = {
            "teamId": payload.get("teamId", 30677937),
            "teamMemberSpend": [
                {"userId": 999, "email": "other-member@example.test", "fastPremiumRequests": 900, "spendCents": 99999},
                {"userId": 42424242, "email": "provider-smoke@example.test", "fastPremiumRequests": 37, "spendCents": 1534, "hardLimitOverrideDollars": 250},
            ],
        }
    elif path == "/api/dashboard/get-filtered-usage-events":
        if mode == "daily-503":
            status, data = 503, {"error": "synthetic optional endpoint failure"}
        elif mode == "excessive-events":
            data = {"totalUsageEventsCount": 2001, "usageEventsDisplay": []}
        else:
            event = {
                "timestamp": str(int(payload.get("startDate", int(time.time() * 1000))) + 3_600_000),
                "chargedCents": 1234,
                "tokenUsage": {"totalCents": 250},
                "cursorTokenFee": 50,
            }
            if payload.get("teamId"):
                event.update({"owningTeam": "30677937", "owningUser": "42424242"})
                if mode == "mismatch-team":
                    event["owningTeam"] = "30677936"
                if mode == "mismatch-user":
                    event["owningUser"] = "999"
            data = {"totalUsageEventsCount": 1, "usageEventsDisplay": [event]}
    else:
        status, data = 404, {"error": "unknown synthetic fixture route"}
    return status, data


def log_request(path, host, method, body, mode, log_path):
    row = {"host": host, "method": method, "path": path.split("?", 1)[0], "mode": mode}
    try:
        payload = json.loads(body or "{}")
        for key in ("teamId", "userId", "page", "pageSize", "startDate", "endDate", "activeOnly"):
            if key in payload:
                row[key] = payload[key]
    except ValueError:
        row["body"] = "<unparsed>"
    with open(log_path, "a", encoding="utf-8") as log_file:
        log_file.write(json.dumps(row, sort_keys=True) + "\n")


def send_response(stream, status, data):
    raw = json.dumps(data, separators=(",", ":")).encode()
    phrase = {200: "OK", 404: "Not Found", 503: "Service Unavailable"}.get(status, "Error")
    stream.sendall(
        f"HTTP/1.1 {status} {phrase}\r\nContent-Type: application/json\r\n"
        f"Content-Length: {len(raw)}\r\nConnection: close\r\n\r\n".encode() + raw
    )


def serve(connection, args, tls_context):
    with connection:
        connection.settimeout(20)
        request_stream = connection.makefile("rb")
        request_line = request_stream.readline(8192)
        parts = request_line.decode("latin1").strip().split()
        if len(parts) < 3 or parts[0].upper() != "CONNECT":
            connection.sendall(b"HTTP/1.1 405 CONNECT Required\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
            return
        host = parts[1].lower()
        if host not in ALLOWED_HOSTS:
            connection.sendall(b"HTTP/1.1 403 Synthetic fixture blocks external hosts\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
            return
        while True:
            header = request_stream.readline(8192)
            if not header or header in (b"\r\n", b"\n"):
                break
        connection.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
        tls = tls_context.wrap_socket(connection, server_side=True)
        with tls:
            stream = tls.makefile("rb")
            request_line = stream.readline(8192)
            if not request_line:
                return
            method, path, *_ = request_line.decode("latin1").strip().split()
            headers = {}
            while True:
                header = stream.readline(8192)
                if not header or header in (b"\r\n", b"\n"):
                    break
                if b":" in header:
                    key, value = header.split(b":", 1)
                    headers[key.decode("latin1").lower()] = value.decode("latin1").strip()
            length = int(headers.get("content-length", "0") or 0)
            body = stream.read(length).decode("utf-8", "replace") if length else ""
            current_mode = read_mode(args.mode_file)
            log_request(path, host, method, body, current_mode, args.log)
            status, data = response_for(path.split("?", 1)[0], body, current_mode)
            send_response(tls, status, data)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bind", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=43972)
    parser.add_argument("--cert", required=True, help="TLS leaf certificate trusted by the disposable host")
    parser.add_argument("--key", required=True, help="TLS leaf private key")
    parser.add_argument("--mode-file", required=True, help="file containing default, daily-503, excessive-events, mismatch-team or mismatch-user")
    parser.add_argument("--log", required=True, help="task-owned JSONL request log; auth headers and cookie values are never written")
    args = parser.parse_args()
    os.makedirs(os.path.dirname(os.path.abspath(args.log)), exist_ok=True)
    if os.path.exists(args.log):
        os.unlink(args.log)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(args.cert, args.key)
    server = socket.socket()
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind((args.bind, args.port))
    server.listen(64)
    print(f"synthetic Cursor HTTPS proxy listening on {args.bind}:{args.port}", flush=True)
    while True:
        connection, _ = server.accept()
        threading.Thread(target=serve, args=(connection, args, context), daemon=True).start()


if __name__ == "__main__":
    main()
