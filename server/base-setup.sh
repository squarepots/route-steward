#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR=${1:-/tmp/route-steward/config}
MARKER=/var/lib/route-steward/host-prepared-v1

if [[ -f "$MARKER" ]]; then
  printf 'BASE_SETUP_OK\n'
  printf 'HOST_PREPARATION=existing\n'
  exit 0
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  jq \
  openssl \
  ufw \
  nftables

install -D -m 0644 "$SOURCE_DIR/99-route-steward-ssh.conf" /etc/ssh/sshd_config.d/99-route-steward-ssh.conf
install -d -m 0755 /run/sshd
sshd -t
systemctl reload ssh.service

sed -i 's/^IPV6=.*/IPV6=yes/' /etc/default/ufw
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
ufw limit 22/tcp comment 'RST SSH key-only' >/dev/null
ufw --force enable >/dev/null

install -d -m 0755 /var/lib/route-steward
touch "$MARKER"
chmod 0644 "$MARKER"

printf 'BASE_SETUP_OK\n'
printf 'HOST_PREPARATION=created\n'
ufw status verbose
