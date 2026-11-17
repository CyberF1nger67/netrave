import os, sys, time, json, subprocess, socket, glob, re
sys.path.insert(0, __import__("os").path.dirname(__file__))
from live_api_helper import call, login, BASE

def sh(c):
    r = subprocess.run(c, shell=True, text=True, capture_output=True)
    return r.stdout + r.stderr

fails = []
def check(name, ok, detail=""):
    print(("[ok]   " if ok else "[FAIL] ") + name + ("" if ok or not detail else "  -> " + str(detail)[:300]))
    if not ok: fails.append(name)

pw = open(__import__("os").environ.get("NETRAVE_PW_FILE", "/tmp/e2e/pw.txt")).read().strip()
st, _ = login("admin", pw); check("admin login", st == 200, st)
call("POST", "/api/images/scan")
st, imgs = call("GET", "/api/images")
imgs = imgs if isinstance(imgs, list) else imgs.get("images", [])
img = next(i for i in imgs if i.get("dirname") == "echo" or "echo" in json.dumps(i))
st, tpls = call("GET", "/api/templates")
tpls = tpls if isinstance(tpls, list) else tpls.get("templates", [])
tpl = max((t for t in tpls if t.get("console") == "serial"), key=lambda t: t.get("nics", 0))
print("template:", tpl["name"], "image id", img["id"])

st, lab = call("POST", "/api/labs", {"name": "live-attach"}); check("lab creation", st in (200, 201), lab)
L = lab["id"]
def add_dev(name, x):
    st, d = call("POST", f"/api/labs/{L}/devices", {"template_id": tpl["id"], "name": name, "image_id": img["id"], "console": "serial", "x": x, "y": 100})
    check(f"adding machine {name}", st in (200, 201), d); return d
A = add_dev("A", 100); B = add_dev("B", 300)
st, net = call("POST", f"/api/labs/{L}/networks", {"type": "nat", "name": "NAT1", "x": 200, "y": 300})
check("NAT network creation", st in (200, 201), net)
NET = net["id"]

def topo():
    st, t = call("GET", f"/api/labs/{L}/topology")
    return t
def rid_of(name):
    return next(d["rid"] for d in topo()["devices"] if d["name"] == name)
def state_of(name):
    return next(d["state"] for d in topo()["devices"] if d["name"] == name)

for n in ["A", "B"]:
    st, r = call("POST", f"/api/devices/{rid_of(n)}/start")
    check(f"start {n}", st in (200, 202), r)
for _ in range(60):
    if all(state_of(n) == "running" for n in ["A", "B"]): break
    time.sleep(1)
check("both machines running", all(state_of(n) == "running" for n in ["A", "B"]), [state_of("A"), state_of("B")])
ra, rb = rid_of("A"), rid_of("B")
print("rid A", ra, "rid B", rb)

def tap_info(rid, nic=0):
    out = sh(f"ip -o link show nrt{rid:x}n{nic} 2>/dev/null")
    return out.strip()
def master(rid, nic=0):
    m = re.search(r"master (\S+)", tap_info(rid, nic))
    return m.group(1) if m else None
def qmp_network(rid_name_dev_path):
    pass
def qmp(sock, cmd_args):
    s = socket.socket(socket.AF_UNIX); s.settimeout(5); s.connect(sock)
    f = s.makefile("rwb")
    f.readline()
    def req(o):
        f.write((json.dumps(o)+"\n").encode()); f.flush()
        while True:
            r = json.loads(f.readline())
            if "return" in r or "error" in r: return r
    req({"execute": "qmp_capabilities"})
    r = req(cmd_args)
    s.close(); return r
RUN = sh("find /tmp/e2e/data -name '*.qmp' | head -20").split()
def qmp_set_link(rid, nic, up):
    sock = os.environ.get("NETRAVE_RUN_DIR", "/tmp/e2e/data/run") + f"/d{rid}.qmp"
    r = qmp(sock, {"execute": "set_link", "arguments": {"name": f"nic{nic}", "up": up}})
    return "error" not in r
def link_ok(name, nic=0):
    return qmp_set_link(rid_of(name), nic, True)

check("before cable: TAP A exists, not in a bridge", tap_info(ra) != "" and master(ra) is None, tap_info(ra))

# 1) Creating wire A.e0 - B.e0 while the machines are running
st, conn = call("POST", f"/api/labs/{L}/connections", {"a_type": "device", "a_id": next(d["id"] for d in topo()["devices"] if d["name"]=="A"), "a_if": 0,
     "b_type": "device", "b_id": next(d["id"] for d in topo()["devices"] if d["name"]=="B"), "b_if": 0})
check("cable creation with running machines: 201", st in (200, 201), conn)
CAB = conn["id"]
check("response contains up=true", conn.get("up") is True, conn)
check("no warnings", not conn.get("warnings"), conn.get("warnings"))
ma, mb = master(ra), master(rb)
check("TAP A joined the cable bridge", ma is not None and ma.startswith("nrl"), tap_info(ra))
check("TAP B in the same bridge as A", ma is not None and ma == mb, (ma, mb))
check("TAP A is up", " UP " in tap_info(ra) or "UP" in tap_info(ra).split(">")[0], tap_info(ra))
check("QEMU: set_link up for A accepted", link_ok("A"))
check("QEMU: set_link up for B accepted", link_ok("B"))
print(sh(f"bridge link show | grep -E '{ma}' ; ip -br link show {ma}"))

# 2) A machine wire to a NAT network, the machine is running
st, conn2 = call("POST", f"/api/labs/{L}/connections", {"a_type": "device", "a_id": next(d["id"] for d in topo()["devices"] if d["name"]=="A"), "a_if": 1,
     "b_type": "network", "b_id": NET, "b_if": 0})
check("cable A.e1 — NAT with running machine: 201", st in (200, 201), conn2)
A_e1 = conn2.get("id")
# A.e1 - does machine A have exactly 2 interfaces? check nics
nics = next(d["nics"] for d in topo()["devices"] if d["name"]=="A")
print("nics A:", nics)
if nics >= 2:
    check("TAP A.e1 in NAT bridge nrnat", master(ra, 1) == "nrnat", tap_info(ra, 1))
    check("isolation: TAP A.e1 in group nft rules", f"nrt{ra:x}n1" in sh("nft list table bridge netrave_iso"), sh("nft list table bridge netrave_iso")[:300])
    check("QEMU: set_link up for A.e1 accepted", link_ok("A", 1))
else:
    print("template 1 has one interface — NAT connection check skipped (see below)")

# 3) Removing a wire on the fly (already implemented earlier): the A-B link is broken
st, r = call("POST", f"/api/labs/{L}/delete", {"connections": [CAB]})
check("removing cable A-B with running machines: 200", st == 200, r)
check("after removal TAP A is outside a bridge", master(ra) is None, tap_info(ra))

# 4) Reconnecting the same port without a restart
st, conn3 = call("POST", f"/api/labs/{L}/connections", {"a_type": "device", "a_id": next(d["id"] for d in topo()["devices"] if d["name"]=="A"), "a_if": 0,
     "b_type": "device", "b_id": next(d["id"] for d in topo()["devices"] if d["name"]=="B"), "b_if": 0})
check("reconnecting A-B: 201", st in (200, 201), conn3)
check("again: TAP A back in bridge", master(ra) is not None, tap_info(ra))
check("again: set_link up for A accepted", link_ok("A"))

check("machines not restarted (state running)", state_of("A") == "running" and state_of("B") == "running")

print("\nFAILED:" if fails else "\nAll checks passed.", fails)
sys.exit(1 if fails else 0)
