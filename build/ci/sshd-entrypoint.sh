#!/usr/bin/env bash
# Disposable CI container only. No ports are published outside its Docker network.
set -euo pipefail
umask 077
ssh-keygen -A
tr -d '-' </proc/sys/kernel/random/uuid >/etc/machine-id
install -o tester -g tester -m 0600 /fixture-key/authorized_keys /home/tester/.ssh/authorized_keys
chown tester:tester /home/tester/.ssh
chmod 0700 /home/tester/.ssh
cat >/etc/ssh/sshd_config <<'CONFIG'
Port 22
ListenAddress 0.0.0.0
HostKey /etc/ssh/ssh_host_ed25519_key
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
PermitRootLogin no
PubkeyAuthentication yes
UsePAM no
AllowUsers tester
AllowTcpForwarding yes
X11Forwarding no
PermitUserEnvironment no
PrintMotd no
ClientAliveInterval 15
ClientAliveCountMax 2
Subsystem sftp internal-sftp
CONFIG
exec /usr/sbin/sshd -D -e
