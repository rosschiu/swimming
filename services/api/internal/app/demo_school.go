package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const demoSchoolSeed = "demo-school-v1"

type demoGroup struct {
	coach, venue, address, region, name, level string
	weekday, hour                              int
}

var demoGroups = []demoGroup{
	{"coach", "Harbour Pool", "Demo pool, Wan Chai", "hk-island", "Water Confidence", "Beginner", 1, 17},
	{"demo-coach-chloe", "Garden Pool", "Demo pool, Sha Tin", "nt-east", "Stroke Development", "Intermediate", 3, 17},
	{"demo-coach-derek", "Seaside Pool", "Demo pool, Tseung Kwan O", "nt-east", "Swim Squad", "Advanced", 5, 17},
}

type demoSwimmer struct {
	id, parent, level, coach, name string
	group                          int
}

func SeedDemoSchool(ctx context.Context, db *pgxpool.Pool, now time.Time) error {
	hash, err := bcrypt.GenerateFromPassword([]byte("Swim-test-2026!"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a := &API{db: db}
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		return err
	}
	var seeded bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM audit_events WHERE action='demo.school.seeded' AND entity_id=$1)", demoSchoolSeed).Scan(&seeded); err != nil {
		return err
	}
	if seeded {
		return tx.Commit(ctx)
	}
	if err = seedDemoAccounts(ctx, tx, hash); err != nil {
		return err
	}
	var shortcuts int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE (id,email,role) IN
		(('admin','admin@swim.test','admin'),('parent','parent@swim.test','parent'),('other-parent','other-parent@swim.test','parent'),
		 ('coach','coach@swim.test','coach'),('student','student@swim.test','student'))`).Scan(&shortcuts)
	if err != nil {
		return err
	}
	if shortcuts != 5 {
		return fmt.Errorf("demo school requires the synthetic shortcut account identities")
	}

	today := now.In(hongKong)
	monday := time.Date(today.Year(), today.Month(), today.Day()-(int(today.Weekday())+6)%7, 0, 0, 0, 0, hongKong)
	studentNames := []string{"Anson Wong", "Bella Lau", "Caleb Lam", "Daisy Ng", "Ethan Ho", "Faye Chan", "Gavin Yip", "Hannah Yu", "Ian Cheung", "Jade Leung", "Kai Lee", "Leah Tsang", "Milo Choi", "Nina Yau", "Owen Luk", "Poppy Mak", "Quinn Hui", "Rose Tang"}
	swimmers := []demoSwimmer{{"amy", "parent", "Beginner", "coach", "", 0}, {"ben", "other-parent", "Intermediate", demoGroups[1].coach, "", 1}}
	for i, name := range studentNames {
		group := i % len(demoGroups)
		id := fmt.Sprintf("demo-student-%02d", i+1)
		swimmers = append(swimmers, demoSwimmer{id, id + "-guardian", demoGroups[group].level, demoGroups[group].coach, name, group})
	}
	exec := func(sql string, args ...any) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}
	for i, name := range []string{"Coach Chloe", "Coach Derek"} {
		id := demoGroups[i+1].coach
		if err := exec("INSERT INTO users(id,email,name,role,password_hash) VALUES($1,$2,$3,'coach',$4)", id, id+"@swim.test", name, string(hash)); err != nil {
			return err
		}
	}
	for i, group := range demoGroups {
		id := fmt.Sprintf("demo-group-%d", i+1)
		if err := seedDemoSeries(ctx, tx, id, group, monday); err != nil {
			return err
		}
	}
	var policy CreditSettings
	if err := tx.QueryRow(ctx, "SELECT policy,version FROM credit_settings WHERE id").Scan(&policy.Policy, &policy.Version); err != nil {
		return err
	}
	terms := PurchaseTerms{OfferID: "demo-8-lessons", OfferVersion: 1, OfferName: "Demo 8-lesson package", Credits: 8,
		PriceMinor: 120000, Currency: "HKD", Policy: policy.Policy, PolicyVersion: policy.Version}
	if err := exec("INSERT INTO package_offers(id,name,credits,price_minor,currency) VALUES($1,$2,$3,$4,$5)",
		terms.OfferID, terms.OfferName, terms.Credits, terms.PriceMinor, terms.Currency); err != nil {
		return err
	}
	a.now = func() time.Time { return monday.AddDate(0, 0, -21).UTC() }
	for _, swimmer := range swimmers {
		if swimmer.name != "" {
			if err := exec("INSERT INTO students(id,name,level) VALUES($1,$2,$3)", swimmer.id, swimmer.name, swimmer.level); err != nil {
				return err
			}
			if err := exec("INSERT INTO users(id,email,name,role,password_hash) VALUES($1,$2,$3,'parent',$4)", swimmer.parent, swimmer.parent+"@swim.test", swimmer.name+"’s guardian", string(hash)); err != nil {
				return err
			}
		}
		if err := exec("INSERT INTO student_access(student_id,user_id,kind) VALUES($1,$2,'guardian'),($1,$3,'coach') ON CONFLICT DO NOTHING", swimmer.id, swimmer.parent, swimmer.coach); err != nil {
			return err
		}
		if err := seedDemoSwimmer(ctx, tx, a, swimmer, terms, monday); err != nil {
			return fmt.Errorf("seed demo swimmer %s: %w", swimmer.id, err)
		}
	}
	if err := audit(ctx, tx, "admin", "demo.school.seeded", demoSchoolSeed, map[string]any{"weekOf": monday.Format("2006-01-02"), "students": len(swimmers)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func seedDemoSeries(ctx context.Context, tx pgx.Tx, id string, group demoGroup, monday time.Time) error {
	start := monday.AddDate(0, 0, group.weekday-1-14)
	end := start.AddDate(0, 0, 28)
	startClock := fmt.Sprintf("%02d:00", group.hour)
	endClock := fmt.Sprintf("%02d:00", group.hour+1)
	for _, step := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO venues(id,name,address,region) VALUES($1,$2,$3,$4)", []any{id, group.venue, group.address, group.region}},
		{"INSERT INTO class_types(id,name,level) VALUES($1,$2,$3)", []any{id, group.name, group.level}},
		{`INSERT INTO class_series(id,venue_id,class_type_id,coach_id,start_date,end_date,weekday,start_time,end_time,capacity)
			VALUES($1,$1,$1,$2,$3::text::date,$4::text::date,$5,$6::text::time,$7::text::time,10)`,
			[]any{id, group.coach, start.Format("2006-01-02"), end.Format("2006-01-02"), group.weekday, startClock, endClock}},
	} {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			return err
		}
	}
	for week := range 5 {
		starts := start.AddDate(0, 0, week*7).Add(time.Duration(group.hour) * time.Hour)
		ends := starts.Add(time.Hour)
		classID := fmt.Sprintf("%s-%d", id, week)
		conflict, err := classConflict(ctx, tx, ClassInput{CoachID: group.coach, VenueID: id, StartsAt: starts, EndsAt: ends}, "")
		if err != nil {
			return err
		}
		if conflict != "" {
			return fmt.Errorf("demo series conflicts with existing class %s", conflict)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO class_sessions(id,venue_id,class_type_id,coach_id,starts_at,ends_at,capacity,series_id)
			VALUES($1,$2,$2,$3,$4,$5,10,$2)`, classID, id, group.coach, starts, ends); err != nil {
			return err
		}
	}
	return nil
}

func seedDemoSwimmer(ctx context.Context, tx pgx.Tx, a *API, swimmer demoSwimmer, terms PurchaseTerms, monday time.Time) error {
	now := a.now()
	expiry := monday.AddDate(0, 0, 42).Format("2006-01-02")
	purchaseID := "demo-purchase-" + swimmer.id
	purchase := Purchase{ID: purchaseID, StudentID: swimmer.id, CreatedBy: swimmer.parent, PurchaseTerms: terms, ExpiresOn: &expiry}
	_, err := tx.Exec(ctx, `INSERT INTO purchases(id,student_id,created_by,idempotency_key,request,terms,status,expires_on,expiry_assigned_by,expiry_assigned_at,version,created_at)
		VALUES($1,$2,$3,$1,jsonb_build_object('studentId',$2::text,'offerId',$4::text,'offerVersion',1),$5,'pending_payment',$6,'admin',$7,2,$7)`,
		purchaseID, swimmer.id, swimmer.parent, terms.OfferID, terms, expiry, now)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO payment_attempts(id,purchase_id,created_by,idempotency_key,status,created_at)
		VALUES($1,$1,$2,$1,'succeeded',$3)`, purchaseID, swimmer.parent, now); err != nil {
		return err
	}
	if err = a.fulfillPurchase(ctx, tx, swimmer.parent, purchase, true); err != nil {
		return err
	}
	var packageID, level string
	if err = tx.QueryRow(ctx, `SELECT cp.id,st.level FROM credit_packages cp JOIN students st ON st.id=cp.student_id WHERE cp.purchase_id=$1`,
		purchaseID).Scan(&packageID, &level); err != nil {
		return err
	}
	seriesID := fmt.Sprintf("demo-group-%d", swimmer.group+1)
	enrolmentID := "demo-enrolment-" + swimmer.id
	if _, err = tx.Exec(ctx, `INSERT INTO enrolments(id,student_id,series_id,credit_package_id,actor_id,idempotency_key,request,created_at)
		VALUES($1,$2,$3,$4,$5,$1,'{"fixture":"demo-school-v1"}',$6)`,
		enrolmentID, swimmer.id, seriesID, packageID, swimmer.parent, now); err != nil {
		return err
	}
	for _, week := range []int{1, 3, 4} {
		classID := fmt.Sprintf("%s-%d", seriesID, week)
		bookingID := "demo-booking-" + swimmer.id + fmt.Sprintf("-%d", week)
		var overlap bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bookings b
			JOIN class_sessions booked ON booked.id=b.class_id CROSS JOIN class_sessions target
			WHERE b.student_id=$1 AND b.status='reserved' AND target.id=$2
			AND booked.starts_at<target.ends_at AND booked.ends_at>target.starts_at)`, swimmer.id, classID).Scan(&overlap)
		if err != nil {
			return err
		}
		if overlap {
			return fmt.Errorf("demo booking overlaps an existing booking for class %s", classID)
		}
		_, err = tx.Exec(ctx, `INSERT INTO bookings(id,student_id,class_id,credit_package_id,actor_id,request,class_version,class_type_version,
			student_level,class_level,override_actor_id,override_reason,validity_rule,created_at,enrolment_id)
			VALUES($1,$2,$3,$4,CASE WHEN $6=$7 THEN $5 ELSE 'admin' END,'{"fixture":"demo-school-v1"}',1,1,$6,$7,
				CASE WHEN $6<>$7 THEN 'admin' END,CASE WHEN $6<>$7 THEN 'Synthetic demo class placement' END,$8,$9,$10)`,
			bookingID, swimmer.id, classID, packageID, swimmer.parent, level, swimmer.level, bookingValidityLessonEnd, now, enrolmentID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
			VALUES($1,$2,$1,-1,1,0,0,$3,'Class booking reservation',$4)`,
			"booking:"+bookingID+":reserved", packageID, swimmer.parent, now); err != nil {
			return err
		}
		if week == 1 {
			var markedAt time.Time
			if err = tx.QueryRow(ctx, "SELECT ends_at FROM class_sessions WHERE id=$1", classID).Scan(&markedAt); err != nil {
				return err
			}
			movement := "booking:" + bookingID + ":present"
			if _, err = tx.Exec(ctx, `INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
				VALUES($1,$2,$1,0,-1,1,0,$3,'Class attendance',$4)`, movement, packageID, swimmer.coach, markedAt); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO booking_attendance(booking_id,status,actor_id,marked_at,movement_id)
				VALUES($1,'present',$2,$3,$4)`, bookingID, swimmer.coach, markedAt, movement); err != nil {
				return err
			}
		}
	}
	body := []string{
		"Practised floating and controlled breathing with a kickboard. Next lesson, try a short independent glide.",
		"Built a steadier freestyle rhythm. Next lesson, practise breathing without lifting the head.",
		"Held a consistent pace over four lengths. Next lesson, focus on the push-off after each turn.",
	}[swimmer.group]
	noteID := "demo-progress-" + swimmer.id
	_, err = tx.Exec(ctx, `INSERT INTO progress_notes(id,student_id,author_id,body,updated_by_id,request_id,original_body,created_at,updated_at)
		VALUES($1,$2,$3,$4,$3,$1,$4,$5,$5)`, noteID, swimmer.id, swimmer.coach, body, monday.AddDate(0, 0, -1))
	return err
}
