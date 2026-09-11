import json, sys, urllib.request

URL = "http://127.0.0.1:7654/mcp"
SESSION = {"id": None}

def rpc(method, params=None, notify=False):
    body = {"jsonrpc": "2.0", "method": method}
    if params is not None:
        body["params"] = params
    if not notify:
        body["id"] = 1
    data = json.dumps(body).encode()
    headers = {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    if SESSION["id"]:
        headers["Mcp-Session-Id"] = SESSION["id"]
    req = urllib.request.Request(URL, data=data, headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=120) as r:
        sid = r.headers.get("Mcp-Session-Id")
        if sid:
            SESSION["id"] = sid
        raw = r.read().decode("utf-8", "replace")
    if notify:
        return None
    # streamable http may reply as SSE
    for line in raw.splitlines():
        if line.startswith("data:"):
            return json.loads(line[5:].strip())
    return json.loads(raw) if raw.strip() else None

def init():
    r = rpc("initialize", {
        "protocolVersion": "2025-06-18",
        "capabilities": {},
        "clientInfo": {"name": "burst", "version": "1"},
    })
    rpc("notifications/initialized", {}, notify=True)
    return r

if __name__ == "__main__":
    init()
    cmd = sys.argv[1]
    if cmd == "list":
        res = rpc("tools/list", {})
        for t in res["result"]["tools"]:
            print("==", t["name"])
            print("  ", (t.get("description") or "").split("\n")[0][:160])
            print("   schema:", json.dumps(t.get("inputSchema", {}))[:400])
    elif cmd == "call":
        name = sys.argv[2]
        args = json.loads(sys.argv[3]) if len(sys.argv) > 3 else {}
        res = rpc("tools/call", {"name": name, "arguments": args})
        print(json.dumps(res, indent=2)[:4000])
