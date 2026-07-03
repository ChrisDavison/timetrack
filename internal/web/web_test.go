package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davison/timetrack/internal/store"
)

func testServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.CreateProject("EngD", "#4a90d9"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("Personal", "#6aa84f"); err != nil {
		t.Fatal(err)
	}
	return s, NewServer(s, 8)
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func postForm(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func addEntry(t *testing.T, s *store.Store, project, subject, date string, minutes int, kind store.Kind, tags string) store.Entry {
	t.Helper()
	e, err := s.AddEntry(store.NewEntry{
		Project: project, Subject: subject, Date: date, Start: "09:00",
		Minutes: minutes, Kind: kind, Tags: tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDashboardRenders(t *testing.T) {
	s, h := testServer(t)
	addEntry(t, s, "EngD", "thesis", today(), 90, store.KindLogged, "")

	rec := get(t, h, "/")
	if rec.Code != 200 {
		t.Fatalf("GET / = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "1.5") {
		t.Errorf("dashboard should show 1.5 logged hours today, got: %s", body)
	}
	if !strings.Contains(body, "EngD") {
		t.Errorf("dashboard should list project EngD")
	}
}

func TestEntriesListFilters(t *testing.T) {
	s, h := testServer(t)
	addEntry(t, s, "EngD", "thesis writing", "2026-07-01", 60, store.KindLogged, "#research")
	addEntry(t, s, "Personal", "tax return", "2026-07-02", 30, store.KindLogged, "")

	body := get(t, h, "/entries?project=EngD").Body.String()
	if !strings.Contains(body, "thesis writing") || strings.Contains(body, "tax return") {
		t.Errorf("project filter not applied: %s", body)
	}

	body = get(t, h, "/entries?tag=research&from=2026-07-01&to=2026-07-03").Body.String()
	if !strings.Contains(body, "thesis writing") || strings.Contains(body, "tax return") {
		t.Errorf("combined filter not applied")
	}
}

func TestCreateEntryViaForm(t *testing.T) {
	s, h := testServer(t)
	rec := postForm(t, h, "/entries", url.Values{
		"project": {"EngD"}, "subject": {"lab work"}, "date": {"2026-07-03"},
		"start": {"10:00"}, "duration": {"2h"}, "kind": {"logged"}, "tags": {"#lab"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /entries = %d: %s", rec.Code, rec.Body.String())
	}
	entries, _ := s.Entries(store.Filter{})
	if len(entries) != 1 || entries[0].Subject != "lab work" || entries[0].Blocks != 4 {
		t.Errorf("entry not created: %+v", entries)
	}
}

func TestCreateEntryValidationErrorReturns422(t *testing.T) {
	_, h := testServer(t)
	rec := postForm(t, h, "/entries", url.Values{
		"project": {"EngD"}, "subject": {""}, "date": {"2026-07-03"},
		"start": {"10:00"}, "duration": {"1h"}, "kind": {"logged"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 for empty subject, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "subject") {
		t.Errorf("error page should mention the problem")
	}
}

func TestEditAndDeleteEntry(t *testing.T) {
	s, h := testServer(t)
	e := addEntry(t, s, "EngD", "draft", "2026-07-01", 30, store.KindLogged, "")

	rec := postForm(t, h, "/entries/1", url.Values{
		"project": {"Personal"}, "subject": {"redraft"}, "date": {"2026-07-01"},
		"start": {"09:00"}, "duration": {"1h"}, "kind": {"logged"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /entries/1 = %d", rec.Code)
	}
	got, _ := s.Entry(e.ID)
	if got.Subject != "redraft" || got.ProjectName != "Personal" {
		t.Errorf("entry not updated: %+v", got)
	}

	if rec := postForm(t, h, "/entries/1/delete", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d", rec.Code)
	}
	if entries, _ := s.Entries(store.Filter{}); len(entries) != 0 {
		t.Error("entry not deleted")
	}
}

func TestCalendarWeekShowsEntry(t *testing.T) {
	s, h := testServer(t)
	addEntry(t, s, "EngD", "calendar item", "2026-07-01", 60, store.KindLogged, "")

	body := get(t, h, "/calendar?week=2026-06-29").Body.String()
	if !strings.Contains(body, "calendar item") {
		t.Errorf("week calendar missing entry: %s", body)
	}
}

func TestTimerStartStopViaWeb(t *testing.T) {
	s, h := testServer(t)
	rec := postForm(t, h, "/timer/start", url.Values{
		"project": {"EngD"}, "subject": {"live work"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("timer start = %d: %s", rec.Code, rec.Body.String())
	}
	if r, _ := s.RunningEntry(); r == nil {
		t.Fatal("timer not running after POST /timer/start")
	}
	if rec := postForm(t, h, "/timer/stop", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("timer stop = %d", rec.Code)
	}
	if r, _ := s.RunningEntry(); r != nil {
		t.Fatal("timer still running after POST /timer/stop")
	}
}

func TestReportCSVDownload(t *testing.T) {
	s, h := testServer(t)
	addEntry(t, s, "EngD", "x", "2026-07-01", 60, store.KindLogged, "")

	rec := get(t, h, "/report?by=project&from=2026-07-01&to=2026-07-07&format=csv")
	if rec.Code != 200 {
		t.Fatalf("csv report = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "EngD,1.0,0.0") {
		t.Errorf("csv body = %q", rec.Body.String())
	}
}
