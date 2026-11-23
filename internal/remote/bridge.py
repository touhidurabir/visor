# Visor bridge. Sent over SSH and run for the length of one connection; it is
# never written to disk on the server. It relays connections to supervisord's
# socket and reads files, so a user who reaches supervisord only through sudo
# runs sudo once per connection instead of once per call.
#
# Works with Python 2.7 and 3.6+. Frames on stdin/stdout:
#   type (1 byte) | id (uint32 BE) | length (uint32 BE) | payload
# From Visor: O open (payload "unix:<path>" or "tcp:<host>:<port>"),
#   D data, X close, R read file, T tail file (json {path, max}),
#   S stat, G glob, P ping, W replace (json {path, data (base64), hash}),
#   C create (json {path, data}), U delete (json {path, hash}),
#   A check (json {path, what: write|remove|folder}), L user exists,
#   M usage (json {pids}).
# To Visor: H hello (json), o opened, D data, X closed, r file, t tail,
#   s stat (json), g glob (json list), p pong, w/c/u/a done, l (json bool),
#   m usage (json {hz, page, cores, mem, pids: {pid: [cpu ticks, rss pages, procs]}}),
#   e error (json {errno, code, msg}; code ECHANGED: the file is no longer
#   what Visor read).
import base64
import errno
import glob
import hashlib
import json
import os
import select
import socket
import stat
import struct
import sys
import tempfile

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


class Changed(Exception):
    """The file is no longer what Visor read, so it is left alone."""


def message(e):
    try:
        return str(e)
    except Exception:  # Python 2: a unicode message that isn't ASCII
        return repr(e)


def send_error(ident, e):
    if isinstance(e, Changed):
        send("e", ident, json.dumps({"errno": 0, "code": "ECHANGED", "msg": message(e)}))
        return
    code = getattr(e, "errno", None) or 0
    msg = getattr(e, "strerror", None) or message(e)
    send("e", ident, json.dumps({"errno": code, "code": errno.errorcode.get(code, ""), "msg": msg}))


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
        "dev": st.st_dev,
        "ino": st.st_ino,
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


def sha(data):
    return hashlib.sha256(data).hexdigest()


def write_fd(fd, data):
    while data:
        n = os.write(fd, data)
        data = data[n:]
    os.fsync(fd)


def sync_dir(d):
    try:
        fd = os.open(d, os.O_RDONLY)
    except OSError:
        return
    try:
        os.fsync(fd)
    except OSError:
        pass
    os.close(fd)


def temp_beside(path):
    # Hidden, so an include glob such as conf.d/* doesn't pick it up.
    return tempfile.mkstemp(prefix="." + os.path.basename(path) + ".visor-",
                            suffix=".tmp", dir=os.path.dirname(path))


def unlink_quietly(name):
    if name:
        try:
            os.unlink(name)
        except OSError:
            pass


def replace_file(path, data, want):
    # The file a symlink points at is written; the link stays.
    target = os.path.realpath(path)
    try:
        st = os.stat(target)
    except OSError as e:
        if e.errno == errno.ENOENT:
            raise Changed(path + " was deleted")
        raise
    if not stat.S_ISREG(st.st_mode):
        raise ValueError(path + " is not a regular file")
    if sha(read_file(target)) != want:
        raise Changed(path)
    fd, tmp = temp_beside(target)
    try:
        try:
            os.fchmod(fd, stat.S_IMODE(st.st_mode))
            if os.getuid() == 0:  # as root (sudo), keep the owner too
                os.fchown(fd, st.st_uid, st.st_gid)
            else:  # keep the group, where this user may set it
                try:
                    os.fchown(fd, -1, st.st_gid)
                except OSError:
                    pass
            write_fd(fd, data)
        finally:
            os.close(fd)
        # Last look: an edit made meanwhile still wins.
        if sha(read_file(target)) != want:
            raise Changed(path)
        os.rename(tmp, target)
        tmp = None
    finally:
        unlink_quietly(tmp)
    sync_dir(os.path.dirname(target))


def create_file(path, data):
    if os.path.lexists(path):
        raise OSError(errno.EEXIST, "file exists")
    fd, tmp = temp_beside(path)
    try:
        try:
            os.fchmod(fd, 0o644)
            write_fd(fd, data)
        finally:
            os.close(fd)
        try:
            # A hard link fails if a file appeared meanwhile; rename wouldn't.
            os.link(tmp, path)
        except OSError as e:
            if e.errno in (errno.EEXIST, errno.EACCES, errno.EPERM):
                raise
            if os.path.lexists(path):  # no hard links here: check, then rename
                raise OSError(errno.EEXIST, "file exists")
            os.rename(tmp, path)
            tmp = None
    finally:
        unlink_quietly(tmp)
    sync_dir(os.path.dirname(path))


def delete_file(path, want):
    if sha(read_file(path)) != want:
        raise Changed(path)
    os.unlink(path)  # a symlink itself
    sync_dir(os.path.dirname(path))


def check(path, what):
    wx = os.W_OK | os.X_OK
    if what == "write":
        target = os.path.realpath(path)
        os.stat(target)
        ok = os.access(target, os.W_OK) and os.access(os.path.dirname(target), wx)
    elif what == "remove":
        if not os.path.lexists(path):
            raise OSError(errno.ENOENT, "no such file or directory")
        ok = os.access(os.path.dirname(path), wx)
    elif what == "folder":
        os.stat(path)
        ok = os.access(path, wx)
    else:
        raise ValueError("unknown check " + what)
    if not ok:
        raise OSError(errno.EACCES, "permission denied")


def user_exists(name):
    import pwd
    try:
        if name.isdigit():
            pwd.getpwuid(int(name))
        else:
            pwd.getpwnam(name)
        return True
    except KeyError:
        return False


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


def usage(pids):
    # Each pid's process tree from /proc, in one pass: CPU ticks (utime,
    # stime and the reaped children's cutime, cstime), resident pages and
    # how many processes. Without /proc (or with hidepid) it answers less.
    root = os.environ.get("VISOR_BRIDGE_PROC", "/proc")
    out = {"hz": os.sysconf("SC_CLK_TCK"), "page": os.sysconf("SC_PAGE_SIZE"), "pids": {}}
    # The machine's size: online cores, and MemTotal (kB) from meminfo.
    # Online cores ignore CPU affinity, unlike Go's runtime.NumCPU here.
    out["cores"] = 0
    out["mem"] = 0
    try:
        out["cores"] = os.sysconf("SC_NPROCESSORS_ONLN")
    except (ValueError, OSError):
        pass
    try:
        with open(os.path.join(root, "meminfo"), "rb") as f:
            for line in f.read().decode("ascii", "replace").splitlines():
                if line.startswith("MemTotal:"):
                    out["mem"] = int(line.split()[1]) * 1024
                    break
    except (IOError, OSError, ValueError, IndexError):
        pass
    if not pids:
        return out
    try:
        names = os.listdir(root)
    except OSError:
        return out
    procs = {}
    kids = {}
    for n in names:
        if not n.isdigit():
            continue
        try:
            with open(os.path.join(root, n, "stat"), "rb") as f:
                line = f.read().decode("utf-8", "replace")
        except (IOError, OSError):
            continue
        # Fields after the command's last ")": rest[k] is field k + 3.
        rest = line[line.rfind(")") + 1:].split()
        if len(rest) < 22:
            continue
        try:
            ppid = int(rest[1])
            ticks = sum(int(x) for x in rest[11:15])
            rss = max(int(rest[21]), 0)
        except ValueError:
            continue
        procs[int(n)] = (ticks, rss)
        kids.setdefault(ppid, []).append(int(n))
    for pid in pids:
        if pid not in procs:
            continue
        ticks = rss = count = 0
        seen = set()
        stack = [pid]
        while stack:
            p = stack.pop()
            if p in seen or p not in procs:
                continue
            seen.add(p)
            ticks += procs[p][0]
            rss += procs[p][1]
            count += 1
            stack.extend(kids.get(p, []))
        out["pids"][str(pid)] = [ticks, rss, count]
    return out


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
        elif kind == b"W":
            req = json.loads(text(payload))
            replace_file(req["path"], base64.b64decode(req.get("data") or ""), req["hash"])
            send("w", ident)
        elif kind == b"C":
            req = json.loads(text(payload))
            create_file(req["path"], base64.b64decode(req.get("data") or ""))
            send("c", ident)
        elif kind == b"U":
            req = json.loads(text(payload))
            delete_file(req["path"], req["hash"])
            send("u", ident)
        elif kind == b"A":
            req = json.loads(text(payload))
            check(req["path"], req["what"])
            send("a", ident)
        elif kind == b"M":
            req = json.loads(text(payload))
            send("m", ident, json.dumps(usage([int(p) for p in req.get("pids") or []])))
        elif kind == b"L":
            send("l", ident, json.dumps(user_exists(text(payload).strip())))
        else:
            raise ValueError("unknown frame")
    except Exception as e:  # report every failure to Visor, keep serving
        send_error(ident, e)


main()
