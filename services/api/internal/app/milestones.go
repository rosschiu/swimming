package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
type MilestoneEvent struct {
	ID               string    `json:"id"`
	StudentID        string    `json:"studentId"`
	SkillID          string    `json:"skillId"`
	State            string    `json:"state"`
	Reason           string    `json:"reason"`
	Version          int       `json:"version"`
	SkillVersion     int       `json:"skillVersion"`
	SkillName        string    `json:"skillName"`
	SkillDescription string    `json:"skillDescription"`
	ActorID          string    `json:"actorId"`
	ActorName        string    `json:"actorName"`
	RecordedAt       time.Time `json:"recordedAt"`
	ReversesEventID  *string   `json:"reversesEventId"`
}
type StudentMilestone struct {
	Skill      Skill            `json:"skill"`
	Current    *MilestoneEvent  `json:"current"`
	History    []MilestoneEvent `json:"history"`
	CanAchieve bool             `json:"canAchieve"`
	CanRevoke  bool             `json:"canRevoke"`
}

const skillColumns = "id,name,description,active,version,created_at,updated_at"
const milestoneColumns = "e.id,e.student_id,e.skill_id,e.state,e.reason,e.version,e.skill_version,e.skill_name,e.skill_description,e.actor_id,u.name,e.recorded_at,e.reverses_event_id"
const milestoneJoin = " FROM milestone_events e JOIN users u ON u.id=e.actor_id"

func scanSkill(row pgx.Row) (Skill, error) {
	var x Skill
	err := row.Scan(&x.ID, &x.Name, &x.Description, &x.Active, &x.Version, &x.CreatedAt, &x.UpdatedAt)
	return x, err
}
func scanMilestone(row pgx.Row) (MilestoneEvent, error) {
	var x MilestoneEvent
	err := row.Scan(&x.ID, &x.StudentID, &x.SkillID, &x.State, &x.Reason, &x.Version, &x.SkillVersion, &x.SkillName, &x.SkillDescription, &x.ActorID, &x.ActorName, &x.RecordedAt, &x.ReversesEventID)
	return x, err
}
func validOptionalProgress(value string, limit int) bool {
	return value == "" || validProgressText(value, limit)
}
func validMilestoneRequestID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func (a *API) milestoneRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/skills", a.auth(a.listSkills))
	mux.HandleFunc("POST /api/v1/skills", a.auth(a.createSkill))
	mux.HandleFunc("PATCH /api/v1/skills/{skillId}", a.auth(a.updateSkill))
	mux.HandleFunc("GET /api/v1/students/{id}/milestones", a.auth(a.listStudentMilestones))
	mux.HandleFunc("POST /api/v1/students/{id}/milestones/{skillId}/events", a.auth(a.createMilestoneEvent))
}
func (a *API) skillAdminAccess(w http.ResponseWriter, r *http.Request, s Session, tx pgx.Tx) bool {
	var active bool
	if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active AND role=$2)", s.User.ID, s.User.Role).Scan(&active); err != nil {
		internal(w, err)
		return false
	}
	if !active {
		fail(w, 401, "unauthenticated", "Sign in again")
		return false
	}
	return admin(w, s)
}
func (a *API) listSkills(w http.ResponseWriter, r *http.Request, s Session) {
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.skillAdminAccess(w, r, s, tx) {
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT "+skillColumns+" FROM skills ORDER BY name,id")
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []Skill{}
	for rows.Next() {
		x, err := scanSkill(rows)
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
func (a *API) createSkill(w http.ResponseWriter, r *http.Request, s Session) {
	var in struct {
		RequestID   string  `json:"requestId"`
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	descriptionInput := ""
	if in.Description != nil {
		descriptionInput = strings.TrimSpace(*in.Description)
	}
	if in.Description == nil || !validMilestoneRequestID(in.RequestID) || !validProgressText(in.Name, 100) || !validOptionalProgress(descriptionInput, 1000) {
		fail(w, 422, "validation_error", "Provide a skill name of 1–100 characters, description up to1000 characters and valid request ID")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.skillAdminAccess(w, r, s, tx) {
		return
	}
	var id, name, description string
	err = tx.QueryRow(r.Context(), "SELECT id,original_name,original_description FROM skills WHERE created_by_id=$1 AND request_id=$2", s.User.ID, in.RequestID).Scan(&id, &name, &description)
	if err == nil {
		if name != in.Name || description != descriptionInput {
			fail(w, 409, "request_conflict", "This request ID was used for different skill details")
			return
		}
		x, err := scanSkill(tx.QueryRow(r.Context(), "SELECT "+skillColumns+" FROM skills WHERE id=$1", id))
		if err != nil {
			internal(w, err)
			return
		}
		respond(w, 200, x)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	id, err = RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	x, err := scanSkill(tx.QueryRow(r.Context(), "INSERT INTO skills(id,name,description,created_by_id,request_id,original_name,original_description) VALUES($1,$2,$3,$4,$5,$2,$3) RETURNING "+skillColumns, id, in.Name, descriptionInput, s.User.ID, in.RequestID))
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "skill.created", x.ID, x); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}
func (a *API) updateSkill(w http.ResponseWriter, r *http.Request, s Session) {
	var in struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Active      *bool   `json:"active"`
		Version     int     `json:"version"`
		Reason      string  `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	descriptionInput := ""
	if in.Description != nil {
		descriptionInput = strings.TrimSpace(*in.Description)
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Description == nil || in.Active == nil || in.Version < 1 || !validProgressText(in.Name, 100) || !validOptionalProgress(descriptionInput, 1000) || !validProgressText(in.Reason, 500) {
		fail(w, 422, "validation_error", "Provide skill details, active status, current version and reason of 1–500 characters")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.skillAdminAccess(w, r, s, tx) {
		return
	}
	before, err := scanSkill(tx.QueryRow(r.Context(), "SELECT "+skillColumns+" FROM skills WHERE id=$1", r.PathValue("skillId")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Skill not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if before.Version != in.Version {
		fail(w, 409, "version_conflict", "This skill changed; reload and review it")
		return
	}
	x, err := scanSkill(tx.QueryRow(r.Context(), "UPDATE skills SET name=$2,description=$3,active=$4,version=version+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING "+skillColumns, before.ID, in.Name, descriptionInput, *in.Active))
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "skill.updated", x.ID, map[string]any{"before": before, "after": x, "reason": in.Reason}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) listStudentMilestones(w http.ResponseWriter, r *http.Request, s Session) {
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.progressAccess(w, r, s, tx, false) {
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT "+skillColumns+" FROM skills WHERE active OR EXISTS(SELECT 1 FROM milestone_events e WHERE e.skill_id=skills.id AND e.student_id=$1) ORDER BY name,id", r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	items := []StudentMilestone{}
	for rows.Next() {
		x, err := scanSkill(rows)
		if err != nil {
			rows.Close()
			internal(w, err)
			return
		}
		items = append(items, StudentMilestone{Skill: x, History: []MilestoneEvent{}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		internal(w, err)
		return
	}
	staff := s.User.Role == "admin" || s.User.Role == "coach"
	for i := range items {
		item := &items[i]
		rows, err := tx.Query(r.Context(), "SELECT "+milestoneColumns+milestoneJoin+" WHERE e.student_id=$1 AND e.skill_id=$2 ORDER BY e.version DESC", r.PathValue("id"), item.Skill.ID)
		if err != nil {
			internal(w, err)
			return
		}
		for rows.Next() {
			e, err := scanMilestone(rows)
			if err != nil {
				rows.Close()
				internal(w, err)
				return
			}
			item.History = append(item.History, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			internal(w, err)
			return
		}
		if len(item.History) > 0 {
			item.Current = &item.History[0]
		}
		item.CanAchieve = staff && item.Skill.Active && (item.Current == nil || item.Current.State == "revoked")
		item.CanRevoke = staff && item.Current != nil && item.Current.State == "achieved" && (s.User.Role == "admin" || item.Current.ActorID == s.User.ID)
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) createMilestoneEvent(w http.ResponseWriter, r *http.Request, s Session) {
	var in struct {
		RequestID    string  `json:"requestId"`
		State        string  `json:"state"`
		Version      *int    `json:"version"`
		SkillVersion int     `json:"skillVersion"`
		Reason       *string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	reason := ""
	if in.Reason != nil {
		reason = strings.TrimSpace(*in.Reason)
	}
	if !validMilestoneRequestID(in.RequestID) || (in.State != "achieved" && in.State != "revoked") || in.Version == nil || *in.Version < 0 || in.SkillVersion < 1 || in.Reason == nil || !validOptionalProgress(reason, 500) || (in.State == "revoked" && reason == "") {
		fail(w, 422, "validation_error", "Provide a state, current milestone and reviewed skill versions, valid request ID, and revocation reason of 1–500 characters")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.progressAccess(w, r, s, tx, true) {
		return
	}
	var id, studentID, skillID, state, originalReason string
	var version, skillVersion int
	err = tx.QueryRow(r.Context(), "SELECT id,student_id,skill_id,state,reason,submitted_version,submitted_skill_version FROM milestone_events WHERE actor_id=$1 AND request_id=$2", s.User.ID, in.RequestID).Scan(&id, &studentID, &skillID, &state, &originalReason, &version, &skillVersion)
	if err == nil {
		if studentID != r.PathValue("id") || skillID != r.PathValue("skillId") || state != in.State || originalReason != reason || version != *in.Version || skillVersion != in.SkillVersion {
			fail(w, 409, "request_conflict", "This request ID was used for different milestone details")
			return
		}
		e, err := scanMilestone(tx.QueryRow(r.Context(), "SELECT "+milestoneColumns+milestoneJoin+" WHERE e.id=$1", id))
		if err != nil {
			internal(w, err)
			return
		}
		respond(w, 200, e)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	skill, err := scanSkill(tx.QueryRow(r.Context(), "SELECT "+skillColumns+" FROM skills WHERE id=$1", r.PathValue("skillId")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Skill not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	current, err := scanMilestone(tx.QueryRow(r.Context(), "SELECT "+milestoneColumns+milestoneJoin+" WHERE e.student_id=$1 AND e.skill_id=$2 ORDER BY e.version DESC LIMIT 1", r.PathValue("id"), skill.ID))
	hasCurrent := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	currentVersion := 0
	if hasCurrent {
		currentVersion = current.Version
	}
	if currentVersion != *in.Version {
		fail(w, 409, "version_conflict", "This milestone changed; reload and review it")
		return
	}
	var reverses *string
	if in.State == "achieved" {
		if !skill.Active {
			fail(w, 409, "skill_archived", "This skill is archived")
			return
		}
		if in.SkillVersion != skill.Version {
			fail(w, 409, "skill_version_conflict", "This skill definition changed; reload and review it")
			return
		}
		if hasCurrent && current.State == "achieved" {
			fail(w, 409, "state_conflict", "This skill is already achieved")
			return
		}
	} else {
		if !hasCurrent || current.State != "achieved" {
			fail(w, 409, "state_conflict", "There is no current achievement to revoke")
			return
		}
		if s.User.Role != "admin" && current.ActorID != s.User.ID {
			fail(w, 403, "forbidden", "Only the achievement author or an administrator may revoke it")
			return
		}
		reverses = &current.ID
		skill.Version = current.SkillVersion
		skill.Name = current.SkillName
		skill.Description = current.SkillDescription
	}
	id, err = RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), "INSERT INTO milestone_events(id,student_id,skill_id,actor_id,state,reason,version,skill_version,skill_name,skill_description,reverses_event_id,request_id,submitted_version,submitted_skill_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)", id, r.PathValue("id"), skill.ID, s.User.ID, in.State, reason, currentVersion+1, skill.Version, skill.Name, skill.Description, reverses, in.RequestID, *in.Version, in.SkillVersion)
	if err != nil {
		internal(w, err)
		return
	}
	e, err := scanMilestone(tx.QueryRow(r.Context(), "SELECT "+milestoneColumns+milestoneJoin+" WHERE e.id=$1", id))
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "milestone."+in.State, e.ID, e); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, e)
}
