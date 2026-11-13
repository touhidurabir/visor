# Visor bridge. Sent over SSH and run for the length of one connection; it is
# never written to disk on the server. It relays connections to supervisord's
# socket and reads files, so a user who reaches supervisord only through sudo
# runs sudo once per connection instead of once per call.
#
# Works with Python 2.7 and 3.6+. Frames on stdin/stdout:
#   type (1 byte) | id (uint32 BE) | length (uint32 BE) | payload
# From Visor: O open (payload "unix:<path>" or "tcp:<host>:<port>"),
#   D data, X close, R read file, T tail file (json {path, max}),
#   S stat, G glob, P ping.
# To Visor: H hello (json), o opened, D data, X closed, r file, t tail,
#   s stat (json), g glob (json list), p pong, e error (json {errno, msg}).
import errno
import glob
import json
import os
import select
import socket
import struct
import sys

MAX_READ = 32 << 20
HEADER = struct.Struct(">cII")


def b(s):
    if isinstance(s, bytes):
        return s
    return s.encode("utf-8")


def text(p):
    return p.decode("utf-8")


def write_all(data):
    while data:
        n = os.write(1, data)
        data = data[n:]


def send(kind, ident, payload=b""):
    payload = b(payload)
    write_all(HEADER.pack(b(kind), ident, len(payload)) + payload)


def send_error(ident, e):
    code = getattr(e, "errno", None) or 0
    msg = getattr(e, "strerror", None) or str(e)
    send("e", ident, json.dumps({"errno": code, "msg": msg}))


def name_of(fn, ident):
    try:
        return fn(ident)[0]
    except (KeyError, ImportError):
        return str(ident)


def user_name(uid):
    try:
        import pwd
        return name_of(pwd.getpwuid, uid)
    except ImportError:
        return str(uid)


def group_name(gid):
    try:
        import grp
        return name_of(grp.getgrgid, gid)
    except ImportError:
        return str(gid)


def stat_json(path):
    st = os.stat(path)
    return json.dumps({
        "mode": st.st_mode,
        "size": st.st_size,
        "mtime": st.st_mtime,
        "uid": st.st_uid,
        "gid": st.st_gid,
        "owner": user_name(st.st_uid),
        "group": group_name(st.st_gid),
    })


def read_file(path):
    f = open(path, "rb")
    try:
        data = f.read(MAX_READ + 1)
    finally:
        f.close()
    if len(data) > MAX_READ:
        raise IOError(errno.EFBIG, "file too large")
    return data


def tail_file(path, most):
    f = open(path, "rb")
    try:
        f.seek(0, 2)
        size = f.tell()
        start = max(0, size - most)
        f.seek(start)
        return f.read(most)
    finally:
        f.close()


def open_conn(spec):
    spec = text(spec)
    if spec.startswith("unix:"):
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.connect(spec[5:])
        return s
    if spec.startswith("tcp:"):
        host, port = spec[4:].rsplit(":", 1)
        return socket.create_connection((host, int(port)), 10)
    raise ValueError("unknown address " + spec)


def hello():
    info = {"v": 1, "uid": os.getuid(), "user": user_name(os.getuid()),
            "python": sys.version.split()[0]}
    try:
        info["groups"] = [group_name(g) for g in os.getgroups()]
    except OSError:
        info["groups"] = []
    send("H", 0, json.dumps(info))


def main():
    conns = {}
    socks = {}
    buf = b""
    hello()
    while True:
        try:
            ready, _, _ = select.select([0] + list(conns.values()), [], [])
        except select.error as e:
            if e.args[0] == errno.EINTR:
                continue
            raise
        for r in ready:
            if r == 0:
                chunk = os.read(0, 65536)
                if not chunk:
                    return
                buf += chunk
                continue
            ident = socks[r]
            try:
                data = r.recv(65536)
            except socket.error:
                data = b""
            if data:
                send("D", ident, data)
            else:
                r.close()
                del conns[ident]
                del socks[r]
                send("X", ident)
        while len(buf) >= HEADER.size:
            kind, ident, n = HEADER.unpack(buf[:HEADER.size])
            if len(buf) < HEADER.size + n:
                break
            payload = buf[HEADER.size:HEADER.size + n]
            buf = buf[HEADER.size + n:]
            handle(kind, ident, payload, conns, socks)


def handle(kind, ident, payload, conns, socks):
    try:
        if kind == b"O":
            s = open_conn(payload)
            conns[ident] = s
            socks[s] = ident
            send("o", ident)
        elif kind == b"D":
            s = conns.get(ident)
            if s is not None:
                try:
                    s.sendall(payload)
                except socket.error:
                    pass
        elif kind == b"X":
            s = conns.pop(ident, None)
            if s is not None:
                del socks[s]
                s.close()
        elif kind == b"R":
            send("r", ident, read_file(text(payload)))
        elif kind == b"T":
            req = json.loads(text(payload))
            send("t", ident, tail_file(req["path"], int(req["max"])))
        elif kind == b"S":
            send("s", ident, stat_json(text(payload)))
        elif kind == b"G":
            send("g", ident, json.dumps(sorted(glob.glob(text(payload)))))
        elif kind == b"P":
            send("p", ident)
        else:
            raise ValueError("unknown frame")
    except Exception as e:  # report every failure to Visor, keep serving
        send_error(ident, e)


main()
