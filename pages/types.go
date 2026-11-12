package pages

import "fmt"

type Lesson struct {
	Number int
	Slug   string
	When   string
	Title  string
}

func (l Lesson) URL() string       { return "/2026-27/" + l.Slug + "/" }
func (l Lesson) SlidesURL() string { return l.URL() + "slides.html" }
func (l Lesson) Label() string     { return fmt.Sprintf("Lesson %d", l.Number) }

type Game struct {
	Slug        string
	Title       string
	Description string
}

func (g Game) URL() string     { return "/games/" + g.Slug + "/" }
func (g Game) PlayURL() string { return g.URL() + "play/" }

type Email struct {
	To      string
	Subject string
	Body    string
}
