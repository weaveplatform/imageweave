#!/bin/bash
set -euo pipefail
test "$(id -u)" = 0
echo "[imageweave] removing build identity and credentials"
cloud-init status --wait
cloud-init clean --logs --machine-id --seed
rm -f /etc/ssh/ssh_host_* /var/lib/systemd/random-seed
# Only the dedicated build account is removed; vendor account policy stays in the source.
userdel --force --remove imageweave
rm -f /etc/sudoers.d/90-cloud-init-users
rm -f /tmp/imageweave-seal.sh
sync
echo "[imageweave] candidate sealed; powering off"
systemctl poweroff
