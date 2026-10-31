#!/usr/bin/env bash
# Throwaway supervisord for development: scripts/sandbox.sh up|down|status
#
# Lives entirely under /tmp/visor-sandbox (override with VISOR_SANDBOX_DIR,
# which must be /tmp/visor-<name>; the UI tests use /tmp/visor-e2e). It never reads or writes the real
# supervisord's config, socket or state. Dependency-free: bash, supervisord,
# curl.
set -euo pipefail

DIR=${VISOR_SANDBOX_DIR:-/tmp/visor-sandbox}
# DIR is rm -rf'd, so only a throwaway directory named visor-* under /tmp is
# accepted.
if ! [[ $DIR =~ ^(/private)?/tmp/visor-[a-z0-9-]+$ ]]; then
  echo "refusing VISOR_SANDBOX_DIR=$DIR: it must match /tmp/visor-[a-z0-9-]+" >&2
  exit 2
fi
# Marks the sample programs' commands so leftovers can be found by ps; one
# marker per directory, so a second sandbox is never touched.
MARK=$(basename "$DIR")
CONF=$DIR/supervisord.conf
SOCK=$DIR/s.sock
PIDFILE=$DIR/supervisord.pid

# True only if the pidfile names a live supervisord started with our config,
# so a stale pidfile whose pid was reused is never trusted.
is_running() {
  [[ -f $PIDFILE ]] || return 1
  local pid cmd
  pid=$(cat "$PIDFILE")
  [[ $pid =~ ^[0-9]+$ ]] || return 1
  cmd=$(ps -o command= -p "$pid" 2>/dev/null) || return 1
  [[ $cmd == *supervisord* && $cmd == *"$CONF"* ]]
}

# Process-group ids of every child. supervisord makes each child a group
# leader, so its pid is the pgid. Prints nothing if the socket is dead.
child_pgids() {
  curl -sf --max-time 3 --unix-socket "$SOCK" http://localhost/RPC2 \
    -H 'Content-Type: text/xml' \
    -d '<?xml version="1.0"?><methodCall><methodName>supervisor.getAllProcessInfo</methodName></methodCall>' 2>/dev/null |
    tr -d ' \n\t' | grep -o '<name>pid</name><value><int>[0-9]*' | grep -o '[0-9]*$' | grep -v '^0$' || true
}

# Fallback when supervisord is already gone: sample programs carry the marker
# ": $MARK;" in their sh -c command, so find their groups via ps.
marked_pgids() {
  ps -eo pgid=,command= | grep -F ": $MARK;"  | grep -v grep | awk '{print $1}' | sort -u || true
}

write_programs() {
  mkdir -p "$DIR/conf.d" "$DIR/logs"
  cat >"$CONF" <<CONF
[unix_http_server]
file=$SOCK

[supervisord]
logfile=$DIR/logs/supervisord.log
pidfile=$PIDFILE
childlogdir=$DIR/logs
nodaemon=false

[rpcinterface:supervisor]
supervisor.rpcinterface_factory = supervisor.rpcinterface:make_main_rpcinterface

[supervisorctl]
serverurl=unix://$SOCK

[include]
files = $DIR/conf.d/*.ini
CONF

  cat >"$DIR/conf.d/shop-api.ini" <<INI
[program:shop-queue]
command=sh -c ': $MARK; while :; do echo "working"; sleep 5; done'
stopasgroup=true
killasgroup=true
numprocs=4
process_name=%(program_name)s_%(process_num)02d

[program:shop-scheduler]
command=sh -c ': $MARK; while :; do echo "tick"; sleep 5; done'
stopasgroup=true
killasgroup=true
INI

  cat >"$DIR/conf.d/mailer.ini" <<INI
[program:mailer-worker]
command=sh -c ': $MARK; while :; do echo "sending"; sleep 5; done'
stopasgroup=true
killasgroup=true
numprocs=2
process_name=%(program_name)s_%(process_num)02d
redirect_stderr=true

[program:mailer-digest]
command=sh -c 'echo boom >&2; exit 1'
stopasgroup=true
killasgroup=true
startsecs=1
startretries=3
INI

  cat >"$DIR/conf.d/reports.ini" <<INI
[program:reports-export]
command=sh -c ': $MARK; while :; do sleep 5; done'
stopasgroup=true
killasgroup=true
autostart=false

[program:reports-sync]
command=/nonexistent/sync
stopasgroup=true
killasgroup=true
INI

  cat >"$DIR/conf.d/notes.ini" <<INI
[program:notes-indexer]
command=sh -c ': $MARK; sleep 2; exit 0'
stopasgroup=true
killasgroup=true
autorestart=false
INI

  cat >"$DIR/conf.d/flaky.ini" <<INI
[program:flaky]
command=sh -c ': $MARK; sleep 20; exit 2'
stopasgroup=true
killasgroup=true
autorestart=true
startsecs=1
INI
}

# Prints the number of processes supervisord reports, or fails.
count_processes() {
  curl -sf --max-time 3 --unix-socket "$SOCK" http://localhost/RPC2 \
    -H 'Content-Type: text/xml' \
    -d '<?xml version="1.0"?><methodCall><methodName>supervisor.getAllProcessInfo</methodName></methodCall>' |
    grep -o '<name>name</name>' | wc -l | tr -d ' '
}

up() {
  if is_running; then
    echo "sandbox already running"
    status
    return
  fi
  command -v supervisord >/dev/null || { echo "supervisord not found: brew install supervisor" >&2; exit 1; }
  rm -rf "$DIR"
  write_programs
  supervisord -c "$CONF"
  for _ in $(seq 1 50); do
    if count_processes >/dev/null 2>&1; then
      echo "sandbox up: $CONF"
      return
    fi
    sleep 0.2
  done
  echo "sandbox did not answer on $SOCK; see $DIR/logs/supervisord.log" >&2
  exit 1
}

status() {
  if ! is_running; then
    echo "sandbox not running"
    return 1
  fi
  echo "sandbox running (pid $(cat "$PIDFILE")), socket $SOCK"
  echo "processes: $(count_processes)"
}

down() {
  local pgids pid pg
  pgids=$(child_pgids)
  if is_running; then
    pid=$(cat "$PIDFILE")
    # SIGTERM makes supervisord stop its children and exit.
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 1 100); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL "$pid" 2>/dev/null || true
    fi
  fi
  # Anything that outlived supervisord (it was killed, crashed, or a child
  # ignored SIGTERM): kill whole process groups.
  pgids=$(printf '%s\n%s\n' "$pgids" "$(marked_pgids)" | grep -E '^[0-9]+$' | sort -u || true)
  for pg in $pgids; do
    [[ $pg -gt 1 ]] && kill -KILL "-$pg" 2>/dev/null || true
  done
  rm -rf "$DIR"
  echo "sandbox removed"
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  status) status ;;
  *) echo "usage: $0 up|down|status" >&2; exit 2 ;;
esac
