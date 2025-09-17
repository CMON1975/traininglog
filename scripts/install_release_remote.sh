#!/usr/bin/env bash
# Usage: ./scripts/install_release_remote.sh traininglog-YYYYMMDDHHMMSS-REV.tar.gz
set -euo pipefail
PKG="$1"
SSH_HOST="${SSH_HOST:-cmon}"   # change if needed

# Upload
scp "$PKG" "${SSH_HOST}:/tmp/"

# Install on droplet (systemd unit already exists; Apache already reverse-proxying)
ssh "$SSH_HOST" bash -c "'
  set -euo pipefail
  sudo systemctl stop traininglog
  sudo tar -C /opt/traininglog -xzf /tmp/${PKG}
  # Move into place (extract creates /opt/traininglog/<PKG>/...)
  cd /opt/traininglog
  # Replace current app with new contents atomically
  rm -f traininglog.new
  cp \"${PKG%.tar.gz}/traininglog\" traininglog.new
  sudo chown tlapp:tlapp traininglog.new
  sudo mv -f traininglog.new traininglog
  rm -rf web
  mv \"${PKG%.tar.gz}/web\" .
  sudo chown -R tlapp:tlapp /opt/traininglog
  sudo systemctl start traininglog
  systemctl is-active --quiet traininglog
'"

echo "Deployed ${PKG}"
