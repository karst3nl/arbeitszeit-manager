# Test-Anleitung für CachyOS (lokal am PC)

Diese Anleitung erklärt, wie du die komplette Testsuite und einen Smoke-Test
der Webapp **auf deinem CachyOS-PC** ausführst – ganz ohne Raspberry Pi, ohne
Docker und ohne GitHub-Zugriff. Das Repo bleibt lokal.

---

## 1. Voraussetzungen installieren

```bash
sudo pacman -Syu
sudo pacman -S --needed go make curl base-devel
go version
```

Erwartung: Go **1.26 oder neuer** (`go.mod` fordert `go 1.26.0`). Falls die
pacman-Version älter ist, lädt Go bei Bedarf automatisch eine passende
Toolchain (`go env GOTOOLCHAIN` → `auto`). Alternativ: AUR-Paket `go-bin` oder
offizielles Tarball von <https://go.dev/dl/> verwenden.

`base-devel` wird nur für `go test -race` benötigt (Race-Detector braucht einen
C-Compiler). Für die normalen Tests reicht `go`.

---

## 2. Ins Projektverzeichnis wechseln

Passe den Pfad an, falls du das Projekt anderswo liegen hast:

```bash
cd ~/Projekte/opencode/arbeitszeit-manager
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
cd ~/Projekte/opencode/arbeitszeit-manager
./arbeitszeit-manager -addr 127.0.0.1:18080 -db ./data/test.db
```

Erwartung: `listening on 127.0.0.1:18080 (db: ./data/test.db, version: …)`

Der Server läuft jetzt. Mit **Strg+C** beenden (sauberer SIGINT-Shutdown).

### 4.3 Kurzcheck per curl (zweites Terminal)

```bash
curl -sS http://127.0.0.1:18080/healthz
# {"status":"ok"}

curl -sS -D - -o /dev/null http://127.0.0.1:18080/login \
  | grep -i 'HTTP/\|cache-control'
# HTTP/1.1 200 OK
# Cache-Control: no-store
```

### 4.4 Kompletter Ablauf per curl (Login → Auftrag → Timer → CSV)

```bash
BASE=http://127.0.0.1:18080
JAR=/tmp/azm-cookies.txt

# CSRF-Token von der Login-Seite holen
CSRF=$(curl -sS -c "$JAR" "$BASE/login" \
  | grep -oP 'name="csrf" value="\K[0-9a-f]{64}' | head -1)
echo "CSRF: $CSRF"

# Login
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'login: %{http_code}\n' \
  -d "csrf=$CSRF&username=admin&password=testpass123" "$BASE/login"

# Auftrag anlegen
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'auftrag: %{http_code}\n' \
  -d "csrf=$CSRF&title=Smoke-Test" "$BASE/orders"

# Aufgabe mit 60 geplanten Minuten
curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w 'aufgabe: %{http_code}\n' \
  -d "csrf=$CSRF&title=Konzeption&planned=60" "$BASE/orders/1/tasks"

# Timer: Start → Pause → Weiter → Ende
for AKTION in start pause resume stop; do
  curl -sSL -b "$JAR" -c "$JAR" -o /dev/null -w "$AKTION: %{http_code}\n" \
    -d "csrf=$CSRF" "$BASE/orders/1/timer/$AKTION"
done

# Auswertung als CSV
curl -sS -b "$JAR" "$BASE/orders/1/report.csv" | head -20
```

Erwartung: alle Statuscodes `200` (mit `-L` folgt curl den Redirects), CSV
enthält `Auftrag;Smoke-Test`, `Soll (Minuten);60`, `Ist (Sekunden);…`.
Ohne `-L` sind die ersten drei Aufrufe `303 See Other` – das ist ebenfalls
normal, dann wird nur die Zielseite nicht mitgeladen.

### 4.5 Alternativ: im Browser klicken

1. <http://127.0.0.1:18080/login> öffnen
2. Anmelden mit `admin` / `testpass123`
3. Auftrag anlegen → Aufgabe mit geplanten Minuten → Timer Start/Pause/Weiter/Ende
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
| `address already in use` | Anderen Port wählen, z. B. `-addr 127.0.0.1:18081` |
| `attempt to write a readonly database` | Test-DB nicht als `root` anlegen; `./data` gehört dem eigenen User |

---

## 8. Kurzfassung zum Kopieren

```bash
cd ~/Projekte/opencode/arbeitszeit-manager
go mod download && gofmt -l . && go vet ./... && go test ./...
make build
mkdir -p data
./arbeitszeit-manager admin create-user -db data/test.db -username admin -role admin -password testpass123
./arbeitszeit-manager -addr 127.0.0.1:18080 -db data/test.db   # Terminal 1, Ende mit Strg+C
# Terminal 2:
curl -sS http://127.0.0.1:18080/healthz
```
