#!/usr/bin/env bash
# Disposable CI container only. No ports are published outside its Docker network.
set -euo pipefail
umask 077
# openssh-server may generate image-layer keys during apt installation.
# Each disposable machine needs its own actual identity, not those clones.
ssh-keygen -q -t ed25519 -N '' -f /run/sshd/dragfm_ed25519
tr -d '-' </proc/sys/kernel/random/uuid >/etc/machine-id
# This public identifier must be readable by the non-root fixture accounts.
# Creating/copying it under the credential-safe umask otherwise yields 0600.
chmod 0644 /etc/machine-id
for fixture_user in tester systemtester passwordtester chroota chrootb; do
  install -d -o "$fixture_user" -g "$fixture_user" -m 0700 "/home/$fixture_user/.ssh"
  install -o "$fixture_user" -g "$fixture_user" -m 0600 /fixture-key/authorized_keys "/home/$fixture_user/.ssh/authorized_keys"
  chown "$fixture_user:$fixture_user" "/home/$fixture_user/.ssh"
  chmod 0700 "/home/$fixture_user/.ssh"
done
install -d -o root -g root -m 0700 /root/.ssh
install -o root -g root -m 0600 /fixture-key/root_authorized_keys /root/.ssh/authorized_keys
# Only disposable containers: unlock the OS account for public-key login,
# while both password methods remain disabled below.
passwd -d root >/dev/null
usermod -s /usr/local/bin/dragfm-system-shell root
cat >/etc/ssh/sshd_config <<'CONFIG'
Port 22
ListenAddress 0.0.0.0
HostKey /run/sshd/dragfm_ed25519
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
PermitRootLogin prohibit-password
PubkeyAuthentication yes
UsePAM no
AllowUsers tester systemtester passwordtester root chroota chrootb
AllowTcpForwarding yes
X11Forwarding no
PermitUserEnvironment no
PrintMotd no
ClientAliveInterval 15
ClientAliveCountMax 2
Subsystem sftp internal-sftp
CONFIG
# Same sshd/key/machine-id, genuinely different pathname roots. A static shell
# keeps exec and SFTP real; no Endpoint or filesystem method is substituted.
if [[ "${DRAGFM_FIXTURE_ROLE:-}" == source ]]; then
  for fixture_user in chroota chrootb; do
    jail="/var/lib/dragfm-chroots/$fixture_user"
    install -d -m 0755 "$jail/bin" "$jail/etc" "$jail/dev"
    # A real POSIX login shell needs its own null device inside the jail.
    # Without it, stat's harmless output redirection fails before stat runs.
    mknod -m 0666 "$jail/dev/null" c 1 3
    install -d -o "$fixture_user" -g "$fixture_user" -m 0700 "$jail/data"
    install -m 0755 /bin/busybox "$jail/bin/busybox"
    for tool in sh stat cat df uname cp mv rm readlink sha256sum sync; do
      ln -s busybox "$jail/bin/$tool"
    done
    install -m 0644 /etc/machine-id "$jail/etc/machine-id"
  done
  cat >>/etc/ssh/sshd_config <<'CONFIG'
Match User chroota,chrootb
  ChrootDirectory /var/lib/dragfm-chroots/%u
Match all
CONFIG
fi
if [[ "${DRAGFM_FIXTURE_ROLE:-}" == posix* ]]; then
  # A real OpenSSH server rejects only the SFTP subsystem. Ordinary exec,
  # signals and filesystem commands retain the production server behavior.
  sed -i 's|^Subsystem sftp internal-sftp$|Subsystem sftp /bin/false|' /etc/ssh/sshd_config
  # These disposable containers also lack the distribution server executable;
  # rejecting only the subsystem would still allow the privileged SFTP path.
  rm -f -- /usr/lib/openssh/sftp-server
fi
exec /usr/sbin/sshd -D -e
