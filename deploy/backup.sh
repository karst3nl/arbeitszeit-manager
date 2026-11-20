#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
# Consistent database snapshot via the application binary (no sqlite3 CLI needed).
set -euo pipefail

BIN="${BIN:-/opt/arbeitszeit-manager/arbeitszeit-manager}"
DB="${DB_PATH:-/var/lib/arbeitszeit-manager/app.db}"
DIR="$(dirname "${DB}")/backups"
OUT="${DIR}/app-$(date +%Y%m%d-%H%M%S).db"

mkdir -p "${DIR}"
"${BIN}" backup -db "${DB}" -out "${OUT}"
gzip -f "${OUT}"
find "${DIR}" -type f -name 'app-*.db.gz' -mtime +7 -delete

echo "Backup: ${OUT}.gz"
