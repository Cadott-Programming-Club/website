package pages

import (
	"fmt"

	"website/store"
)

type YearNav struct {
	Years    []int
	Selected int
	Current  int
	Base     string
}

func (n YearNav) Param() string {
	if n.Selected == 0 {
		return "all"
	}
	return fmt.Sprint(n.Selected)
}

func (n YearNav) Href(year int) string {
	if year == n.Current {
		return n.Base
	}
	if year == 0 {
		return n.Base + "?year=all"
	}
	return fmt.Sprintf("%s?year=%d", n.Base, year)
}

func (n YearNav) Label() string {
	if n.Selected == 0 {
		return "all years"
	}
	return store.YearLabel(n.Selected)
}

type StudentView struct {
	Nav      YearNav
	Meetings int
	EditYear int
}

func (v StudentView) URL(format string, id int64) string {
	return fmt.Sprintf(format, id) + "?year=" + v.Nav.Param()
}

func yearShort(y int) string { return store.YearLabel(y) }
