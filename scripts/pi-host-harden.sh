#!/usr/bin/env bash
set -euo pipefail

SSH_ALLOW_CIDR=${SSH_ALLOW_CIDR:-}
ALLOW_TCP_FORWARDING=${ALLOW_TCP_FORWARDING:-no}

if [[ "${EUID}" -ne 0 ]]; then
  echo "Run this script with sudo on the Pi." >&2
  exit 1
fi

if [[ -z "${SSH_ALLOW_CIDR}" ]]; then
  echo "Set SSH_ALLOW_CIDR to the LAN or VPN CIDR that should be allowed to reach port 22." >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y unattended-upgrades fail2ban ufw

cat >/etc/ssh/sshd_config.d/page-patrol-hardening.conf <<EOF
PasswordAuthentication no
PubkeyAuthentication yes
PermitRootLogin no
KbdInteractiveAuthentication no
X11Forwarding no
AllowTcpForwarding ${ALLOW_TCP_FORWARDING}
EOF

cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF

mkdir -p /etc/fail2ban/jail.d
cat >/etc/fail2ban/jail.d/sshd.local <<'EOF'
[sshd]
enabled = true
EOF

ufw default deny incoming
ufw default allow outgoing
ufw allow from "${SSH_ALLOW_CIDR}" to any port 22 proto tcp
ufw --force enable

systemctl enable --now unattended-upgrades
systemctl enable --now fail2ban
systemctl restart ssh

echo "Host hardening applied. Verify a new SSH session before disconnecting the current one."
