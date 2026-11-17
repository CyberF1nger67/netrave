import json, urllib.request, http.cookiejar, sys
import os
BASE = os.environ.get("NETRAVE_URL", "http://127.0.0.1:18080")
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
CSRF = {"v": ""}
def call(method, path, body=None, expect=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    req.add_header("Origin", BASE)
    if method != "GET" and CSRF["v"]:
        req.add_header("X-CSRF-Token", CSRF["v"])
    try:
        with opener.open(req, timeout=60) as r:
            raw = r.read()
            out = json.loads(raw) if raw else None
            return r.status, out
    except urllib.error.HTTPError as e:
        raw = e.read()
        try: out = json.loads(raw)
        except Exception: out = raw.decode(errors="replace")
        return e.code, out
def login(user, pw):
    st, out = call("POST", "/api/login", {"username": user, "password": pw})
    CSRF["v"] = out.get("csrf", "") if isinstance(out, dict) else ""
    return st, out
