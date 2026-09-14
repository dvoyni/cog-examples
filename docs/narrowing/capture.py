"""Launch one demo build, pause it, step it onto an exact tick, capture twice."""
import json, os, subprocess, sys, time
import mcpc

def wait_port(timeout=40):
    import socket
    end = time.time() + timeout
    while time.time() < end:
        s = socket.socket()
        s.settimeout(0.5)
        try:
            s.connect(("127.0.0.1", 7654)); s.close(); return True
        except Exception:
            time.sleep(0.5)
        finally:
            try: s.close()
            except Exception: pass
    return False

def call(name, args):
    r = mcpc.rpc("tools/call", {"name": name, "arguments": args})
    if "error" in r:
        raise SystemExit("tool error: " + json.dumps(r["error"])[:400])
    return r["result"].get("structuredContent", {})

def main():
    exe, cwd, tick, out = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
    proc = subprocess.Popen([exe], cwd=cwd,
                            stdout=open(out + ".log", "w"), stderr=subprocess.STDOUT)
    try:
        if not wait_port():
            raise SystemExit("mcp server never came up")
        time.sleep(3)  # let residency settle
        mcpc.SESSION["id"] = None
        mcpc.init()
        st = call("app_time", {"action": "pause"})
        now = st["tick"]
        if now > tick:
            raise SystemExit(f"already past the target tick: {now} > {tick}")
        while now < tick:
            step = min(tick - now, 600)
            st = call("app_time", {"action": "step", "steps": step})
            now = st["tick"]
        print("paused at tick", now)
        a = call("gfx_capture", {"path": out + "_a.png"})
        b = call("gfx_capture", {"path": out + "_b.png"})
        print("captured", a["pixelWidth"], "x", a["pixelHeight"])
        frame = call("gfx_frame", {"path": out + "_frame.json"})
        print("frame dumped")
    finally:
        proc.terminate()
        try: proc.wait(timeout=10)
        except Exception: proc.kill()

main()
