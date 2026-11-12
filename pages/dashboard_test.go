package pages

import (
	"testing"

	"website/store"
)

func TestBuildDashboard(t *testing.T) {
	sts := []store.Student{{ID: 1, FirstName: "A", LastName: "A", Grade: 6}, {ID: 2, FirstName: "B", LastName: "B", Grade: 6}, {ID: 3, FirstName: "C", LastName: "C"}}
	ms := []store.Meeting{{ID: 30, Date: "2026-11-10"}, {ID: 10, Date: "2026-10-20"}, {ID: 20, Date: "2026-11-03"}}
	pairs := []store.Pair{{MeetingID: 10, StudentID: 1}, {MeetingID: 20, StudentID: 1}, {MeetingID: 30, StudentID: 1}, {MeetingID: 10, StudentID: 2}, {MeetingID: 30, StudentID: 2}}
	d := BuildDashboard(sts, ms, pairs)
	if d.Meetings[0].ID != 10 || d.Meetings[2].ID != 30 {
		t.Fatalf("meetings not sorted by date: %+v", d.Meetings)
	}
	if len(d.Grades) != 2 || d.Grades[0].Grade != 6 || d.Grades[1].Grade != 0 {
		t.Fatalf("grades = %+v", d.Grades)
	}
	a := d.Grades[0].Rows[0]
	if a.Student.ID != 1 || a.Attended != 3 || a.Streak != 3 || a.Best != 3 {
		t.Fatalf("row A = %+v", a)
	}
	b := d.Grades[0].Rows[1]
	if b.Streak != 1 || b.Best != 1 {
		t.Fatalf("row B streaks = %d %d", b.Streak, b.Best)
	}
	if d.Grades[0].Rate != 83 || d.Grades[0].Latest != 2 {
		t.Fatalf("grade 6 rate %d latest %d", d.Grades[0].Rate, d.Grades[0].Latest)
	}
	l := d.Leaders()
	if len(l) != 2 || l[0].Student.ID != 1 || l[1].Rank != 2 {
		t.Fatalf("leaders = %+v", l)
	}
}
