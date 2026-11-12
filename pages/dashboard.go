package pages

import (
	"fmt"
	"slices"

	"website/store"
)

type GradeColor struct {
	Name string
	Hex  string
}

var gradeColors = map[int]string{
	6: "#5db7ff", 7: "#3fd49f", 8: "#b38cff", 9: "#ff9d4d", 10: "#ff6f9f", 11: "#ffd21f", 12: "#6fe3ea",
}

func gradeHex(g int) string {
	if c, ok := gradeColors[g]; ok {
		return c
	}
	return "#8b8a80"
}

type DashRow struct {
	Student  store.Student
	Present  []bool
	Attended int
	Streak   int
	Best     int
	Hex      string
}

func (r DashRow) Recent(n int) []bool {
	if len(r.Present) <= n {
		return r.Present
	}
	return r.Present[len(r.Present)-n:]
}

func (r DashRow) Percent() int {
	if len(r.Present) == 0 {
		return 0
	}
	return r.Attended * 100 / len(r.Present)
}

func streaks(p []bool) (current, best int) {
	run := 0
	for _, here := range p {
		if here {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return run, best
}

type DashGrade struct {
	Grade  int
	Hex    string
	Rows   []DashRow
	Latest int
	Rate   int
}

func (g DashGrade) Label() string {
	if g.Grade == 0 {
		return "Grade unknown"
	}
	return fmt.Sprintf("Grade %d", g.Grade)
}

type Dashboard struct {
	Meetings []store.Meeting
	Grades   []DashGrade
	Roster   int
}

func (d Dashboard) Latest() (store.Meeting, bool) {
	if len(d.Meetings) == 0 {
		return store.Meeting{}, false
	}
	return d.Meetings[len(d.Meetings)-1], true
}

func BuildDashboard(students []store.Student, meetings []store.Meeting, pairs []store.Pair) Dashboard {
	ms := slices.Clone(meetings)
	slices.SortFunc(ms, func(a, b store.Meeting) int {
		if a.Date < b.Date {
			return -1
		}
		if a.Date > b.Date {
			return 1
		}
		return 0
	})
	col := map[int64]int{}
	for i, m := range ms {
		col[m.ID] = i
	}
	here := map[int64][]bool{}
	for _, s := range students {
		here[s.ID] = make([]bool, len(ms))
	}
	for _, p := range pairs {
		if row, ok := here[p.StudentID]; ok {
			if c, ok := col[p.MeetingID]; ok {
				row[c] = true
			}
		}
	}
	byGrade := map[int]*DashGrade{}
	for _, s := range students {
		g := byGrade[s.Grade]
		if g == nil {
			g = &DashGrade{Grade: s.Grade, Hex: gradeHex(s.Grade)}
			byGrade[s.Grade] = g
		}
		r := DashRow{Student: s, Present: here[s.ID], Hex: gradeHex(s.Grade)}
		for _, p := range r.Present {
			if p {
				r.Attended++
			}
		}
		r.Streak, r.Best = streaks(r.Present)
		g.Rows = append(g.Rows, r)
	}
	var grades []DashGrade
	for _, g := range byGrade {
		total, attended := 0, 0
		for _, r := range g.Rows {
			total += len(r.Present)
			attended += r.Attended
			if len(r.Present) > 0 && r.Present[len(r.Present)-1] {
				g.Latest++
			}
		}
		if total > 0 {
			g.Rate = attended * 100 / total
		}
		slices.SortStableFunc(g.Rows, func(a, b DashRow) int { return b.Attended - a.Attended })
		grades = append(grades, *g)
	}
	slices.SortFunc(grades, func(a, b DashGrade) int {
		ag, bg := a.Grade, b.Grade
		if ag == 0 {
			ag = 99
		}
		if bg == 0 {
			bg = 99
		}
		return ag - bg
	})
	return Dashboard{Meetings: ms, Grades: grades, Roster: len(students)}
}

func (g DashGrade) Summary() string {
	kids := "kids"
	if len(g.Rows) == 1 {
		kids = "kid"
	}
	return fmt.Sprintf("%d %s, %d%%", len(g.Rows), kids, g.Rate)
}

const RecentCount = 6

func (d Dashboard) Recent() []store.Meeting {
	if len(d.Meetings) <= RecentCount {
		return d.Meetings
	}
	return d.Meetings[len(d.Meetings)-RecentCount:]
}

type Ranked struct {
	DashRow
	Rank int
}

func (d Dashboard) Leaders() []Ranked {
	var all []DashRow
	for _, g := range d.Grades {
		all = append(all, g.Rows...)
	}
	slices.SortStableFunc(all, func(a, b DashRow) int {
		if a.Attended != b.Attended {
			return b.Attended - a.Attended
		}
		if a.Streak != b.Streak {
			return b.Streak - a.Streak
		}
		return b.Best - a.Best
	})
	var out []Ranked
	for i, r := range all {
		if r.Attended == 0 {
			break
		}
		rank := i + 1
		if i > 0 && r.Attended == all[i-1].Attended && r.Streak == all[i-1].Streak && r.Best == all[i-1].Best {
			rank = out[i-1].Rank
		}
		out = append(out, Ranked{DashRow: r, Rank: rank})
	}
	return out
}

func (d Dashboard) GradeStandings() []DashGrade {
	gs := slices.Clone(d.Grades)
	slices.SortStableFunc(gs, func(a, b DashGrade) int { return b.Rate - a.Rate })
	return gs
}

func (d Dashboard) ShowYear() bool { return len(d.Meetings) > RecentCount }
