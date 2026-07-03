package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// pairA returns a store seeded with a small hierarchy and three entries.
func pairA(t *testing.T) *Store {
	t.Helper()
	s := testStore(t)
	for _, p := range [][2]string{{"DDC", "#2a78d6"}, {"DDC/CV", ""}, {"Personal", "#1baf7a"}} {
		if _, err := s.CreateProject(p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []NewEntry{
		{Project: "DDC", Subject: "admin", Date: "2026-07-01", Start: "09:00", Minutes: 60, Kind: KindLogged, Tags: "#ops"},
		{Project: "DDC/CV", Subject: "annotate", Date: "2026-07-01", Start: "10:00", Minutes: 120, Kind: KindLogged},
		{Project: "Personal", Subject: "tax", Date: "2026-07-02", Start: "09:00", Minutes: 30, Kind: KindPlanned},
	} {
		if _, err := s.AddEntry(e); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func entriesBySubject(t *testing.T, s *Store) map[string]Entry {
	t.Helper()
	es, err := s.Entries(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]Entry{}
	for _, e := range es {
		m[e.Subject] = e
	}
	return m
}

func mergeFrom(t *testing.T, dst, src *Store) MergeStats {
	t.Helper()
	snap, err := src.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := dst.MergeSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func TestMergeIntoEmptyStore(t *testing.T) {
	a := pairA(t)
	b := testStore(t)

	stats := mergeFrom(t, b, a)
	if stats.Added != 3 {
		t.Errorf("stats = %+v, want 3 added", stats)
	}
	got := entriesBySubject(t, b)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got["annotate"].ProjectPath() != "DDC/CV" {
		t.Errorf("sub-project not recreated: %+v", got["annotate"])
	}
	if got["admin"].Tags[0] != "ops" {
		t.Errorf("tags not merged: %+v", got["admin"])
	}
	p, err := b.ProjectByName("DDC")
	if err != nil || p.Color != "#2a78d6" {
		t.Errorf("project color not carried: %+v, %v", p, err)
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	a := pairA(t)
	b := testStore(t)
	mergeFrom(t, b, a)

	stats := mergeFrom(t, b, a)
	if stats.Added != 0 || stats.Updated != 0 || stats.Deleted != 0 || stats.Unchanged != 3 {
		t.Errorf("second merge should be a no-op: %+v", stats)
	}
}

func TestMergeLastWriteWins(t *testing.T) {
	a := pairA(t)
	b := testStore(t)
	mergeFrom(t, b, a)

	// Edit the same entry on B later than A's version.
	time.Sleep(2 * time.Millisecond)
	bEntry := entriesBySubject(t, b)["admin"]
	if _, err := b.UpdateEntry(bEntry.ID, NewEntry{Project: "DDC", Subject: "admin reworked", Date: "2026-07-01", Start: "09:00", Minutes: 90, Kind: KindLogged}); err != nil {
		t.Fatal(err)
	}

	// Newer B version wins on A.
	mergeFrom(t, a, b)
	if got := entriesBySubject(t, a); got["admin reworked"].Blocks != 3 {
		t.Errorf("newer edit should win on A: %+v", got)
	}
	// Stale A version does not overwrite B.
	stats := mergeFrom(t, b, a)
	if stats.Updated != 0 {
		t.Errorf("stale version must not win: %+v", stats)
	}
}

func TestMergeDeletionPropagatesAndStaysDead(t *testing.T) {
	a := pairA(t)
	b := testStore(t)
	mergeFrom(t, b, a)

	time.Sleep(2 * time.Millisecond)
	aEntry := entriesBySubject(t, a)["tax"]
	if err := a.DeleteEntry(aEntry.ID); err != nil {
		t.Fatal(err)
	}

	stats := mergeFrom(t, b, a)
	if stats.Deleted != 1 {
		t.Errorf("stats = %+v, want 1 deleted", stats)
	}
	if got := entriesBySubject(t, b); len(got) != 2 {
		t.Errorf("deletion should propagate to B: %v", got)
	}
	// Merging B (which has the tombstone) back into A must not resurrect.
	mergeFrom(t, a, b)
	if got := entriesBySubject(t, a); len(got) != 2 {
		t.Errorf("deleted entry resurrected on A: %v", got)
	}
}

func TestMergeSkipsUnknownTombstones(t *testing.T) {
	a := pairA(t)
	aEntry := entriesBySubject(t, a)["tax"]
	if err := a.DeleteEntry(aEntry.ID); err != nil {
		t.Fatal(err)
	}
	// A fresh store never saw the entry; the tombstone should not insert.
	c := testStore(t)
	mergeFrom(t, c, a)
	snap, _ := c.ExportSnapshot()
	if len(snap.Entries) != 2 {
		t.Errorf("tombstone for never-seen entry should be skipped: %+v", snap.Entries)
	}
}

func TestExportExcludesRunningTimer(t *testing.T) {
	a := pairA(t)
	if _, err := a.StartTimer("DDC", "live", "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	snap, err := a.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != 3 {
		t.Errorf("running timer must not be exported: %d entries", len(snap.Entries))
	}
}

func TestJSONRoundTrip(t *testing.T) {
	a := pairA(t)
	snap, err := a.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}

	c, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	stats, err := c.MergeSnapshot(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 3 {
		t.Errorf("round-trip import stats = %+v", stats)
	}
	if got := entriesBySubject(t, c); got["annotate"].ProjectPath() != "DDC/CV" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}
