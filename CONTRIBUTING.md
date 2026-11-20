# Mitmachen

Danke fürs Interesse! Contributions sind willkommen.

## Grundregeln

- Code ist AGPL-3.0-only (siehe `LICENSE`). Neue Dateien bekommen den Kopf
  `// SPDX-License-Identifier: AGPL-3.0-only` (bei HTML/CSS/Shell entsprechend
  `<!-- SPDX-License-Identifier: AGPL-3.0-only -->` bzw. `# SPDX-...`).
- Keine neuen externen Laufzeit-Abhängigkeiten ohne Absprache. Das Projekt muss
  offline auf einem Pi 3 mit 1 GB RAM laufen.
- Keine CDNs: neue Frontend-Assets werden unter `web/static/vendor/` eingecheckt
  und in `THIRD_PARTY_NOTICES.md` mit Lizenz und Quelle dokumentiert.

## Lokal entwickeln

```bash
make test
make vet
make build
go run ./cmd/server -db ./data/app.db
```

Bitte vor einem Pull Request:

- [ ] `make fmt`, `make vet` und `make test` laufen fehlerfrei
- [ ] Neue Logik hat Tests (In-Memory-SQLite, `httptest`)
- [ ] Änderungen an der DB bekommen eine neue Migration in `internal/store/migrations/`
- [ ] README / Doku bei Bedarf aktualisiert
- [ ] `CHANGELOG.md` ergänzt

## Commit-Nachrichten

Kurz und im Imperativ, z. B. `add pause recovery for interrupted timers`.

## Bugreports

Bitte das Issue-Template nutzen und möglichst Version, Pi-Modell und die
letzten Zeilen aus `journalctl -u arbeitszeit-manager -n 100` anhängen.
