#!/usr/bin/env bash
# Throwaway supervisord for development: scripts/sandbox.sh up|down|status
#
# Lives entirely under /tmp/visor-sandbox. It never reads or writes the real
# supervisord's config, socket or state. Dependency-free: bash, supervisord,
# curl.
set -euo pipefail

DIR=/tmp/visor-sandbox
CONF=$DIR/supervisord.conf
SOCK=$DIR/s.sock
PIDFILE=$DIR/supervisord.pid

is_running() {
  [[ -f $PIDFILE ]] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
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

  cat >"$DIR/conf.d/shop-api.ini" <<'INI'
[program:shop-queue]
command=sh -c 'while :; do echo "working"; sleep 5; done'
numprocs=4
process_name=%(program_name)s_%(process_num)02d

[program:shop-scheduler]
command=sh -c 'while :; do echo "tick"; sleep 5; done'
INI

  cat >"$DIR/conf.d/mailer.ini" <<'INI'
[program:mailer-worker]
command=sh -c 'while :; do echo "sending"; sleep 5; done'
numprocs=2
process_name=%(program_name)s_%(process_num)02d

[program:mailer-digest]
command=sh -c 'echo boom >&2; exit 1'
startsecs=1
startretries=3
INI

  cat >"$DIR/conf.d/reports.ini" <<'INI'
[program:reports-export]
command=sh -c 'while :; do sleep 5; done'
autostart=false

[program:reports-sync]
command=/nonexistent/sync
INI

  cat >"$DIR/conf.d/notes.ini" <<'INI'
[program:notes-indexer]
command=sh -c 'sleep 2; exit 0'
autorestart=false
INI

  cat >"$DIR/conf.d/flaky.ini" <<'INI'
[program:flaky]
command=sh -c 'sleep 20; exit 2'
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
  rm -rf "$DIR"
  write_programs
  command -v supervisord >/dev/null || { echo "supervisord not found: brew install supervisor" >&2; exit 1; }
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
  if [[ -f $PIDFILE ]]; then
    local pid
    pid=$(cat "$PIDFILE")
    if kill -0 "$pid" 2>/dev/null; then
      # SIGTERM makes supervisord stop its children and exit.
      kill -TERM "$pid" 2>/dev/null || true
      for _ in $(seq 1 100); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.1
      done
      kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
    fi
  fi
  # Children that outlived supervisord: anything whose command line mentions
  # the sandbox dir.
  pkill -KILL -f "$DIR" 2>/dev/null || true
  rm -rf "$DIR"
  echo "sandbox removed"
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  status) status ;;
  *) echo "usage: $0 up|down|status" >&2; exit 2 ;;
esac
