package web

import (
	"fmt"
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

func TestDashboardShowsFoldedSubProjectBars(t *testing.T) {
	s, h := testServer(t)
	makeSub(t, s, "EngD/Thesis")
	addEntry(t, s, "EngD", "root work", today(), 60, store.KindLogged, "")
	addEntry(t, s, "EngD/Thesis", "chapter 3", today(), 60, store.KindLogged, "")

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, `class="disclosure"`) {
		t.Errorf("dashboard should render a disclosure toggle for the EngD rollup bar: %s", body)
	}
	if !strings.Contains(body, `class="bar-row subrow" data-group="EngD" hidden`) {
		t.Errorf("sub-project bars should be collapsed by default: %s", body)
	}
	if !strings.Contains(body, "(direct)") {
		t.Errorf("dashboard should break out the parent's own time as (direct): %s", body)
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

func TestProjectsPageHasRenameForm(t *testing.T) {
	_, h := testServer(t)
	body := get(t, h, "/projects").Body.String()
	if !strings.Contains(body, `name="name" value="EngD"`) {
		t.Errorf("projects page should render an editable name input per project: %s", body)
	}
}

func TestRenameProjectViaForm(t *testing.T) {
	s, h := testServer(t)
	engD, err := s.ProjectByName("EngD")
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, h, fmt.Sprintf("/projects/%d", engD.ID), url.Values{"name": {"EngD Thesis"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rename = %d", rec.Code)
	}
	if _, err := s.ProjectByName("EngD Thesis"); err != nil {
		t.Errorf("project not renamed: %v", err)
	}
}

func makeSub(t *testing.T, s *store.Store, path string) {
	t.Helper()
	if _, err := s.CreateProject(path, ""); err != nil {
		t.Fatal(err)
	}
}

func TestEntryFormListsSubProjectPaths(t *testing.T) {
	s, h := testServer(t)
	makeSub(t, s, "EngD/Thesis")

	body := get(t, h, "/entries/new").Body.String()
	if !strings.Contains(body, `value="EngD/Thesis"`) {
		t.Errorf("entry form select should offer sub-project path: %s", body)
	}
}

func TestEntriesFilterByParentIncludesChildren(t *testing.T) {
	s, h := testServer(t)
	makeSub(t, s, "EngD/Thesis")
	addEntry(t, s, "EngD", "general admin", "2026-07-01", 30, store.KindLogged, "")
	addEntry(t, s, "EngD/Thesis", "chapter 3", "2026-07-01", 60, store.KindLogged, "")

	body := get(t, h, "/entries?project=EngD").Body.String()
	if !strings.Contains(body, "general admin") || !strings.Contains(body, "chapter 3") {
		t.Errorf("parent filter should include sub-project entries")
	}
	if !strings.Contains(body, "EngD/Thesis") {
		t.Errorf("entries table should display full project path")
	}
}

func TestNewEntryFormShowsSpanControls(t *testing.T) {
	_, h := testServer(t)
	body := get(t, h, "/entries/new").Body.String()
	if !strings.Contains(body, `name="end"`) || !strings.Contains(body, `name="weekdays_only"`) {
		t.Errorf("new entry form should offer a repeat-until end date and weekdays-only toggle: %s", body)
	}
}

func TestEditEntryFormHidesSpanControls(t *testing.T) {
	s, h := testServer(t)
	e := addEntry(t, s, "EngD", "draft", "2026-07-01", 30, store.KindLogged, "")
	body := get(t, h, fmt.Sprintf("/entries/%d/edit", e.ID)).Body.String()
	if strings.Contains(body, `name="end"`) {
		t.Errorf("edit entry form should not offer span controls: %s", body)
	}
}

func TestCreateActivityViaFormGroupsEntries(t *testing.T) {
	s, h := testServer(t)
	// Mon 2026-07-06 through Fri 2026-07-10.
	rec := postForm(t, h, "/entries", url.Values{
		"project": {"EngD"}, "subject": {"conference"}, "date": {"2026-07-06"},
		"start": {"09:00"}, "duration": {"8h"}, "kind": {"planned"}, "tags": {"#travel"},
		"end": {"2026-07-10"}, "weekdays_only": {"1"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /entries (activity) = %d: %s", rec.Code, rec.Body.String())
	}
	entries, _ := s.Entries(store.Filter{})
	if len(entries) != 5 {
		t.Fatalf("want 5 grouped entries, got %d", len(entries))
	}
	activityID := entries[0].ActivityID
	if activityID == 0 {
		t.Fatal("entries should share a non-zero activity id")
	}
	for _, e := range entries {
		if e.ActivityID != activityID {
			t.Errorf("entry %d: activity id %d, want %d", e.ID, e.ActivityID, activityID)
		}
	}

	body := get(t, h, "/entries").Body.String()
	if !strings.Contains(body, fmt.Sprintf("/activities/%d", activityID)) {
		t.Errorf("entries list should link grouped rows to their activity: %s", body)
	}
}

func TestActivityPageShowsAndUpdatesGroup(t *testing.T) {
	s, h := testServer(t)
	entries, err := s.AddActivity(store.NewEntry{
		Project: "EngD", Subject: "conference", Date: "2026-07-06", Start: "09:00",
		Minutes: 480, Kind: store.KindPlanned, Tags: "#travel",
	}, "2026-07-08", false)
	if err != nil {
		t.Fatal(err)
	}
	id := entries[0].ActivityID

	body := get(t, h, fmt.Sprintf("/activities/%d", id)).Body.String()
	if !strings.Contains(body, "conference") || !strings.Contains(body, "2026-07-06") {
		t.Errorf("activity page should show subject and dates: %s", body)
	}

	rec := postForm(t, h, fmt.Sprintf("/activities/%d", id), url.Values{
		"project": {"EngD"}, "subject": {"offsite"}, "duration": {"4h"}, "kind": {"planned"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /activities/%d = %d: %s", id, rec.Code, rec.Body.String())
	}
	members, err := s.ActivityEntries(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.Subject != "offsite" || m.Blocks != 8 {
			t.Errorf("member not updated: %+v", m)
		}
	}

	if rec := postForm(t, h, fmt.Sprintf("/activities/%d/confirm", id), nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("confirm = %d", rec.Code)
	}
	members, _ = s.ActivityEntries(id)
	for _, m := range members {
		if m.Kind != store.KindLogged {
			t.Errorf("member %d not confirmed: %s", m.ID, m.Kind)
		}
	}

	if rec := postForm(t, h, fmt.Sprintf("/activities/%d/delete", id), nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d", rec.Code)
	}
	if remaining, _ := s.Entries(store.Filter{}); len(remaining) != 0 {
		t.Errorf("want no entries left after activity delete, got %d", len(remaining))
	}
}

func TestCreateSubProjectViaForm(t *testing.T) {
	s, h := testServer(t)
	rec := postForm(t, h, "/projects", url.Values{
		"name": {"Thesis"}, "parent": {"EngD"}, "color": {""},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create sub-project = %d", rec.Code)
	}
	p, err := s.ProjectByName("EngD/Thesis")
	if err != nil {
		t.Fatalf("sub-project not created: %v", err)
	}
	if p.Color != "#4a90d9" {
		t.Errorf("sub-project should inherit parent color, got %q", p.Color)
	}
}

func TestReportShowsPercentAndFoldedSubProjects(t *testing.T) {
	s, h := testServer(t)
	makeSub(t, s, "EngD/Thesis")
	addEntry(t, s, "EngD", "root work", "2026-07-01", 60, store.KindLogged, "")
	addEntry(t, s, "EngD/Thesis", "chapter 3", "2026-07-01", 60, store.KindLogged, "")
	addEntry(t, s, "Personal", "tax return", "2026-07-01", 120, store.KindLogged, "")

	body := get(t, h, "/report?by=project&from=2026-07-01&to=2026-07-01").Body.String()
	if !strings.Contains(body, `class="disclosure"`) {
		t.Errorf("report should render a disclosure toggle for the EngD rollup: %s", body)
	}
	if !strings.Contains(body, `class="subrow" data-group="EngD" hidden`) {
		t.Errorf("sub-project rows should be collapsed by default: %s", body)
	}
	// EngD (2h of 4h total) should show a 50% share.
	if !strings.Contains(body, "50.0%") {
		t.Errorf("report should show EngD's percentage of total logged hours: %s", body)
	}
}

func TestProjectsPageHasParentSelect(t *testing.T) {
	_, h := testServer(t)
	body := get(t, h, "/projects").Body.String()
	if !strings.Contains(body, `name="parent"`) {
		t.Errorf("projects page add form should have a parent select")
	}
}

func TestDeleteEmptyProjectViaForm(t *testing.T) {
	s, h := testServer(t)
	personal, err := s.ProjectByName("Personal")
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, h, fmt.Sprintf("/projects/%d/delete", personal.ID), nil) // Personal, no entries
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete empty project = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := s.ProjectByName("Personal"); err == nil {
		t.Error("project should be deleted")
	}
}

func TestDeleteProjectWithEntriesFails(t *testing.T) {
	s, h := testServer(t)
	addEntry(t, s, "EngD", "work", "2026-07-01", 60, store.KindLogged, "")
	engD, err := s.ProjectByName("EngD")
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, h, fmt.Sprintf("/projects/%d/delete", engD.ID), nil) // EngD, has an entry
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete non-empty project = %d, want error page", rec.Code)
	}
	if _, err := s.ProjectByName("EngD"); err != nil {
		t.Error("project with entries should not be deleted")
	}
}

func TestHolidayProjectCannotBeDeletedOrArchivedViaForm(t *testing.T) {
	s, h := testServer(t)
	holiday, err := s.ProjectByName("Holiday")
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, h, fmt.Sprintf("/projects/%d/delete", holiday.ID), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete Holiday = %d, want error page", rec.Code)
	}
	if _, err := s.ProjectByName("Holiday"); err != nil {
		t.Error("Holiday project should still exist")
	}

	rec = postForm(t, h, fmt.Sprintf("/projects/%d", holiday.ID), url.Values{"archived": {"1"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("archive Holiday = %d, want error page", rec.Code)
	}
	holiday, err = s.ProjectByName("Holiday")
	if err != nil || holiday.Archived {
		t.Errorf("Holiday project should not be archived: %+v, %v", holiday, err)
	}
}

func TestReparentProjectViaForm(t *testing.T) {
	s, h := testServer(t)
	personal, err := s.ProjectByName("Personal")
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, h, fmt.Sprintf("/projects/%d/parent", personal.ID), url.Values{"parent": {"EngD"}}) // Personal -> EngD
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reparent = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := s.ProjectByName("EngD/Personal"); err != nil {
		t.Fatalf("Personal should now resolve under EngD: %v", err)
	}

	body := get(t, h, "/projects").Body.String()
	if !strings.Contains(body, `<option selected>EngD</option>`) {
		t.Errorf("projects page should show Personal's parent select on EngD: %s", body)
	}

	// Unassign back to top level.
	rec = postForm(t, h, fmt.Sprintf("/projects/%d/parent", personal.ID), url.Values{"parent": {""}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unassign = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := s.ProjectByName("Personal"); err != nil {
		t.Errorf("Personal should resolve top-level again: %v", err)
	}
}

func TestProjectsPageHidesParentSelectForProjectWithChildren(t *testing.T) {
	s, h := testServer(t)
	makeSub(t, s, "EngD/Thesis")
	body := get(t, h, "/projects").Body.String()
	if !strings.Contains(body, "has sub-projects") {
		t.Errorf("projects page should hide the parent select for EngD, which has a sub-project: %s", body)
	}
}
