package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"website/store"
)

func TestBuildEmail(t *testing.T) {
	m := store.Meeting{Date: "2026-11-10", StartTime: "15:00", EndTime: "17:30"}
	e := buildEmail(m, []store.Student{{FirstName: "Ada", LastName: "Lovelace", Grade: 7}, {FirstName: "Alan", LastName: "Turing"}}, "Grace Hopper")
	if e.To != "boonea@cadott.k12.wi.us" {
		t.Errorf("to = %q", e.To)
	}
	if e.Subject != "Programming Club attendance - Tuesday, November 10, 2026" {
		t.Errorf("subject = %q", e.Subject)
	}
	want := "Programming Club attendance for Tuesday, November 10, 2026, 3:00 to 5:30 PM.\n\nPresent (2):\nAda Lovelace, grade 7\nAlan Turing\n\nGrace Hopper\n"
	if e.Body != want {
		t.Errorf("body =\n%s\nwant\n%s", e.Body, want)
	}
}

func TestParseStudentLine(t *testing.T) {
	tests := []struct {
		in          string
		first, last string
		grade       int
		err         bool
	}{
		{"Jordan Smith, 7", "Jordan", "Smith", 7, false},
		{"Calvin Arneson-Bazal", "Calvin", "Arneson-Bazal", 0, false},
		{"Mary Jo Van Dyke, grade 9", "Mary", "Jo Van Dyke", 9, false},
		{"Cher", "", "", 0, true},
	}
	for _, tt := range tests {
		first, last, grade, err := parseStudentLine(tt.in)
		if (err != nil) != tt.err || first != tt.first || last != tt.last || grade != tt.grade {
			t.Errorf("parseStudentLine(%q) = %q %q %d %v", tt.in, first, last, grade, err)
		}
	}
}

func TestImportData(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	in := "student,Ada,Lovelace,7\nstudent,Alan,Turing,\nmeeting,2026-11-03,15:00,17:30,Linus Torvalds\npresent,2026-11-03,Ada,Lovelace\n"
	for range 2 {
		if err := importData(ctx, st, strings.NewReader(in)); err != nil {
			t.Fatal(err)
		}
	}
	ms, _ := st.Meetings(ctx)
	if len(ms) != 1 || ms[0].Present != 1 || ms[0].TakenBy != "Linus Torvalds" {
		t.Fatalf("meetings = %+v", ms)
	}
	rows, total, _ := st.Report(ctx)
	if total != 1 || len(rows) != 2 || rows[0].Attended != 1 {
		t.Fatalf("report = %+v total %d", rows, total)
	}
}

func TestNewStudents(t *testing.T) {
	got := newStudents(map[string][]string{
		"new_first": {"Jordan", "", "Sam"},
		"new_last":  {"Smith", "", ""},
		"new_grade": {"7", "", "8"},
	})
	if len(got) != 2 || got[0] != (newStudent{"Jordan", "Smith", 7}) || got[1] != (newStudent{"Sam", "", 8}) {
		t.Fatalf("newStudents = %+v", got)
	}
}
