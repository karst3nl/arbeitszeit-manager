#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
# Installs a release binary as a systemd service on Raspberry Pi OS.
set -euo pipefail

BIN="${1:-}"
if [[ -z "${BIN}" || ! -f "${BIN}" ]]; then
  echo "Usage: sudo ./install.sh <arbeitszeit-manager-linux-arm64>" >&2
  exit 1
fi
if [[ "${EUID}" -ne 0 ]]; then
  echo "Bitte mit sudo ausführen." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR=/opt/arbeitszeit-manager
DATA_DIR=/var/lib/arbeitszeit-manager

if ! id -u arbeitszeit >/dev/null 2>&1; then
  useradd --system --home-dir "${DATA_DIR}" --shell /usr/sbin/nologin arbeitszeit
fi

install -d -m 0750 -o arbeitszeit -g arbeitszeit "${DATA_DIR}" "${DATA_DIR}/backups"
# The database must belong to the service user, not to root, or the systemd
# service cannot enable WAL mode or write any data.
if [[ ! -e "${DATA_DIR}/app.db" ]]; then
  touch "${DATA_DIR}/app.db"
  chown arbeitszeit:arbeitszeit "${DATA_DIR}/app.db"
  chmod 0640 "${DATA_DIR}/app.db"
fi
install -d -m 0755 "${INSTALL_DIR}"
install -m 0755 "${BIN}" "${INSTALL_DIR}/arbeitszeit-manager"
install -m 0755 "${SCRIPT_DIR}/backup.sh" "${INSTALL_DIR}/backup.sh"
install -m 0755 "${SCRIPT_DIR}/update.sh" "${INSTALL_DIR}/update.sh"
install -m 0644 "${SCRIPT_DIR}/arbeitszeit-manager.service" /etc/systemd/system/arbeitszeit-manager.service

systemctl daemon-reload
echo "Installiert. Nächste Schritte:"
echo "  sudo -u arbeitszeit ${INSTALL_DIR}/arbeitszeit-manager admin create-user -username admin -role admin"
echo "  sudo systemctl enable --now arbeitszeit-manager"
echo "  curl http://localhost:8080/healthz"
