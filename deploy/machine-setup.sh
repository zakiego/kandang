#!/usr/bin/env bash
# Creates (or brings up to date) the isolated OrbStack machine Kandang runs in.
# Run it from the Mac. Safe to run again: every step checks before it acts.
#
#   deploy/machine-setup.sh [machine]     (default machine name: kandang)
#
# What it does:
#   1. orb create --isolated --isolate-network (Debian 13, 3 GB, 2 CPUs), so the
#      machine has no Mac file sharing and can't reach other machines or host
#      ports.
#   2. Inside the machine, as root (deploy/machine-inside.sh): runsc from
#      gVisor's signed apt repo, an nftables rule that drops outbound traffic to
#      private and link-local ranges, the kandang user and dirs, the bearer
#      token, and the systemd unit.
#
# The token is generated inside the machine (/etc/kandang/token, mode 0600)
# and never printed here. deploy/show-token.sh prints it when you need it.
set -euo pipefail

machine="${1:-kandang}"
here="$(cd "$(dirname "$0")" && pwd)"
distro="debian:trixie"

case "$machine" in
  *[!a-z0-9-]* | "" | -*)
    echo "machine-setup: machine name must be lowercase letters, digits and dashes" >&2
    exit 2
    ;;
esac

command -v orb >/dev/null || { echo "machine-setup: orb (OrbStack) not found" >&2; exit 1; }

if orb info "$machine" >/dev/null 2>&1; then
  echo "machine-setup: machine $machine exists, updating it"
  orb start "$machine" >/dev/null 2>&1 || true
else
  echo "machine-setup: creating isolated machine $machine ($distro, 3 GB, 2 CPUs)"
  orb create --isolated --isolate-network --memory 3G --cpus 2 "$distro" "$machine"
fi

echo "machine-setup: configuring $machine"
orb -m "$machine" -u root bash -s < "$here/machine-inside.sh"

echo "machine-setup: installing the systemd unit"
orb -m "$machine" -u root bash -c 'cat > /etc/systemd/system/kandang.service.new && mv -f /etc/systemd/system/kandang.service.new /etc/systemd/system/kandang.service && systemctl daemon-reload && systemctl enable kandang.service >/dev/null 2>&1' < "$here/kandang.service"

echo "machine-setup: done. Next: deploy/deploy.sh $machine"
