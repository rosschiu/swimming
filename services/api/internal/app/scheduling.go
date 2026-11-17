package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type Venue struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Region  string `json:"region"`
	Active  bool   `json:"active"`
	Version int    `json:"version"`
}
type ClassType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Level   string `json:"level"`
	Active  bool   `json:"active"`
	Version int    `json:"version"`
}
type SchedulingSettings struct {
	TravelGapMinutes int `json:"travelGapMinutes"`
	Version          int `json:"version"`
}
type ClassInput struct {
	VenueID     string    `json:"venueId"`
	ClassTypeID string    `json:"classTypeId"`
	CoachID     string    `json:"coachId"`
	StartsAt    time.Time `json:"startsAt"`
	EndsAt      time.Time `json:"endsAt"`
	Capacity    int       `json:"capacity"`
}
type ClassSession struct {
	ID string `json:"id"`
	ClassInput
	VenueName     string  `json:"venueName"`
	Region        string  `json:"region"`
	ClassTypeName string  `json:"classTypeName"`
	Level         string  `json:"level"`
	CoachName     string  `json:"coachName"`
	SeriesID      *string `json:"seriesId"`
	Version       int     `json:"version"`
}
type SeriesInput struct {
	VenueID     string `json:"venueId"`
	ClassTypeID string `json:"classTypeId"`
	CoachID     string `json:"coachId"`
	StartDate   string `json:"startDate"`
	EndDate     string `json:"endDate"`
	Weekday     int    `json:"weekday"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	Capacity    int    `json:"capacity"`
}
type ClassSeries struct {
	ID string `json:"id"`
	SeriesInput
	Sessions []ClassSession `json:"sessions"`
}

var hongKong = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		panic(err)
	}
	return loc
}()

func validRegion(region string) bool {
	switch region {
	case "hk-island", "nt-west-islands", "kowloon-east", "nt-east", "kowloon-west":
		return true
	}
	return false
}
func dateRange(from, to string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation("2006-01-02", from, hongKong)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := time.ParseInLocation("2006-01-02", to, hongKong)
	if err != nil || end.Before(start) || end.Sub(start) > 92*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("select an inclusive range of 1–93 days")
	}
	return start, end.AddDate(0, 0, 1), nil
}
func (a *API) schedulingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/scheduling", a.auth(a.getSchedulingSettings))
	mux.HandleFunc("PATCH /api/v1/settings/scheduling", a.auth(a.updateSchedulingSettings))
	mux.HandleFunc("GET /api/v1/venues", a.auth(a.listVenues))
	mux.HandleFunc("POST /api/v1/venues", a.auth(a.createVenue))
	mux.HandleFunc("PATCH /api/v1/venues/{id}", a.auth(a.updateVenue))
	mux.HandleFunc("GET /api/v1/class-types", a.auth(a.listClassTypes))
	mux.HandleFunc("POST /api/v1/class-types", a.auth(a.createClassType))
	mux.HandleFunc("PATCH /api/v1/class-types/{id}", a.auth(a.updateClassType))
	mux.HandleFunc("GET /api/v1/classes", a.auth(a.listClasses))
	mux.HandleFunc("GET /api/v1/classes/{id}", a.auth(a.getClass))
	mux.HandleFunc("POST /api/v1/classes", a.auth(a.createClass))
	mux.HandleFunc("PATCH /api/v1/classes/{id}", a.auth(a.updateClass))
	mux.HandleFunc("GET /api/v1/class-series", a.auth(a.listSeries))
	mux.HandleFunc("GET /api/v1/class-series/{id}", a.auth(a.getSeries))
	mux.HandleFunc("POST /api/v1/class-series", a.auth(a.createSeries))
}
func (a *API) beginScheduling(ctx context.Context) (pgx.Tx, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417003)"); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func (a *API) getSchedulingSettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var x SchedulingSettings
	if err := a.db.QueryRow(r.Context(), "SELECT travel_gap_minutes,version FROM scheduling_settings WHERE id").Scan(&x.TravelGapMinutes, &x.Version); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) updateSchedulingSettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		TravelGapMinutes *int `json:"travelGapMinutes"`
		Version          int  `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.TravelGapMinutes == nil || *input.TravelGapMinutes < 0 || *input.TravelGapMinutes > 1440 || input.Version < 1 {
		fail(w, 422, "validation_error", "Travel gap must be 0–1440 minutes with a valid version")
		return
	}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var before SchedulingSettings
	if err = tx.QueryRow(r.Context(), "SELECT travel_gap_minutes,version FROM scheduling_settings WHERE id").Scan(&before.TravelGapMinutes, &before.Version); err != nil {
		internal(w, err)
		return
	}
	if before.Version != input.Version {
		fail(w, 409, "version_conflict", "Scheduling settings changed; reload before saving")
		return
	}
	var conflict string
	err = tx.QueryRow(r.Context(), `SELECT a.id||' and '||b.id FROM class_sessions a JOIN class_sessions b ON a.coach_id=b.coach_id AND a.id<b.id WHERE a.ends_at>$1 AND b.ends_at>$1 AND a.starts_at < b.ends_at + CASE WHEN a.venue_id<>b.venue_id THEN $2*interval '1 minute' ELSE interval '0' END AND b.starts_at < a.ends_at + CASE WHEN a.venue_id<>b.venue_id THEN $2*interval '1 minute' ELSE interval '0' END LIMIT 1`, a.now(), *input.TravelGapMinutes).Scan(&conflict)
	if err == nil {
		fail(w, 409, "scheduling_conflict", "Travel gap conflicts with classes "+conflict)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	x := SchedulingSettings{*input.TravelGapMinutes, before.Version + 1}
	if _, err = tx.Exec(r.Context(), "UPDATE scheduling_settings SET travel_gap_minutes=$1,version=$2 WHERE id", x.TravelGapMinutes, x.Version); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "scheduling.settings.updated", "scheduling", map[string]any{"before": before, "after": x}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) listVenues(w http.ResponseWriter, r *http.Request, s Session) {
	region := r.URL.Query().Get("region")
	if region != "" && !validRegion(region) {
		fail(w, 422, "validation_error", "Invalid region")
		return
	}
	rows, err := a.db.Query(r.Context(), "SELECT id,name,address,region,active,version FROM venues WHERE ($1='admin' OR active) AND ($2='' OR region=$2) ORDER BY name,id", s.User.Role, region)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []Venue{}
	for rows.Next() {
		var x Venue
		if err = rows.Scan(&x.ID, &x.Name, &x.Address, &x.Region, &x.Active, &x.Version); err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func validVenue(x *Venue) bool {
	x.Name = strings.TrimSpace(x.Name)
	x.Address = strings.TrimSpace(x.Address)
	return validName(x.Name) && utf8.ValidString(x.Address) && utf8.RuneCountInString(x.Address) >= 1 && utf8.RuneCountInString(x.Address) <= 300 && !strings.ContainsRune(x.Address, 0) && validRegion(x.Region)
}
func (a *API) createVenue(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Region  string `json:"region"`
	}
	if !decode(w, r, &input) {
		return
	}
	x := Venue{Name: input.Name, Address: input.Address, Region: input.Region, Active: true, Version: 1}
	if !validVenue(&x) {
		fail(w, 422, "validation_error", "Provide a name, address and valid region")
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	x.ID = id
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO venues(id,name,address,region) VALUES($1,$2,$3,$4)", x.ID, x.Name, x.Address, x.Region); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "venue.created", x.ID, x); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}
func (a *API) updateVenue(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Region  string `json:"region"`
		Active  *bool  `json:"active"`
		Version int    `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	x := Venue{ID: r.PathValue("id"), Name: input.Name, Address: input.Address, Region: input.Region, Version: input.Version}
	if !validVenue(&x) || input.Active == nil || x.Version < 1 {
		fail(w, 422, "validation_error", "Provide a valid venue, active status and version")
		return
	}
	x.Active = *input.Active
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var before Venue
	err = tx.QueryRow(r.Context(), "SELECT id,name,address,region,active,version FROM venues WHERE id=$1", x.ID).Scan(&before.ID, &before.Name, &before.Address, &before.Region, &before.Active, &before.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Venue not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if before.Version != x.Version {
		fail(w, 409, "version_conflict", "Venue changed; reload before saving")
		return
	}
	x.Version++
	if _, err = tx.Exec(r.Context(), "UPDATE venues SET name=$2,address=$3,region=$4,active=$5,version=$6 WHERE id=$1", x.ID, x.Name, x.Address, x.Region, x.Active, x.Version); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "venue.updated", x.ID, map[string]any{"before": before, "after": x}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) listClassTypes(w http.ResponseWriter, r *http.Request, s Session) {
	rows, err := a.db.Query(r.Context(), "SELECT id,name,level,active,version FROM class_types WHERE ($1='admin' OR active) ORDER BY name,id", s.User.Role)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []ClassType{}
	for rows.Next() {
		var x ClassType
		if err = rows.Scan(&x.ID, &x.Name, &x.Level, &x.Active, &x.Version); err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) createClassType(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		Name  string `json:"name"`
		Level string `json:"level"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Level = strings.TrimSpace(input.Level)
	if !validName(input.Name) || !validName(input.Level) {
		fail(w, 422, "validation_error", "Provide a name and level of 1–100 characters")
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	x := ClassType{id, input.Name, input.Level, true, 1}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO class_types(id,name,level) VALUES($1,$2,$3)", x.ID, x.Name, x.Level); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "class_type.created", x.ID, x); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}
func (a *API) updateClassType(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		Name    string `json:"name"`
		Level   string `json:"level"`
		Active  *bool  `json:"active"`
		Version int    `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Level = strings.TrimSpace(input.Level)
	if !validName(input.Name) || !validName(input.Level) || input.Active == nil || input.Version < 1 {
		fail(w, 422, "validation_error", "Provide a valid class type, active status and version")
		return
	}
	x := ClassType{r.PathValue("id"), input.Name, input.Level, *input.Active, input.Version}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var before ClassType
	err = tx.QueryRow(r.Context(), "SELECT id,name,level,active,version FROM class_types WHERE id=$1", x.ID).Scan(&before.ID, &before.Name, &before.Level, &before.Active, &before.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class type not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if before.Version != x.Version {
		fail(w, 409, "version_conflict", "Class type changed; reload before saving")
		return
	}
	x.Version++
	if _, err = tx.Exec(r.Context(), "UPDATE class_types SET name=$2,level=$3,active=$4,version=$5 WHERE id=$1", x.ID, x.Name, x.Level, x.Active, x.Version); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "class_type.updated", x.ID, map[string]any{"before": before, "after": x}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}

const classColumns = "c.id,c.venue_id,c.class_type_id,c.coach_id,c.starts_at,c.ends_at,c.capacity,v.name,v.region,t.name,t.level,u.name,c.series_id,c.version"
const classJoin = " FROM class_sessions c JOIN venues v ON v.id=c.venue_id JOIN class_types t ON t.id=c.class_type_id JOIN users u ON u.id=c.coach_id "

func scanClass(row pgx.Row) (ClassSession, error) {
	var x ClassSession
	err := row.Scan(&x.ID, &x.VenueID, &x.ClassTypeID, &x.CoachID, &x.StartsAt, &x.EndsAt, &x.Capacity, &x.VenueName, &x.Region, &x.ClassTypeName, &x.Level, &x.CoachName, &x.SeriesID, &x.Version)
	return x, err
}
func (a *API) listClasses(w http.ResponseWriter, r *http.Request, s Session) {
	start, end, err := dateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	region := r.URL.Query().Get("region")
	if err != nil || (region != "" && !validRegion(region)) {
		fail(w, 422, "validation_error", "Provide valid Hong Kong dates spanning 1–93 days and a valid region")
		return
	}
	rows, err := a.db.Query(r.Context(), "SELECT "+classColumns+classJoin+" WHERE c.starts_at>=$1 AND c.starts_at<$2 AND ($3<>'coach' OR c.coach_id=$4) AND ($5='' OR c.coach_id=$5) AND ($6='' OR c.venue_id=$6) AND ($7='' OR v.region=$7) ORDER BY c.starts_at,c.id", start, end, s.User.Role, s.User.ID, r.URL.Query().Get("coachId"), r.URL.Query().Get("venueId"), region)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []ClassSession{}
	for rows.Next() {
		x, err := scanClass(rows)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) getClass(w http.ResponseWriter, r *http.Request, s Session) {
	x, err := scanClass(a.db.QueryRow(r.Context(), "SELECT "+classColumns+classJoin+" WHERE c.id=$1 AND ($2<>'coach' OR c.coach_id=$3)", r.PathValue("id"), s.User.Role, s.User.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func validClass(x ClassInput) bool {
	return x.VenueID != "" && x.ClassTypeID != "" && x.CoachID != "" && !x.StartsAt.IsZero() && !x.EndsAt.IsZero() && x.StartsAt.Year() >= 1 && x.StartsAt.Year() <= 9999 && x.EndsAt.Year() >= 1 && x.EndsAt.Year() <= 9999 && x.EndsAt.After(x.StartsAt) && x.Capacity > 0 && int64(x.Capacity) <= 2147483647
}
func eligibleClass(ctx context.Context, tx pgx.Tx, x ClassInput, previous *ClassInput) error {
	var active bool
	var role string
	// Share locks serialize eligibility checks with concurrent archive/disable writes.
	if err := tx.QueryRow(ctx, "SELECT active,role FROM users WHERE id=$1 FOR SHARE", x.CoachID).Scan(&active, &role); err != nil {
		return err
	}
	if !active || role != "coach" {
		return pgx.ErrNoRows
	}
	if err := tx.QueryRow(ctx, "SELECT active FROM venues WHERE id=$1 FOR SHARE", x.VenueID).Scan(&active); err != nil {
		return err
	}
	if !active && (previous == nil || previous.VenueID != x.VenueID) {
		return pgx.ErrNoRows
	}
	if err := tx.QueryRow(ctx, "SELECT active FROM class_types WHERE id=$1 FOR SHARE", x.ClassTypeID).Scan(&active); err != nil {
		return err
	}
	if !active && (previous == nil || previous.ClassTypeID != x.ClassTypeID) {
		return pgx.ErrNoRows
	}
	return nil
}
func classConflict(ctx context.Context, tx pgx.Tx, x ClassInput, exceptID string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT c.id FROM class_sessions c CROSS JOIN scheduling_settings st WHERE c.coach_id=$1 AND c.id<>$2 AND c.starts_at < $4::timestamptz + CASE WHEN c.venue_id<>$5 THEN st.travel_gap_minutes*interval '1 minute' ELSE interval '0' END AND c.ends_at + CASE WHEN c.venue_id<>$5 THEN st.travel_gap_minutes*interval '1 minute' ELSE interval '0' END > $3::timestamptz LIMIT 1`, x.CoachID, exceptID, x.StartsAt, x.EndsAt, x.VenueID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}
func checkClass(w http.ResponseWriter, r *http.Request, tx pgx.Tx, x ClassInput, id string, previous *ClassInput) bool {
	if err := eligibleClass(r.Context(), tx, x, previous); errors.Is(err, pgx.ErrNoRows) {
		fail(w, 422, "validation_error", "Choose an active coach, venue and class type")
		return false
	} else if err != nil {
		internal(w, err)
		return false
	}
	conflict, err := classConflict(r.Context(), tx, x, id)
	if err != nil {
		internal(w, err)
		return false
	}
	if conflict != "" {
		fail(w, 409, "scheduling_conflict", "Coach overlap or travel gap conflicts with class "+conflict)
		return false
	}
	return true
}
func insertClass(ctx context.Context, tx pgx.Tx, x ClassInput, series *string) (ClassSession, error) {
	id, err := RandomToken()
	if err != nil {
		return ClassSession{}, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO class_sessions(id,venue_id,class_type_id,coach_id,starts_at,ends_at,capacity,series_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", id, x.VenueID, x.ClassTypeID, x.CoachID, x.StartsAt, x.EndsAt, x.Capacity, series); err != nil {
		return ClassSession{}, err
	}
	return scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1", id))
}
func (a *API) createClass(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input ClassInput
	if !decode(w, r, &input) {
		return
	}
	if !validClass(input) {
		fail(w, 422, "validation_error", "Provide references, RFC3339 start/end times and positive capacity")
		return
	}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	if !checkClass(w, r, tx, input, "", nil) {
		return
	}
	x, err := insertClass(r.Context(), tx, input, nil)
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "class.created", x.ID, x); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}
func (a *API) updateClass(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		ClassInput
		Version int `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validClass(input.ClassInput) || input.Version < 1 {
		fail(w, 422, "validation_error", "Provide valid class details and version")
		return
	}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	before, err := scanClass(tx.QueryRow(r.Context(), "SELECT "+classColumns+classJoin+" WHERE c.id=$1", r.PathValue("id")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if before.Version != input.Version {
		fail(w, 409, "version_conflict", "Class changed; reload before saving")
		return
	}
	if !checkClass(w, r, tx, input.ClassInput, before.ID, &before.ClassInput) {
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE class_sessions SET venue_id=$2,class_type_id=$3,coach_id=$4,starts_at=$5,ends_at=$6,capacity=$7,version=version+1 WHERE id=$1", before.ID, input.VenueID, input.ClassTypeID, input.CoachID, input.StartsAt, input.EndsAt, input.Capacity); err != nil {
		internal(w, err)
		return
	}
	x, err := scanClass(tx.QueryRow(r.Context(), "SELECT "+classColumns+classJoin+" WHERE c.id=$1", before.ID))
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "class.updated", x.ID, map[string]any{"before": before, "after": x}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func expandSeries(input SeriesInput) ([]ClassInput, error) {
	start, end, err := dateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}
	if input.Weekday < 1 || input.Weekday > 7 {
		return nil, fmt.Errorf("weekday must be 1–7")
	}
	startTime, err := time.Parse("15:04", input.StartTime)
	if err != nil || startTime.Format("15:04") != input.StartTime {
		return nil, fmt.Errorf("invalid start time")
	}
	endTime, err := time.Parse("15:04", input.EndTime)
	if err != nil || endTime.Format("15:04") != input.EndTime || !endTime.After(startTime) {
		return nil, fmt.Errorf("end time must be later on the same day")
	}
	items := []ClassInput{}
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		weekday := int(day.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		if weekday != input.Weekday {
			continue
		}
		x := ClassInput{VenueID: input.VenueID, ClassTypeID: input.ClassTypeID, CoachID: input.CoachID, StartsAt: time.Date(day.Year(), day.Month(), day.Day(), startTime.Hour(), startTime.Minute(), 0, 0, hongKong), EndsAt: time.Date(day.Year(), day.Month(), day.Day(), endTime.Hour(), endTime.Minute(), 0, 0, hongKong), Capacity: input.Capacity}
		if !validClass(x) {
			return nil, fmt.Errorf("invalid session references or capacity")
		}
		items = append(items, x)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("range contains no occurrence of selected weekday")
	}
	return items, nil
}

const seriesColumns = "id,venue_id,class_type_id,coach_id,to_char(start_date,'YYYY-MM-DD'),to_char(end_date,'YYYY-MM-DD'),weekday,to_char(start_time,'HH24:MI'),to_char(end_time,'HH24:MI'),capacity"

func scanSeries(row pgx.Row) (ClassSeries, error) {
	var x ClassSeries
	x.Sessions = []ClassSession{}
	err := row.Scan(&x.ID, &x.VenueID, &x.ClassTypeID, &x.CoachID, &x.StartDate, &x.EndDate, &x.Weekday, &x.StartTime, &x.EndTime, &x.Capacity)
	return x, err
}
func seriesSessions(ctx context.Context, tx pgx.Tx, id string) ([]ClassSession, error) {
	rows, err := tx.Query(ctx, "SELECT "+classColumns+classJoin+" WHERE c.series_id=$1 ORDER BY c.starts_at,c.id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ClassSession{}
	for rows.Next() {
		x, err := scanClass(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}
func (a *API) createSeries(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input SeriesInput
	if !decode(w, r, &input) {
		return
	}
	items, err := expandSeries(input)
	if err != nil {
		fail(w, 422, "validation_error", err.Error())
		return
	}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	if err = eligibleClass(r.Context(), tx, items[0], nil); errors.Is(err, pgx.ErrNoRows) {
		fail(w, 422, "validation_error", "Choose an active coach, venue and class type")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO class_series(id,venue_id,class_type_id,coach_id,start_date,end_date,weekday,start_time,end_time,capacity) VALUES($1,$2,$3,$4,$5::date,$6::date,$7,$8::time,$9::time,$10)", id, input.VenueID, input.ClassTypeID, input.CoachID, input.StartDate, input.EndDate, input.Weekday, input.StartTime, input.EndTime, input.Capacity); err != nil {
		internal(w, err)
		return
	}
	x := ClassSeries{ID: id, SeriesInput: input, Sessions: []ClassSession{}}
	for _, session := range items {
		if !checkClass(w, r, tx, session, "", nil) {
			return
		}
		created, err := insertClass(r.Context(), tx, session, &id)
		if err != nil {
			internal(w, err)
			return
		}
		x.Sessions = append(x.Sessions, created)
	}
	if err = audit(r.Context(), tx, s.User.ID, "class_series.created", id, x); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}
func (a *API) getSeries(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	tx, err := a.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	x, err := scanSeries(tx.QueryRow(r.Context(), "SELECT "+seriesColumns+" FROM class_series WHERE id=$1", r.PathValue("id")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Series not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	x.Sessions, err = seriesSessions(r.Context(), tx, x.ID)
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) listSeries(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	start, end, err := dateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		fail(w, 422, "validation_error", "Provide valid Hong Kong dates spanning 1–93 days")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	rows, err := tx.Query(r.Context(), "SELECT "+seriesColumns+" FROM class_series WHERE start_date<$2::date AND end_date>=$1::date ORDER BY start_date,id", start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		internal(w, err)
		return
	}
	items := []ClassSeries{}
	for rows.Next() {
		x, err := scanSeries(rows)
		if err != nil {
			rows.Close()
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	for i := range items {
		items[i].Sessions, err = seriesSessions(r.Context(), tx, items[i].ID)
		if err != nil {
			internal(w, err)
			return
		}
	}
	respond(w, 200, map[string]any{"items": items})
}
