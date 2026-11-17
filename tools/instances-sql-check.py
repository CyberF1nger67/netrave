"""Checks SQL queries in the Go code: each query from the Go code is prepared in SQLite against the current schema.
Catches typos in table and column names. Run: python3 tools/instances-sql-check.py"""
import glob, os, re, sqlite3, sys
root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "internal")
src = open(os.path.join(root, "database", "schema.go")).read()
db = sqlite3.connect(":memory:")
db.execute("PRAGMA foreign_keys=ON")
db.executescript(re.search(r"const schema = `(.*?)`", src, re.S).group(1))
db.execute("INSERT INTO users(id,username,password_hash,role) VALUES(1,'admin','x','admin'),(2,'u','x','user')")
# Lab topology (labs, machines, networks, wires) is stored in files; the database holds only instances.
db.execute("INSERT INTO lab_instances(id,lab_id,user_id) VALUES(1,10,2)")
db.execute("INSERT INTO instance_devices(id,instance_id,device_id) VALUES(9,1,100)")
db.commit()

ok = bad = skipped = 0
seen = set()
for path in glob.glob(os.path.join(root, "**", "*.go"), recursive=True):
    if path.endswith("_test.go"):
        continue
    text = open(path).read()
    for m in re.finditer(r"`((?:SELECT|INSERT|UPDATE|DELETE)[^`]*)`", text):
        q = m.group(1)
        if q.count("(") != q.count(")"):
            continue  # fragment appended in Go by concatenation
        if q in seen:
            continue
        seen.add(q)
        q2 = q.replace("%s", "1")
        n = q2.count("?")
        try:
            db.execute("EXPLAIN " + q2, [1] * n)
            ok += 1
        except sqlite3.Error as e:
            if "incomplete input" in str(e):
                skipped += 1  # fragment that Go appends by concatenation elsewhere
                continue
            bad += 1
            print("FAIL", os.path.relpath(path, root), "::", " ".join(q2.split())[:110], "->", e)
print("queries checked: %d, errors: %d, fragments skipped: %d" % (ok + bad, bad, skipped))
sys.exit(1 if bad else 0)
