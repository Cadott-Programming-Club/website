package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestYearOf(t *testing.T) {
	for date, want := range map[string]int{"2026-08-01": 2026, "2027-07-31": 2026, "2026-11-12": 2026, "2026-07-31": 2025} {
		if got := YearOfDate(date); got != want {
			t.Errorf("YearOfDate(%s) = %d, want %d", date, got, want)
		}
	}
	if YearLabel(2026) != "2026-27" || YearLabel(2099) != "2099-00" {
		t.Errorf("YearLabel wrong: %s %s", YearLabel(2026), YearLabel(2099))
	}
}

func TestRosters(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	ada, err := st.UpsertStudent(ctx, "Ada", "Lovelace", 7, 2026)
	if err != nil {
		t.Fatal(err)
	}
	alan, _ := st.UpsertStudent(ctx, "Alan", "Turing", 9, 2026)

	if _, err := st.SaveMeeting(ctx, Meeting{Date: "2026-11-10", StartTime: "15:00", EndTime: "17:30"}, []int64{ada}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveMeeting(ctx, Meeting{Date: "2027-09-14", StartTime: "15:00", EndTime: "17:30"}, []int64{ada}); err != nil {
		t.Fatal(err)
	}

	next, _ := st.Students(ctx, 2027)
	if len(next) != 1 || next[0].ID != ada || next[0].Grade != 8 || next[0].Attended != 1 {
		t.Fatalf("2027 roster = %+v, want Ada in grade 8 with 1 meeting", next)
	}
	cur, _ := st.Students(ctx, 2026)
	if len(cur) != 2 {
		t.Fatalf("2026 roster has %d students, want 2", len(cur))
	}
	all, _ := st.Students(ctx, 0)
	for _, s := range all {
		if s.ID == ada && (s.Attended != 2 || len(s.Years) != 2) {
			t.Errorf("Ada across all years = %+v", s)
		}
	}

	if err := st.UpdateStudent(ctx, Student{ID: alan, FirstName: "Alan", LastName: "Turing", Grade: 10, OnRoster: true}, 2026); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.Student(ctx, alan, 2026); s.Grade != 10 {
		t.Errorf("Alan grade = %d, want 10", s.Grade)
	}
	if err := st.UpdateStudent(ctx, Student{ID: alan, FirstName: "Alan", LastName: "Turing", Grade: 10}, 2026); err != nil {
		t.Fatal(err)
	}
	if cur, _ := st.Students(ctx, 2026); len(cur) != 1 {
		t.Errorf("after removing Alan, 2026 roster has %d", len(cur))
	}
	if ms, _ := st.Meetings(ctx, 2026); len(ms) != 1 {
		t.Errorf("2026 meetings = %d, want 1", len(ms))
	}
	years, _ := st.Years(ctx)
	if len(years) < 2 || years[0] < 2027 {
		t.Errorf("years = %v", years)
	}
}
