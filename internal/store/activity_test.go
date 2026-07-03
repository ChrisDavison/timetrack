package store

import (
	"reflect"
	"testing"
)

func TestAddActivitySingleDayIsUngrouped(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")

	entries, err := s.AddActivity(NewEntry{
		Project: "EngD", Subject: "thesis", Date: "2026-07-03", Start: "09:00",
		Minutes: 60, Kind: KindLogged,
	}, "2026-07-03", false)
	if err != nil {
		t.Fatalf("AddActivity: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].ActivityID != 0 {
		t.Errorf("single-day activity should be ungrouped, got activity_id %d", entries[0].ActivityID)
	}
}

func TestAddActivityRangeGroupsEntries(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")

	// Mon 2026-07-06 through Fri 2026-07-10: 5 weekdays.
	entries, err := s.AddActivity(NewEntry{
		Project: "EngD", Subject: "conference", Date: "2026-07-06", Start: "09:00",
		Minutes: 480, Kind: KindPlanned, Tags: "#travel",
	}, "2026-07-10", false)
	if err != nil {
		t.Fatalf("AddActivity: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("want 5 entries, got %d", len(entries))
	}
	wantDates := []string{"2026-07-06", "2026-07-07", "2026-07-08", "2026-07-09", "2026-07-10"}
	for i, e := range entries {
		if e.Date != wantDates[i] {
			t.Errorf("entry %d: date = %s, want %s", i, e.Date, wantDates[i])
		}
		if e.ActivityID == 0 {
			t.Errorf("entry %d: expected non-zero activity id", i)
		}
		if e.ActivityID != entries[0].ActivityID {
			t.Errorf("entry %d: activity id %d differs from group %d", i, e.ActivityID, entries[0].ActivityID)
		}
		if !reflect.DeepEqual(e.Tags, []string{"travel"}) {
			t.Errorf("entry %d: tags = %v", i, e.Tags)
		}
		if e.Kind != KindPlanned {
			t.Errorf("entry %d: kind = %s", i, e.Kind)
		}
	}
}

func TestAddActivityWeekdaysOnlySkipsWeekend(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")

	// Wed 2026-07-01 through Fri 2026-07-10 spans a full weekend (04-05).
	entries, err := s.AddActivity(NewEntry{
		Project: "EngD", Subject: "leave", Date: "2026-07-01", Start: "09:00",
		Minutes: 480, Kind: KindPlanned,
	}, "2026-07-10", true)
	if err != nil {
		t.Fatalf("AddActivity: %v", err)
	}
	for _, e := range entries {
		if e.Date == "2026-07-04" || e.Date == "2026-07-05" {
			t.Errorf("weekdays-only should skip weekend day %s", e.Date)
		}
	}
	if len(entries) != 8 { // 10 calendar days minus 2 weekend days
		t.Fatalf("want 8 entries, got %d", len(entries))
	}
}

func TestAddActivityValidation(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	base := NewEntry{Project: "EngD", Subject: "x", Date: "2026-07-06", Start: "09:00", Minutes: 60, Kind: KindLogged}

	if _, err := s.AddActivity(base, "2026-07-05", false); err == nil {
		t.Error("end before start: want error, got nil")
	}
	if _, err := s.AddActivity(base, "not-a-date", false); err == nil {
		t.Error("invalid end date: want error, got nil")
	}
	if _, err := s.AddActivity(base, "2028-07-06", false); err == nil {
		t.Error("range over the day cap: want error, got nil")
	}
	// A weekend-only range with weekdays-only should error rather than
	// silently creating zero entries.
	weekendOnly := base
	weekendOnly.Date = "2026-07-04" // Saturday
	if _, err := s.AddActivity(weekendOnly, "2026-07-05", true); err == nil {
		t.Error("all-weekend range with weekdays-only: want error, got nil")
	}
}

func TestActivityManagement(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	mustProject(t, s, "Personal")

	entries, err := s.AddActivity(NewEntry{
		Project: "EngD", Subject: "conference", Date: "2026-07-06", Start: "09:00",
		Minutes: 480, Kind: KindPlanned, Tags: "#travel",
	}, "2026-07-08", false)
	if err != nil {
		t.Fatalf("AddActivity: %v", err)
	}
	activityID := entries[0].ActivityID

	members, err := s.ActivityEntries(activityID)
	if err != nil {
		t.Fatalf("ActivityEntries: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("want 3 members, got %d", len(members))
	}

	// UpdateActivity changes shared fields but keeps each member's own date.
	if err := s.UpdateActivity(activityID, NewEntry{
		Project: "Personal", Subject: "offsite", Notes: "n", Minutes: 240, Kind: KindPlanned, Tags: "#fun",
	}); err != nil {
		t.Fatalf("UpdateActivity: %v", err)
	}
	updated, err := s.ActivityEntries(activityID)
	if err != nil {
		t.Fatalf("ActivityEntries after update: %v", err)
	}
	wantDates := map[string]bool{"2026-07-06": true, "2026-07-07": true, "2026-07-08": true}
	for _, e := range updated {
		if e.ProjectName != "Personal" || e.Subject != "offsite" || e.Blocks != 8 {
			t.Errorf("update not applied to member %d: %+v", e.ID, e)
		}
		if !wantDates[e.Date] {
			t.Errorf("member date changed unexpectedly: %s", e.Date)
		}
	}

	// ConfirmActivity flips planned members to logged.
	if err := s.ConfirmActivity(activityID); err != nil {
		t.Fatalf("ConfirmActivity: %v", err)
	}
	confirmed, err := s.ActivityEntries(activityID)
	if err != nil {
		t.Fatalf("ActivityEntries after confirm: %v", err)
	}
	for _, e := range confirmed {
		if e.Kind != KindLogged {
			t.Errorf("member %d: kind = %s, want logged", e.ID, e.Kind)
		}
	}

	// DeleteActivity removes every member and the group itself.
	if err := s.DeleteActivity(activityID); err != nil {
		t.Fatalf("DeleteActivity: %v", err)
	}
	if _, err := s.ActivityEntries(activityID); err == nil {
		t.Error("ActivityEntries after delete: want error, got nil")
	}
	all, err := s.Entries(Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("want no entries left, got %d", len(all))
	}
}
