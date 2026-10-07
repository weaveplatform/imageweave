#!/bin/bash
set -euo pipefail
echo "[imageweave] waiting for cloud-init completion"
sudo cloud-init status --wait
. /etc/os-release
test "$ID" = "$IMAGEWEAVE_FAMILY"
test "$VERSION_ID" = "$IMAGEWEAVE_RELEASE"
case "$IMAGEWEAVE_ARCH" in
  amd64) test "$(uname -m)" = x86_64 ;;
  arm64) test "$(uname -m)" = aarch64 ;;
  *) exit 1 ;;
esac
echo "[imageweave] expected OS release and architecture observed"
