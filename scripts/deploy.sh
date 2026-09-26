#!/usr/bin/env bash
# Deploy build/ship-grip-fim to the hosts in hosts.conf and (re)start their agents.
#
#   scripts/deploy.sh                 all hosts that have sshUser and binaryPath
#   scripts/deploy.sh <alias>         one host
#   scripts/deploy.sh --no-restart    copy files only
#   scripts/deploy.sh --local[=DIR]   install on THIS machine (default DIR:
#                                     /opt/ship-grip-fim when writable, else
#                                     ~/ship-grip-fim); add --service to install
#                                     and start the systemd unit (uses sudo)
#
# Local install copies the binary, installs the config files only if missing,
# and (without --service) restarts the agent with nohup from DIR.
#
# For each remote host (over SSH, using your existing keys):
#   1. copies the binary to binaryPath (atomically, via a .new file)
#   2. copies integrity.conf and the two ignore files next to it ONLY if they
#      are not there yet (users.db, agent.crt, agent.key are never touched)
#   3. restarts the agent from the binary's directory
#   4. reads the agent's TLS fingerprint over SSH and pins it in the local
#      known_agents file, so the first TLS connection is verified rather than
#      trusted-on-first-use.
set -euo pipefail
cd "$(dirname "$0")/.."

HOSTS_CONF=${HOSTS_CONF:-hosts.conf}
KNOWN_AGENTS=${KNOWN_AGENTS:-known_agents}
BIN=build/ship-grip-fim
RESTART=1
ONLY=""
LOCAL=0
LOCAL_DIR=""
SERVICE=0
for a in "$@"; do
  case "$a" in
    --no-restart) RESTART=0 ;;
    --local)      LOCAL=1 ;;
    --local=*)    LOCAL=1; LOCAL_DIR="${a#--local=}" ;;
    --service)    SERVICE=1 ;;
    *) ONLY="$a" ;;
  esac
done

[[ -x "$BIN" ]] || { echo "ERROR - $BIN not found; run scripts/build.sh first" >&2; exit 1; }

install_config_files() { # dir  (copy config files only if missing; never touch users.db / keys)
  local dir=$1 f
  for f in integrity.conf integrity_ignore.cfg integrity_ignore_no_walk.cfg hosts.conf schedule.conf; do
    [[ -f "$f" ]] || continue
    if [[ -e "$dir/$f" ]]; then echo "   $f already present, left untouched"
    else cp "$f" "$dir/$f"; echo "   installed $f"; fi
  done
}

if [[ $LOCAL -eq 1 ]]; then
  if [[ -z "$LOCAL_DIR" ]]; then
    if [[ -w /opt/ship-grip-fim || ( ! -e /opt/ship-grip-fim && -w /opt ) ]]; then LOCAL_DIR=/opt/ship-grip-fim
    else LOCAL_DIR="$HOME/ship-grip-fim"; fi
  fi
  echo "[local] installing to $LOCAL_DIR"
  mkdir -p "$LOCAL_DIR"
  cp "$BIN" "$LOCAL_DIR/ship-grip-fim.new" && chmod 755 "$LOCAL_DIR/ship-grip-fim.new" && mv -f "$LOCAL_DIR/ship-grip-fim.new" "$LOCAL_DIR/ship-grip-fim"
  install_config_files "$LOCAL_DIR"
  if [[ $SERVICE -eq 1 ]]; then
    sed "s#/opt/ship-grip-fim#$LOCAL_DIR#g; s#^User=shipgrip#User=${SUDO_USER:-$USER}#" scripts/ship-grip-fim-agent.service > /tmp/ship-grip-fim-agent.service
    sudo cp /tmp/ship-grip-fim-agent.service /etc/systemd/system/ship-grip-fim-agent.service
    sudo systemctl daemon-reload
    sudo systemctl enable --now ship-grip-fim-agent
    sudo systemctl restart ship-grip-fim-agent
    echo "[local] systemd unit ship-grip-fim-agent installed and (re)started (User=${SUDO_USER:-$USER}; edit /etc/systemd/system/ship-grip-fim-agent.service to change)"
  elif [[ $RESTART -eq 1 ]]; then
    pkill -f "$LOCAL_DIR/ship-grip-fim.* agent" >/dev/null 2>&1 || true; sleep 1
    ( cd "$LOCAL_DIR" && nohup ./ship-grip-fim --agentHost=0.0.0.0 agent > agent.log 2>&1 & )
    echo "[local] agent started with nohup (log: $LOCAL_DIR/agent.log); use --service for a systemd unit"
  fi
  echo "[local] fingerprint: $( cd "$LOCAL_DIR" && ./ship-grip-fim fingerprint | grep '^SHA256:' | tail -1 )"
  echo "Done. New installs start with user admin / changeme; change it with: $LOCAL_DIR/ship-grip-fim user passwd admin <new-password>"
  exit 0
fi

[[ -f "$HOSTS_CONF" ]] || { echo "ERROR - $HOSTS_CONF not found" >&2; exit 1; }

pin_fingerprint() { # addr fingerprint
  local addr=$1 fp=$2
  touch "$KNOWN_AGENTS"
  grep -v "^$addr " "$KNOWN_AGENTS" > "$KNOWN_AGENTS.tmp" || true
  echo "$addr $fp" >> "$KNOWN_AGENTS.tmp"
  mv "$KNOWN_AGENTS.tmp" "$KNOWN_AGENTS"
}

deployed=0
while IFS='|' read -r alias address port path reportName sshUser binaryPath rest; do
  alias=$(echo "${alias:-}" | xargs); [[ -z "$alias" || "$alias" == \#* ]] && continue
  address=$(echo "$address" | xargs); port=$(echo "$port" | xargs)
  sshUser=$(echo "${sshUser:-}" | xargs); binaryPath=$(echo "${binaryPath:-}" | xargs)
  [[ -n "$ONLY" && "$ONLY" != "$alias" ]] && continue
  if [[ -z "$sshUser" || -z "$binaryPath" ]]; then
    echo "[$alias] skipped: sshUser and binaryPath must be set in $HOSTS_CONF"; continue
  fi
  target="$sshUser@$address"
  dir=$(dirname "$binaryPath")
  echo "[$alias] deploying to $target:$binaryPath"

  ssh -o BatchMode=yes "$target" "mkdir -p '$dir'"
  scp -q "$BIN" "$target:$binaryPath.new"
  ssh -o BatchMode=yes "$target" "chmod 755 '$binaryPath.new' && mv -f '$binaryPath.new' '$binaryPath'"

  for f in integrity.conf integrity_ignore.cfg integrity_ignore_no_walk.cfg; do
    [[ -f "$f" ]] || continue
    if ssh -o BatchMode=yes "$target" "test -e '$dir/$f'"; then
      echo "[$alias]   $f already present, left untouched"
    else
      scp -q "$f" "$target:$dir/$f"; echo "[$alias]   installed $f"
    fi
  done

  if [[ $RESTART -eq 1 ]]; then
    # Stop and start are separate ssh calls, and the pattern brackets the last
    # character of the binary name: otherwise pkill -f matches the remote
    # shell's own command line and kills it before nohup runs.
    ssh -o BatchMode=yes "$target" "pkill -f '${binaryPath%?}[${binaryPath: -1}].* agent' >/dev/null 2>&1; sleep 1; true"
    ssh -o BatchMode=yes "$target" "cd '$dir' && nohup '$binaryPath' --agentHost=0.0.0.0 --agentPort=$port agent > agent.log 2>&1 < /dev/null &"
    echo "[$alias]   agent restarted on port $port (log: $dir/agent.log)"
  fi

  fp=$(ssh -o BatchMode=yes "$target" "cd '$dir' && '$binaryPath' fingerprint" | grep '^SHA256:' | tail -1 || true)
  if [[ -n "$fp" ]]; then
    pin_fingerprint "$address:$port" "$fp"
    echo "[$alias]   pinned $fp in $KNOWN_AGENTS"
  else
    echo "[$alias]   WARN - could not read the agent fingerprint"
  fi
  deployed=$((deployed+1))
done < "$HOSTS_CONF"

echo "Done - $deployed host(s) deployed."
[[ $deployed -gt 0 ]] && echo "Reminder: new agents start with user admin / changeme; change it with: ./ship-grip-fim remote <host> <port> user passwd admin <new-password>"
