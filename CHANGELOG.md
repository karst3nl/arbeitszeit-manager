# Changelog

Alle nennenswerten Änderungen werden hier dokumentiert.
Format angelehnt an [Keep a Changelog](https://keepachangelog.com/de/1.1.0/).

## [Unreleased]

### Hinzugefügt
- Desktop-Oberfläche mit fester Seitenleiste, Titelleiste und Statusleiste ab 1100 px; Navigation zeigt den aktiven Bereich
- Startseite nach Rolle: Admins sehen alle offenen und in Bearbeitung befindlichen Aufträge plus Formular zum Anlegen; Nutzer nur die ihnen zugewiesenen Aufträge
- Aufgaben-Vorlagen im Adminbereich; beim Anlegen eines Auftrags sind mehrere Vorlagen per Mehrfachauswahl wählbar und werden direkt kopiert
- Desktop-Layout: breitere Ansicht und zweispaltige Seiten ab 1100 px; Smartphone-Layout bleibt unverändert
- Auftragstitel wird automatisch aus Kundenname und Kennzeichen gebildet
- Soll-Zeit ist ausschließlich die Summe der geplanten Minuten aller Aufgaben und passt sich bei Änderungen automatisch an; das separate Soll-Feld entfällt

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
