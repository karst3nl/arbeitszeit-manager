# Changelog

Alle nennenswerten Änderungen werden hier dokumentiert.
Format angelehnt an [Keep a Changelog](https://keepachangelog.com/de/1.1.0/).

## [Unreleased]

### Hinzugefügt
- Soll-Zeit direkt beim Anlegen eines Auftrags; `0` übernimmt weiterhin die Summe der Aufgaben und lässt sich im Auftrag später ändern

### Behoben
- Live-Ist-Zeit in Übersicht und Aufträgen während eines laufenden Timers
- Auftragstitel in gespeicherter Auswertung und CSV-Export
- CSV-Formel-Injektion bei Werten mit führenden Sonderzeichen
- Sauberer Shutdown beim SIGTERM durch systemd
- Berechtigungslücke: Aufgaben konnten über die URL eines fremden Auftrags geändert oder gelöscht werden
- Aufgaben sind nach Abschluss eines Auftrags gesperrt, damit die Auswertung nicht veraltet
- Login-Fehlversuche sperren nur das betroffene Konto, nicht alle Nutzer hinter einem Reverse Proxy
- SQLite-Datei wird bei der Installation dem Dienstbenutzer gehören; Passwortwechsel und Session-Reset sind atomar
- Letzter aktiver Admin kann nicht deaktiviert werden; eigener Status/passwort nicht über Adminseite änderbar
- Zuordnungen werden auf existierende aktive Nutzer geprüft und gleichzeitige Ansprüche sicher abgewiesen
- `Cache-Control: no-store` für dynamische Seiten, `foreign_keys` und `busy_timeout` DSN-basiert
- "Timer korrigiert" verschwindet nach Weiter/Ende; CI läuft auch auf `master`; Update-Skript prüft SHA-256-Prüfsummen
- CLI-Unterbefehle funktionieren auch nach globalen Flags (`-db … admin create-user`)
- Relative Datenbankpfade wie `./data/app.db` funktionieren wieder (SQLite-URI ohne Authority)

### Geplant
- Wiederkehrende Aufträge / Vorlagen
- Optionaler Fernzugriff (Tailscale)
- Mehrsprachigkeit (DE/EN)

## [1.0.0] – noch nicht veröffentlicht

### Hinzugefügt
- Arbeitsaufträge mit Aufgaben und geplanten Minuten
- Gesamttimer pro Auftrag mit Start, Pause, Weiter und Ende
- Sekündliche Timer-Anzeige im Browser mit 30-Sekunden-Server-Sync
- Soll-Ist-Auswertung mit Abweichung in Minuten und Prozent, Notiz und CSV-Export
- Login für mehrere Nutzer, Rollen `admin` / `user`, Sessions und CSRF-Schutz
- Konto-Seite: Anzeigename und eigenes Passwort ändern, Admin-Passwort-Reset, Selbstzuordnung offener Aufträge
- Auftrag löschen (gesperrt bei laufendem Timer), automatische Aktualisierung der Auftragsübersicht
- SQLite-Speicher mit Migrationen, Backup- und Reboot-Recovery
- systemd-Dienst sowie Installations- und Update-Skripte für den Raspberry Pi
- AGPL-3.0-Lizenz und Hinweis in der Oberfläche
