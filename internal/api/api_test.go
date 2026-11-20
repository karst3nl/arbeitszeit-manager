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
		"csrf": {csrf}, "title": {"Kunde Muster"}, "description": {"Relaunch"},
	})
	if !strings.Contains(body, "Kunde Muster") {
		t.Fatalf("order page missing title: %s", body)
	}

	body = post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Konzeption"}, "planned": {"60"},
	})
	if !strings.Contains(body, "Konzeption") {
		t.Fatalf("task missing on order page: %s", body)
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
	if !strings.Contains(body, "Nutzer verwalten") {
		t.Fatalf("admin page missing")
	}
}

func TestAdminTaskTemplatesAndOrderDropdown(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	body := get(t, client, base+"/admin")
	if !strings.Contains(body, "Aufgaben-Vorlagen") || !strings.Contains(body, "Noch keine Vorlagen angelegt.") {
		t.Fatalf("admin page missing template section: %s", body)
	}

	post(t, client, base+"/admin/task-templates", url.Values{
		"csrf": {csrf}, "title": {"Konzeption"}, "planned": {"60"},
	})
	body = get(t, client, base+"/")
	if !strings.Contains(body, `<option value="1">Konzeption (60 min)</option>`) {
		t.Fatalf("template missing in dropdown: %s", body)
	}

	body = post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "title": {"Vorlagen-Auftrag"}, "planned": {"0"}, "template_id": {"1"},
	})
	if !strings.Contains(body, "Konzeption") {
		t.Fatalf("template task not copied into order: %s", body)
	}

	mia, miaCSRF := loginAs(t, base, "mia")
	if got := postStatus(t, mia, base+"/admin/task-templates", url.Values{
		"csrf": {miaCSRF}, "title": {"Schmuggel"}, "planned": {"5"},
	}); got != http.StatusForbidden {
		t.Fatalf("non-admin created a template: %d", got)
	}
	if got := postStatus(t, mia, base+"/admin/task-templates/1/delete", url.Values{
		"csrf": {miaCSRF},
	}); got != http.StatusForbidden {
		t.Fatalf("non-admin deleted a template: %d", got)
	}

	body = post(t, client, base+"/admin/task-templates/1/delete", url.Values{"csrf": {csrf}})
	if !strings.Contains(body, "Nutzer verwalten") {
		t.Fatalf("delete did not return to admin page: %s", body)
	}
	body = get(t, client, base+"/")
	if strings.Contains(body, "Konzeption (60 min)") {
		t.Fatal("deleted template is still in the dropdown")
	}
}

func TestUserCanClaimUnassignedOrder(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL

	post(t, client, base+"/orders", url.Values{
		"csrf": {csrf}, "title": {"Freier Auftrag"},
	})

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
	if !strings.Contains(body, "Mein Konto") {
		t.Fatalf("password change did not return to account page: %s", body)
	}
	loginAsWithPassword(t, base, "admin", "newpassword1")

	body = post(t, client, base+"/admin/users/2/password", url.Values{
		"csrf": {csrf}, "password": {"mianewpass1"},
	})
	if !strings.Contains(body, "Nutzer verwalten") {
		t.Fatalf("admin reset did not return to user list: %s", body)
	}
	loginAsWithPassword(t, base, "mia", "mianewpass1")
}

func TestCSVFormulaInjectionIsNeutralised(t *testing.T) {
	ts, client, csrf := setup(t)
	base := ts.URL
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "title": {"=1+1"}})
	post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"=SUM(A1)"}, "planned": {"10"},
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
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "title": {"Wegwerfauftrag"}})

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
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "title": {"Auftrag A"}})
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "title": {"Auftrag B"}})
	post(t, client, base+"/orders/2/tasks", url.Values{
		"csrf": {csrf}, "title": {"Geheim"}, "planned": {"10"},
	})

	if got := postStatus(t, client, base+"/orders/1/tasks/1", url.Values{
		"csrf": {csrf}, "title": {"Überschrieben"}, "planned": {"99"},
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
	post(t, client, base+"/orders", url.Values{"csrf": {csrf}, "title": {"Fertig"}})
	post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Konzeption"}, "planned": {"60"},
	})
	post(t, client, base+"/orders/1/timer/start", url.Values{"csrf": {csrf}})
	post(t, client, base+"/orders/1/timer/stop", url.Values{"csrf": {csrf}})

	if got := postStatus(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Zu spät"}, "planned": {"5"},
	}); got != http.StatusConflict {
		t.Fatalf("task add on done order returned %d, want 409", got)
	}
	if got := postStatus(t, client, base+"/orders/1/tasks/1", url.Values{
		"csrf": {csrf}, "title": {"Zu spät"}, "planned": {"5"},
	}); got != http.StatusConflict {
		t.Fatalf("task update on done order returned %d, want 409", got)
	}

	post(t, client, base+"/orders/1/timer/reopen", url.Values{"csrf": {csrf}})
	body := post(t, client, base+"/orders/1/tasks", url.Values{
		"csrf": {csrf}, "title": {"Nachtrag"}, "planned": {"5"},
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
