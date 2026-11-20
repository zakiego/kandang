#!/usr/bin/env bash
# Runs inside the Kandang machine as root, streamed in by machine-setup.sh.
# Idempotent: each step checks first, so running it again only fixes drift.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

log() { echo "  inside: $*"; }

# Packages: curl and gnupg for the gVisor key, nftables for the egress rule,
# ca-certificates for https.
need=()
for p in ca-certificates curl gnupg nftables; do
  dpkg -s "$p" >/dev/null 2>&1 || need+=("$p")
done
if [ "${#need[@]}" -gt 0 ]; then
  log "installing ${need[*]}"
  apt-get update -qq
  apt-get install -y -qq "${need[@]}" >/dev/null
fi

# runsc from gVisor's apt repo, verified with its signing key.
keyring=/usr/share/keyrings/gvisor-archive-keyring.gpg
list=/etc/apt/sources.list.d/gvisor.list
arch="$(dpkg --print-architecture)"
want_list="deb [arch=$arch signed-by=$keyring] https://storage.googleapis.com/gvisor/releases release main"
if [ ! -s "$keyring" ]; then
  log "adding gVisor's signing key"
  curl -fsSL https://gvisor.dev/archive.key | gpg --dearmor --yes -o "$keyring"
fi
if [ "$(cat "$list" 2>/dev/null)" != "$want_list" ]; then
  echo "$want_list" > "$list"
  apt-get update -qq
fi
if ! command -v runsc >/dev/null; then
  log "installing runsc"
  apt-get update -qq
  apt-get install -y -qq runsc >/dev/null
fi
log "$(runsc --version | head -1)"

# Egress rule: nothing leaving this machine may reach a private, shared
# (100.64.0.0/10, where a VPN mesh on the Mac would live) or link-local
# address, except replies to connections that came in (the Mac and Palui call
# Kandang; Kandang never calls them). IPv6 neighbor discovery and DHCP lease
# renewals (the machine's gateway is a private address) stay allowed so the
# link keeps working. The machine is dedicated to Kandang, so this file
# is the whole ruleset.
cat > /etc/nftables.conf.new <<'EOF'
#!/usr/sbin/nft -f
# Managed by kandang deploy/machine-inside.sh. Edits are overwritten.
flush ruleset

table inet kandang_egress {
  set private4 {
    type ipv4_addr
    flags interval
    elements = { 10.0.0.0/8, 100.64.0.0/10, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16 }
  }
  set private6 {
    type ipv6_addr
    flags interval
    elements = { fc00::/7, fe80::/10 }
  }
  chain output {
    type filter hook output priority filter; policy accept;
    ct state established,related accept
    oifname "lo" accept
    icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-solicit } accept
    udp sport 68 udp dport 67 accept
    ip daddr @private4 counter drop
    ip6 daddr @private6 counter drop
  }
}
EOF
# Check the new file before it replaces the old one: a broken nftables.conf
# would only show at the next boot, as a machine with no egress rule at all.
nft -c -f /etc/nftables.conf.new
mv -f /etc/nftables.conf.new /etc/nftables.conf
nft -f /etc/nftables.conf
systemctl enable nftables.service >/dev/null 2>&1
log "egress rule loaded (private and link-local ranges dropped)"

# uid 10001 is the sandbox user inside every run. Naming it here means files
# staged for a run show an owner, and nothing else on the machine gets that uid.
if ! getent group kandang >/dev/null; then
  groupadd --gid 10001 kandang
elif [ "$(getent group kandang | cut -d: -f3)" != 10001 ]; then
  groupmod --gid 10001 kandang
fi
if ! id kandang >/dev/null 2>&1; then
  useradd --uid 10001 --gid 10001 --no-create-home \
    --home-dir /nonexistent --shell /usr/sbin/nologin kandang
  log "created user kandang (10001:10001)"
fi

install -d -m 0700 -o root -g root /etc/kandang
install -d -m 0711 -o root -g root /var/lib/kandang
install -d -m 0700 -o root -g root /var/lib/kandang/state
install -d -m 0755 -o root -g root /opt/kandang /opt/kandang/bin

# The bearer token: 32 random bytes, URL-safe base64. Made once, never
# printed. deploy/show-token.sh reads it when the owner asks.
if [ ! -s /etc/kandang/token ]; then
  (umask 077 && head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n' > /etc/kandang/token.new)
  mv -f /etc/kandang/token.new /etc/kandang/token
  log "generated the bearer token (/etc/kandang/token)"
fi
chmod 0600 /etc/kandang/token
chown root:root /etc/kandang/token

# cgroup v2 with the three controllers kandang uses.
for c in memory pids cpu; do
  grep -qw "$c" /sys/fs/cgroup/cgroup.controllers || { echo "inside: cgroup controller $c missing" >&2; exit 1; }
done
log "cgroup v2 has memory, pids, cpu"
