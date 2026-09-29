#!/usr/bin/env bash
# Run only on a disposable GitHub-hosted Ubuntu runner with Docker.
set -euo pipefail
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$project_root"
: "${RUNNER_TEMP:?RUNNER_TEMP must identify the temporary directory of the disposable runner}"
mkdir -p test-results
exec > >(tee test-results/ssh-fixture.log) 2>&1
fixture=$(mktemp -d "$RUNNER_TEMP/dragfm-ssh.XXXXXX")
prefix="dragfm-ci-${GITHUB_RUN_ID:-manual}-${GITHUB_RUN_ATTEMPT:-1}-$$"
network="$prefix-net"
containers=()
cleanup() {
  local result=$?
  trap - EXIT
  for name in "${containers[@]}"; do
    docker logs "$name" >"test-results/${name##${prefix}-}-sshd.log" 2>&1 || true
    docker rm -f "$name" >/dev/null 2>&1 || true
  done
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf -- "$fixture"
  exit "$result"
}
trap cleanup EXIT
mkdir -p "$fixture/key"
ssh-keygen -q -t ed25519 -N '' -f "$fixture/id_ed25519"
cp "$fixture/id_ed25519.pub" "$fixture/key/authorized_keys"
chmod 0600 "$fixture/id_ed25519"
# Root accepts a DIFFERENT fixture identity. Reusing the ordinary user's key
# must fail, even though that user can independently approve scoped sudo.
ssh-keygen -q -t ed25519 -N '' -f "$fixture/root_ed25519"
cp "$fixture/root_ed25519.pub" "$fixture/key/root_authorized_keys"
chmod 0600 "$fixture/root_ed25519"
fixture_sudo_password=$(openssl rand -hex 24)
docker build -f build/ci/sshd.Dockerfile -t "$prefix-image" .
docker run --rm --entrypoint /usr/bin/dpkg-query "$prefix-image" -W -f='${Package} ${Version}\n' bash libc6 >test-results/transfer-tools.txt
docker network create "$network" >/dev/null
if [[ ! -c /dev/net/tun ]]; then
  sudo mkdir -p /dev/net
  sudo mknod /dev/net/tun c 10 200
fi
for role in source target relay posix posix-target; do
  name="$prefix-$role"
  containers+=("$name")
  docker run -d --name "$name" --network "$network" \
    --env "DRAGFM_FIXTURE_ROLE=$role" \
    --cap-add NET_ADMIN --cap-add NET_RAW --cap-add SYS_PTRACE --device /dev/net/tun \
    --mount "type=bind,source=$fixture/key,target=/fixture-key,readonly" \
    "$prefix-image" >/dev/null
  printf 'passwordtester:%s\n' "$fixture_sudo_password" | docker exec -i "$name" chpasswd
  ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$name")
  cat >>"$fixture/config" <<CONFIG
Host dragfm-$role
  HostName $ip
  Port 22
  User tester
  IdentityFile $fixture/id_ed25519
  IdentitiesOnly yes
  StrictHostKeyChecking accept-new
  UserKnownHostsFile $fixture/known_hosts
  BatchMode yes
  ConnectTimeout 3
Host dragfm-$role-system
  HostName $ip
  Port 22
  User systemtester
  IdentityFile $fixture/id_ed25519
  IdentitiesOnly yes
  StrictHostKeyChecking accept-new
  UserKnownHostsFile $fixture/known_hosts
  BatchMode yes
  ConnectTimeout 3
Host dragfm-$role-password
  HostName $ip
  Port 22
  User passwordtester
  IdentityFile $fixture/id_ed25519
  IdentitiesOnly yes
  StrictHostKeyChecking accept-new
  UserKnownHostsFile $fixture/known_hosts
  BatchMode yes
  ConnectTimeout 3
CONFIG
  # Wait on this container's actual sshd readiness event, not repeated login
  # attempts. One timer bounds the blocking log stream; the login below is the
  # single real authentication check, not inferred from a running container.
  python3 - "$name" <<'PY'
import subprocess, sys, threading
log = subprocess.Popen(['docker', 'logs', '--follow', sys.argv[1]], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
timer = threading.Timer(30, log.terminate)
timer.start()
ready = False
try:
    for line in iter(log.stdout.readline, b''):
        if b'Server listening on 0.0.0.0 port 22.' in line:
            ready = True
            break
finally:
    timer.cancel()
    log.terminate()
    try:
        log.wait(timeout=3)
    except subprocess.TimeoutExpired:
        log.kill()
        log.wait()
    log.stdout.close()
if not ready:
    raise SystemExit('disposable sshd did not report readiness before timeout/exit')
PY
  uid=$(ssh -F "$fixture/config" "dragfm-$role" id -u)
  [[ "$uid" != 0 ]] || { echo 'Fixture login must be genuinely non-root' >&2; exit 1; }
  [[ "$(ssh -F "$fixture/config" "dragfm-$role" sudo -n id -u)" == 0 ]]
done
relay_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$prefix-relay")
docker exec -d "$prefix-relay" python3 /usr/local/lib/socks-fixture.py
export DRAGFM_E2E_SOURCE_SSH=dragfm-source
export DRAGFM_E2E_TARGET_SSH=dragfm-target
export DRAGFM_E2E_RELAY_SSH=dragfm-relay
export DRAGFM_E2E_POSIX_SSH=dragfm-posix
export DRAGFM_E2E_POSIX_TARGET_SSH=dragfm-posix-target
export DRAGFM_E2E_SSH_CONFIG="$fixture/config"
export DRAGFM_E2E_SOCKS_SPEC="dragfm-ci:fixture-only@$relay_ip:1080"
export DRAGFM_E2E_NCAT=1
export DRAGFM_E2E_HANS=1
export DRAGFM_E2E_SUDO=1
export DRAGFM_E2E_SUDO_PASSWORD="$fixture_sudo_password"
export DRAGFM_E2E_ROOT_KEY="$fixture/root_ed25519"
set +e
go test -mod=vendor -tags=integration -race -count=1 -timeout=20m -json ./internal/webgui \
  -run "${DRAGFM_E2E_RUN:-^Test(Hosted.*|RemoteTransferMethodsOnHostedFixtures)$}" \
  -skip "${DRAGFM_E2E_SKIP:-}" \
  2>&1 | tee test-results/ssh-integration.jsonl
ssh_status=${PIPESTATUS[0]}
set -e
# Check the official Hans executable through the agent service as root only
# inside the disposable container, not via a private runner or user machine.
CGO_ENABLED=0 go test -mod=vendor -tags=integration -c -o "$fixture/hans.test" ./internal/agentservice
docker cp "$fixture/hans.test" "$prefix-target:/tmp/hans.test"
docker exec "$prefix-target" /tmp/hans.test -test.v -test.timeout=90s \
  -test.run '^TestOfficialHansReleaseThroughAgentService$' 2>&1 | tee test-results/hans-agent.log
exit "$ssh_status"
