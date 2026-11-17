package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scaleClass struct {
	ID, CoachID, VenueID, SeriesID string
	StartsAt, EndsAt               time.Time
}

type scaleFixture struct {
	DB                                                                  *pgxpool.Pool
	Students, Guardians, StudentUsers, Coaches, Venues, Regions, Series []string
	Classes                                                             []scaleClass
	FirstMonday                                                         time.Time
	ClassType, Skill                                                    string
}

func newScaleFixture(t *testing.T) *scaleFixture {
	t.Helper()
	f := &scaleFixture{DB: testDB(t), ClassType: "scale-class-type", Skill: fmt.Sprintf("%064x", 10000)}
	loc, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(loc)
	f.FirstMonday = time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc).AddDate(0, 0, 14)
	for f.FirstMonday.Weekday() != time.Monday {
		f.FirstMonday = f.FirstMonday.AddDate(0, 0, 1)
	}
	at := now.AddDate(0, 0, -7).Truncate(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var hash string
	if err = tx.QueryRow(ctx, "SELECT password_hash FROM users WHERE id='parent'").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	var users, students, access, venues, series, classes, notes, milestones, notifications, deliveries, audits [][]any
	f.Coaches = append(f.Coaches, "coach")
	for i := 1; i < 10; i++ {
		id := fmt.Sprintf("scale-coach-%02d", i)
		f.Coaches = append(f.Coaches, id)
		users = append(users, []any{id, id + "@swim.test", fmt.Sprintf("Scale Coach %02d", i), "coach", hash})
	}
	for i := 0; i < 500; i++ {
		student, guardian, self := fmt.Sprintf("scale-student-%03d", i), fmt.Sprintf("scale-guardian-%03d", i), fmt.Sprintf("scale-self-%03d", i)
		switch i {
		case 0:
			student, guardian, self = "amy", "parent", "student"
		case 1:
			student, guardian = "ben", "other-parent"
		}
		f.Students = append(f.Students, student)
		f.Guardians = append(f.Guardians, guardian)
		f.StudentUsers = append(f.StudentUsers, self)
		if i >= 2 {
			students = append(students, []any{student, fmt.Sprintf("Scale Student %03d", i), "Beginner"})
			users = append(users, []any{guardian, guardian + "@swim.test", fmt.Sprintf("Scale Guardian %03d", i), "parent", hash})
			access = append(access, []any{student, guardian, "guardian"})
		}
		if i >= 1 {
			users = append(users, []any{self, self + "@swim.test", fmt.Sprintf("Scale Student Login %03d", i), "student", hash})
			access = append(access, []any{student, self, "self"})
		}
		if i != 0 {
			access = append(access, []any{student, f.Coaches[i/50], "coach"})
		}
	}
	regions := []string{"hk-island", "nt-west-islands", "kowloon-east", "nt-east", "kowloon-west"}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("scale-venue-%02d", i)
		region := regions[i%len(regions)]
		f.Venues = append(f.Venues, id)
		f.Regions = append(f.Regions, region)
		venues = append(venues, []any{id, fmt.Sprintf("Scale Venue %02d", i), "Synthetic pool address", region})
	}
	for slot := 0; slot < 50; slot++ {
		id := fmt.Sprintf("scale-series-%02d", slot)
		f.Series = append(f.Series, id)
		coach, venue := f.Coaches[slot/5], f.Venues[slot%20]
		first := f.FirstMonday.AddDate(0, 0, slot%5)
		series = append(series, []any{id, venue, f.ClassType, coach, pgtype.Date{Time: first, Valid: true}, pgtype.Date{Time: first.AddDate(0, 0, 84), Valid: true}, slot%5 + 1, pgtype.Time{Microseconds: 9 * 60 * 60 * 1000000, Valid: true}, pgtype.Time{Microseconds: 10 * 60 * 60 * 1000000, Valid: true}, 6})
		for week := 0; week < 13; week++ {
			start := first.AddDate(0, 0, 7*week)
			class := scaleClass{ID: fmt.Sprintf("scale-class-%02d-%02d", slot, week), CoachID: coach, VenueID: venue, SeriesID: id, StartsAt: start, EndsAt: start.Add(time.Hour)}
			f.Classes = append(f.Classes, class)
			classes = append(classes, []any{class.ID, venue, f.ClassType, coach, class.StartsAt, class.EndsAt, 6, id})
		}
	}
	// One note and achievement per student; Amy's extra notes cross the largest cursor page.
	sourceNumber := 0
	for i, student := range f.Students {
		coach := f.Coaches[i/50]
		noteCount := 1
		if i == 0 {
			noteCount += 125
		}
		for n := 0; n < noteCount; n++ {
			sourceNumber++
			id := fmt.Sprintf("%064x", 20000+sourceNumber)
			body := fmt.Sprintf("Synthetic scale progress %d", sourceNumber)
			notes = append(notes, []any{id, student, coach, body, at, at, coach, "scale-note-" + id, body})
			audits = append(audits, []any{coach, "progress.created", id, `{"synthetic":true}`, at})
			f.addTeachingNotifications(&notifications, &deliveries, student, i, "progress.created", id, at)
		}
		sourceNumber++
		id := fmt.Sprintf("%064x", 20000+sourceNumber)
		milestones = append(milestones, []any{id, student, f.Skill, coach, "achieved", "", 1, 1, "Synthetic scale skill", "Synthetic assessment", at, "scale-milestone-" + id, 0, 1})
		audits = append(audits, []any{coach, "milestone.achieved", id, `{"synthetic":true}`, at})
		f.addTeachingNotifications(&notifications, &deliveries, student, i, "milestone.achieved", id, at)
	}
	// Keep a distinct status for administrator filtering; all other deliveries are sent.
	failed := deliveries[len(deliveries)-1]
	failed[2], failed[3], failed[4], failed[5], failed[8], failed[9] = "failed", 5, 5, at.Add(30*time.Minute), "smtp_failed", 11
	copyRows := func(table string, columns []string, rows [][]any) {
		t.Helper()
		count, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(rows))
		if err != nil {
			t.Fatalf("scale fixture %s: %v", table, err)
		}
		if count != int64(len(rows)) {
			t.Fatalf("scale fixture %s copied %d of %d records", table, count, len(rows))
		}
	}
	copyRows("users", []string{"id", "email", "name", "role", "password_hash"}, users)
	copyRows("students", []string{"id", "name", "level"}, students)
	copyRows("student_access", []string{"student_id", "user_id", "kind"}, access)
	copyRows("venues", []string{"id", "name", "address", "region"}, venues)
	copyRows("class_types", []string{"id", "name", "level"}, [][]any{{f.ClassType, "Synthetic scale class", "Beginner"}})
	copyRows("class_series", []string{"id", "venue_id", "class_type_id", "coach_id", "start_date", "end_date", "weekday", "start_time", "end_time", "capacity"}, series)
	copyRows("class_sessions", []string{"id", "venue_id", "class_type_id", "coach_id", "starts_at", "ends_at", "capacity", "series_id"}, classes)
	copyRows("skills", []string{"id", "name", "description", "created_by_id", "request_id", "original_name", "original_description", "created_at", "updated_at"}, [][]any{{f.Skill, "Synthetic scale skill", "Synthetic assessment", "admin", "scale-skill", "Synthetic scale skill", "Synthetic assessment", at.Add(-24 * time.Hour), at.Add(-24 * time.Hour)}})
	copyRows("progress_notes", []string{"id", "student_id", "author_id", "body", "created_at", "updated_at", "updated_by_id", "request_id", "original_body"}, notes)
	copyRows("milestone_events", []string{"id", "student_id", "skill_id", "actor_id", "state", "reason", "version", "skill_version", "skill_name", "skill_description", "recorded_at", "request_id", "submitted_version", "submitted_skill_version"}, milestones)
	copyRows("notifications", []string{"id", "recipient_id", "student_id", "kind", "source_id", "source_version", "created_at", "read_at"}, notifications)
	copyRows("notification_deliveries", []string{"id", "notification_id", "status", "attempts", "cycle_attempts", "completed_at", "created_at", "recipient", "last_failure", "version"}, deliveries)
	copyRows("audit_events", []string{"actor_id", "action", "entity_id", "details", "created_at"}, audits)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *scaleFixture) addTeachingNotifications(notifications, deliveries *[][]any, student string, index int, kind, source string, at time.Time) {
	for _, recipient := range []string{f.Guardians[index], f.StudentUsers[index]} {
		number := len(*notifications) + 1
		id, delivery := fmt.Sprintf("%064x", 40000+number), fmt.Sprintf("%064x", 50000+number)
		read := number%3 == 0
		var readAt any
		if read {
			readAt = at
		}
		*notifications = append(*notifications, []any{id, recipient, student, kind, source, 1, at, readAt})
		*deliveries = append(*deliveries, []any{delivery, id, "sent", 1, 1, at.Add(time.Minute), at, recipient + "@swim.test", nil, 3})
	}
}
