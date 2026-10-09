#!/bin/sh
# Installs only a login-user synchronization service. Never installs networking.
set -eu
if [ "$(id -u)" = 0 ]; then
  echo "Run this as your login user; Workspace Host/network services are separate." >&2
  exit 1
fi
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: install-team-connector.sh /absolute/connector.json [--write-only]" >&2
  exit 1
fi
config_path=$1
case "$config_path" in /*) ;; *) echo "Use an absolute config path." >&2; exit 1 ;; esac
connector_binary=$(command -v werkbord-team)
case "$connector_binary" in /*) ;; *) echo "werkbord-team must be installed at an absolute path." >&2; exit 1 ;; esac
write_only=false
if [ "$#" = 2 ]; then
  [ "$2" = --write-only ] || exit 1
  write_only=true
fi
umask 077
case "$(uname -s)" in
  Darwin)
    service_dir="$HOME/Library/LaunchAgents"
    mkdir -p "$service_dir"
    service_path="$service_dir/com.werkbord.team.connector.plist"
    python3 - "$service_path" "$connector_binary" "$config_path" <<'PY'
import plistlib, sys
path, binary, config = sys.argv[1:]
with open(path, 'wb') as f:
    plistlib.dump({'Label': 'com.werkbord.team.connector', 'ProgramArguments': [binary, 'connector', 'run', '--config', config], 'RunAtLoad': True, 'KeepAlive': True, 'ThrottleInterval': 30}, f)
PY
    if [ "$write_only" = false ]; then
      launchctl bootout "gui/$(id -u)" "$service_path" 2>/dev/null || true
      launchctl bootstrap "gui/$(id -u)" "$service_path"
    fi
    ;;
  Linux)
    service_dir="$HOME/.config/systemd/user"
    mkdir -p "$service_dir"
    service_path="$service_dir/werkbord-team-connector.service"
    python3 - "$service_path" "$connector_binary" "$config_path" <<'PY'
import sys
path, binary, config = sys.argv[1:]
def quote(s):
    if any(c in s for c in '\n\r\0'): raise ValueError('invalid service path')
    return '"' + s.replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%').replace('$', '$$') + '"'
with open(path, 'w') as f:
    f.write('[Unit]\nDescription=Werkbord Team user connector\n\n[Service]\nType=simple\nExecStart=' + quote(binary) + ' connector run --config ' + quote(config) + '\nRestart=on-failure\nRestartSec=30\nUMask=0077\n\n[Install]\nWantedBy=default.target\n')
PY
    if [ "$write_only" = false ]; then
      systemctl --user daemon-reload
      systemctl --user enable --now werkbord-team-connector.service
    fi
    ;;
  *) echo "The connector service supports macOS and Linux." >&2; exit 1 ;;
esac
echo "User connector service: $service_path"
