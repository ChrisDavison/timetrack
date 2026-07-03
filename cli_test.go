package main

import (
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	now := time.Date(2026, 7, 3, 14, 0, 0, 0, time.Local)
	cases := []struct{ in, want string }{
		{"today", "2026-07-03"},
		{"yesterday", "2026-07-02"},
		{"tomorrow", "2026-07-04"},
		{"2026-06-15", "2026-06-15"},
		{"", "2026-07-03"}, // default today
	}
	for _, c := range cases {
		got, err := parseDate(c.in, now)
		if err != nil {
			t.Errorf("parseDate(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := parseDate("15/06/2026", now); err == nil {
		t.Error("want error for non-ISO date")
	}
}

func TestParseDurationMinutes(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"1.5h", 90},
		{"90m", 90},
		{"2h30m", 150},
		{"30m", 30},
	}
	for _, c := range cases {
		got, err := parseDurationMinutes(c.in)
		if err != nil {
			t.Errorf("parseDurationMinutes(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseDurationMinutes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "banana", "-30m"} {
		if _, err := parseDurationMinutes(bad); err == nil {
			t.Errorf("parseDurationMinutes(%q): want error", bad)
		}
	}
}
