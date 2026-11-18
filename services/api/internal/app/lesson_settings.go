package app

import (
	"net/http"

	"github.com/jackc/pgx/v5"
)

type LessonSettings struct {
	ParentChangeCutoffHours int32 `json:"parentChangeCutoffHours"`
	AttendanceOpenMinutes   int32 `json:"attendanceOpenMinutes"`
	CorrectionWindowDays    int32 `json:"correctionWindowDays"`
	// CancellationEmail adds a generic family email to in-app cancellation notices.
	CancellationEmail bool  `json:"cancellationEmail"`
	Version           int32 `json:"version"`
}

func (a *API) lessonSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/lessons", a.auth(a.getLessonSettings))
	mux.HandleFunc("PATCH /api/v1/settings/lessons", a.auth(a.updateLessonSettings))
}

const (
	lessonSettingsColumns      = "parent_change_cutoff_hours,attendance_open_minutes,correction_window_days,cancellation_email,version"
	maxParentChangeCutoffHours = 8760
	maxAttendanceOpenMinutes   = 1440
	maxCorrectionWindowDays    = 365
)

func scanLessonSettings(row pgx.Row) (LessonSettings, error) {
	var x LessonSettings
	err := row.Scan(&x.ParentChangeCutoffHours, &x.AttendanceOpenMinutes, &x.CorrectionWindowDays, &x.CancellationEmail, &x.Version)
	return x, err
}

func (a *API) getLessonSettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	x, err := scanLessonSettings(tx.QueryRow(ctx, "SELECT "+lessonSettingsColumns+" FROM lesson_settings WHERE id"))
	if err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}

func (a *API) updateLessonSettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var in struct {
		ParentChangeCutoffHours *int32 `json:"parentChangeCutoffHours"`
		AttendanceOpenMinutes   *int32 `json:"attendanceOpenMinutes"`
		CorrectionWindowDays    *int32 `json:"correctionWindowDays"`
		CancellationEmail       *bool  `json:"cancellationEmail"`
		Version                 int32  `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Version < 1 || in.ParentChangeCutoffHours == nil || *in.ParentChangeCutoffHours < 0 || *in.ParentChangeCutoffHours > maxParentChangeCutoffHours ||
		in.AttendanceOpenMinutes == nil || *in.AttendanceOpenMinutes < 0 || *in.AttendanceOpenMinutes > maxAttendanceOpenMinutes ||
		in.CorrectionWindowDays == nil || *in.CorrectionWindowDays < 0 || *in.CorrectionWindowDays > maxCorrectionWindowDays {
		fail(w, 422, "validation_error", "Provide 0–8760 cutoff hours, 0–1440 attendance minutes, 0–365 correction days and a valid version")
		return
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	before, err := scanLessonSettings(tx.QueryRow(ctx, "SELECT "+lessonSettingsColumns+" FROM lesson_settings WHERE id FOR UPDATE"))
	if err != nil {
		internal(w, err)
		return
	}
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	if before.Version != in.Version {
		fail(w, 409, "version_conflict", "Lesson settings changed; reload before saving")
		return
	}
	x, err := scanLessonSettings(tx.QueryRow(ctx, `UPDATE lesson_settings
		SET parent_change_cutoff_hours=$1,attendance_open_minutes=$2,correction_window_days=$3,cancellation_email=COALESCE($4,cancellation_email),version=version+1
		WHERE id RETURNING `+lessonSettingsColumns, *in.ParentChangeCutoffHours, *in.AttendanceOpenMinutes, *in.CorrectionWindowDays, in.CancellationEmail))
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "lesson.settings.updated", "lessons", map[string]any{"before": before, "after": x})
	}
	if err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
