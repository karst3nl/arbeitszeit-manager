#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
# Downloads the latest GitHub release, creates a backup and restarts the service.
set -euo pipefail

REPO="${REPO:-karst3nl/arbeitszeit-manager}"
INSTALL_DIR="${INSTALL_DIR:-/opt/arbeitszeit-manager}"
BIN="${INSTALL_DIR}/arbeitszeit-manager"

case "$(uname -m)" in
  aarch64) SUFFIX=arm64 ;;
  armv7l|armv6l) SUFFIX=armv7 ;;
  *) echo "Kein Release-Binary für $(uname -m) vorhanden." >&2; exit 1 ;;
esac

TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep -m1 '"tag_name"' | cut -d'"' -f4)"
if [[ -z "${TAG}" ]]; then
  echo "Kein Release gefunden." >&2
  exit 1
fi

ASSET="arbeitszeit-manager-linux-${SUFFIX}"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

echo "Lade ${TAG} (${SUFFIX}) …"
curl -fsSL "${BASE_URL}/${ASSET}" -o "${TMP}/${ASSET}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP}/checksums.txt"
if ! (cd "${TMP}" && grep "  ${ASSET}$" checksums.txt | sha256sum -c -); then
  echo "Prüfsumme ungültig – Update abgebrochen." >&2
  exit 1
fi
chmod 0755 "${TMP}/${ASSET}"

echo "Erstelle Backup …"
"${INSTALL_DIR}/backup.sh"

install -m 0755 "${TMP}/${ASSET}" "${BIN}"
systemctl restart arbeitszeit-manager
echo "Update auf ${TAG} abgeschlossen."
