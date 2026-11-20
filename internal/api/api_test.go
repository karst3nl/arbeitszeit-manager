// SPDX-License-Identifier: AGPL-3.0-only

package api_test

import (
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/karst3nl/arbeitszeit-manager/internal/api"
	"github.com/karst3nl/arbeitszeit-manager/internal/auth"
	"github.com/karst3nl/arbeitszeit-manager/internal/store"
)

var csrfPattern = regexp.MustCompile(`name="csrf" value="([0-9a-f]{64})"`)

func setup(t *testing.T) (*httptest.Server, *http.Client, string) {
	t.Helper()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	hash, err := auth.HashPassword("testpassword")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("admin", "Admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("mia", "Mia", hash, "user"); err != nil {
		t.Fatal(err)
	}

	a := auth.New(st, false)
	srv, err := api.New(st, a, "test", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	page := get(t, client, ts.URL+"/login")
	csrf := csrfPattern.FindStringSubmatch(page)
	if len(csrf) != 2 {
		t.Fatalf("no CSRF token in login page")
	}

	form := url.Values{
		"csrf":     {csrf[1]},
		"username": {"admin"},
		"password": {"testpassword"},
	}
	body := post(t, client, ts.URL+"/login", form)
	if !strings.Contains(body, "Arbeitsaufträge") {
		t.Fatalf("login did not reach dashboard: %s", body)
	}
	return ts, client, csrf[1]
}

func get(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d: %s", target, resp.StatusCode, b)
	}
	return string(b)
}

func post(t *testing.T, client *http.Client, target string, values url.Values) string {
	t.Helper()
	resp, err := client.Post(target, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: %d: %s", target, resp.StatusCode, b)
	}
	return string(b)
}

func postHTMX(t *testing.T, client *http.Client, target string, values url.Values) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTMX POST %s: %d: %s", target, resp.StatusCode, b)
	}
	return string(b)
}

func postStatus(t *testing.T, client *http.Client, target string, values url.Values) int {
	t.Helper()
	resp, err := client.Post(target, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func loginAs(t *testing.T, base, username string) (*http.Client, string) {
	t.Helper()
	return loginAsWithPassword(t, base, username, "testpassword")
}

func loginAsWithPassword(t *testing.T, base, username, password string) (*http.Client, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	page := get(t, client, base+"/login")
	match := csrfPattern.FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatal("no CSRF token on login page")
	}
	post(t, client, base+"/login", url.Values{
		"csrf": {match[1]}, "username": {username}, "password": {password},
	})
	return client, match[1]
}

func statusOf(t *testing.T, client *http.Client, target string) int {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestFullOrderLifecycle(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	body := post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"Kunde Muster"}, "plate": {"M-AB 123"}, "description": {"Relaunch"},
		"aw": {"20"}, "assignee_id": {"1"},
	})
	if !strings.Contains(body, "Kunde Muster") {
		t.Fatalf("order page missing title: %s", body)
	}

	body = post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Konzeption"},
	})
	if !strings.Contains(body, "Konzeption") || strings.Contains(body, "Geplante Minuten") {
		t.Fatalf("task missing on order page or still timed: %s", body)
	}

	body = post(t, client, base+"/orders/1/timer/start", url.Values{"csrf": {csrf}})
	if !strings.Contains(body, "läuft") {
		t.Fatalf("timer did not start: %s", body)
	}

	body = post(t, client, base+"/orders/1/timer/pause", url.Values{"csrf": {csrf}})
	if !strings.Contains(body, "pausiert") {
		t.Fatalf("timer did not pause: %s", body)
	}

	body = post(t, client, base+"/orders/1/timer/resume", url.Values{"csrf": {csrf}})
	if !strings.Contains(body, "läuft") {
		t.Fatalf("timer did not resume: %s", body)
	}

	body = post(t, client, base+"/orders/1/timer/stop", url.Values{"csrf": {csrf}})
	if !strings.Contains(body, "Auswertung ansehen") {
		t.Fatalf("timer did not stop: %s", body)
	}

	body = get(t, client, base+"/orders/1/report")
	if !strings.Contains(body, "Soll / Ist") || !strings.Contains(body, "Abweichung") ||
		!strings.Contains(body, "Kunde Muster") {
		t.Fatalf("report page incomplete: %s", body)
	}

	body = get(t, client, base+"/orders/1/report.csv")
	if !strings.Contains(body, "Soll (Minuten)") || !strings.Contains(body, "Konzeption") {
		t.Fatalf("CSV export incomplete: %s", body)
	}

	body = get(t, client, base+"/about")
	if !strings.Contains(body, "AGPL-3.0-only") {
		t.Fatalf("about page missing license notice")
	}

	body = get(t, client, base+"/admin")
	if !strings.Contains(body, "Neuer Nutzer") {
		t.Fatalf("admin page missing")
	}
}

func TestDashboardIsRoleSpecific(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Offener Auftrag"}, "plate": {"M-AB 1"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Mia Auftrag"}, "plate": {"M-AB 2"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Admin Auftrag"}, "plate": {"M-AB 3"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders/1/assignee", url.Values{"csrf": {csrf}, "assignee_id": {"0"}})
	post(t, client, base+"/orders/2/assignee", url.Values{"csrf": {csrf}, "assignee_id": {"2"}})
	post(t, client, base+"/orders/3/assignee", url.Values{"csrf": {csrf}, "assignee_id": {"1"}})

	body := get(t, client, base+"/")
	for _, want := range []string{"Neuer Auftrag", "Nicht zugeteilt", "@mia", "Läuft",
		"Offener Auftrag", "Mia Auftrag"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin dashboard missing %q", want)
		}
	}
	if strings.Contains(body, "@admin") {
		t.Fatal("admin dashboard lists the current admin as another user")
	}

	mia, _ := loginAs(t, base, "mia")
	body = get(t, mia, base+"/")
	if strings.Contains(body, "Neuer Auftrag") {
		t.Fatal("user dashboard shows the admin create form")
	}
	if !strings.Contains(body, "Hier siehst du ausschließlich") || !strings.Contains(body, "Mia Auftrag") {
		t.Fatalf("user dashboard missing own order: %s", body)
	}
	for _, foreign := range []string{"Offener Auftrag", "Admin Auftrag"} {
		if strings.Contains(body, foreign) {
			t.Fatalf("user dashboard shows foreign order %q", foreign)
		}
	}

	// The 30-second refresh must apply the same role filter.
	body = get(t, mia, base+"/orders")
	if !strings.Contains(body, "Mia Auftrag") || strings.Contains(body, "Admin Auftrag") ||
		strings.Contains(body, "Offener Auftrag") {
		t.Fatalf("refresh fragment leaks foreign orders: %s", body)
	}
}

func TestOrderSummaryCountersAndFilter(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Offen Nr.1"}, "plate": {"M-AB 1"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Laufend Nr.2"}, "plate": {"M-AB 2"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Fertig Nr.3"}, "plate": {"M-AB 3"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders/1/assignee", url.Values{"csrf": {csrf}, "assignee_id": {"0"}})
	post(t, client, base+"/orders/2/assignee", url.Values{"csrf": {csrf}, "assignee_id": {"2"}})
	post(t, client, base+"/orders/3/assignee", url.Values{"csrf": {csrf}, "assignee_id": {"2"}})
	post(t, client, base+"/orders/2/timer/start", url.Values{"csrf": {csrf}})
	post(t, client, base+"/orders/3/timer/start", url.Values{"csrf": {csrf}})
	post(t, client, base+"/orders/3/timer/stop", url.Values{"csrf": {csrf}})

	body := get(t, client, base+"/")
	for _, want := range []string{
		`data-count="all" data-value="3"`,
		`data-count="open" data-value="1"`,
		`data-count="active" data-value="1"`,
		`data-count="done" data-value="1"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing counter %q: %s", want, body)
		}
	}

	for _, tc := range []struct {
		filter string
		want   string
		hidden []string
	}{
		{"open", "Offen Nr.1", []string{"Laufend Nr.2", "Fertig Nr.3"}},
		{"active", "Laufend Nr.2", []string{"Offen Nr.1", "Fertig Nr.3"}},
		{"done", "Fertig Nr.3", []string{"Offen Nr.1", "Laufend Nr.2"}},
	} {
		body = get(t, client, base+"/orders?filter="+tc.filter)
		if !strings.Contains(body, tc.want) {
			t.Fatalf("filter %q missing %q: %s", tc.filter, tc.want, body)
		}
		for _, absent := range tc.hidden {
			if strings.Contains(body, absent) {
				t.Fatalf("filter %q leaked %q: %s", tc.filter, absent, body)
			}
		}
		// Filtering must not change the counters themselves.
		if !strings.Contains(body, `data-count="all" data-value="3"`) {
			t.Fatalf("filter %q changed the counters: %s", tc.filter, body)
		}
	}

	mia, _ := loginAs(t, base, "mia")
	body = get(t, mia, base+"/")
	for _, want := range []string{
		`data-count="all" data-value="2"`,
		`data-count="open" data-value="0"`,
		`data-count="active" data-value="1"`,
		`data-count="done" data-value="1"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("user dashboard missing counter %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Offen Nr.1") {
		t.Fatal("user dashboard shows an unassigned order")
	}
}

func TestAdminTaskTemplatesAndOrderCreation(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	body := get(t, client, base+"/admin")
	if !strings.Contains(body, "Aufgaben-Vorlagen") || !strings.Contains(body, "Noch keine Vorlagen angelegt.") {
		t.Fatalf("admin page missing template section: %s", body)
	}

	post(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Konzeption"},
	})
	post(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Umsetzung"},
	})
	body = get(t, client, base+"/")
	if !strings.Contains(body, `name="template_id" value="1"`) ||
		!strings.Contains(body, `name="template_id" value="2"`) {
		t.Fatalf("templates missing in multi-select: %s", body)
	}

	// The creator sets the Soll; templates only supply task names.
	body = post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"Muster GmbH"}, "plate": {"M-AB 1234"},
		"aw": {"30"}, "assignee_id": {"1"}, "template_id": {"1", "2"},
	})
	for _, want := range []string{"Muster GmbH · M-AB 1234", "Konzeption", "Umsetzung", "3 h 0 min"} {
		if !strings.Contains(body, want) {
			t.Fatalf("order page missing %q: %s", want, body)
		}
	}

	mia, miaCSRF := loginAs(t, base, "mia")
	if got := postStatus(t, mia, base+"/admin/task-templates", url.Values{
		"csrf": {miaCSRF}, "title": {"Schmuggel"},
	}); got != http.StatusForbidden {
		t.Fatalf("non-admin created a template: %d", got)
	}
	if got := postStatus(t, mia, base+"/admin/task-templates/1", url.Values{
		"csrf": {miaCSRF}, "title": {"Schmuggel"},
	}); got != http.StatusForbidden {
		t.Fatalf("non-admin renamed a template: %d", got)
	}

	body = post(t, client, base+"/admin/task-templates/1", url.Values{
		"csrf": {csrf}, "title": {"Konzeption neu"},
	})
	if !strings.Contains(body, "Konzeption neu") || strings.Contains(body, `value="Konzeption"`) {
		t.Fatalf("template rename failed: %s", body)
	}
	body = get(t, client, base+"/")
	if !strings.Contains(body, `name="template_id" value="1"`) ||
		!strings.Contains(body, "Konzeption neu") || strings.Contains(body, ">Konzeption<") {
		t.Fatalf("renamed template missing in dropdown: %s", body)
	}
	if got := postStatus(t, mia, base+"/admin/task-templates/1/delete", url.Values{
		"csrf": {miaCSRF},
	}); got != http.StatusForbidden {
		t.Fatalf("non-admin deleted a template: %d", got)
	}

	body = post(t, client, base+"/admin/task-templates/2/delete", url.Values{"csrf": {csrf}})
	if !strings.Contains(body, "Neuer Nutzer") {
		t.Fatalf("delete did not return to admin page: %s", body)
	}
	body = get(t, client, base+"/")
	if strings.Contains(body, `name="template_id" value="2"`) {
		t.Fatal("deleted template is still selectable")
	}
	body = get(t, client, base+"/orders/1")
	if !strings.Contains(body, "Umsetzung") {
		t.Fatal("deleting a template removed it from an existing order")
	}
}

func TestOrderCreationRequiresAssigneeAndAW(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	body := post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"Ohne Nutzer"}, "plate": {"M-AB 1"}, "aw": {"10"},
	})
	if !strings.Contains(body, "Bitte einen Nutzer für den Auftrag wählen.") {
		t.Fatalf("missing assignee was accepted: %s", body)
	}
	body = post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"Falsches AW"}, "plate": {"M-AB 2"},
		"aw": {"0"}, "assignee_id": {"1"},
	})
	if !strings.Contains(body, "Soll in AW angeb") {
		t.Fatalf("invalid AW was accepted: %s", body)
	}
	body = get(t, client, base+"/")
	if strings.Contains(body, "Ohne Nutzer") || strings.Contains(body, "Falsches AW") {
		t.Fatal("an invalid order was created")
	}

	body = post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"AW Test"}, "plate": {"M-AB 3"},
		"aw": {"2,5"}, "assignee_id": {"1"},
	})
	if !strings.Contains(body, "15 min") || !strings.Contains(body, "2,5 AW") {
		t.Fatalf("AW was not converted and displayed: %s", body)
	}

	post(t, client, base+"/admin/users/2/active", url.Values{
		"csrf": {csrf}, "active": {"0"},
	})
	body = post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"Inaktiver Nutzer"}, "plate": {"M-AB 4"},
		"aw": {"10"}, "assignee_id": {"2"},
	})
	if !strings.Contains(body, "Der gewählte Nutzer ist nicht verfügbar.") {
		t.Fatalf("inactive assignee was accepted: %s", body)
	}
	body = get(t, client, base+"/")
	if strings.Contains(body, "Inaktiver Nutzer") {
		t.Fatal("an order was created for an inactive user")
	}
}

func TestAdminAWFactorIsUsed(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	body := post(t, client, base+"/admin/aw-factor", url.Values{
		"csrf": {csrf}, "minutes_per_aw": {"10"},
	})
	if !strings.Contains(body, "AW-Faktor gespeichert.") {
		t.Fatalf("AW factor was not saved: %s", body)
	}

	body = get(t, client, base+"/")
	if !strings.Contains(body, "1 AW = 10 Minuten") {
		t.Fatalf("dashboard hint does not show the configured factor: %s", body)
	}
	body = post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"Faktor Auftrag"}, "plate": {"M-FA 1"},
		"aw": {"6"}, "assignee_id": {"1"},
	})
	if !strings.Contains(body, "1 h 0 min") || !strings.Contains(body, "6 AW") {
		t.Fatalf("AW factor was not used for Soll: %s", body)
	}
	post(t, client, base+"/orders/1/timer/start", url.Values{"csrf": {csrf}})
	post(t, client, base+"/orders/1/timer/stop", url.Values{"csrf": {csrf}})
	csv := get(t, client, base+"/orders/1/report.csv")
	if !strings.Contains(csv, "Soll (AW);6") {
		t.Fatalf("CSV did not use the configured factor: %s", csv)
	}

	body = post(t, client, base+"/admin/aw-factor", url.Values{
		"csrf": {csrf}, "minutes_per_aw": {"0"},
	})
	if !strings.Contains(body, "größer als 0") {
		t.Fatalf("invalid AW factor was accepted: %s", body)
	}
}

func TestInlineTemplateCreationFromDashboard(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	body := post(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Direkt aus Auftrag"}, "return_to": {"/"},
	})
	if !strings.Contains(body, "Neuer Auftrag") || !strings.Contains(body, "Direkt aus Auftrag") {
		t.Fatalf("inline template did not return to the dashboard: %s", body)
	}
	body = get(t, client, base+"/")
	if !strings.Contains(body, `name="template_id" value="1"`) ||
		!strings.Contains(body, "Direkt aus Auftrag") {
		t.Fatalf("new template missing in dropdown: %s", body)
	}
}

func TestInlineTemplateCreationKeepsOrderFormState(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	post(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Erste"},
	})
	post(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Zweite"},
	})

	fragment := postHTMX(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Neu"}, "template_id": {"1"},
	})
	for _, want := range []string{
		`value="1" checked`,
		`value="3" checked`,
		"Aufgabe angelegt und ausgewählt.",
		`id="template-options"`,
	} {
		if !strings.Contains(fragment, want) {
			t.Fatalf("fragment missing %q: %s", want, fragment)
		}
	}
	if strings.Contains(fragment, `action="/orders"`) {
		t.Fatalf("fragment replaced the order form: %s", fragment)
	}
	if strings.Contains(fragment, `value="Neu"`) {
		t.Fatalf("new template input was not cleared: %s", fragment)
	}

	// A failed creation keeps the entered text and the previous selection.
	fragment = postHTMX(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Neu"}, "template_id": {"1", "3"},
	})
	for _, want := range []string{
		"Aufgabe mit diesem Namen existiert bereits.",
		`value="Neu"`,
		`value="1" checked`,
		`value="3" checked`,
	} {
		if !strings.Contains(fragment, want) {
			t.Fatalf("error fragment missing %q: %s", want, fragment)
		}
	}
}

func TestUserCanClaimUnassignedOrder(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "customer": {"Freier Auftrag"}, "plate": {"M-AB 4"}, "aw": {"10"}, "assignee_id": {"1"},
	})

	post(t, client, base+"/orders/1/assignee", url.Values{"csrf": {csrf}, "assignee_id": {"0"}})
	mia, miaCSRF := loginAs(t, base, "mia")
	body := post(t, mia, base+"/orders/1/assignee", url.Values{
		"csrf": {miaCSRF}, "assignee_id": {"2"},
	})
	if !strings.Contains(body, "Bearbeitet von Mia") {
		t.Fatalf("claim did not show assignee: %s", body)
	}

	body = post(t, mia, base+"/orders/1/timer/start", url.Values{"csrf": {miaCSRF}})
	if !strings.Contains(body, "läuft") {
		t.Fatalf("claimed order should be editable: %s", body)
	}
}

func TestPasswordChangeAndAdminReset(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	body := get(t, client, base+"/account")
	if !strings.Contains(body, "Passwort ändern") {
		t.Fatal("account page missing password form")
	}

	body = post(t, client, base+"/account/password", url.Values{
		"csrf": {csrf}, "old_password": {"testpassword"},
		"new_password": {"newpassword1"}, "repeat_password": {"newpassword1"},
	})
	if !strings.Contains(body, "Anzeigename") {
		t.Fatalf("password change did not return to account page: %s", body)
	}
	loginAsWithPassword(t, base, "admin", "newpassword1")

	body = post(t, client, base+"/admin/users/2/password", url.Values{
		"csrf": {csrf}, "password": {"mianewpass1"},
	})
	if !strings.Contains(body, "Neuer Nutzer") {
		t.Fatalf("admin reset did not return to user list: %s", body)
	}
	loginAsWithPassword(t, base, "mia", "mianewpass1")
}

func TestCSVFormulaInjectionIsNeutralised(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"=1+1"}, "plate": {"M-AB 5"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"=SUM(A1)"},
	})
	post(t, client, base+"/orders/1/timer/start", url.Values{"csrf": {csrf}})
	post(t, client, base+"/orders/1/timer/stop", url.Values{"csrf": {csrf}})

	body := get(t, client, base+"/orders/1/report.csv")
	if !strings.Contains(body, "'=1+1") || !strings.Contains(body, "'=SUM(A1)") {
		t.Fatalf("formula values not escaped: %s", body)
	}
}

func TestDeleteOrder(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Wegwerfauftrag"}, "plate": {"M-AB 6"}, "aw": {"10"}, "assignee_id": {"1"}})

	body := post(t, client, base+"/orders/1/delete", url.Values{"csrf": {csrf}})
	if !strings.Contains(body, "Arbeitsaufträge") {
		t.Fatalf("delete did not return to dashboard: %s", body)
	}
	if got := statusOf(t, client, base+"/orders/1"); got != http.StatusNotFound {
		t.Fatalf("deleted order still reachable: %d", got)
	}
}

func TestTaskCannotBeChangedThroughAnotherOrder(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Auftrag A"}, "plate": {"M-AB 7"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Auftrag B"}, "plate": {"M-AB 8"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders/2/tasks", url.Values{
		"csrf": {csrf}, "title": {"Geheim"},
	})

	if got := postStatus(t, client, base+"/orders/1/tasks/1", url.Values{
		"csrf": {csrf}, "title": {"Überschrieben"},
	}); got != http.StatusNotFound {
		t.Fatalf("cross-order task update returned %d, want 404", got)
	}
	if got := postStatus(t, client, base+"/orders/1/tasks/1/delete", url.Values{
		"csrf": {csrf},
	}); got != http.StatusNotFound {
		t.Fatalf("cross-order task delete returned %d, want 404", got)
	}
	body := get(t, client, base+"/orders/2")
	if !strings.Contains(body, "Geheim") {
		t.Fatal("task was changed through a foreign order")
	}
}

func TestCompletedOrderTasksAreLocked(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "customer": {"Fertig"}, "plate": {"M-AB 9"}, "aw": {"10"}, "assignee_id": {"1"}})
	post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Konzeption"},
	})
	post(t, client, base+"/orders/1/timer/start", url.Values{"csrf": {csrf}})
	post(t, client, base+"/orders/1/timer/stop", url.Values{"csrf": {csrf}})

	if got := postStatus(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Zu spät"},
	}); got != http.StatusConflict {
		t.Fatalf("task add on done order returned %d, want 409", got)
	}
	if got := postStatus(t, client, base+"/orders/1/tasks/1", url.Values{
		"csrf": {csrf}, "title": {"Zu spät"},
	}); got != http.StatusConflict {
		t.Fatalf("task update on done order returned %d, want 409", got)
	}

	post(t, client, base+"/orders/1/timer/reopen", url.Values{"csrf": {csrf}})
	body := post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Nachtrag"},
	})
	if !strings.Contains(body, "Nachtrag") {
		t.Fatal("task could not be added after reopening the order")
	}
}

func TestFailedLoginsDoNotBlockOtherAccounts(t *testing.T) {
	ts, _, _ := setup(t)
	base := ts.URL
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	page := get(t, client, base+"/login")
	csrf := csrfPattern.FindStringSubmatch(page)[1]
	for i := 0; i < 3; i++ {
		post(t, client, base+"/login", url.Values{
			"csrf": {csrf}, "username": {"admin"}, "password": {"falsch"},
		})
	}

	mia, _ := loginAs(t, base, "mia")
	if got := statusOf(t, mia, base+"/"); got != http.StatusOK {
		t.Fatalf("other account locked out: %d", got)
	}
}

func TestAdminCannotChangeOwnStatusOrPassword(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL
	if got := postStatus(t, client, base+"/admin/users/1/active", url.Values{
		"csrf": {csrf}, "active": {"0"},
	}); got != http.StatusForbidden {
		t.Fatalf("self deactivation returned %d, want 403", got)
	}
	if got := postStatus(t, client, base+"/admin/users/1/password", url.Values{
		"csrf": {csrf}, "password": {"irgendwas1"},
	}); got != http.StatusForbidden {
		t.Fatalf("self password reset returned %d, want 403", got)
	}
}

func TestCSRFIsEnforced(t *testing.T) {
	ts, client, _ := setup(t)
	resp, err := client.Post(ts.URL+"/orders", "application/x-www-form-urlencoded",
		strings.NewReader("title=Ohne+Token"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d", resp.StatusCode)
	}
}
