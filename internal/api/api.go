// SPDX-License-Identifier: AGPL-3.0-only

// Package api wires HTTP routes, templates and business logic together.
package api

import (
	"encoding/csv"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/karst3nl/arbeitszeit-manager/internal/auth"
	"github.com/karst3nl/arbeitszeit-manager/internal/model"
	"github.com/karst3nl/arbeitszeit-manager/internal/store"
	"github.com/karst3nl/arbeitszeit-manager/internal/timer"
	"github.com/karst3nl/arbeitszeit-manager/web"
)

// Server holds all dependencies of the web application.
type Server struct {
	store     *store.Store
	auth      *auth.Auth
	templates *template.Template
	version   string
	logger    *log.Logger
}

// userOrders is one card on the admin dashboard: a user together with the
// orders assigned to them and their status counts.
type userOrders struct {
	User        model.User
	Orders      []model.Order
	CountOpen   int
	CountActive int
	CountDone   int
}

type pageData struct {
	Title   string
	Nav     string
	User    *model.User
	CSRF    string
	Flash   string
	Error   string
	Version string

	Orders        []model.Order
	OpenOrders    []model.Order
	ActiveOrders  []model.Order
	DoneOrders    []model.Order
	OrderFilter   string
	UserOrders    []userOrders
	Unassigned    []model.Order
	CountAll      int
	CountOpen     int
	CountActive   int
	CountDone     int
	Order         *model.Order
	Tasks         []model.Task
	TaskTemplates []model.TaskTemplate
	Users         []model.User
	Report        *model.Report

	Elapsed   int64
	Delta     int64
	DeltaPct  float64
	State     string
	Since     int64
	ServerNow int64
	BarSoll   float64
	BarIst    float64
}

var templateFuncs = template.FuncMap{
	"dur":     timer.Duration,
	"signed":  timer.SignedDuration,
	"minutes": timer.Minutes,
	"date":    timer.DateTime,
	"upperFirst": func(s string) string {
		if s == "" {
			return "?"
		}
		r := []rune(s)
		return strings.ToUpper(string(r[0]))
	},
	"pct": func(v float64) string {
		return fmt.Sprintf("%+.1f %%", v)
	},
}

// New parses templates and creates the server.
func New(s *store.Store, a *auth.Auth, version string, logger *log.Logger) (*Server, error) {
	t, err := template.New("").Funcs(templateFuncs).ParseFS(web.Templates, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{store: s, auth: a, templates: t, version: version, logger: logger}, nil
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	static, err := fs.Sub(web.Static, "static")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	files := http.FileServer(http.FS(static))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)

	mux.Handle("GET /", s.auth.Require(http.HandlerFunc(s.dashboard)))
	mux.Handle("GET /about", s.auth.Require(http.HandlerFunc(s.about)))
	mux.Handle("GET /account", s.auth.Require(http.HandlerFunc(s.account)))
	mux.Handle("POST /account/password", s.auth.Require(http.HandlerFunc(s.changePassword)))
	mux.Handle("POST /account/display-name", s.auth.Require(http.HandlerFunc(s.changeDisplayName)))
	mux.Handle("GET /orders", s.auth.Require(http.HandlerFunc(s.orderList)))
	mux.Handle("POST /orders", s.auth.Require(http.HandlerFunc(s.createOrder)))
	mux.Handle("GET /orders/{id}", s.auth.Require(http.HandlerFunc(s.orderPage)))
	mux.Handle("POST /orders/{id}/assignee", s.auth.Require(http.HandlerFunc(s.setAssignee)))
	mux.Handle("POST /orders/{id}/tasks", s.auth.Require(http.HandlerFunc(s.addTask)))
	mux.Handle("POST /orders/{id}/tasks/{taskID}", s.auth.Require(http.HandlerFunc(s.updateTask)))
	mux.Handle("POST /orders/{id}/tasks/{taskID}/delete", s.auth.Require(http.HandlerFunc(s.deleteTask)))
	mux.Handle("POST /orders/{id}/notes", s.auth.Require(http.HandlerFunc(s.saveNotes)))
	mux.Handle("GET /orders/{id}/timer", s.auth.Require(http.HandlerFunc(s.timerFragment)))
	mux.Handle("POST /orders/{id}/timer/{action}", s.auth.Require(http.HandlerFunc(s.timerAction)))
	mux.Handle("GET /orders/{id}/report", s.auth.Require(http.HandlerFunc(s.report)))
	mux.Handle("GET /orders/{id}/report.csv", s.auth.Require(http.HandlerFunc(s.reportCSV)))
	mux.Handle("POST /orders/{id}/delete", s.auth.Require(http.HandlerFunc(s.deleteOrder)))
	mux.Handle("GET /admin", s.auth.RequireAdmin(http.HandlerFunc(s.admin)))
	mux.Handle("POST /admin/users", s.auth.RequireAdmin(http.HandlerFunc(s.createUser)))
	mux.Handle("POST /admin/users/{id}/active", s.auth.RequireAdmin(http.HandlerFunc(s.setUserActive)))
	mux.Handle("POST /admin/users/{id}/password", s.auth.RequireAdmin(http.HandlerFunc(s.resetPassword)))
	mux.Handle("POST /admin/task-templates", s.auth.RequireAdmin(http.HandlerFunc(s.createTaskTemplate)))
	mux.Handle("POST /admin/task-templates/{id}", s.auth.RequireAdmin(http.HandlerFunc(s.updateTaskTemplate)))
	mux.Handle("POST /admin/task-templates/{id}/delete", s.auth.RequireAdmin(http.HandlerFunc(s.deleteTaskTemplate)))

	return s.securityHeaders(s.auth.Middleware(mux))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		// Static assets override this with a long-lived cache header below.
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// --- helpers ---------------------------------------------------------------

func (s *Server) base(r *http.Request) pageData {
	path := r.URL.Path
	nav := "orders"
	switch {
	case strings.HasPrefix(path, "/admin"):
		nav = "admin"
	case strings.HasPrefix(path, "/account"):
		nav = "account"
	case strings.HasPrefix(path, "/about"):
		nav = "about"
	case path == "/login":
		nav = ""
	}
	return pageData{
		Nav:     nav,
		User:    auth.UserFrom(r),
		CSRF:    auth.CSRF(r),
		Version: s.version,
		Flash:   r.URL.Query().Get("ok"),
		Error:   r.URL.Query().Get("err"),
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data pageData) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Printf("render %s: %v", name, err)
	}
}

func (s *Server) fail(w http.ResponseWriter, status int, message string) {
	http.Error(w, message, status)
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func canEdit(u *model.User, o *model.Order) bool {
	if u == nil || o == nil {
		return false
	}
	return u.IsAdmin() || o.OwnerID == u.ID || (o.AssigneeID != 0 && o.AssigneeID == u.ID)
}

func (s *Server) timerData(r *http.Request, o *model.Order) (pageData, error) {
	data := s.base(r)
	data.Order = o
	snap, err := s.store.TimerSnapshot(o.ID)
	if err != nil {
		return data, err
	}
	data.State = snap.State
	data.Elapsed = snap.Elapsed
	data.Since = snap.Since
	data.ServerNow = time.Now().UnixMilli()
	data.Delta = snap.Elapsed - o.PlannedMinutes*60
	if o.PlannedMinutes > 0 {
		data.DeltaPct = float64(data.Delta) / float64(o.PlannedMinutes*60) * 100
	}
	return data, nil
}

// --- public pages ----------------------------------------------------------

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if auth.UserFrom(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	data := s.base(r)
	data.Title = "Anmelden"
	s.render(w, "login.html", data)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	if s.auth.LoginBlocked(r, username) {
		http.Redirect(w, r, "/login?err=Zu+viele+Fehlversuche.+Bitte+60+Sekunden+warten.", http.StatusSeeOther)
		return
	}
	u, err := s.auth.Login(w, username, r.FormValue("password"))
	if err != nil {
		s.auth.LoginFailed(r, username)
		s.logger.Printf("failed login for %q", username)
		http.Redirect(w, r, "/login?err=Benutzername+oder+Passwort+falsch.", http.StatusSeeOther)
		return
	}
	s.auth.LoginOK(r, username)
	s.logger.Printf("user %q logged in", u.Username)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/about" {
		s.fail(w, http.StatusNotFound, "Nicht gefunden")
		return
	}
	data := s.base(r)
	data.Title = "Über"
	s.render(w, "about.html", data)
}

// --- account ---------------------------------------------------------------

func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	data := s.base(r)
	data.Title = "Mein Konto"
	s.render(w, "account.html", data)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r)
	old := r.FormValue("old_password")
	next := r.FormValue("new_password")
	repeat := r.FormValue("repeat_password")

	if !auth.CheckPassword(u.PasswordHash, old) {
		http.Redirect(w, r, "/account?err=Das+aktuelle+Passwort+ist+falsch.", http.StatusSeeOther)
		return
	}
	if len(next) < 8 || next != repeat {
		http.Redirect(w, r, "/account?err=Neues+Passwort+mindestens+8+Zeichen+und+beide+Felder+gleich.", http.StatusSeeOther)
		return
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Passwort konnte nicht gehasht werden")
		return
	}
	// Hash and session invalidation happen in one transaction, then this
	// device gets a fresh session so the user stays logged in here.
	if err := s.store.ChangePassword(u.ID, hash); err != nil {
		s.fail(w, http.StatusInternalServerError, "Passwort konnte nicht gespeichert werden")
		return
	}
	if _, err := s.auth.Login(w, u.Username, next); err != nil {
		s.fail(w, http.StatusInternalServerError, "Neuanmeldung fehlgeschlagen")
		return
	}
	http.Redirect(w, r, "/account?ok=Passwort+ge%C3%A4ndert.+Andere+Ger%C3%A4te+wurden+abgemeldet.", http.StatusSeeOther)
}

func (s *Server) changeDisplayName(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r)
	name := r.FormValue("display_name")
	if name == "" {
		http.Redirect(w, r, "/account?err=Anzeigename+darf+nicht+leer+sein.", http.StatusSeeOther)
		return
	}
	if err := s.store.SetDisplayName(u.ID, name); err != nil {
		s.fail(w, http.StatusInternalServerError, "Anzeigename konnte nicht gespeichert werden")
		return
	}
	http.Redirect(w, r, "/account?ok=Anzeigename+gespeichert.", http.StatusSeeOther)
}

// --- orders ----------------------------------------------------------------

// orderGroups splits the orders visible to the current user into the three
// states shown in the summary bar: not started, in progress and done. Admins
// see the whole team, normal users only orders assigned to them. The optional
// filter keeps only one group while the counters always stay complete.
func (s *Server) orderGroups(r *http.Request, data *pageData) error {
	orders, err := s.store.Orders()
	if err != nil {
		return err
	}
	u := auth.UserFrom(r)
	for _, o := range orders {
		if u == nil || (!u.IsAdmin() && o.AssigneeID != u.ID) {
			continue
		}
		switch orderState(o) {
		case "done":
			data.DoneOrders = append(data.DoneOrders, o)
		case "active":
			data.ActiveOrders = append(data.ActiveOrders, o)
		default:
			data.OpenOrders = append(data.OpenOrders, o)
		}
	}
	data.CountOpen = len(data.OpenOrders)
	data.CountActive = len(data.ActiveOrders)
	data.CountDone = len(data.DoneOrders)
	data.CountAll = data.CountOpen + data.CountActive + data.CountDone

	switch data.OrderFilter {
	case "open":
		data.ActiveOrders, data.DoneOrders = nil, nil
	case "active":
		data.OpenOrders, data.DoneOrders = nil, nil
	case "done":
		data.OpenOrders, data.ActiveOrders = nil, nil
	}
	return nil
}

// orderFilter validates the filter query parameter so only known values reach
// the templates.
func orderFilter(r *http.Request) string {
	filter := r.URL.Query().Get("filter")
	switch filter {
	case "open", "active", "done":
		return filter
	default:
		return ""
	}
}

// orderState maps an order to one of the three summary counters.
func orderState(o model.Order) string {
	switch {
	case o.Done():
		return "done"
	case o.TimerState == "running" || o.TimerState == "paused":
		return "active"
	default:
		return "open"
	}
}

// adminOrders builds the user-centric dashboard data: every other user with
// their assigned orders, plus a section for orders nobody has claimed yet.
// The counters always cover all orders, even when a filter is active.
func (s *Server) adminOrders(r *http.Request, data *pageData) error {
	users, err := s.store.Users()
	if err != nil {
		return err
	}
	orders, err := s.store.Orders()
	if err != nil {
		return err
	}

	for _, o := range orders {
		switch orderState(o) {
		case "open":
			data.CountOpen++
		case "active":
			data.CountActive++
		case "done":
			data.CountDone++
		}
	}
	data.CountAll = data.CountOpen + data.CountActive + data.CountDone

	current := auth.UserFrom(r)
	for _, u := range users {
		if u.ID == current.ID {
			continue
		}
		entry := userOrders{User: u}
		for _, o := range orders {
			if o.AssigneeID != u.ID {
				continue
			}
			state := orderState(o)
			switch state {
			case "open":
				entry.CountOpen++
			case "active":
				entry.CountActive++
			case "done":
				entry.CountDone++
			}
			if data.OrderFilter == "" || data.OrderFilter == state {
				entry.Orders = append(entry.Orders, o)
			}
		}
		data.UserOrders = append(data.UserOrders, entry)
	}

	for _, o := range orders {
		if o.AssigneeID != 0 {
			continue
		}
		state := orderState(o)
		if data.OrderFilter == "" || data.OrderFilter == state {
			data.Unassigned = append(data.Unassigned, o)
		}
	}
	return nil
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.fail(w, http.StatusNotFound, "Nicht gefunden")
		return
	}
	data := s.base(r)
	data.Title = "Arbeitsaufträge"
	data.OrderFilter = orderFilter(r)
	if auth.UserFrom(r).IsAdmin() {
		templates, err := s.store.TaskTemplates()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "Aufgaben-Vorlagen konnten nicht geladen werden")
			return
		}
		data.TaskTemplates = templates
	}
	var err error
	if auth.UserFrom(r).IsAdmin() {
		err = s.adminOrders(r, &data)
	} else {
		err = s.orderGroups(r, &data)
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufträge konnten nicht geladen werden")
		return
	}
	s.render(w, "dashboard.html", data)
}

// orderList renders the summary bar and the role-specific order overview so
// HTMX can refresh both every 30 seconds without touching the create form.
func (s *Server) orderList(w http.ResponseWriter, r *http.Request) {
	data := s.base(r)
	data.OrderFilter = orderFilter(r)
	view := "orders-view"
	var err error
	if auth.UserFrom(r).IsAdmin() {
		err = s.adminOrders(r, &data)
		view = "admin-orders-view"
	} else {
		err = s.orderGroups(r, &data)
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufträge konnten nicht geladen werden")
		return
	}
	s.render(w, view, data)
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	customer := strings.TrimSpace(r.FormValue("customer"))
	plate := strings.TrimSpace(r.FormValue("plate"))
	if customer == "" || plate == "" {
		http.Redirect(w, r, "/?err=Kundenname+und+Kennzeichen+sind+erforderlich.", http.StatusSeeOther)
		return
	}
	title := customer + " · " + plate

	if err := r.ParseForm(); err != nil {
		s.fail(w, http.StatusBadRequest, "Ungültige Formulardaten")
		return
	}
	planned, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("planned")), 10, 64)
	if err != nil || planned < 1 || planned > 60000 {
		http.Redirect(w, r, "/?err=Soll+zwischen+1+und+60000+Minuten+angeben.", http.StatusSeeOther)
		return
	}
	var templateIDs []int64
	seen := make(map[int64]bool)
	for _, value := range r.Form["template_id"] {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		templateIDs = append(templateIDs, id)
	}

	var id int64
	if len(templateIDs) > 0 {
		id, err = s.store.CreateOrderFromTemplates(title, r.FormValue("description"),
			auth.UserFrom(r).ID, planned, templateIDs)
	} else {
		id, err = s.store.CreateOrder(title, r.FormValue("description"), auth.UserFrom(r).ID, planned)
	}
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, "/?err=Ausgew%C3%A4hlte+Aufgabe+nicht+mehr+vorhanden.", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Auftrag konnte nicht erstellt werden")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/orders/%d?ok=Auftrag+erstellt.", id), http.StatusSeeOther)
}

func (s *Server) orderPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	o, err := s.store.Order(id)
	if err == store.ErrNotFound {
		s.fail(w, http.StatusNotFound, "Auftrag nicht gefunden")
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Auftrag konnte nicht geladen werden")
		return
	}

	tasks, err := s.store.Tasks(id)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufgaben konnten nicht geladen werden")
		return
	}
	users, err := s.store.Users()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Nutzer konnten nicht geladen werden")
		return
	}

	data, err := s.timerData(r, o)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Timer konnte nicht geladen werden")
		return
	}
	data.Title = o.Title
	data.Tasks = tasks
	data.Users = users
	s.render(w, "order.html", data)
}

func (s *Server) timerFragment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	o, err := s.store.Order(id)
	if err != nil || o == nil {
		s.fail(w, http.StatusNotFound, "Auftrag nicht gefunden")
		return
	}
	if err := s.store.TouchTimer(id); err != nil {
		s.logger.Printf("touch timer %d: %v", id, err)
	}
	data, err := s.timerData(r, o)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Timer konnte nicht geladen werden")
		return
	}
	s.render(w, "timer", data)
}

func (s *Server) timerAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	o, err := s.store.Order(id)
	if err != nil || o == nil {
		s.fail(w, http.StatusNotFound, "Auftrag nicht gefunden")
		return
	}
	if !canEdit(auth.UserFrom(r), o) {
		s.fail(w, http.StatusForbidden, "Keine Berechtigung für diesen Auftrag")
		return
	}

	action := r.PathValue("action")
	if err := s.store.TimerAction(id, auth.UserFrom(r).ID, action); err != nil {
		if err == store.ErrConflict {
			data, _ := s.timerData(r, o)
			data.Error = "Der Timer wurde inzwischen von jemand anderem geändert."
			s.render(w, "timer", data)
			return
		}
		s.fail(w, http.StatusInternalServerError, "Timeraktion fehlgeschlagen")
		return
	}

	updated, err := s.store.Order(id)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Auftrag konnte nicht geladen werden")
		return
	}
	data, err := s.timerData(r, updated)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Timer konnte nicht geladen werden")
		return
	}
	s.render(w, "timer", data)
}

func (s *Server) setAssignee(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	o, err := s.store.Order(id)
	if err != nil || o == nil {
		s.fail(w, http.StatusNotFound, "Auftrag nicht gefunden")
		return
	}
	user := auth.UserFrom(r)
	assignee, _ := strconv.ParseInt(r.FormValue("assignee_id"), 10, 64)
	if assignee != 0 {
		active, err := s.store.IsActiveUser(assignee)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "Nutzer konnte nicht geprüft werden")
			return
		}
		if !active {
			s.fail(w, http.StatusBadRequest, "Ungültiger Nutzer")
			return
		}
	}
	// An unassigned order may be claimed by any logged-in user. Editing an
	// already assigned order remains limited to owner, assignee and admin.
	claiming := !canEdit(user, o)
	if claiming && (o.AssigneeID != 0 || assignee != user.ID) {
		s.fail(w, http.StatusForbidden, "Keine Berechtigung")
		return
	}
	if claiming {
		if err := s.store.ClaimOrder(id, assignee); err != nil {
			if errors.Is(err, store.ErrConflict) {
				http.Redirect(w, r, fmt.Sprintf("/orders/%d?err=Auftrag+wurde+inzwischen+zugewiesen.", id), http.StatusSeeOther)
				return
			}
			s.fail(w, http.StatusInternalServerError, "Zuordnung konnte nicht gespeichert werden")
			return
		}
	} else if err := s.store.SetAssignee(id, assignee); err != nil {
		s.fail(w, http.StatusInternalServerError, "Zuordnung konnte nicht gespeichert werden")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/orders/%d?ok=Zuordnung+gespeichert.", id), http.StatusSeeOther)
}

func (s *Server) deleteOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	o, err := s.store.Order(id)
	if err != nil || o == nil {
		s.fail(w, http.StatusNotFound, "Auftrag nicht gefunden")
		return
	}
	user := auth.UserFrom(r)
	if !user.IsAdmin() && o.OwnerID != user.ID {
		s.fail(w, http.StatusForbidden, "Nur der Ersteller oder ein Admin darf löschen")
		return
	}
	if o.TimerState == "running" {
		http.Redirect(w, r, fmt.Sprintf("/orders/%d?err=Timer+l%%C3%%A4uft.+Erst+beenden.", id), http.StatusSeeOther)
		return
	}
	if err := s.store.DeleteOrder(id); err != nil {
		s.fail(w, http.StatusInternalServerError, "Auftrag konnte nicht gelöscht werden")
		return
	}
	http.Redirect(w, r, "/?ok=Auftrag+gel%C3%B6scht.", http.StatusSeeOther)
}

// --- tasks -----------------------------------------------------------------

func (s *Server) addTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	o, err := s.store.Order(id)
	if err != nil || o == nil || !canEdit(auth.UserFrom(r), o) {
		s.fail(w, http.StatusForbidden, "Keine Berechtigung")
		return
	}
	if o.Done() {
		s.fail(w, http.StatusConflict, "Auftrag ist abgeschlossen. Erst wieder öffnen.")
		return
	}
	title := r.FormValue("title")
	if title == "" {
		http.Redirect(w, r, fmt.Sprintf("/orders/%d?err=Ung%%C3%%BCltige+Aufgabe.", id), http.StatusSeeOther)
		return
	}
	if err := s.store.AddTask(id, title); err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufgabe konnte nicht gespeichert werden")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/orders/%d?ok=Aufgabe+hinzugef%%C3%%BCgt.", id), http.StatusSeeOther)
}

func (s *Server) taskIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	orderID, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return 0, 0, false
	}
	taskID, ok := pathID(r, "taskID")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Aufgabennummer")
		return 0, 0, false
	}
	o, err := s.store.Order(orderID)
	if err != nil || o == nil || !canEdit(auth.UserFrom(r), o) {
		s.fail(w, http.StatusForbidden, "Keine Berechtigung")
		return 0, 0, false
	}
	if o.Done() {
		s.fail(w, http.StatusConflict, "Auftrag ist abgeschlossen. Erst wieder öffnen.")
		return 0, 0, false
	}
	// The task must actually belong to this order; otherwise a user who may
	// edit order A could change a task in order B through A's URL.
	belongs, err := s.store.TaskInOrder(orderID, taskID)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufgabe konnte nicht geprüft werden")
		return 0, 0, false
	}
	if !belongs {
		s.fail(w, http.StatusNotFound, "Aufgabe nicht gefunden")
		return 0, 0, false
	}
	return orderID, taskID, true
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	orderID, taskID, ok := s.taskIDs(w, r)
	if !ok {
		return
	}
	title := r.FormValue("title")
	if title == "" {
		http.Redirect(w, r, fmt.Sprintf("/orders/%d?err=Ung%%C3%%BCltige+Aufgabe.", orderID), http.StatusSeeOther)
		return
	}
	done := r.FormValue("done") == "on"
	if err := s.store.UpdateTask(orderID, taskID, title, done); err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufgabe konnte nicht gespeichert werden")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/orders/%d", orderID), http.StatusSeeOther)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	orderID, taskID, ok := s.taskIDs(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteTask(orderID, taskID); err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufgabe konnte nicht gelöscht werden")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/orders/%d", orderID), http.StatusSeeOther)
}

// --- reports and admin -----------------------------------------------------

func (s *Server) saveNotes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	o, err := s.store.Order(id)
	if err != nil || o == nil || !canEdit(auth.UserFrom(r), o) {
		s.fail(w, http.StatusForbidden, "Keine Berechtigung")
		return
	}
	if err := s.store.SetNotes(id, r.FormValue("notes")); err != nil {
		s.fail(w, http.StatusInternalServerError, "Notiz konnte nicht gespeichert werden")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/orders/%d/report?ok=Notiz+gespeichert.", id), http.StatusSeeOther)
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	rep, err := s.store.Report(id)
	if err != nil || rep == nil {
		s.fail(w, http.StatusNotFound, "Auswertung nicht gefunden")
		return
	}
	max := rep.PlannedMinutes * 60
	if rep.ActualSeconds > max {
		max = rep.ActualSeconds
	}
	if max == 0 {
		max = 1
	}
	data := s.base(r)
	data.Title = "Auswertung: " + rep.Title
	data.Report = rep
	data.BarSoll = float64(rep.PlannedMinutes*60) / float64(max) * 100
	data.BarIst = float64(rep.ActualSeconds) / float64(max) * 100
	s.render(w, "report.html", data)
}

// csvSafe prevents spreadsheet formula injection from user supplied text.
func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

// reportCSV exports the evaluation as a semicolon separated CSV for Excel.
func (s *Server) reportCSV(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Auftragsnummer")
		return
	}
	rep, err := s.store.Report(id)
	if err != nil || rep == nil {
		s.fail(w, http.StatusNotFound, "Auswertung nicht gefunden")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="auswertung-%d.csv"`, id))

	cw := csv.NewWriter(w)
	cw.Comma = ';'
	rows := [][]string{
		{"Auftrag", csvSafe(rep.Title)},
		{"Abgeschlossen", timer.DateTime(rep.CreatedAt)},
		{"Soll (Minuten)", strconv.FormatInt(rep.PlannedMinutes, 10)},
		{"Ist (Sekunden)", strconv.FormatInt(rep.ActualSeconds, 10)},
		{"Abweichung (Sekunden)", strconv.FormatInt(rep.DeviationSeconds, 10)},
		{"Abweichung (Prozent)", fmt.Sprintf("%.1f", rep.DeviationPercent)},
		{"Notiz", csvSafe(rep.Notes)},
		{},
		{"Aufgabe"},
	}
	for _, task := range rep.Tasks {
		rows = append(rows, []string{csvSafe(task.Title)})
	}
	if err := cw.WriteAll(rows); err != nil {
		s.logger.Printf("write csv: %v", err)
	}
}

func (s *Server) admin(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.Users()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Nutzer konnten nicht geladen werden")
		return
	}
	templates, err := s.store.TaskTemplates()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Aufgaben-Vorlagen konnten nicht geladen werden")
		return
	}
	data := s.base(r)
	data.Title = "Nutzer verwalten"
	data.Users = users
	data.TaskTemplates = templates
	s.render(w, "admin.html", data)
}

func (s *Server) createTaskTemplate(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Redirect(w, r, "/admin?err=Bezeichnung+erforderlich.", http.StatusSeeOther)
		return
	}
	if _, err := s.store.CreateTaskTemplate(title); err != nil {
		http.Redirect(w, r, "/admin?err=Aufgabe+mit+diesem+Namen+existiert+bereits.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?ok=Aufgaben-Vorlage+angelegt.", http.StatusSeeOther)
}

func (s *Server) updateTaskTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Vorlagennummer")
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Redirect(w, r, "/admin?err=Bezeichnung+erforderlich.", http.StatusSeeOther)
		return
	}
	if err := s.store.UpdateTaskTemplate(id, title); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, http.StatusNotFound, "Vorlage nicht gefunden")
			return
		}
		http.Redirect(w, r, "/admin?err=Aufgabe+mit+diesem+Namen+existiert+bereits.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?ok=Aufgaben-Vorlage+gespeichert.", http.StatusSeeOther)
}

func (s *Server) deleteTaskTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Vorlagennummer")
		return
	}
	if err := s.store.DeleteTaskTemplate(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, http.StatusNotFound, "Vorlage nicht gefunden")
			return
		}
		s.fail(w, http.StatusInternalServerError, "Vorlage konnte nicht gelöscht werden")
		return
	}
	http.Redirect(w, r, "/admin?ok=Aufgaben-Vorlage+gel%C3%B6scht.", http.StatusSeeOther)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	role := r.FormValue("role")
	if role != "admin" {
		role = "user"
	}
	if username == "" || len(password) < 8 {
		http.Redirect(w, r, "/admin?err=Benutzername+und+Passwort+(min.+8+Zeichen)+erforderlich.", http.StatusSeeOther)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Passwort konnte nicht gehasht werden")
		return
	}
	if _, err := s.store.CreateUser(username, r.FormValue("display_name"), hash, role); err != nil {
		http.Redirect(w, r, "/admin?err=Benutzername+existiert+bereits.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?ok=Nutzer+angelegt.", http.StatusSeeOther)
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Nutzernummer")
		return
	}
	if auth.UserFrom(r).ID == id {
		s.fail(w, http.StatusForbidden, "Das eigene Passwort bitte unter Konto ändern")
		return
	}
	if _, err := s.store.UserByID(id); err != nil {
		s.fail(w, http.StatusNotFound, "Nutzer nicht gefunden")
		return
	}
	password := r.FormValue("password")
	if len(password) < 8 {
		http.Redirect(w, r, "/admin?err=Passwort+muss+mindestens+8+Zeichen+haben.", http.StatusSeeOther)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "Passwort konnte nicht gehasht werden")
		return
	}
	if err := s.store.ChangePassword(id, hash); err != nil {
		s.fail(w, http.StatusInternalServerError, "Passwort konnte nicht gespeichert werden")
		return
	}
	http.Redirect(w, r, "/admin?ok=Passwort+zur%C3%BCckgesetzt.+Nutzer+muss+sich+neu+anmelden.", http.StatusSeeOther)
}

func (s *Server) setUserActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.fail(w, http.StatusBadRequest, "Ungültige Nutzernummer")
		return
	}
	if auth.UserFrom(r).ID == id {
		s.fail(w, http.StatusForbidden, "Der eigene Status kann nicht geändert werden")
		return
	}
	active := r.FormValue("active") == "1"
	if err := s.store.SetUserActive(id, active); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			http.Redirect(w, r, "/admin?err=Mindestens+ein+Admin+muss+aktiv+bleiben.", http.StatusSeeOther)
			return
		}
		s.fail(w, http.StatusInternalServerError, "Status konnte nicht geändert werden")
		return
	}
	http.Redirect(w, r, "/admin?ok=Status+gespeichert.", http.StatusSeeOther)
}
