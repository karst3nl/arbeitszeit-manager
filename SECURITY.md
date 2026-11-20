# Sicherheitsrichtlinie

## Unterstützte Versionen

Sicherheitsupdates gibt es für die jeweils neueste veröffentlichte Version.

## Problem melden

Bitte **kein** öffentliches Issue für Sicherheitslücken anlegen. Stattdessen
eine private Nachricht über GitHub Security Advisories
(„Report a vulnerability“) senden oder – falls nicht verfügbar – den
Repository-Inhaber direkt kontaktieren.

Bitte möglichst angeben:

- betroffene Version
- Reproduktionsschritte
- mögliche Auswirkung
- Vorschlag für eine Behebung (optional)

Wir melden uns innerhalb weniger Tage.

## Hinweise zum Betrieb

- Die App ist für ein vertrautes LAN ausgelegt. Bei HTTP werden Passwörter
  unverschlüsselt übertragen – HTTPS empfohlen (siehe `deploy/nginx.conf.example`).
- `-secure-cookie` nur aktivieren, wenn HTTPS tatsächlich genutzt wird.
- Cookies sind `HttpOnly` und `SameSite=Lax`; alle schreibenden Requests sind
  durch ein CSRF-Token geschützt.
- Passwörter werden mit bcrypt gespeichert, Session-Token nur als Hash.
- Zugriff auf die SQLite-Datei sollte ausschließlich dem Dienstbenutzer gehören.
