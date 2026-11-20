# Arbeitszeit-Manager

[![CI](https://github.com/karst3nl/arbeitszeit-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/karst3nl/arbeitszeit-manager/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8.svg)](https://go.dev/)

Schlanke Webapp für Arbeitsaufträge mit geplanten Zeiten, einem Timer für die tatsächliche Arbeitszeit und einer Soll-Ist-Auswertung nach Abschluss. Entwickelt für einen **Raspberry Pi 3 (1 GB RAM)**, läuft aber überall wo Go läuft.

## Funktionen

- **Arbeitsaufträge** mit Titel aus Kundenname und Kennzeichen, Beschreibung und beliebigen Aufgaben ohne einzelne Zeitangaben
- **Soll-Zeit** wird vom Ersteller beim Anlegen in AW gesetzt; Nachkommastellen sind erlaubt. Der Faktor (Minuten pro AW, Standard 6 = 10 AW pro Stunde) ist im Adminbereich einstellbar. Anzeige und Auswertung zeigen AW zusätzlich zu Stunden/Minuten
- **Zuweisung beim Anlegen**: der Auftrag wird direkt einem aktiven Nutzer zugeordnet
- **Aufgaben-Vorlagen**: Admins pflegen die Liste im Adminbereich oder legen neue Aufgaben direkt im Dropdown des Anlegeformulars an; mehrere Vorlagen werden per Mehrfachauswahl in den Auftrag kopiert
- **Ein Timer pro Auftrag**: Start → Pause → Weiter → Ende, beliebig viele Pausen
- **Zeitverlauf**: Zeitstempel für Übergabe an den Mitarbeiter, jeden Start, jede Pause inklusive Dauer und das Ende; sichtbar im Auftrag, in der Auswertung und im CSV-Export
- **Live-Anzeige** der Ist-Zeit mit sekündlichem Browser-Tick und 30-Sekunden-Server-Sync
- **Auswertung** nach Abschluss: Soll, Ist und Abweichung in AW und Stunden/Minuten sowie in Prozent, Balkenvergleich, Notiz und CSV-Export
- **Mehrnutzerfähig** (mind. 6): Login, Rollen `admin` / `user`, Sessions, CSRF-Schutz; Nutzer können das eigene Passwort ändern, Admins können Passwörter zurücksetzen
- **Klar getrennte Rechte**: nur Admins und der Ersteller vergeben Aufträge und pflegen die Aufgabenliste; zugewiesene Nutzer können Aufgaben abhaken, aber keine Zuordnung ändern und keine Aufgaben anlegen, umbenennen oder löschen
- **Rollenbasierte Startseite**: Admins sehen rechts die anderen Nutzer mit ihren zugeteilten Aufträgen sowie eine Rubrik „Nicht zugeteilt“ und links das Formular zum Anlegen neuer Aufträge; normale Nutzer sehen nur die ihnen zugewiesenen Aufträge
- **Zählerleiste**: Offen / Läuft / Abgeschlossen mit anklickbaren Zählern zum Filtern; Zähler und Liste aktualisieren sich automatisch alle 30 Sekunden
- **Pflege**: Aufträge löschen (nicht bei laufendem Timer), Übersicht aktualisiert sich automatisch alle 30 Sekunden
- **Robust**: SQLite im WAL-Modus, Backup-Skript, Reboot-Recovery für hängende Timer
- **Responsiv**: Oberfläche im Stil der Home-Assistant-Finance-App mit oberer Navigation und Karten; auf Smartphones scrollbare Reiter und Karten statt Tabellen – ohne separate App oder Umschalter
- **Offline-fähig**: keine CDNs, keine externen Dienste, alle Assets liegen im Repo
- **Schlank**: ein Binary, ~20 MB RAM für die App, kein Docker nötig

## Schnellstart

```bash
git clone https://github.com/karst3nl/arbeitszeit-manager.git
cd arbeitszeit-manager
make build

# Ersten Administrator anlegen (Passwort wird abgefragt, wenn -password fehlt)
./arbeitszeit-manager admin create-user -db ./data/app.db -username admin -role admin

# Starten (DB und Assets lokal)
./arbeitszeit-manager -db ./data/app.db -addr :8080
```

Danach `http://localhost:8080` im Browser öffnen.

> Der Modulpfad `github.com/karst3nl/arbeitszeit-manager` ist auf dieses Repo
> eingestellt. Bei einem Fork unter anderem Namen `go.mod`, Imports und Links
> entsprechend anpassen.

## Raspberry Pi 3 – Installation

Empfohlen: **Raspberry Pi OS Lite (64-Bit)**, kein Desktop, kein Docker.

1. Release-Binary für `arm64` (oder `armv7` bei 32-Bit-OS) von der
   [Releases-Seite](https://github.com/karst3nl/arbeitszeit-manager/releases) herunterladen.
2. Auf den Pi kopieren und installieren:

```bash
chmod +x arbeitszeit-manager-linux-arm64 install.sh
sudo ./install.sh ./arbeitszeit-manager-linux-arm64
sudo -u arbeitszeit /opt/arbeitszeit-manager/arbeitszeit-manager admin create-user -username admin -role admin
sudo systemctl enable --now arbeitszeit-manager
```

3. Im WLAN unter `http://<pi-ip>:8080` erreichbar.

**Optional, aber empfohlen:** self-signed HTTPS über Nginx (siehe
[`deploy/nginx.conf.example`](deploy/nginx.conf.example)), damit Passwörter nicht
unverschlüsselt durchs WLAN gehen. Die Login-Cookies können mit
`-secure-cookie` auf `Secure` gesetzt werden.

Details: [`deploy/README.md`](deploy/README.md).

## Konfiguration

| Flag | Env-Variable | Standard | Beschreibung |
|---|---|---|---|
| `-addr` | `ADDR` | `:8080` | Listen-Adresse |
| `-db` | `DB_PATH` | `/var/lib/arbeitszeit-manager/app.db` | Pfad zur SQLite-Datei |
| `-secure-cookie` | `SECURE_COOKIE` | `false` | `Secure`-Attribut für Cookies (nur mit HTTPS) |
| `-version` | – | – | Version ausgeben und beenden |

Alle angezeigten Zeitstempel nutzen die Zeitzone des Prozesses. Über die
Umgebungsvariable `TZ` (z. B. `TZ=Europe/Berlin`) lässt sich das unabhängig von
der Systemkonfiguration des Pi festlegen; die systemd-Unit setzt bereits
`Europe/Berlin`.

## Entwicklung

```bash
make test     # go test ./...
make vet      # go vet ./...
make fmt      # gofmt
make build    # lokales Binary
make release  # Cross-Builds arm64 + armv7 nach dist/
```

Die Datenbank wird beim Start automatisch migriert. Tests laufen gegen eine
In-Memory-SQLite und benötigen keine externen Dienste.

### Aufbau

```text
cmd/server/            Einstiegspunkt + Admin-CLI
internal/config/       Konfiguration
internal/model/        Datenstrukturen
internal/store/        SQLite, Migrationen, Abfragen, Timer-Transaktionen
internal/auth/         Passwörter, Sessions, CSRF, Rollen
internal/timer/        Zeitformatierung
internal/api/          HTTP-Routen und Handler
web/                   Eingebettete Templates und statische Dateien
deploy/                systemd, Installations-, Backup- und Update-Skripte
```

## Backup

```bash
sudo /opt/arbeitszeit-manager/backup.sh
```

Sichert die Datenbank konsistent via `sqlite3 .backup` nach
`/var/lib/arbeitszeit-manager/backups/` und rotiert auf 7 Tage. Ein Backup
wird außerdem automatisch vor jedem Update erstellt.

## Lizenz

Freie Software unter der **GNU Affero General Public License v3.0**
([LICENSE](LICENSE), SPDX: `AGPL-3.0-only`). Wer die App über ein Netzwerk
bereitstellt, muss den Nutzerinnen und Nutzern den Quelltext zugänglich machen –
deshalb enthält die Oberfläche einen Hinweis mit Link auf dieses Repository.

Abhängigkeiten und deren Lizenzen stehen in
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).
