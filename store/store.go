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
CREATE TABLE IF NOT EXISTS attendance (
	meeting_id INTEGER NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
	student_id INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
	PRIMARY KEY (meeting_id, student_id)
);
`

type Store struct{ db *sql.DB }

type Student struct {
	ID        int64
	FirstName string
	LastName  string
	Grade       int
	Active      bool
	InClassroom bool
}

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
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Students(ctx context.Context, activeOnly bool) ([]Student, error) {
	q := `SELECT id, first_name, last_name, COALESCE(grade, 0), active, in_classroom FROM students`
	if activeOnly {
		q += ` WHERE active = 1`
	}
	q += ` ORDER BY last_name COLLATE NOCASE, first_name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Student
	for rows.Next() {
		var st Student
		if err := rows.Scan(&st.ID, &st.FirstName, &st.LastName, &st.Grade, &st.Active, &st.InClassroom); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) NeedClassroom(ctx context.Context) ([]Student, error) {
	all, err := s.Students(ctx, true)
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

func (s *Store) UpsertStudent(ctx context.Context, first, last string, grade int) (int64, error) {
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	if first == "" || last == "" {
		return 0, errors.New("first and last name are required")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO students (first_name, last_name, grade, in_classroom) VALUES (?, ?, NULLIF(?, 0), 0)
		ON CONFLICT(first_name, last_name) DO UPDATE SET grade = COALESCE(NULLIF(excluded.grade, 0), students.grade), active = 1
		RETURNING id`, first, last, grade).Scan(&id)
	return id, err
}

func (s *Store) UpdateStudent(ctx context.Context, st Student) error {
	_, err := s.db.ExecContext(ctx, `UPDATE students SET first_name = ?, last_name = ?, grade = NULLIF(?, 0), active = ?, in_classroom = ? WHERE id = ?`,
		strings.TrimSpace(st.FirstName), strings.TrimSpace(st.LastName), st.Grade, st.Active, st.InClassroom, st.ID)
	return err
}

func (s *Store) Meetings(ctx context.Context) ([]Meeting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.date, m.start_time, m.end_time, m.taken_by, m.notes, COUNT(a.student_id)
		FROM meetings m LEFT JOIN attendance a ON a.meeting_id = m.id
		GROUP BY m.id ORDER BY m.date DESC`)
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
