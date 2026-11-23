#!/usr/bin/env bash
# Prints Kandang's bearer token so you can paste it into the client (an agent
# app's sandbox connector, for example). It's the only place the token is ever
# shown. Run it yourself, in your own terminal.
#
#   deploy/show-token.sh [machine]     (default machine name: kandang)
set -euo pipefail

machine="${1:-kandang}"
case "$machine" in
  *[!a-z0-9-]* | "" | -*)
    echo "show-token: machine name must be lowercase letters, digits and dashes" >&2
    exit 2
    ;;
esac
if [ ! -t 1 ]; then
  echo "show-token: stdout isn't a terminal; refusing to print the token into a pipe or file" >&2
  exit 1
fi
echo "Kandang bearer token for $machine (treat it like a password):" >&2
orb -m "$machine" -u root cat /etc/kandang/token
echo
