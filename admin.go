package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"website/pages"
	"website/store"
)

const amberEmail = "boonea@cadott.k12.wi.us"

type admin struct {
	store    *store.Store
	loc      *time.Location
	advisors map[string]string
}

func (ad *admin) routes(mux *http.ServeMux, a *auth) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, a.require(fn)) }
	h("GET /admin", ad.home)
	h("GET /admin/meetings/new", ad.newMeeting)
	h("POST /admin/meetings", ad.createMeeting)
	h("GET /admin/meetings/{id}", ad.editMeeting)
	h("POST /admin/meetings/{id}", ad.updateMeeting)
	h("POST /admin/meetings/{id}/delete", ad.deleteMeeting)
	h("GET /admin/meetings/{id}/email", ad.email)
	h("GET /admin/students", ad.students)
	h("POST /admin/students", ad.addStudent)
	h("GET /admin/students/{id}/row", ad.viewStudent)
	h("GET /admin/students/{id}/edit", ad.editStudent)
	h("POST /admin/students/{id}", ad.updateStudent)
	h("GET /admin/reports", ad.reports)
	h("POST /admin/students/{id}/classroom", ad.markClassroom)
	h("GET /admin/leaderboard", ad.leaderboard)
}

func (ad *admin) name(ctx context.Context) string {
	email := currentUser(ctx)
	if n := ad.advisors[email]; n != "" {
		return n
	}
	return email
}

func (ad *admin) fail(w http.ResponseWriter, err error) {
	log.Print(err)
	http.Error(w, "something went wrong: "+err.Error(), http.StatusInternalServerError)
}

func (ad *admin) dashboard(ctx context.Context) (pages.Dashboard, error) {
	ms, err := ad.store.Meetings(ctx)
	if err != nil {
		return pages.Dashboard{}, err
	}
	sts, err := ad.store.Students(ctx, true)
	if err != nil {
		return pages.Dashboard{}, err
	}
	pairs, err := ad.store.Pairs(ctx)
	if err != nil {
		return pages.Dashboard{}, err
	}
	return pages.BuildDashboard(sts, ms, pairs), nil
}

func (ad *admin) leaderboard(w http.ResponseWriter, r *http.Request) {
	d, err := ad.dashboard(r.Context())
	if err != nil {
		ad.fail(w, err)
		return
	}
	render(w, r, pages.Leaderboard(ad.name(r.Context()), d))
}

func (ad *admin) home(w http.ResponseWriter, r *http.Request) {
	d, err := ad.dashboard(r.Context())
	if err != nil {
		ad.fail(w, err)
		return
	}
	need, err := ad.store.NeedClassroom(r.Context())
	if err != nil {
		ad.fail(w, err)
		return
	}
	render(w, r, pages.AdminHome(ad.name(r.Context()), d, need))
}

func defaultTimes(day time.Time) (string, string) {
	if day.Weekday() == time.Tuesday {
		return "15:00", "17:30"
	}
	return "15:30", "17:30"
}

func (ad *admin) newMeeting(w http.ResponseWriter, r *http.Request) {
	today := time.Now().In(ad.loc)
	start, end := defaultTimes(today)
	m := store.Meeting{Date: today.Format(time.DateOnly), StartTime: start, EndTime: end, TakenBy: ad.name(r.Context())}
	if id, err := ad.store.MeetingByDate(r.Context(), m.Date); err == nil {
		http.Redirect(w, r, fmt.Sprintf("/admin/meetings/%d", id), http.StatusSeeOther)
		return
	}
	ad.meetingForm(w, r, m, nil, "")
}

func (ad *admin) meetingForm(w http.ResponseWriter, r *http.Request, m store.Meeting, present []int64, msg string) {
	sts, err := ad.store.Students(r.Context(), true)
	if err != nil {
		ad.fail(w, err)
		return
	}
	set := map[int64]bool{}
	for _, id := range present {
		set[id] = true
	}
	render(w, r, pages.MeetingForm(ad.name(r.Context()), m, sts, set, msg))
}

func meetingFromForm(r *http.Request) (store.Meeting, []int64, []newStudent, error) {
	if err := r.ParseForm(); err != nil {
		return store.Meeting{}, nil, nil, err
	}
	m := store.Meeting{
		Date:      r.FormValue("date"),
		StartTime: r.FormValue("start"),
		EndTime:   r.FormValue("end"),
		TakenBy:   strings.TrimSpace(r.FormValue("taken_by")),
		Notes:     strings.TrimSpace(r.FormValue("notes")),
	}
	if _, err := time.Parse(time.DateOnly, m.Date); err != nil {
		return m, nil, nil, errors.New("pick a date")
	}
	var present []int64
	for _, v := range r.Form["present"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			present = append(present, id)
		}
	}
	return m, present, newStudents(r.Form), nil
}

type newStudent struct {
	first, last string
	grade       int
}

func newStudents(form map[string][]string) []newStudent {
	firsts, lasts, grades := form["new_first"], form["new_last"], form["new_grade"]
	var out []newStudent
	for i := range firsts {
		ns := newStudent{first: strings.TrimSpace(firsts[i])}
		if i < len(lasts) {
			ns.last = strings.TrimSpace(lasts[i])
		}
		if i < len(grades) {
			ns.grade, _ = strconv.Atoi(grades[i])
		}
		if ns.first != "" || ns.last != "" {
			out = append(out, ns)
		}
	}
	return out
}

func parseStudentLine(line string) (first, last string, grade int, err error) {
	name, g, _ := strings.Cut(line, ",")
	g = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(g)), "grade"))
	if g != "" {
		grade, _ = strconv.Atoi(strings.TrimSpace(g))
	}
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return "", "", 0, fmt.Errorf("%q needs a first and last name", line)
	}
	return fields[0], strings.Join(fields[1:], " "), grade, nil
}

func (ad *admin) saveMeeting(w http.ResponseWriter, r *http.Request, id int64) {
	m, present, newKids, err := meetingFromForm(r)
	m.ID = id
	for _, ns := range newKids {
		if err == nil && (ns.first == "" || ns.last == "") {
			err = fmt.Errorf("new student %q needs a first and last name", strings.TrimSpace(ns.first+" "+ns.last))
		}
	}
	if err == nil {
		for _, ns := range newKids {
			sid, uerr := ad.store.UpsertStudent(r.Context(), ns.first, ns.last, ns.grade)
			if uerr != nil {
				err = uerr
				break
			}
			present = append(present, sid)
		}
	}
	if err == nil {
		id, err = ad.store.SaveMeeting(r.Context(), m, present)
	}
	if err != nil {
		ad.meetingForm(w, r, m, present, err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/meetings/%d/email", id), http.StatusSeeOther)
}

func (ad *admin) createMeeting(w http.ResponseWriter, r *http.Request) { ad.saveMeeting(w, r, 0) }

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (ad *admin) updateMeeting(w http.ResponseWriter, r *http.Request) {
	ad.saveMeeting(w, r, pathID(r))
}

func (ad *admin) editMeeting(w http.ResponseWriter, r *http.Request) {
	m, present, err := ad.store.Meeting(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		ad.fail(w, err)
		return
	}
	ad.meetingForm(w, r, m, present, "")
}

func (ad *admin) deleteMeeting(w http.ResponseWriter, r *http.Request) {
	if err := ad.store.DeleteMeeting(r.Context(), pathID(r)); err != nil {
		ad.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func clock(hhmm string) string {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return hhmm
	}
	return t.Format("3:04")
}

func buildEmail(m store.Meeting, present []store.Student, signer string) pages.Email {
	day := m.Day().Format("Monday, January 2, 2006")
	var b strings.Builder
	fmt.Fprintf(&b, "Programming Club attendance for %s, %s to %s PM.\n\n", day, clock(m.StartTime), clock(m.EndTime))
	fmt.Fprintf(&b, "Present (%d):\n", len(present))
	for _, s := range present {
		if s.Grade > 0 {
			fmt.Fprintf(&b, "%s, grade %d\n", s.Name(), s.Grade)
		} else {
			fmt.Fprintf(&b, "%s\n", s.Name())
		}
	}
	if signer != "" {
		fmt.Fprintf(&b, "\n%s\n", signer)
	}
	return pages.Email{To: amberEmail, Subject: "Programming Club attendance - " + day, Body: b.String()}
}

func (ad *admin) email(w http.ResponseWriter, r *http.Request) {
	m, _, err := ad.store.Meeting(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		ad.fail(w, err)
		return
	}
	present, err := ad.store.PresentStudents(r.Context(), m.ID)
	if err != nil {
		ad.fail(w, err)
		return
	}
	signer := m.TakenBy
	if signer == "" {
		signer = ad.name(r.Context())
	}
	need, err := ad.store.NeedClassroom(r.Context())
	if err != nil {
		ad.fail(w, err)
		return
	}
	render(w, r, pages.EmailPage(ad.name(r.Context()), m, buildEmail(m, present, signer), need, r.URL.Path))
}

func (ad *admin) students(w http.ResponseWriter, r *http.Request) {
	sts, err := ad.store.Students(r.Context(), false)
	if err != nil {
		ad.fail(w, err)
		return
	}
	sort.SliceStable(sts, func(i, j int) bool {
		if sts[i].Active != sts[j].Active {
			return sts[i].Active
		}
		gi, gj := sts[i].Grade, sts[j].Grade
		if gi == 0 {
			gi = 99
		}
		if gj == 0 {
			gj = 99
		}
		if gi != gj {
			return gi < gj
		}
		return sts[i].LastName+sts[i].FirstName < sts[j].LastName+sts[j].FirstName
	})
	render(w, r, pages.StudentsAdmin(ad.name(r.Context()), sts))
}

func (ad *admin) addStudent(w http.ResponseWriter, r *http.Request) {
	first, last := strings.TrimSpace(r.FormValue("first")), strings.TrimSpace(r.FormValue("last"))
	if first == "" || last == "" {
		http.Error(w, "first and last name required", http.StatusUnprocessableEntity)
		return
	}
	grade, _ := strconv.Atoi(r.FormValue("grade"))
	id, err := ad.store.UpsertStudent(r.Context(), first, last, grade)
	if err != nil {
		ad.fail(w, err)
		return
	}
	ad.studentRow(w, r, id, false)
}

func (ad *admin) studentRow(w http.ResponseWriter, r *http.Request, id int64, edit bool) {
	sts, err := ad.store.Students(r.Context(), false)
	if err != nil {
		ad.fail(w, err)
		return
	}
	for _, s := range sts {
		if s.ID == id {
			if edit {
				render(w, r, pages.StudentEditRow(s))
				return
			}
			render(w, r, pages.StudentRow(s))
			active := 0
			for _, x := range sts {
				if x.Active {
					active++
				}
			}
			render(w, r, pages.RosterCount(active, true))
			return
		}
	}
	http.NotFound(w, r)
}

func (ad *admin) viewStudent(w http.ResponseWriter, r *http.Request) {
	ad.studentRow(w, r, pathID(r), false)
}
func (ad *admin) editStudent(w http.ResponseWriter, r *http.Request) {
	ad.studentRow(w, r, pathID(r), true)
}

func (ad *admin) updateStudent(w http.ResponseWriter, r *http.Request) {
	grade, _ := strconv.Atoi(r.FormValue("grade"))
	st := store.Student{ID: pathID(r), FirstName: strings.TrimSpace(r.FormValue("first")), LastName: strings.TrimSpace(r.FormValue("last")), Grade: grade, Active: r.FormValue("active") == "on", InClassroom: r.FormValue("classroom") == "on"}
	if st.FirstName == "" || st.LastName == "" {
		http.Error(w, "first and last name required", http.StatusUnprocessableEntity)
		return
	}
	if err := ad.store.UpdateStudent(r.Context(), st); err != nil {
		ad.fail(w, err)
		return
	}
	ad.studentRow(w, r, st.ID, false)
}

func (ad *admin) markClassroom(w http.ResponseWriter, r *http.Request) {
	if err := ad.store.SetInClassroom(r.Context(), pathID(r), true); err != nil {
		ad.fail(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/admin") {
		back = "/admin"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (ad *admin) reports(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// importData reads CSV rows from in:
//
//	student,First,Last,Grade
//	meeting,YYYY-MM-DD,HH:MM,HH:MM,Taken By
//	present,YYYY-MM-DD,First,Last
func importData(ctx context.Context, st *store.Store, in io.Reader) error {
	rd := csv.NewReader(bufio.NewReader(in))
	rd.FieldsPerRecord = -1
	type pending struct {
		m       store.Meeting
		present []int64
	}
	meetings := map[string]*pending{}
	var order []string
	ids := map[string]int64{}
	sts, err := st.Students(ctx, false)
	if err != nil {
		return err
	}
	for _, s := range sts {
		ids[strings.ToLower(s.Name())] = s.ID
	}
	for {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch rec[0] {
		case "student":
			grade, _ := strconv.Atoi(rec[3])
			id, err := st.UpsertStudent(ctx, rec[1], rec[2], grade)
			if err != nil {
				return err
			}
			ids[strings.ToLower(rec[1]+" "+rec[2])] = id
		case "meeting":
			meetings[rec[1]] = &pending{m: store.Meeting{Date: rec[1], StartTime: rec[2], EndTime: rec[3], TakenBy: rec[4]}}
			order = append(order, rec[1])
		case "present":
			p := meetings[rec[1]]
			id, ok := ids[strings.ToLower(rec[2]+" "+rec[3])]
			if p == nil || !ok {
				return fmt.Errorf("present row for unknown meeting or student: %v", rec)
			}
			p.present = append(p.present, id)
		}
	}
	for _, date := range order {
		p := meetings[date]
		if existing, err := st.MeetingByDate(ctx, date); err == nil {
			p.m.ID = existing
		}
		if _, err := st.SaveMeeting(ctx, p.m, p.present); err != nil {
			return fmt.Errorf("%s: %w", date, err)
		}
		log.Printf("imported %s with %d present", date, len(p.present))
	}
	return nil
}
