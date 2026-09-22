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
docker build -f build/ci/sshd.Dockerfile -t "$prefix-image" .
docker network create "$network" >/dev/null
if [[ ! -c /dev/net/tun ]]; then
  sudo mkdir -p /dev/net
  sudo mknod /dev/net/tun c 10 200
fi
for role in source target relay; do
  name="$prefix-$role"
  containers+=("$name")
  docker run -d --name "$name" --network "$network" \
    --cap-add NET_ADMIN --cap-add NET_RAW --device /dev/net/tun \
    --mount "type=bind,source=$fixture/key,target=/fixture-key,readonly" \
    "$prefix-image" >/dev/null
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
CONFIG
  ready=false
  for attempt in {1..40}; do
    if ssh -F "$fixture/config" "dragfm-$role" true >/dev/null 2>&1; then ready=true; break; fi
    sleep 0.25
  done
  [[ "$ready" == true ]] || { echo "SSH fixture $role did not become ready" >&2; exit 1; }
  uid=$(ssh -F "$fixture/config" "dragfm-$role" id -u)
  [[ "$uid" != 0 ]] || { echo 'Fixture login must be genuinely non-root' >&2; exit 1; }
  [[ "$(ssh -F "$fixture/config" "dragfm-$role" sudo -n id -u)" == 0 ]]
done
relay_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$prefix-relay")
docker exec -d "$prefix-relay" python3 /usr/local/lib/socks-fixture.py
export DRAGFM_E2E_SOURCE_SSH=dragfm-source
export DRAGFM_E2E_TARGET_SSH=dragfm-target
export DRAGFM_E2E_RELAY_SSH=dragfm-relay
export DRAGFM_E2E_SSH_CONFIG="$fixture/config"
export DRAGFM_E2E_SOCKS_SPEC="dragfm-ci:fixture-only@$relay_ip:1080"
export DRAGFM_E2E_NCAT=1
export DRAGFM_E2E_HANS=1
export DRAGFM_E2E_SUDO=1
set +e
go test -mod=vendor -tags=integration -race -count=1 -timeout=20m -json ./internal/webgui \
  -run 'Test(RemoteTransferMethodsOnHostedFixtures|HostedSSHQueueAndHistory|HostedNonRootSudoTransfers|HostedReviewedTransportMatrix|HostedReviewedHansRolesAndMethods|HostedSSHCommandCancellationLatency|HostedReviewedRouteFailureRecovery|HostedReviewedFailedRelayCacheReprobes)' \
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
