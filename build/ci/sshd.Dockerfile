FROM ubuntu:24.04
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    openssh-server openssh-client rsync ncat sudo iproute2 net-tools python3 procps ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/bash tester \
    && passwd -d tester \
    && mkdir -p /run/sshd /home/tester/.ssh \
    && printf 'tester ALL=(root) NOPASSWD: ALL\n' >/etc/sudoers.d/dragfm-fixture \
    && chmod 0440 /etc/sudoers.d/dragfm-fixture
COPY build/ci/sshd-entrypoint.sh /usr/local/bin/sshd-fixture
COPY build/ci/socks-fixture.py /usr/local/lib/socks-fixture.py
RUN chmod 0755 /usr/local/bin/sshd-fixture
ENTRYPOINT ["/usr/local/bin/sshd-fixture"]
