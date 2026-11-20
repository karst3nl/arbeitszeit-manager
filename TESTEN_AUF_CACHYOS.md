# Test-Anleitung für CachyOS (lokal am PC)

Diese Anleitung erklärt, wie du die komplette Testsuite und einen Smoke-Test
der Webapp **auf einem CachyOS-PC** ausführst – ganz ohne Raspberry Pi und ohne
Docker. Das Repo ist öffentlich, du kannst es auf jedem PC frisch clonen.

---

## 1. Voraussetzungen installieren

```bash
sudo pacman -Syu
sudo pacman -S --needed go make git curl base-devel
go version
```

Erwartung: Go **1.26 oder neuer** (`go.mod` fordert `go 1.26.0`). Falls die
pacman-Version älter ist, lädt Go bei Bedarf automatisch eine passende
Toolchain (`go env GOTOOLCHAIN` → `auto`). Alternativ: AUR-Paket `go-bin` oder
offizielles Tarball von <https://go.dev/dl/> verwenden.

`base-devel` wird nur für `go test -race` benötigt (Race-Detector braucht einen
C-Compiler). Für die normalen Tests reicht `go`.

---

## 2. Repo clonen und ins Projektverzeichnis wechseln

Auf einem **frischen PC** das öffentliche Repo holen:

```bash
cd ~
git clone https://github.com/karst3nl/arbeitszeit-manager.git
cd arbeitszeit-manager
```

Falls das Repo schon lokal liegt, einfach dorthin wechseln (Pfad anpassen):

```bash
cd ~/Projekte/opencode/arbeitszeit-manager
```

Danach kurz prüfen:

```bash
pwd
ls
```

Du solltest u. a. `go.mod`, `README.md`, `cmd/`, `internal/` und `web/` sehen.

---

## 3. Schnelltest: Format, Vet, Unit-Tests

```bash
go mod download
gofmt -l .          # darf nichts ausgeben
go vet ./...
go test ./...
```

Oder über das Makefile:

```bash
make fmt
make vet
make test
```

Erwartung (ähnliche Ausgabe):

```text
ok  	github.com/karst3nl/arbeitszeit-manager/internal/api
ok  	github.com/karst3nl/arbeitszeit-manager/internal/store
ok  	github.com/karst3nl/arbeitszeit-manager/internal/timer
?   	github.com/karst3nl/arbeitszeit-manager/cmd/server  [no test files]
```

Optional – Tests mit Race-Detector (etwas langsamer, braucht `base-devel`):

```bash
go test -race ./...
```

---

## 4. Lokalen Server testen (Smoke-Test)

### 4.1 Binary bauen und Test-Datenbank anlegen

```bash
make build
mkdir -p ./data
./arbeitszeit-manager admin create-user \
  -db ./data/test.db \
  -username admin -role admin \
  -password testpass123
```

Erwartung: `Nutzer "admin" (ID 1, Rolle admin) angelegt.`

### 4.2 Server starten

In **einem eigenen Terminal**:

```bash
cd ~/arbeitszeit-manager   # oder dein lokaler Pfad
./arbeitszeit-manager -addr 127.0.0.1:8080 -db ./data/test.db
```

Erwartung: `listening on 127.0.0.1:8080 (db: ./data/test.db, version: …)`

Der Server läuft jetzt. Mit **Strg+C** beenden (sauberer SIGINT-Shutdown).
Port `8080` ist der Standard der App; falls er auf dem PC belegt ist, einfach
einen anderen nehmen (z. B. `-addr 127.0.0.1:8081`) und in allen curl-/Browser-
URLs denselben Port verwenden.

**Von einem anderen Gerät im LAN zugreifen?** Dann nicht auf Loopback binden,
sondern auf alle Interfaces:

```bash
./arbeitszeit-manager -addr :8080 -db ./data/test.db
```

Danach `http://<IP-des-PCs>:8080` im Browser öffnen. Falls eine Firewall aktiv
ist (z. B. ufw), den Port freigeben: `sudo ufw allow 8080/tcp`.
Hinweis: Bei HTTP gehen Passwörter unverschlüsselt durchs Netz – für einen
echten LAN-Betrieb später HTTPS über Nginx nutzen (siehe `deploy/README.md`).

### 4.3 Kurzcheck per curl (zweites Terminal)

```bash
curl -sS http://127.0.0.1:8080/healthz
# {"status":"ok"}

curl -sS -D - -o /dev/null http://127.0.0.1:8080/login \
  | grep -i 'HTTP/\|cache-control'
# HTTP/1.1 200 OK
# Cache-Control: no-store
```

### 4.4 Kompletter Ablauf per curl (Login → Auftrag → Timer → CSV)

```bash
BASE=http://127.0.0.1:8080
JAR=/tmp/azm-cookies.txt

# CSRF-Token von der Login-Seite holen
CSRF=$(curl -sS -c "$JAR" "$BASE/login" \
  | grep -oP 'name="csrf" value="\K[0-9a-f]{64}' | head -1)
echo "CSRF: $CSRF"

# Login
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'login: %{http_code}\n' \
  -d "csrf=$CSRF&username=admin&password=testpass123" "$BASE/login"

# Aufgaben-Vorlage anlegen (nur Admin)
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'vorlage: %{http_code}\n' \
  -d "csrf=$CSRF&title=Konzeption" "$BASE/admin/task-templates"

# AW-Faktor im Adminbereich ändern (hier 10 Minuten pro AW; Standard wäre 6)
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'aw-faktor: %{http_code}\n' \
  -d "csrf=$CSRF&minutes_per_aw=10" "$BASE/admin/aw-factor"

# Auftrag anlegen (Kunde + Kennzeichen ergeben den Titel; 9 AW = 90 Minuten; assignee_id ist der Nutzer)
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'auftrag: %{http_code}\n' \
  -d "csrf=$CSRF&customer=Smoke-Test&plate=M-AB%20123&aw=9&assignee_id=1&template_id=1" "$BASE/orders"

# Aufgabe ohne eigene Zeitangabe
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'aufgabe: %{http_code}\n' \
  -d "csrf=$CSRF&title=Konzeption" "$BASE/orders/1/tasks"

# Timer: Start → Pause → Weiter → Ende
for AKTION in start pause resume stop; do
  curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w "$AKTION: %{http_code}\n" \
    -d "csrf=$CSRF" "$BASE/orders/1/timer/$AKTION"
done

# Auswertung als CSV
curl -sS -b "$JAR" "$BASE/orders/1/report.csv" | head -20
```

Erwartung: alle Statuscodes `200` (mit `-L` folgt curl den Redirects), CSV
enthält `Auftrag;Smoke-Test`, `Soll (Minuten);90`, `Soll (AW);9`,
`Ist (Sekunden);…`. Die `9 AW` entsprechen bei dem gesetzten Faktor `10`
`90 Minuten`; die Aufgaben selbst haben keine Zeitangabe mehr.
Ohne `-L` sind die ersten drei Aufrufe `303 See Other` – das ist ebenfalls
normal, dann wird nur die Zielseite nicht mitgeladen.

### 4.5 Alternativ: im Browser klicken

1. <http://127.0.0.1:8080/login> öffnen
2. Anmelden mit `admin` / `testpass123`
3. Unter **Nutzer & Vorlagen** den AW-Faktor ändern und eine Aufgaben-Vorlage anlegen (auch umbenennen/löschen testen), dann einen Auftrag mit Soll in AW, gewähltem Nutzer und optional mehreren Vorlagen aus dem Dropdown anlegen; neue Aufgaben lassen sich direkt im Dropdown anlegen → Timer Start/Pause/Weiter/Ende
4. „Auswertung ansehen" und den CSV-Export prüfen
5. Oben rechts **Konto** (Passwort ändern) und **Nutzer** ( Admin-Seite) testen

---

## 5. Optional: Cross-Builds für den Pi prüfen

Läuft auch auf x86_64 – es entstehen nur keine ausführbaren PC-Binaries:

```bash
make release
file dist/arbeitszeit-manager-linux-arm64
file dist/arbeitszeit-manager-linux-armv7
```

---

## 6. Aufräumen

Server im ersten Terminal mit **Strg+C** stoppen. Danach:

```bash
rm -f /tmp/azm-cookies.txt
make clean          # entfernt ./arbeitszeit-manager und dist/
rm -rf ./data       # Test-Datenbank löschen
```

---

## 7. Häufige Fehler

| Problem | Lösung |
|---|---|
| `go: go.mod requires go >= 1.26` | `go version` prüfen; `sudo pacman -S go`, AUR `go-bin` oder offizielles Tarball |
| `go test -race` scheitert an fehlendem C-Compiler | `sudo pacman -S base-devel` |
| `curl: (7) Failed to connect` | Server läuft nicht oder anderer Port; Adresse prüfen |
| `403` bei POST-Requests | CSRF-Token neu von `/login` holen und Cookies mit `-b/-c` mitschicken |
| `address already in use` | Anderen Port wählen, z. B. `-addr 127.0.0.1:8081` |
| `attempt to write a readonly database` | Test-DB nicht als `root` anlegen; `./data` gehört dem eigenen User |

---

## 8. Kurzfassung zum Kopieren

```bash
# Nur auf einem frischen PC:
git clone https://github.com/karst3nl/arbeitszeit-manager.git && cd arbeitszeit-manager

go mod download && gofmt -l . && go vet ./... && go test ./...
make build
mkdir -p data
./arbeitszeit-manager admin create-user -db data/test.db -username admin -role admin -password testpass123
./arbeitszeit-manager -addr 127.0.0.1:8080 -db data/test.db   # Terminal 1, Ende mit Strg+C
# Terminal 2:
curl -sS http://127.0.0.1:8080/healthz
```

Für LAN-Zugriff oben `-addr :8080` verwenden und `http://<IP-des-PCs>:8080` öffnen.
