"""Check plugin MCP routing through a started mock task on a disposable host."""

import json
import os
import pathlib
import urllib.request

url = os.environ["SMOKE_MCP_URL"]
headers = {
    "Content-Type": "application/json",
    "Accept": "application/json, text/event-stream",
}
records = []


def rpc(request_id, method, params):
    request = urllib.request.Request(
        url,
        data=json.dumps({
            "jsonrpc": "2.0", "id": request_id, "method": method, "params": params,
        }).encode(),
        headers=headers,
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        session_id = response.headers.get("Mcp-Session-Id")
        if session_id:
            headers["Mcp-Session-Id"] = session_id
        raw = response.read().decode()
    payload = raw if raw.startswith("{") else next(
        line[6:] for line in raw.splitlines() if line.startswith("data: ")
    )
    result = json.loads(payload)
    records.append({"method": method, "response": result})
    return result


initialized = rpc(1, "initialize", {
    "protocolVersion": "2025-03-26", "capabilities": {},
    "clientInfo": {"name": "provider-usage-sdk-smoke", "version": "1"},
})
assert initialized["result"]["serverInfo"]["name"] == "kandev-mcp"
catalog = rpc(2, "tools/list", {})
name = "kandev_kandev_provider_usage_get_provider_usage"
assert len([tool for tool in catalog["result"]["tools"] if tool["name"] == name]) == 1
success = rpc(3, "tools/call", {"name": name, "arguments": {}})
assert not success["result"].get("isError", False)
assert success["result"]["structuredContent"]["schema_version"] == "1"
assert success["result"]["structuredContent"]["scope"]["user_scoped"] is False
invalid = rpc(4, "tools/call", {
    "name": name, "arguments": {"workspace_id": "cannot-select"},
})
assert "error" in invalid or invalid["result"].get("isError")
output = pathlib.Path(__file__).resolve().parents[2] / ".build" / "sdk-migration"
output.mkdir(parents=True, exist_ok=True)
(output / "mcp-result.json").write_text(json.dumps(records, indent=2) + "\n")
print("MCP initialize, tool discovery, structured result and argument rejection passed")
