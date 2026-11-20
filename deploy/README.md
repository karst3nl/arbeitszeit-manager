# Deployment auf dem Raspberry Pi

Empfohlen: **Raspberry Pi OS Lite (64-Bit)**. Kein Desktop, kein Docker.
Der RAM-Bedarf liegt bei ca. 250–350 MB inklusive Betriebssystem.

## 1. Binary bauen oder herunterladen

Auf dem Entwicklungsrechner:

```bash
make release          # erzeugt dist/arbeitszeit-manager-linux-arm64 und -armv7
```

Oder ein fertiges Binary aus den GitHub Releases nutzen.

## 2. Installieren

```bash
chmod +x arbeitszeit-manager-linux-arm64 deploy/*.sh
sudo ./deploy/install.sh ./arbeitszeit-manager-linux-arm64
```

Das Skript legt den Systembenutzer `arbeitszeit`, die Verzeichnisse
`/opt/arbeitszeit-manager` und `/var/lib/arbeitszeit-manager` sowie die
systemd-Unit an.

## 3. Ersten Admin anlegen

```bash
sudo -u arbeitszeit /opt/arbeitszeit-manager/arbeitszeit-manager admin create-user \
  -username admin -role admin
```

Wichtig: als Dienstbenutzer ausführen – sonst gehört die SQLite-Datei `root`
und der systemd-Dienst kann sie nicht beschreiben.

Das Passwort wird abgefragt (Mindestlänge 8). Alternativ `-password` oder
die Env-Variable `ADMIN_PASSWORD` nutzen. Weitere Nutzer legt man später in der
Oberfläche unter **Nutzer** an – das Rollenkonzept ist auf mind. 6 Konten ausgelegt.

## 4. Starten

```bash
sudo systemctl enable --now arbeitszeit-manager
curl http://localhost:8080/healthz
```

Im WLAN: `http://<IP-des-Pi>:8080`.

## 5. Optional: HTTPS

Bei HTTP gehen Passwörter unverschlüsselt durchs WLAN. Für ein selbstsigniertes
Zertifikat:

```bash
sudo apt install -y nginx openssl
sudo cp deploy/nginx.conf.example /etc/nginx/sites-available/arbeitszeit
sudo ln -s /etc/nginx/sites-available/arbeitszeit /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl restart nginx
```

Dann in `/etc/systemd/system/arbeitszeit-manager.service` die Adresse auf
`-addr 127.0.0.1:8080` ändern und `-secure-cookie` ergänzen:

```ini
ExecStart=/opt/arbeitszeit-manager/arbeitszeit-manager \
  -addr 127.0.0.1:8080 -db /var/lib/arbeitszeit-manager/app.db -secure-cookie
```

Danach `systemctl daemon-reload && systemctl restart arbeitszeit-manager`.

## 6. Backup und Update

```bash
sudo /opt/arbeitszeit-manager/backup.sh     # Snapshot + gzip, 7 Tage Rotation
sudo /opt/arbeitszeit-manager/update.sh     # letztes Release laden + Backup + Neustart
```

Der Backup-Befehl nutzt `VACUUM INTO` direkt im Binary – `sqlite3` muss nicht
installiert sein. `update.sh` lädt zusätzlich `checksums.txt` des Releases und
bricht ab, wenn die SHA-256-Prüfsumme des Binaries nicht stimmt.

## 7. SD-Karte schonen

- `log2ram` installieren (Logs im RAM statt auf der Karte)
- SQLite läuft bereits im WAL-Modus mit `synchronous=NORMAL`
- langfristig: Boot von USB-Stick statt microSD

## Troubleshooting

```bash
journalctl -u arbeitszeit-manager -n 100 -f
sudo systemctl status arbeitszeit-manager
```
