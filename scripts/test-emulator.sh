#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
state_dir=$(mktemp -d "${TMPDIR:-/tmp}/ec2ctl-emulator.XXXXXX")
run_name="ec2ctl-emulator-$(basename "$state_dir")"
port=${EC2CTL_EMULATOR_PORT:-9297}
image=ec2ctl-test-emulator

cleanup() {
  docker stop "$run_name" >/dev/null 2>&1 || true
  docker network rm "$run_name" >/dev/null 2>&1 || true
  # These files are generated credentials for this run only.
  rm -f "$state_dir/config" "$state_dir/emulator_credentials" "$state_dir/ca.pem"
  rmdir "$state_dir" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker build --pull --no-cache -t "$image" "$repo_dir/scripts/emulator"
docker network create "$run_name" >/dev/null
container_uid=1000
container_gid=1000
if [[ $(uname -s) == Linux ]]; then
  container_uid=$(id -u)
  container_gid=$(id -g)
fi
docker run -d --rm --name "$run_name" --network "$run_name" \
  --user "$container_uid:$container_gid" --cap-drop ALL --security-opt no-new-privileges --read-only \
  -p "127.0.0.1:$port:$port" \
  -v "$state_dir:/home/emulator/.aws/emulator/$port" \
  -e EMULATOR_TELEMETRY=off \
  "$image" -p "$port" --idle-timeout 900 >/dev/null

ready=false
for ((attempt = 0; attempt < 30; attempt++)); do
  if curl --silent --fail --cacert "$state_dir/ca.pem" "https://127.0.0.1:$port/__admin/health" >/dev/null; then
    ready=true
    break
  fi
  sleep 1
done
if [[ $ready != true ]]; then
  echo 'AWS Emulator did not become ready' >&2
  exit 1
fi
docker exec "$run_name" aws-emulator -v
cd "$repo_dir"
EC2CTL_EMULATOR_DIR="$state_dir" EC2CTL_EMULATOR_ENDPOINT="https://127.0.0.1:$port" \
  go test -tags=integration -race -count=1 -timeout=5m -v ./cmd
