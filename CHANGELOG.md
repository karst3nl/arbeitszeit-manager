# Changelog

Alle nennenswerten Änderungen werden hier dokumentiert.
Format angelehnt an [Keep a Changelog](https://keepachangelog.com/de/1.1.0/).

## [Unreleased]

### Hinzugefügt
- Admin-Startseite rechts nutzerzentriert: andere Nutzer mit ihren zugeteilten Aufträgen sowie eine Rubrik „Nicht zugeteilt“; Zählerleiste und Filter bleiben erhalten
- Zählerleiste über der Auftragsliste für offene, laufende und abgeschlossene Aufträge; Klick filtert die Liste, Zähler bleiben vollständig und aktualisieren sich alle 30 Sekunden
- Komplett neue Oberfläche im Stil der Home-Assistant-Finance-App: obere Navigation mit aktivem Bereich, Kartenraster, kompakte Tabellen und helle/dunkle HA-Farbvariablen; PicoCSS und Desktop-Seitenleiste entfallen
- Startseite nach Rolle: Admins sehen alle offenen und in Bearbeitung befindlichen Aufträge plus Formular zum Anlegen; Nutzer nur die ihnen zugewiesenen Aufträge
- Aufgaben-Vorlagen im Adminbereich vollständig pflegbar (anlegen, umbenennen, löschen); beim Anlegen eines Auftrags wählt man mehrere Vorlagen aus einem Dropdown und kopiert sie direkt in den Auftrag
- Responsives Layout: breitere zweispaltige Ansicht am Desktop, auf schmalen Displays stapeln sich die Karten und Tabellen werden zu Karten mit Beschriftungen
- Auftragstitel wird automatisch aus Kundenname und Kennzeichen gebildet
- Soll-Zeit wird beim Anlegen des Auftrags vom Ersteller in AW gesetzt (10 AW = 1 Stunde, Nachkommastellen erlaubt); bestehende Aufträge behalten ihre bisherige Soll-Summe durch eine Migration
- Aufträge werden beim Anlegen direkt einem aktiven Nutzer zugewiesen; neue Aufgaben können im Dropdown des Anlegeformulars als Vorlage angelegt werden

### Geändert
- Aufgaben und Vorlagen haben keine einzelnen Zeitangaben mehr; Soll und Auswertung beziehen sich auf den gesamten Auftrag. Timer, Auswertung und CSV zeigen AW-Werte zusätzlich zu Stunden/Minuten
- Große Seitenüberschriften entfernt; Auftrags- und Auswertungstitel bleiben als kompakte Zeile, Navigation und angemeldeter Nutzer stehen in der oberen Leiste

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
