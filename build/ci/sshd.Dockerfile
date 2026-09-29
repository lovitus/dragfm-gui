FROM ubuntu:24.04
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    openssh-server openssh-client rsync ncat sudo iptables iproute2 net-tools python3 python3-socks python3-paramiko procps ca-certificates busybox-static \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/bash tester \
    && useradd --create-home --shell /usr/local/bin/dragfm-system-shell systemtester \
    && useradd --create-home --shell /usr/local/bin/dragfm-system-shell passwordtester \
    && useradd --create-home --shell /bin/sh chroota \
    && useradd --create-home --shell /bin/sh chrootb \
    && passwd -d tester \
    && passwd -d systemtester \
    && passwd -d chroota && passwd -d chrootb \
    && mkdir -p /run/sshd /home/tester/.ssh /home/systemtester/.ssh /home/passwordtester/.ssh \
    && printf 'tester ALL=(root) NOPASSWD: ALL\nsystemtester ALL=(root) NOPASSWD: ALL\n' >/etc/sudoers.d/dragfm-fixture \
    && printf 'Defaults:passwordtester timestamp_timeout=0,passwd_tries=1\npasswordtester ALL=(root) PASSWD: ALL\n' >/etc/sudoers.d/dragfm-password-fixture \
    && chmod 0440 /etc/sudoers.d/dragfm-fixture /etc/sudoers.d/dragfm-password-fixture
COPY build/ci/sshd-entrypoint.sh /usr/local/bin/sshd-fixture
COPY build/ci/socks-fixture.py /usr/local/lib/socks-fixture.py
COPY build/ci/system-only-shell.sh /usr/local/bin/dragfm-system-shell
RUN chmod 0755 /usr/local/bin/sshd-fixture /usr/local/bin/dragfm-system-shell
ENTRYPOINT ["/usr/local/bin/sshd-fixture"]
