#!/usr/bin/env python3
"""Self-test of the netrave L2 scheme on a real Linux kernel (without QEMU).

Checks what the network model relies on:
  * two TAPs on the same bridge exchange Ethernet frames;
  * a TAP on another bridge receives no frames (segments are isolated);
  * a TAP with no bridge in the DOWN state receives nothing (interface without a cable).

Requirements: root, /dev/net/tun. Creates temporary nrtest* interfaces and removes them.
Run: sudo python3 tools/l2-selftest.py
"""
import fcntl, os, select, socket, struct, sys, time

SIOCGIFINDEX, SIOCGIFFLAGS, SIOCSIFFLAGS = 0x8933, 0x8913, 0x8914
SIOCBRADDBR, SIOCBRDELBR, SIOCBRADDIF, SIOCBRDELIF = 0x89A0, 0x89A1, 0x89A2, 0x89A3
TUNSETIFF = 0x400454CA
IFF_TAP, IFF_NO_PI, IFF_UP = 0x0002, 0x1000, 0x1
MARK = b"NETRAVE-L2-SELFTEST"

_sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)

def ifreq(name, extra=b""):
    return struct.pack("16s", name.encode()) + extra.ljust(24, b"\0")

def ifindex(name):
    return struct.unpack("i", fcntl.ioctl(_sock, SIOCGIFINDEX, ifreq(name))[16:20])[0]

def bridge_add(name):
    fcntl.ioctl(_sock, SIOCBRADDBR, name.encode().ljust(16, b"\0"))

def bridge_del(name):
    # The kernel does not delete a bridge that is up (EBUSY): bring the interface down first.
    set_up(name, up=False)
    fcntl.ioctl(_sock, SIOCBRDELBR, name.encode().ljust(16, b"\0"))

def bridge_addif(br, port):
    fcntl.ioctl(_sock, SIOCBRADDIF, ifreq(br, struct.pack("i", ifindex(port))))

def set_up(name, up=True):
    flags = struct.unpack("H", fcntl.ioctl(_sock, SIOCGIFFLAGS, ifreq(name))[16:18])[0]
    flags = (flags | IFF_UP) if up else (flags & ~IFF_UP)
    fcntl.ioctl(_sock, SIOCSIFFLAGS, ifreq(name, struct.pack("H", flags)))

def tap_open(name):
    fd = os.open("/dev/net/tun", os.O_RDWR)
    fcntl.ioctl(fd, TUNSETIFF, struct.pack("16sH", name.encode(), IFF_TAP | IFF_NO_PI))
    return fd

def drain(fd, wait=0.6):
    got, end = [], time.time() + wait
    while True:
        t = end - time.time()
        if t <= 0 or not select.select([fd], [], [], t)[0]:
            break
        try:
            got.append(os.read(fd, 4096))
        except OSError:
            break
    return got

def seen(fd):
    return any(MARK in f for f in drain(fd))

def main():
    if os.geteuid() != 0:
        print("root required"); return 2
    BR_A, BR_B = "nrtestA", "nrtestB"
    ports = {"nrtest-a1": "A", "nrtest-a2": "A", "nrtest-b1": "B", "nrtest-iso": None}
    for b in (BR_A, BR_B):
        try:
            bridge_del(b)
        except OSError:
            pass  # no bridge from the previous run - that is normal
    results = []
    fds = {}
    try:
        bridge_add(BR_A); bridge_add(BR_B)
        set_up(BR_A); set_up(BR_B)
        fds = {n: tap_open(n) for n in ports}
        bridge_addif(BR_A, "nrtest-a1"); bridge_addif(BR_A, "nrtest-a2")
        bridge_addif(BR_B, "nrtest-b1")
        for n in ports:
            set_up(n, up=(ports[n] is not None))   # isolated: no bridge and DOWN
        time.sleep(0.2)
        for fd in fds.values():
            drain(fd, 0.05)

        mac = b"\x52\x54\x00\x00\x00\x01"
        os.write(fds["nrtest-a1"], b"\xff" * 6 + mac + b"\x88\xb5" + MARK + b"-SEG-A")
        results.append(("A1 -> A2 (same bridge) delivered", seen(fds["nrtest-a2"])))
        results.append(("A1 -> B1 (other bridge) NOT delivered", not seen(fds["nrtest-b1"])))
        results.append(("A1 -> iso (no bridge, DOWN) NOT delivered", not seen(fds["nrtest-iso"])))
        os.write(fds["nrtest-b1"], b"\xff" * 6 + mac + b"\x88\xb5" + MARK + b"-SEG-B")
        results.append(("B1 -> A2 NOT delivered", not seen(fds["nrtest-a2"])))
    finally:
        # First close TAPs (without TUNSETPERSIST they disappear with the fd), then the bridges.
        for n in list(fds):
            os.close(fds[n])
        for b in (BR_A, BR_B):
            try:
                bridge_del(b)
            except OSError as e:
                print("WARNING: bridge %s not removed: %s" % (b, e))
    ok = all(v for _, v in results)
    for label, v in results:
        print(("PASS " if v else "FAIL ") + label)
    print("RESULT: OK" if ok else "RESULT: ERRORS FOUND")
    return 0 if ok else 1

if __name__ == "__main__":
    sys.exit(main())
