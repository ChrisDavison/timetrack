package web

import (
	"net/http"
	"time"

	"github.com/davison/timetrack/internal/store"
)

// The week grid shows these hours as 30-minute rows.
const (
	gridFirstHour = 6
	gridLastHour  = 22 // exclusive
	slotsPerDay   = (gridLastHour - gridFirstHour) * 2
)

type calDay struct {
	Date    string
	Label   string // "Mon 29"
	IsToday bool
	Col     int // css grid column
}

type calSlot struct {
	Label string // "06:00"; empty for half-hour rows
	Row   int
}

type calCell struct {
	Col, Row    int
	Date, Start string
	HalfHour    bool // true for :00 rows, so the :00/:30 divider renders dotted
}

type calBlock struct {
	Entry            store.Entry
	Col              int
	RowStart, RowEnd int
	Color            string
	Planned          bool
}

// rowFor converts an HH:MM start to a grid row (row 1 is the header).
func rowFor(hhmm string) int {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 2
	}
	mins := t.Hour()*60 + t.Minute()
	row := (mins-gridFirstHour*60)/30 + 2
	return min(max(row, 2), slotsPerDay+1)
}

func (s *server) calendar(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("view") == "month" {
		s.calendarMonth(w, r)
		return
	}
	s.calendarWeek(w, r)
}

func (s *server) calendarWeek(w http.ResponseWriter, r *http.Request) {
	anchor := time.Now()
	if wk := r.URL.Query().Get("week"); wk != "" {
		if t, err := time.ParseInLocation("2006-01-02", wk, time.Local); err == nil {
			anchor = t
		}
	}
	monday := weekOf(anchor)

	var days []calDay
	for i := range 7 {
		d := monday.AddDate(0, 0, i)
		days = append(days, calDay{
			Date:    d.Format("2006-01-02"),
			Label:   d.Format("Mon 2"),
			IsToday: d.Format("2006-01-02") == today(),
			Col:     i + 2,
		})
	}

	var slots []calSlot
	for i := range slotsPerDay {
		label := ""
		if i%2 == 0 {
			label = time.Date(0, 1, 1, gridFirstHour+i/2, 0, 0, 0, time.UTC).Format("15:04")
		}
		slots = append(slots, calSlot{Label: label, Row: i + 2})
	}

	// Empty-cell links for click-to-create.
	var cells []calCell
	for _, day := range days {
		for i := range slotsPerDay {
			mins := gridFirstHour*60 + i*30
			cells = append(cells, calCell{
				Col: day.Col, Row: i + 2, Date: day.Date,
				Start:    time.Date(0, 1, 1, mins/60, mins%60, 0, 0, time.UTC).Format("15:04"),
				HalfHour: i%2 == 0,
			})
		}
	}

	entries, err := s.store.Entries(store.Filter{
		From: days[0].Date, To: days[6].Date,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	dayCol := map[string]int{}
	for _, d := range days {
		dayCol[d.Date] = d.Col
	}
	var blocks []calBlock
	for _, e := range entries {
		rowStart := rowFor(e.Start)
		rowEnd := min(rowStart+max(e.Blocks, 1), slotsPerDay+2)
		color := e.ProjectColor
		if color == "" {
			color = "#7a86c2"
		}
		blocks = append(blocks, calBlock{
			Entry: e, Col: dayCol[e.Date],
			RowStart: rowStart, RowEnd: rowEnd,
			Color: color, Planned: e.Kind == store.KindPlanned,
		})
	}

	s.render(w, http.StatusOK, "calendar_week.html", map[string]any{
		"Title": "Calendar", "Active": "calendar",
		"RangeLabel": monday.Format("2 Jan") + " – " + monday.AddDate(0, 0, 6).Format("2 Jan 2006"),
		"PrevWeek":   monday.AddDate(0, 0, -7).Format("2006-01-02"),
		"NextWeek":   monday.AddDate(0, 0, 7).Format("2006-01-02"),
		"ThisMonth":  monday.Format("2006-01"),
		"Days":       days,
		"Slots":      slots,
		"Cells":      cells,
		"Blocks":     blocks,
		"GridRows":   slotsPerDay + 1,
	})
}

type monthDay struct {
	Day       int
	Date      string
	InMonth   bool
	IsToday   bool
	Logged    float64
	Planned   float64
	Intensity int // 0-4 shading bucket by total hours
}

func (s *server) calendarMonth(w http.ResponseWriter, r *http.Request) {
	anchor := time.Now()
	if m := r.URL.Query().Get("month"); m != "" {
		if t, err := time.ParseInLocation("2006-01", m, time.Local); err == nil {
			anchor = t
		}
	}
	first := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.Local)
	last := first.AddDate(0, 1, -1)

	entries, err := s.store.Entries(store.Filter{
		From: first.Format("2006-01-02"), To: last.Format("2006-01-02"),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	logged := map[string]float64{}
	planned := map[string]float64{}
	for _, e := range entries {
		if e.Kind == store.KindPlanned {
			planned[e.Date] += e.Hours()
		} else {
			logged[e.Date] += e.Hours()
		}
	}

	var weeks [][]monthDay
	for d := weekOf(first); !d.After(last); d = d.AddDate(0, 0, 7) {
		var week []monthDay
		for i := range 7 {
			day := d.AddDate(0, 0, i)
			date := day.Format("2006-01-02")
			total := logged[date] + planned[date]
			intensity := 0
			for _, threshold := range []float64{0, 2, 4, 6} {
				if total > threshold {
					intensity++
				}
			}
			week = append(week, monthDay{
				Day: day.Day(), Date: date,
				InMonth: day.Month() == first.Month(),
				IsToday: date == today(),
				Logged:  logged[date], Planned: planned[date],
				Intensity: intensity,
			})
		}
		weeks = append(weeks, week)
	}

	s.render(w, http.StatusOK, "calendar_month.html", map[string]any{
		"Title": "Calendar", "Active": "calendar",
		"MonthLabel": first.Format("January 2006"),
		"PrevMonth":  first.AddDate(0, -1, 0).Format("2006-01"),
		"NextMonth":  first.AddDate(0, 1, 0).Format("2006-01"),
		"ThisWeek":   first.Format("2006-01-02"),
		"Weeks":      weeks,
	})
}
