// Command tt is a personal time tracker: a CLI and a small web app over sqlite.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davison/timetrack/internal/report"
	"github.com/davison/timetrack/internal/store"
)

const usage = `usage: tt <command> [flags]

commands:
  serve     run the web UI (default :8090)
  add       add an entry
  start     start a live timer
  stop      stop the running timer
  status    show the running timer
  list      list entries with optional filters
  report    summarize hours by project, tag, or day
  projects  manage projects

Run 'tt <command> -h' for command flags.
Database: $TIMETRACK_DB or ~/.local/share/timetrack/timetrack.db (-db to override).
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	commands := map[string]func([]string) error{
		"serve":    runServe,
		"add":      runAdd,
		"start":    runStart,
		"stop":     runStop,
		"status":   runStatus,
		"list":     runList,
		"report":   runReport,
		"projects": runProjects,
	}
	run, ok := commands[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "tt: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err := run(args); err != nil {
		fmt.Fprintf(os.Stderr, "tt %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

func defaultDBPath() string {
	if p := os.Getenv("TIMETRACK_DB"); p != "" {
		return p
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, ".local", "share", "timetrack", "timetrack.db")
}

// newFlagSet returns a flag set pre-populated with the shared -db flag.
func newFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	db := fs.String("db", defaultDBPath(), "path to sqlite database")
	return fs, db
}

func openStore(path string) (*store.Store, error) {
	return store.Open(path)
}

// parseDate resolves "", "today", "yesterday", "tomorrow", or YYYY-MM-DD
// relative to now.
func parseDate(s string, now time.Time) (string, error) {
	switch s {
	case "", "today":
		return now.Format("2006-01-02"), nil
	case "yesterday":
		return now.AddDate(0, 0, -1).Format("2006-01-02"), nil
	case "tomorrow":
		return now.AddDate(0, 0, 1).Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("invalid date %q (want YYYY-MM-DD, today, yesterday, or tomorrow)", s)
	}
	return s, nil
}

// parseDurationMinutes parses durations like "1.5h", "90m", "2h30m".
func parseDurationMinutes(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("duration is required (e.g. 1.5h, 90m)")
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid duration %q (e.g. 1.5h, 90m)", s)
	}
	return int(d.Minutes()), nil
}

func runAdd(args []string) error {
	fs, db := newFlagSet("add")
	project := fs.String("p", "", "project name (required)")
	subject := fs.String("s", "", "subject of work (required)")
	notes := fs.String("n", "", "notes")
	date := fs.String("date", "", "date: YYYY-MM-DD, today (default), yesterday, tomorrow")
	at := fs.String("at", "09:00", "start time HH:MM (snapped to half hour)")
	dur := fs.String("d", "", "duration, e.g. 1.5h or 90m (required)")
	tags := fs.String("t", "", "tags, e.g. 'research,writing' or '#research #writing'")
	plan := fs.Bool("plan", false, "record as planned (future) time instead of logged work")
	fs.Parse(args)

	day, err := parseDate(*date, time.Now())
	if err != nil {
		return err
	}
	minutes, err := parseDurationMinutes(*dur)
	if err != nil {
		return err
	}
	kind := store.KindLogged
	if *plan {
		kind = store.KindPlanned
	}
	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	e, err := s.AddEntry(store.NewEntry{
		Project: *project, Subject: *subject, Notes: *notes,
		Date: day, Start: *at, Minutes: minutes, Kind: kind, Tags: *tags,
	})
	if err != nil {
		return err
	}
	fmt.Printf("added #%d: %s\n", e.ID, formatEntry(e))
	return nil
}

func runStart(args []string) error {
	fs, db := newFlagSet("start")
	project := fs.String("p", "", "project name (required)")
	subject := fs.String("s", "", "subject of work (required)")
	notes := fs.String("n", "", "notes")
	tags := fs.String("t", "", "tags")
	fs.Parse(args)

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	e, err := s.StartTimer(*project, *subject, *notes, *tags, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("timer started: %s / %s at %s\n", e.ProjectName, e.Subject, e.Start)
	return nil
}

func runStop(args []string) error {
	fs, db := newFlagSet("stop")
	fs.Parse(args)

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	e, err := s.StopTimer(time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("logged #%d: %s\n", e.ID, formatEntry(e))
	return nil
}

func runStatus(args []string) error {
	fs, db := newFlagSet("status")
	fs.Parse(args)

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	e, err := s.RunningEntry()
	if err != nil {
		return err
	}
	if e == nil {
		fmt.Println("no timer running")
		return nil
	}
	fmt.Printf("running: %s / %s since %s (%s elapsed)\n",
		e.ProjectName, e.Subject, e.StartedAt.Format("15:04"),
		time.Since(e.StartedAt).Round(time.Minute))
	return nil
}

func runList(args []string) error {
	fs, db := newFlagSet("list")
	project := fs.String("project", "", "filter by project")
	tag := fs.String("tag", "", "filter by tag")
	from := fs.String("from", "", "start date (inclusive)")
	to := fs.String("to", "", "end date (inclusive)")
	kind := fs.String("kind", "", "filter by kind: planned or logged")
	search := fs.String("search", "", "substring of subject/notes")
	fs.Parse(args)

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	entries, err := s.Entries(store.Filter{
		Project: *project, Tag: *tag, From: *from, To: *to,
		Kind: store.Kind(*kind), Search: *search,
	})
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("no entries")
		return nil
	}
	for _, e := range entries {
		fmt.Printf("#%-4d %s\n", e.ID, formatEntry(e))
	}
	return nil
}

func formatEntry(e store.Entry) string {
	line := fmt.Sprintf("%s %s %4.1fh  %-12s %s", e.Date, e.Start, e.Hours(), e.ProjectPath(), e.Subject)
	if len(e.Tags) > 0 {
		line += "  #" + strings.Join(e.Tags, " #")
	}
	if e.Kind == store.KindPlanned {
		line += "  [planned]"
	}
	return line
}

func runReport(args []string) error {
	fs, db := newFlagSet("report")
	by := fs.String("by", "project", "group by: project, tag, or day")
	week := fs.Bool("week", false, "current week (Mon-Sun)")
	month := fs.Bool("month", false, "current month")
	from := fs.String("from", "", "start date (inclusive)")
	to := fs.String("to", "", "end date (inclusive)")
	project := fs.String("project", "", "filter by project")
	tag := fs.String("tag", "", "filter by tag")
	csv := fs.Bool("csv", false, "output CSV")
	fs.Parse(args)

	f := store.Filter{From: *from, To: *to, Project: *project, Tag: *tag}
	now := time.Now()
	if *week {
		f.From, f.To = weekRange(now)
	}
	if *month {
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		f.From = first.Format("2006-01-02")
		f.To = first.AddDate(0, 1, -1).Format("2006-01-02")
	}

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	r, err := report.Build(s, f, report.GroupBy(*by))
	if err != nil {
		return err
	}
	if *csv {
		fmt.Print(r.CSV())
		return nil
	}
	if f.From != "" || f.To != "" {
		fmt.Printf("%s to %s\n", f.From, f.To)
	}
	for _, l := range r.Lines {
		key := l.Key
		if l.Sub {
			key = "  " + key
		}
		fmt.Printf("%-20s %6.1fh logged", key, l.LoggedHours)
		if l.PlannedHours > 0 {
			fmt.Printf("  %6.1fh planned", l.PlannedHours)
		}
		fmt.Println()
	}
	fmt.Printf("%-20s %6.1fh logged  %6.1fh planned\n", "total", r.TotalLogged, r.TotalPlanned)
	return nil
}

// weekRange returns the Monday and Sunday of now's week.
func weekRange(now time.Time) (string, string) {
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday belongs to the week that started 6 days ago
	}
	monday := now.AddDate(0, 0, 1-weekday)
	return monday.Format("2006-01-02"), monday.AddDate(0, 0, 6).Format("2006-01-02")
}

func runProjects(args []string) error {
	fs, db := newFlagSet("projects")
	fs.Parse(args)
	rest := fs.Args()

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()

	sub := "list"
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "list":
		ps, err := s.Projects(true)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			fmt.Println("no projects; add one with: tt projects add <name> [color]")
			return nil
		}
		for _, p := range ps {
			suffix := ""
			if p.Archived {
				suffix = "  [archived]"
			}
			name := p.Name
			if p.ParentName != "" {
				name = "  " + name
			}
			fmt.Printf("%-20s %s%s\n", name, p.Color, suffix)
		}
		return nil
	case "add":
		if len(rest) < 2 {
			return fmt.Errorf("usage: tt projects add <name or Parent/Sub> [color]")
		}
		color := ""
		if len(rest) > 2 {
			color = rest[2]
		}
		p, err := s.CreateProject(rest[1], color)
		if err != nil {
			return err
		}
		fmt.Printf("added project %s\n", p.Name)
		return nil
	case "archive":
		if len(rest) < 2 {
			return fmt.Errorf("usage: tt projects archive <name>")
		}
		p, err := s.ProjectByName(rest[1])
		if err != nil {
			return err
		}
		p.Archived = true
		if err := s.UpdateProject(p); err != nil {
			return err
		}
		fmt.Printf("archived project %s\n", p.Name)
		return nil
	case "delete":
		if len(rest) < 2 {
			return fmt.Errorf("usage: tt projects delete <name>")
		}
		p, err := s.ProjectByName(rest[1])
		if err != nil {
			return err
		}
		if err := s.DeleteProject(p.ID); err != nil {
			return err
		}
		fmt.Printf("deleted project %s\n", p.Path())
		return nil
	case "reparent":
		if len(rest) < 2 {
			return fmt.Errorf("usage: tt projects reparent <name> [parent]  (omit parent to move to top level)")
		}
		p, err := s.ProjectByName(rest[1])
		if err != nil {
			return err
		}
		parent := ""
		if len(rest) > 2 && rest[2] != "-" {
			parent = rest[2]
		}
		if err := s.SetParent(p.ID, parent); err != nil {
			return err
		}
		moved, err := s.ProjectByID(p.ID)
		if err != nil {
			return err
		}
		fmt.Printf("moved project to %s\n", moved.Path())
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q (want list, add, archive, delete, or reparent)", sub)
	}
}
