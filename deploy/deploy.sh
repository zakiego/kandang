#!/usr/bin/env bash
# Builds Kandang and ships it into its machine. Run it from the Mac, after
# deploy/machine-setup.sh.
#
#   deploy/deploy.sh [machine]     (default machine name: kandang)
#
# 1. Cross-compiles kandang for linux/arm64.
# 2. Builds image/Dockerfile with the Mac's Docker, exports a container's
#    filesystem as a tar, and streams it into the machine (an isolated machine
#    has no file sharing, so everything goes through stdin of `orb -u root`).
# 3. Unpacks it into a new rootfs dir and swaps the rootfs symlink to it in
#    one rename, keeping the previous rootfs for a rollback.
# 4. Replaces the binary, restarts the service and prints /health.
#
# It never prints or creates a secret. The image and container it builds are
# removed at the end.
set -euo pipefail

machine="${1:-kandang}"
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/.." && pwd)"
image="kandang-sandbox:deploy-$$"
work="$(mktemp -d "${TMPDIR:-/tmp}/kandang-deploy.XXXXXX")"
cid=""

cleanup() {
  [ -n "$cid" ] && docker rm -f "$cid" >/dev/null 2>&1 || true
  docker image rm -f "$image" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

case "$machine" in
  *[!a-z0-9-]* | "" | -*)
    echo "deploy: machine name must be lowercase letters, digits and dashes" >&2
    exit 2
    ;;
esac
for tool in go docker orb; do
  command -v "$tool" >/dev/null || { echo "deploy: $tool not found" >&2; exit 1; }
done
orb info "$machine" >/dev/null 2>&1 || { echo "deploy: machine $machine doesn't exist; run deploy/machine-setup.sh $machine first" >&2; exit 1; }

version="$(git -C "$repo" describe --tags --always --dirty 2>/dev/null || echo dev)"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"

echo "deploy: building kandang $version for linux/arm64"
(cd "$repo/apps/server" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$work/kandang" ./cmd/kandang)

echo "deploy: building the sandbox image"
docker build --quiet --platform linux/arm64 -t "$image" "$repo/image" >/dev/null
cid="$(docker create --platform linux/arm64 "$image" true)"

echo "deploy: streaming the rootfs into $machine (rootfs-$stamp)"
docker export "$cid" | orb -m "$machine" -u root bash -c "
  set -euo pipefail
  dir=/var/lib/kandang/rootfs-$stamp
  rm -rf \"\$dir.partial\"
  mkdir -p \"\$dir.partial\"
  tar -x --numeric-owner -C \"\$dir.partial\"
  rm -f \"\$dir.partial/.dockerenv\"
  mv \"\$dir.partial\" \"\$dir\"
"
docker rm -f "$cid" >/dev/null
cid=""

echo "deploy: streaming the binary"
orb -m "$machine" -u root bash -c '
  set -euo pipefail
  cat > /opt/kandang/bin/kandang.new
  chmod 0755 /opt/kandang/bin/kandang.new
  /opt/kandang/bin/kandang.new version >/dev/null
' < "$work/kandang"

echo "deploy: swapping the rootfs and the binary, restarting"
orb -m "$machine" -u root bash -c "
  set -euo pipefail
  cd /var/lib/kandang
  # A plain directory from an older layout moves aside first.
  if [ -d rootfs ] && [ ! -L rootfs ]; then mv rootfs rootfs-legacy; fi
  ln -sfn rootfs-$stamp rootfs.next
  mv -T rootfs.next rootfs
  mv -f /opt/kandang/bin/kandang.new /opt/kandang/bin/kandang
  systemctl restart kandang.service
  # Keep the live rootfs and the one before it; remove older ones.
  ls -1d rootfs-2*/ 2>/dev/null | sed 's#/\$##' | sort -r | tail -n +3 | while read -r old; do
    [ \"\$old\" = rootfs-$stamp ] || rm -rf \"\$old\"
  done
"

echo "deploy: waiting for /health"
orb -m "$machine" -u root bash -c '
  for i in $(seq 1 50); do
    if out=$(curl -fsS -m 2 http://127.0.0.1:8080/health 2>/dev/null); then
      echo "$out"
      exit 0
    fi
    sleep 0.2
  done
  echo "deploy: /health did not answer; last log lines:" >&2
  journalctl -u kandang.service -n 30 --no-pager >&2
  exit 1
'
echo "deploy: kandang $version is live in $machine"
