// Package web serves the timetrack browser UI.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/davison/timetrack/internal/report"
	"github.com/davison/timetrack/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type server struct {
	store       *store.Store
	tmpl        *template.Template
	capacityDay float64 // hours considered "full" for one day
}

// NewServer returns the HTTP handler for the whole UI. capacityDay is the
// number of hours per day treated as fully committed.
func NewServer(s *store.Store, capacityDay float64) http.Handler {
	srv := &server{
		store: s,
		tmpl: template.Must(template.New("").Funcs(template.FuncMap{
			"hours": func(h float64) string { return strconv.FormatFloat(h, 'f', -1, 64) + "h" },
			"pct": func(v, of float64) string {
				if of <= 0 {
					return "0"
				}
				return strconv.FormatFloat(min(100*v/of, 100), 'f', 1, 64)
			},
			"texton": textOn,
		}).ParseFS(templateFS, "templates/*.html")),
		capacityDay: capacityDay,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", srv.dashboard)
	mux.HandleFunc("GET /entries", srv.entries)
	mux.HandleFunc("GET /entries/new", srv.entryNew)
	mux.HandleFunc("POST /entries", srv.entryCreate)
	mux.HandleFunc("GET /entries/{id}/edit", srv.entryEdit)
	mux.HandleFunc("POST /entries/{id}", srv.entryUpdate)
	mux.HandleFunc("POST /entries/{id}/delete", srv.entryDelete)
	mux.HandleFunc("POST /entries/{id}/confirm", srv.entryConfirm)
	mux.HandleFunc("GET /calendar", srv.calendar)
	mux.HandleFunc("GET /projects", srv.projects)
	mux.HandleFunc("POST /projects", srv.projectCreate)
	mux.HandleFunc("POST /projects/{id}", srv.projectUpdate)
	mux.HandleFunc("POST /projects/{id}/delete", srv.projectDelete)
	mux.HandleFunc("POST /projects/{id}/parent", srv.projectReparent)
	mux.HandleFunc("POST /timer/start", srv.timerStart)
	mux.HandleFunc("POST /timer/stop", srv.timerStop)
	mux.HandleFunc("GET /report", srv.report)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	return mux
}

// Serve runs the web UI until the process is stopped.
func Serve(s *store.Store, addr string, capacityDay float64) error {
	log.Printf("timetrack listening on http://localhost%s", addr)
	return http.ListenAndServe(addr, NewServer(s, capacityDay))
}

func today() string { return time.Now().Format("2006-01-02") }

// textOn picks a readable foreground colour (near-black or white) for text
// placed on top of the given hex background colour, based on perceived
// luminance. Falls back to white for empty or malformed input.
func textOn(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b int64
	var err error
	switch len(hex) {
	case 3:
		r, err = strconv.ParseInt(strings.Repeat(string(hex[0]), 2), 16, 0)
		if err == nil {
			g, err = strconv.ParseInt(strings.Repeat(string(hex[1]), 2), 16, 0)
		}
		if err == nil {
			b, err = strconv.ParseInt(strings.Repeat(string(hex[2]), 2), 16, 0)
		}
	case 6:
		r, err = strconv.ParseInt(hex[0:2], 16, 0)
		if err == nil {
			g, err = strconv.ParseInt(hex[2:4], 16, 0)
		}
		if err == nil {
			b, err = strconv.ParseInt(hex[4:6], 16, 0)
		}
	default:
		err = fmt.Errorf("texton: invalid colour %q", hex)
	}
	if err != nil {
		return "#ffffff"
	}
	luminance := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	if luminance > 150 {
		return "#0b0b0b"
	}
	return "#ffffff"
}

// weekOf returns the Monday of the week containing t.
func weekOf(t time.Time) time.Time {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return time.Date(t.Year(), t.Month(), t.Day()-weekday+1, 0, 0, 0, 0, time.Local)
}

func (s *server) render(w http.ResponseWriter, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, page, data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

func (s *server) fail(w http.ResponseWriter, err error) {
	log.Print(err)
	s.render(w, http.StatusInternalServerError, "error.html", map[string]any{
		"Title": "Error", "Active": "", "Message": err.Error(),
	})
}

// --- dashboard ---

type projectBar struct {
	Name    string
	Color   string
	Logged  float64
	Planned float64
	// Percentages of the widest bar, for CSS widths.
	LoggedPct  float64
	PlannedPct float64
}

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	day := now.Format("2006-01-02")
	monday := weekOf(now)
	weekFrom, weekTo := monday.Format("2006-01-02"), monday.AddDate(0, 0, 6).Format("2006-01-02")

	todayRep, err := report.Build(s.store, store.Filter{From: day, To: day}, report.ByProject)
	if err != nil {
		s.fail(w, err)
		return
	}
	weekRep, err := report.Build(s.store, store.Filter{From: weekFrom, To: weekTo}, report.ByProject)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Bars show top-level projects only; rollup lines already include
	// sub-project hours.
	var bars []projectBar
	maxHours := 0.1
	for _, l := range weekRep.Lines {
		if l.Sub {
			continue
		}
		if t := l.LoggedHours + l.PlannedHours; t > maxHours {
			maxHours = t
		}
	}
	projectColors := map[string]string{}
	projects, err := s.store.Projects(false)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, p := range projects {
		projectColors[p.Path()] = p.Color
	}
	for _, l := range weekRep.Lines {
		if l.Sub {
			continue
		}
		bars = append(bars, projectBar{
			Name: l.Key, Color: projectColors[l.Key],
			Logged: l.LoggedHours, Planned: l.PlannedHours,
			LoggedPct:  100 * l.LoggedHours / maxHours,
			PlannedPct: 100 * l.PlannedHours / maxHours,
		})
	}

	todayEntries, err := s.store.Entries(store.Filter{From: day, To: day})
	if err != nil {
		s.fail(w, err)
		return
	}
	running, err := s.store.RunningEntry()
	if err != nil {
		s.fail(w, err)
		return
	}
	var elapsed time.Duration
	if running != nil {
		elapsed = time.Since(running.StartedAt).Round(time.Minute)
	}

	capacityWeek := s.capacityDay * 5
	s.render(w, http.StatusOK, "dashboard.html", map[string]any{
		"Title":        "Dashboard",
		"Active":       "dashboard",
		"Date":         now.Format("Monday 2 January 2006"),
		"TodayLogged":  todayRep.TotalLogged,
		"TodayPlanned": todayRep.TotalPlanned,
		"TodayTotal":   todayRep.TotalLogged + todayRep.TotalPlanned,
		"WeekLogged":   weekRep.TotalLogged,
		"WeekPlanned":  weekRep.TotalPlanned,
		"WeekTotal":    weekRep.TotalLogged + weekRep.TotalPlanned,
		"CapacityDay":  s.capacityDay,
		"CapacityWeek": capacityWeek,
		"OverToday":    todayRep.TotalLogged+todayRep.TotalPlanned > s.capacityDay,
		"OverWeek":     weekRep.TotalLogged+weekRep.TotalPlanned > capacityWeek,
		"Bars":         bars,
		"TodayEntries": todayEntries,
		"Running":      running,
		"Elapsed":      elapsed,
		"Projects":     projects,
	})
}

// --- entries ---

func (s *server) entryFilter(r *http.Request) store.Filter {
	q := r.URL.Query()
	return store.Filter{
		Project: q.Get("project"),
		Tag:     q.Get("tag"),
		From:    q.Get("from"),
		To:      q.Get("to"),
		Kind:    store.Kind(q.Get("kind")),
		Search:  q.Get("search"),
	}
}

func (s *server) entries(w http.ResponseWriter, r *http.Request) {
	f := s.entryFilter(r)
	entries, err := s.store.Entries(f)
	if err != nil {
		s.fail(w, err)
		return
	}
	projects, err := s.store.Projects(false)
	if err != nil {
		s.fail(w, err)
		return
	}
	var totalLogged, totalPlanned float64
	for _, e := range entries {
		switch e.Kind {
		case store.KindLogged:
			totalLogged += e.Hours()
		case store.KindPlanned:
			totalPlanned += e.Hours()
		}
	}
	data := map[string]any{
		"Title":        "Entries",
		"Active":       "entries",
		"Entries":      entries,
		"Projects":     projects,
		"Filter":       f,
		"TotalLogged":  totalLogged,
		"TotalPlanned": totalPlanned,
	}
	// htmx filter requests swap just the table.
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, http.StatusOK, "entry_table", data)
		return
	}
	s.render(w, http.StatusOK, "entries.html", data)
}

type entryForm struct {
	Action   string
	Title    string
	Active   string
	Error    string
	Project  string
	Subject  string
	Notes    string
	Date     string
	Start    string
	Duration string
	Kind     string
	Tags     string
	Projects []store.Project
}

func (s *server) formFromRequest(r *http.Request) (store.NewEntry, entryForm, error) {
	form := entryForm{
		Project:  r.FormValue("project"),
		Subject:  r.FormValue("subject"),
		Notes:    r.FormValue("notes"),
		Date:     r.FormValue("date"),
		Start:    r.FormValue("start"),
		Duration: r.FormValue("duration"),
		Kind:     r.FormValue("kind"),
		Tags:     r.FormValue("tags"),
	}
	minutes := 0
	if form.Duration != "" {
		d, err := time.ParseDuration(form.Duration)
		if err != nil || d <= 0 {
			return store.NewEntry{}, form, fmt.Errorf("invalid duration %q (e.g. 1.5h or 90m)", form.Duration)
		}
		minutes = int(d.Minutes())
	}
	return store.NewEntry{
		Project: form.Project, Subject: form.Subject, Notes: form.Notes,
		Date: form.Date, Start: form.Start, Minutes: minutes,
		Kind: store.Kind(form.Kind), Tags: form.Tags,
	}, form, nil
}

func (s *server) renderEntryForm(w http.ResponseWriter, status int, form entryForm) {
	projects, err := s.store.Projects(false)
	if err != nil {
		s.fail(w, err)
		return
	}
	form.Projects = projects
	form.Active = "entries"
	s.render(w, status, "entry_form.html", form)
}

func (s *server) entryNew(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	form := entryForm{
		Action: "/entries", Title: "New entry",
		Date: q.Get("date"), Start: q.Get("start"),
		Duration: "30m", Kind: "logged",
	}
	if form.Date == "" {
		form.Date = today()
	}
	if form.Start == "" {
		form.Start = "09:00"
	}
	s.renderEntryForm(w, http.StatusOK, form)
}

func (s *server) entryCreate(w http.ResponseWriter, r *http.Request) {
	newEntry, form, err := s.formFromRequest(r)
	form.Action, form.Title = "/entries", "New entry"
	if err == nil {
		_, err = s.store.AddEntry(newEntry)
	}
	if err != nil {
		form.Error = err.Error()
		s.renderEntryForm(w, http.StatusUnprocessableEntity, form)
		return
	}
	http.Redirect(w, r, "/entries", http.StatusSeeOther)
}

func (s *server) entryID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func (s *server) entryEdit(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	e, err := s.store.Entry(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tags := ""
	for _, t := range e.Tags {
		if tags != "" {
			tags += " "
		}
		tags += "#" + t
	}
	s.renderEntryForm(w, http.StatusOK, entryForm{
		Action: fmt.Sprintf("/entries/%d", e.ID), Title: "Edit entry",
		Project: e.ProjectPath(), Subject: e.Subject, Notes: e.Notes,
		Date: e.Date, Start: e.Start,
		Duration: fmt.Sprintf("%dm", e.Blocks*store.BlockMinutes),
		Kind:     string(e.Kind), Tags: tags,
	})
}

func (s *server) entryUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	newEntry, form, err := s.formFromRequest(r)
	form.Action, form.Title = fmt.Sprintf("/entries/%d", id), "Edit entry"
	if err == nil {
		_, err = s.store.UpdateEntry(id, newEntry)
	}
	if err != nil {
		form.Error = err.Error()
		s.renderEntryForm(w, http.StatusUnprocessableEntity, form)
		return
	}
	http.Redirect(w, r, "/entries", http.StatusSeeOther)
}

func (s *server) entryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteEntry(id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, redirectTarget(r, "/entries"), http.StatusSeeOther)
}

func (s *server) entryConfirm(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.ConfirmPlanned(id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, redirectTarget(r, "/entries"), http.StatusSeeOther)
}

// redirectTarget sends the user back to the page the action came from.
func redirectTarget(r *http.Request, fallback string) string {
	if ref := r.FormValue("back"); ref != "" {
		return ref
	}
	return fallback
}

// --- projects ---

func (s *server) projects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.Projects(true)
	if err != nil {
		s.fail(w, err)
		return
	}
	// A project with sub-projects can't itself be re-parented (would create
	// a third tier), so the template hides its parent control.
	hasChildren := map[int64]bool{}
	for _, p := range ps {
		if p.ParentID != 0 {
			hasChildren[p.ParentID] = true
		}
	}
	s.render(w, http.StatusOK, "projects.html", map[string]any{
		"Title": "Projects", "Active": "projects", "Projects": ps, "HasChildren": hasChildren,
	})
}

func (s *server) projectCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if parent := r.FormValue("parent"); parent != "" {
		name = parent + "/" + name
	}
	if _, err := s.store.CreateProject(name, r.FormValue("color")); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func (s *server) projectUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ps, err := s.store.Projects(true)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, p := range ps {
		if p.ID != id {
			continue
		}
		if name := r.FormValue("name"); name != "" {
			p.Name = name
		}
		if color := r.FormValue("color"); color != "" {
			p.Color = color
		}
		switch r.FormValue("archived") {
		case "1":
			p.Archived = true
		case "0":
			p.Archived = false
		}
		if err := s.store.UpdateProject(p); err != nil {
			s.fail(w, err)
			return
		}
		break
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func (s *server) projectDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteProject(id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func (s *server) projectReparent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.SetParent(id, r.FormValue("parent")); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

// --- timer ---

func (s *server) timerStart(w http.ResponseWriter, r *http.Request) {
	_, err := s.store.StartTimer(r.FormValue("project"), r.FormValue("subject"),
		r.FormValue("notes"), r.FormValue("tags"), time.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) timerStop(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.StopTimer(time.Now()); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- report ---

func (s *server) report(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	by := report.GroupBy(q.Get("by"))
	if by == "" {
		by = report.ByProject
	}
	f := store.Filter{
		Project: q.Get("project"), Tag: q.Get("tag"),
		From: q.Get("from"), To: q.Get("to"),
	}
	if f.From == "" && f.To == "" {
		monday := weekOf(time.Now())
		f.From = monday.Format("2006-01-02")
		f.To = monday.AddDate(0, 0, 6).Format("2006-01-02")
	}
	rep, err := report.Build(s.store, f, by)
	if err != nil {
		s.fail(w, err)
		return
	}
	if q.Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=timetrack-%s-%s-to-%s.csv", by, f.From, f.To))
		fmt.Fprint(w, rep.CSV())
		return
	}
	projects, err := s.store.Projects(false)
	if err != nil {
		s.fail(w, err)
		return
	}
	csvURL := *r.URL
	cq := csvURL.Query()
	cq.Set("format", "csv")
	cq.Set("from", f.From)
	cq.Set("to", f.To)
	csvURL.RawQuery = cq.Encode()
	s.render(w, http.StatusOK, "report.html", map[string]any{
		"Title": "Report", "Active": "report",
		"Report": rep, "Filter": f, "By": string(by),
		"Projects": projects, "CSVURL": csvURL.String(),
	})
}
