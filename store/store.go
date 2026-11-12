package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS students (
	id INTEGER PRIMARY KEY,
	first_name TEXT NOT NULL,
	last_name TEXT NOT NULL,
	grade INTEGER,
	active INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(first_name, last_name)
);
CREATE TABLE IF NOT EXISTS meetings (
	id INTEGER PRIMARY KEY,
	date TEXT NOT NULL UNIQUE,
	start_time TEXT NOT NULL,
	end_time TEXT NOT NULL,
	taken_by TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS rosters (
	student_id INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
	year INTEGER NOT NULL,
	grade INTEGER,
	PRIMARY KEY (student_id, year)
);
CREATE TABLE IF NOT EXISTS attendance (
	meeting_id INTEGER NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
	student_id INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
	PRIMARY KEY (meeting_id, student_id)
);
`

type Store struct{ db *sql.DB }

type Student struct {
	ID          int64
	FirstName   string
	LastName    string
	Grade       int
	Active      bool
	InClassroom bool
	OnRoster    bool
	Attended    int
	Years       []int
}

// YearOf returns the school year a date falls in, named by its starting
// calendar year. School years run August 1 through July 31.
func YearOf(t time.Time) int {
	if t.Month() >= time.August {
		return t.Year()
	}
	return t.Year() - 1
}

func YearOfDate(date string) int {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return YearOf(time.Now())
	}
	return YearOf(t)
}

func YearLabel(y int) string { return fmt.Sprintf("%d-%02d", y, (y+1)%100) }

func yearRange(y int) (string, string) {
	return fmt.Sprintf("%d-08-01", y), fmt.Sprintf("%d-07-31", y+1)
}

const yearExpr = `(CAST(substr(%[1]s, 1, 4) AS INTEGER) - (CAST(substr(%[1]s, 6, 2) AS INTEGER) < 8))`

func (s Student) Name() string { return s.FirstName + " " + s.LastName }

func (s Student) GradeLabel() string {
	if s.Grade == 0 {
		return ""
	}
	return fmt.Sprintf("%d", s.Grade)
}

type Meeting struct {
	ID        int64
	Date      string
	StartTime string
	EndTime   string
	TakenBy   string
	Notes     string
	Present   int
}

func (m Meeting) Day() time.Time {
	t, _ := time.Parse(time.DateOnly, m.Date)
	return t
}

type StudentReport struct {
	Student
	Attended int
	Total    int
	Last     string
}

func (r StudentReport) Percent() int {
	if r.Total == 0 {
		return 0
	}
	return r.Attended * 100 / r.Total
}

var ErrNotFound = errors.New("not found")

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	var hasClassroom int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('students') WHERE name = 'in_classroom'`).Scan(&hasClassroom); err != nil {
		db.Close()
		return nil, err
	}
	if hasClassroom == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE students ADD COLUMN in_classroom INTEGER NOT NULL DEFAULT 1`); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate in_classroom: %w", err)
		}
	}
	var rosterRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rosters`).Scan(&rosterRows); err != nil {
		db.Close()
		return nil, err
	}
	if rosterRows == 0 {
		seed := fmt.Sprintf(`
			INSERT OR IGNORE INTO rosters (student_id, year, grade)
			SELECT a.student_id, %s, s.grade FROM attendance a
			JOIN meetings m ON m.id = a.meeting_id JOIN students s ON s.id = a.student_id;
			INSERT OR IGNORE INTO rosters (student_id, year, grade) SELECT id, ?, grade FROM students WHERE active = 1;`, fmt.Sprintf(yearExpr, "m.date"))
		if _, err := db.ExecContext(ctx, seed, YearOf(time.Now())); err != nil {
			db.Close()
			return nil, fmt.Errorf("seed rosters: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Students lists students on the roster for year, or every student when
// year is 0. Grade is that year's grade and Attended counts meetings in it.
func (s *Store) Students(ctx context.Context, year int) ([]Student, error) {
	from, to := "0000-00-00", "9999-99-99"
	if year > 0 {
		from, to = yearRange(year)
	}
	q := `
		SELECT s.id, s.first_name, s.last_name,
			COALESCE(r.grade, s.grade, 0), s.active, s.in_classroom, r.student_id IS NOT NULL,
			(SELECT COUNT(*) FROM attendance a JOIN meetings m ON m.id = a.meeting_id
				WHERE a.student_id = s.id AND m.date BETWEEN ? AND ?),
			(SELECT COALESCE(GROUP_CONCAT(year), '') FROM (SELECT year FROM rosters WHERE student_id = s.id ORDER BY year))
		FROM students s
		LEFT JOIN rosters r ON r.student_id = s.id AND r.year = ?`
	if year > 0 {
		q += ` WHERE r.student_id IS NOT NULL`
	}
	q += ` ORDER BY s.last_name COLLATE NOCASE, s.first_name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q, from, to, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Student
	for rows.Next() {
		var st Student
		var years string
		if err := rows.Scan(&st.ID, &st.FirstName, &st.LastName, &st.Grade, &st.Active, &st.InClassroom, &st.OnRoster, &st.Attended, &years); err != nil {
			return nil, err
		}
		for _, y := range strings.Split(years, ",") {
			var n int
			if _, err := fmt.Sscan(y, &n); err == nil {
				st.Years = append(st.Years, n)
			}
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) Student(ctx context.Context, id int64, year int) (Student, error) {
	all, err := s.Students(ctx, 0)
	if err != nil {
		return Student{}, err
	}
	for _, st := range all {
		if st.ID != id {
			continue
		}
		if year > 0 {
			on, err := s.Students(ctx, year)
			if err != nil {
				return Student{}, err
			}
			for _, o := range on {
				if o.ID == id {
					return o, nil
				}
			}
		}
		return st, nil
	}
	return Student{}, ErrNotFound
}

// Years lists school years that have a roster or meetings, plus the
// current one, newest first.
func (s *Store) Years(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT year FROM rosters UNION SELECT %s FROM meetings UNION SELECT ? ORDER BY 1 DESC`, fmt.Sprintf(yearExpr, "date")), YearOf(time.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		out = append(out, y)
	}
	return out, rows.Err()
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// joinRoster puts a student on a year's roster. A zero grade is guessed
// from their last roster year, moved up one grade per year.
func joinRoster(ctx context.Context, db execer, id int64, year, grade int) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO rosters (student_id, year, grade) VALUES (?, ?, COALESCE(NULLIF(?, 0),
			(SELECT MIN(12, grade + (? - year)) FROM rosters WHERE student_id = ? AND year < ? AND grade IS NOT NULL ORDER BY year DESC LIMIT 1),
			(SELECT grade FROM students WHERE id = ?)))
		ON CONFLICT (student_id, year) DO UPDATE SET grade = COALESCE(NULLIF(?, 0), rosters.grade)`,
		id, year, grade, year, id, year, id, grade)
	return err
}

func (s *Store) SetRoster(ctx context.Context, id int64, year int, on bool) error {
	if on {
		return joinRoster(ctx, s.db, id, year, 0)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM rosters WHERE student_id = ? AND year = ?`, id, year)
	return err
}

func (s *Store) NeedClassroom(ctx context.Context, year int) ([]Student, error) {
	all, err := s.Students(ctx, year)
	if err != nil {
		return nil, err
	}
	var out []Student
	for _, st := range all {
		if !st.InClassroom {
			out = append(out, st)
		}
	}
	return out, nil
}

func (s *Store) SetInClassroom(ctx context.Context, id int64, in bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE students SET in_classroom = ? WHERE id = ?`, in, id)
	return err
}

// UpsertStudent adds or finds a student by name and puts them on the
// roster for year.
func (s *Store) UpsertStudent(ctx context.Context, first, last string, grade, year int) (int64, error) {
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	if first == "" || last == "" {
		return 0, errors.New("first and last name are required")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO students (first_name, last_name, grade, in_classroom) VALUES (?, ?, NULLIF(?, 0), 0)
		ON CONFLICT(first_name, last_name) DO UPDATE SET grade = COALESCE(NULLIF(excluded.grade, 0), students.grade), active = 1
		RETURNING id`, first, last, grade).Scan(&id)
	if err != nil {
		return 0, err
	}
	if year > 0 {
		err = joinRoster(ctx, s.db, id, year, grade)
	}
	return id, err
}

// UpdateStudent saves a student's details and their grade for year.
func (s *Store) UpdateStudent(ctx context.Context, st Student, year int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE students SET first_name = ?, last_name = ?, grade = COALESCE(NULLIF(?, 0), grade), in_classroom = ? WHERE id = ?`,
		strings.TrimSpace(st.FirstName), strings.TrimSpace(st.LastName), st.Grade, st.InClassroom, st.ID)
	if err != nil || year == 0 {
		return err
	}
	if !st.OnRoster {
		return s.SetRoster(ctx, st.ID, year, false)
	}
	if err := joinRoster(ctx, s.db, st.ID, year, st.Grade); err != nil {
		return err
	}
	if st.Grade == 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE rosters SET grade = NULL WHERE student_id = ? AND year = ?`, st.ID, year)
	}
	return err
}

func (s *Store) Meetings(ctx context.Context, year int) ([]Meeting, error) {
	from, to := "0000-00-00", "9999-99-99"
	if year > 0 {
		from, to = yearRange(year)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.date, m.start_time, m.end_time, m.taken_by, m.notes, COUNT(a.student_id)
		FROM meetings m LEFT JOIN attendance a ON a.meeting_id = m.id
		WHERE m.date BETWEEN ? AND ?
		GROUP BY m.id ORDER BY m.date DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Meeting
	for rows.Next() {
		var m Meeting
		if err := rows.Scan(&m.ID, &m.Date, &m.StartTime, &m.EndTime, &m.TakenBy, &m.Notes, &m.Present); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Meeting(ctx context.Context, id int64) (Meeting, []int64, error) {
	var m Meeting
	err := s.db.QueryRowContext(ctx, `SELECT id, date, start_time, end_time, taken_by, notes FROM meetings WHERE id = ?`, id).
		Scan(&m.ID, &m.Date, &m.StartTime, &m.EndTime, &m.TakenBy, &m.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil, ErrNotFound
	}
	if err != nil {
		return m, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT student_id FROM attendance WHERE meeting_id = ?`, id)
	if err != nil {
		return m, nil, err
	}
	defer rows.Close()
	var present []int64
	for rows.Next() {
		var sid int64
		if err := rows.Scan(&sid); err != nil {
			return m, nil, err
		}
		present = append(present, sid)
	}
	m.Present = len(present)
	return m, present, rows.Err()
}

func (s *Store) MeetingByDate(ctx context.Context, date string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM meetings WHERE date = ?`, date).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Store) PresentStudents(ctx context.Context, meetingID int64) ([]Student, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.first_name, s.last_name, COALESCE(s.grade, 0), s.active
		FROM attendance a JOIN students s ON s.id = a.student_id
		WHERE a.meeting_id = ? ORDER BY s.last_name COLLATE NOCASE, s.first_name COLLATE NOCASE`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Student
	for rows.Next() {
		var st Student
		if err := rows.Scan(&st.ID, &st.FirstName, &st.LastName, &st.Grade, &st.Active); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) SaveMeeting(ctx context.Context, m Meeting, present []int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id := m.ID
	if id == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO meetings (date, start_time, end_time, taken_by, notes) VALUES (?, ?, ?, ?, ?) RETURNING id`,
			m.Date, m.StartTime, m.EndTime, m.TakenBy, m.Notes).Scan(&id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE meetings SET date = ?, start_time = ?, end_time = ?, taken_by = ?, notes = ? WHERE id = ?`,
			m.Date, m.StartTime, m.EndTime, m.TakenBy, m.Notes, id)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, fmt.Errorf("there is already a meeting on %s", m.Date)
		}
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attendance WHERE meeting_id = ?`, id); err != nil {
		return 0, err
	}
	for _, sid := range present {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO attendance (meeting_id, student_id) VALUES (?, ?)`, id, sid); err != nil {
			return 0, err
		}
		if err := joinRoster(ctx, tx, sid, YearOfDate(m.Date), 0); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (s *Store) DeleteMeeting(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM meetings WHERE id = ?`, id)
	return err
}

func (s *Store) Report(ctx context.Context) ([]StudentReport, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meetings`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.first_name, s.last_name, COALESCE(s.grade, 0), s.active,
			COUNT(a.meeting_id), COALESCE(MAX(m.date), '')
		FROM students s
		LEFT JOIN attendance a ON a.student_id = s.id
		LEFT JOIN meetings m ON m.id = a.meeting_id
		WHERE s.active = 1
		GROUP BY s.id
		ORDER BY COUNT(a.meeting_id) DESC, s.last_name COLLATE NOCASE`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []StudentReport
	for rows.Next() {
		var r StudentReport
		if err := rows.Scan(&r.ID, &r.FirstName, &r.LastName, &r.Grade, &r.Active, &r.Attended, &r.Last); err != nil {
			return nil, 0, err
		}
		r.Total = total
		out = append(out, r)
	}
	return out, total, rows.Err()
}

type Pair struct {
	MeetingID int64
	StudentID int64
}

func (s *Store) Pairs(ctx context.Context) ([]Pair, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT meeting_id, student_id FROM attendance`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pair
	for rows.Next() {
		var p Pair
		if err := rows.Scan(&p.MeetingID, &p.StudentID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
