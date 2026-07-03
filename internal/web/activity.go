package web

import (
	"fmt"
	"net/http"

	"github.com/davison/timetrack/internal/store"
)

// activitySummary is the display/edit view of a multi-day activity group,
// derived from its member entries (there's no separate metadata table —
// see internal/store/migrations/003_activities.sql).
type activitySummary struct {
	ID       int64
	Project  string
	Subject  string
	Notes    string
	Duration string
	Kind     string
	Tags     string
	From, To string
	Hours    float64
	Members  []store.Entry
}

func summarizeActivity(id int64, members []store.Entry) activitySummary {
	first := members[0]
	tags := ""
	for _, t := range first.Tags {
		if tags != "" {
			tags += " "
		}
		tags += "#" + t
	}
	sum := activitySummary{
		ID: id, Project: first.ProjectPath(), Subject: first.Subject, Notes: first.Notes,
		Duration: fmt.Sprintf("%dm", first.Blocks*store.BlockMinutes),
		Kind:     string(first.Kind), Tags: tags,
		From: members[0].Date, To: members[len(members)-1].Date,
		Members: members,
	}
	for _, m := range members {
		sum.Hours += m.Hours()
	}
	return sum
}

func (s *server) activityShow(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	members, err := s.store.ActivityEntries(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	projects, err := s.store.Projects(false)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "activity.html", map[string]any{
		"Title": "Activity", "Active": "entries",
		"Activity": summarizeActivity(id, members), "Projects": projects,
	})
}

func (s *server) activityUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	newEntry, form, err := s.formFromRequest(r)
	if err == nil {
		err = s.store.UpdateActivity(id, newEntry)
	}
	if err != nil {
		members, merr := s.store.ActivityEntries(id)
		if merr != nil {
			s.fail(w, merr)
			return
		}
		sum := summarizeActivity(id, members)
		sum.Project, sum.Subject, sum.Notes = form.Project, form.Subject, form.Notes
		sum.Duration, sum.Kind, sum.Tags = form.Duration, form.Kind, form.Tags
		projects, perr := s.store.Projects(false)
		if perr != nil {
			s.fail(w, perr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "activity.html", map[string]any{
			"Title": "Activity", "Active": "entries",
			"Activity": sum, "Projects": projects, "Error": err.Error(),
		})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/activities/%d", id), http.StatusSeeOther)
}

func (s *server) activityConfirm(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.ConfirmActivity(id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/activities/%d", id), http.StatusSeeOther)
}

func (s *server) activityDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.entryID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteActivity(id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/entries", http.StatusSeeOther)
}
