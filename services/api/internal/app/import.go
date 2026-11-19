package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The migration import moves an existing school's swimmers, guardians and remaining credits into the system from
// one Excel sheet with one row per swimmer. An administrator previews the file, then imports it all or nothing.
// Guardians get accounts without any email; administrators send their invitations when ready.

const (
	maxImportBytes = 5 << 20
	maxImportRows  = 2000
	xlsxMediaType  = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

var errImportTooLarge = errors.New("too many rows")

type importColumn struct{ key, en, zh string }

var importColumns = []importColumn{
	{"student", "Student name", "學員姓名"},
	{"level", "Level", "程度"},
	{"guardian", "Guardian name", "家長姓名"},
	{"guardianEmail", "Guardian email", "家長電郵"},
	{"guardian2", "Second guardian name", "第二位家長姓名"},
	{"guardian2Email", "Second guardian email", "第二位家長電郵"},
	{"credits", "Credits", "剩餘堂數"},
	{"expiresOn", "Credits expire on", "堂數到期日"},
}

type ImportGuardian struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Existing bool   `json:"existing"`
}

type ImportRow struct {
	Row         int              `json:"row"`
	StudentName string           `json:"studentName"`
	Level       string           `json:"level"`
	Guardians   []ImportGuardian `json:"guardians"`
	Credits     int              `json:"credits"`
	ExpiresOn   string           `json:"expiresOn,omitempty"`
	Errors      []string         `json:"errors"`
}

type ImportSummary struct {
	Students         int `json:"students"`
	GuardiansCreated int `json:"guardiansCreated"`
	GuardiansLinked  int `json:"guardiansLinked"`
	Packages         int `json:"packages"`
	Credits          int `json:"credits"`
}

type ImportPreview struct {
	FileSHA256     string        `json:"fileSha256"`
	Valid          bool          `json:"valid"`
	Errors         []string      `json:"errors"`
	IgnoredColumns []string      `json:"ignoredColumns"`
	Rows           []ImportRow   `json:"rows"`
	Summary        ImportSummary `json:"summary"`
}

type ImportResult struct {
	ID         string `json:"id"`
	FileSHA256 string `json:"fileSha256"`
	ImportSummary
	CreatedAt time.Time `json:"createdAt"`
}

func (a *API) importRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/imports/template", a.auth(a.importTemplate))
	mux.HandleFunc("POST /api/v1/imports/preview", a.auth(a.previewImport))
	mux.HandleFunc("POST /api/v1/imports", a.auth(a.runImport))
}

func (a *API) importTemplate(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	header := make([]string, len(importColumns))
	for i, c := range importColumns {
		header[i] = c.en
	}
	example := []string{"Example Swimmer", "Beginner", "Example Guardian", "guardian@example.com", "", "", "8", a.now().In(hongKong).AddDate(0, 6, 0).Format("2006-01-02")}
	w.Header().Set("Content-Type", xlsxMediaType)
	w.Header().Set("Content-Disposition", `attachment; filename="swimcoach-import-template.xlsx"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(writeXLSX([][]string{header, example}))
}

// readImportFile reads the uploaded workbook body; it writes the error response itself.
func readImportFile(w http.ResponseWriter, r *http.Request) ([]byte, [][]string, bool) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), xlsxMediaType) {
		fail(w, 415, "unsupported_media_type", "Upload an Excel (.xlsx) file")
		return nil, nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	if err != nil {
		fail(w, 413, "too_large", "The file must be 5 MB or smaller")
		return nil, nil, false
	}
	rows, err := readXLSX(data)
	if errors.Is(err, errImportTooLarge) {
		fail(w, 422, "validation_error", "The sheet can hold at most 2000 swimmers")
		return nil, nil, false
	}
	if err != nil {
		fail(w, 422, "validation_error", "The file is not a readable Excel (.xlsx) workbook")
		return nil, nil, false
	}
	return data, rows, true
}

type existingAccount struct {
	id, role string
}

// buildImportPreview validates every row against the sheet itself and the current accounts and students.
func (a *API) buildImportPreview(ctx context.Context, tx pgx.Tx, data []byte, sheet [][]string) (ImportPreview, error) {
	sum := sha256.Sum256(data)
	p := ImportPreview{FileSHA256: hex.EncodeToString(sum[:]), Errors: []string{}, IgnoredColumns: []string{}, Rows: []ImportRow{}}
	start := 0
	for start < len(sheet) && blankRow(sheet[start]) {
		start++
	}
	if start == len(sheet) {
		p.Errors = append(p.Errors, "The sheet is empty. Use the template's column headings in the first row.")
		return p, nil
	}
	columns := map[string]int{}
	for i, heading := range sheet[start] {
		heading = strings.TrimSpace(heading)
		if heading == "" {
			continue
		}
		known := false
		for _, c := range importColumns {
			if strings.EqualFold(heading, c.en) || heading == c.zh {
				if _, dup := columns[c.key]; dup {
					p.Errors = append(p.Errors, "The column \""+heading+"\" appears twice.")
				}
				columns[c.key], known = i, true
			}
		}
		if !known {
			p.IgnoredColumns = append(p.IgnoredColumns, heading)
		}
	}
	for _, key := range []string{"student", "level"} {
		if _, ok := columns[key]; !ok {
			p.Errors = append(p.Errors, "Add the required columns \"Student name\" and \"Level\" to the first row.")
			return p, nil
		}
	}
	cell := func(row []string, key string) string {
		i, ok := columns[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	today := a.now().In(hongKong).Format("2006-01-02")
	accounts := map[string]*existingAccount{}
	guardianNames := map[string]string{}
	seen := map[string]int{}
	for index := start + 1; index < len(sheet); index++ {
		raw := sheet[index]
		if blankRow(raw) {
			continue
		}
		row := ImportRow{Row: index + 1, StudentName: cell(raw, "student"), Level: cell(raw, "level"), Guardians: []ImportGuardian{}, Errors: []string{}}
		problem := func(message string) { row.Errors = append(row.Errors, message) }
		if !validName(row.StudentName) {
			problem("Student name must be 1–100 characters.")
		}
		if !validName(row.Level) {
			problem("Level must be 1–100 characters.")
		}
		for _, g := range [][2]string{{"guardian", "guardianEmail"}, {"guardian2", "guardian2Email"}} {
			name, email := cell(raw, g[0]), strings.ToLower(cell(raw, g[1]))
			if name == "" && email == "" {
				continue
			}
			guardian := ImportGuardian{Name: name, Email: email}
			address, err := mail.ParseAddress(email)
			if email == "" || err != nil || address.Address != email || len(email) > 254 {
				problem("Each guardian needs a valid email address.")
				continue
			}
			account, ok := accounts[email]
			if !ok {
				var x existingAccount
				err = tx.QueryRow(ctx, "SELECT id,role FROM users WHERE email=$1", email).Scan(&x.id, &x.role)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return p, err
				}
				if err == nil {
					account = &x
				}
				accounts[email] = account
			}
			if account != nil {
				guardian.Existing = true
				if account.role != "parent" {
					problem(email + " belongs to a staff or student account and cannot be a guardian.")
				}
			} else if !validName(name) {
				problem("A new guardian (" + email + ") needs a name of 1–100 characters.")
			} else if earlier, ok := guardianNames[email]; ok && earlier != name {
				problem(email + " has a different guardian name on another row.")
			} else {
				guardianNames[email] = name
			}
			for _, other := range row.Guardians {
				if other.Email == email {
					problem("The two guardians on a row must be different people.")
				}
			}
			row.Guardians = append(row.Guardians, guardian)
		}
		if text := cell(raw, "credits"); text != "" && text != "0" {
			credits, err := strconv.Atoi(text)
			if err != nil || credits < 1 || credits > 1000 {
				problem("Credits must be a whole number from 0 to 1000.")
			}
			row.Credits = credits
			row.ExpiresOn = importDate(cell(raw, "expiresOn"))
			if row.ExpiresOn == "" {
				problem("Remaining credits need an expiry date (YYYY-MM-DD).")
			} else if !validFutureDate(row.ExpiresOn, today) {
				problem("The credit expiry must be a real date, today or later.")
			}
		}
		key := strings.ToLower(row.StudentName) + "\x00"
		for _, g := range row.Guardians {
			key += g.Email + ","
		}
		if first, ok := seen[key]; ok {
			problem("Duplicate of row " + strconv.Itoa(first) + ".")
		} else {
			seen[key] = row.Row
		}
		for _, g := range row.Guardians {
			if account := accounts[g.Email]; account != nil && account.role == "parent" {
				var exists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students st JOIN student_access sa ON sa.student_id=st.id
					WHERE sa.user_id=$1 AND sa.kind='guardian' AND lower(st.name)=lower($2))`, account.id, row.StudentName).Scan(&exists); err != nil {
					return p, err
				}
				if exists {
					problem(g.Email + " already has a swimmer named " + row.StudentName + " in the system.")
				}
			}
		}
		p.Rows = append(p.Rows, row)
	}
	if len(p.Rows) == 0 {
		p.Errors = append(p.Errors, "The sheet has headings but no swimmers.")
	}
	created, linked := map[string]bool{}, map[string]bool{}
	p.Valid = len(p.Errors) == 0
	for _, row := range p.Rows {
		if len(row.Errors) > 0 {
			p.Valid = false
		}
		p.Summary.Students++
		if row.Credits > 0 {
			p.Summary.Packages++
			p.Summary.Credits += row.Credits
		}
		for _, g := range row.Guardians {
			if g.Existing {
				linked[g.Email] = true
			} else {
				created[g.Email] = true
			}
		}
	}
	p.Summary.GuardiansCreated, p.Summary.GuardiansLinked = len(created), len(linked)
	return p, nil
}

func blankRow(row []string) bool {
	for _, v := range row {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// importDate accepts YYYY-MM-DD text or an Excel date serial number.
func importDate(value string) string {
	if serial, err := strconv.ParseFloat(value, 64); err == nil && serial >= 1 && serial < 2958466 && !strings.Contains(value, "-") {
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(serial)).Format("2006-01-02")
	}
	return value
}

func (a *API) previewImport(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	data, sheet, ok := readImportFile(w, r)
	if !ok {
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
	preview, err := a.buildImportPreview(ctx, tx, data, sheet)
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, preview)
}

// runImport imports the previewed file all or nothing. The client sends the hash it previewed, so a changed file is
// never imported unseen; importing the same file again returns the first result.
func (a *API) runImport(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	data, sheet, ok := readImportFile(w, r)
	if !ok {
		return
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if r.URL.Query().Get("fileSha256") != hash {
		fail(w, 409, "file_changed", "This file differs from the one previewed; preview it again")
		return
	}
	ctx := r.Context()
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		internal(w, err)
		return
	}
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var replay string
	err = tx.QueryRow(ctx, "SELECT id FROM imports WHERE file_sha256=$1", hash).Scan(&replay)
	if err == nil {
		respondImport(w, r, tx, replay, 200)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	preview, err := a.buildImportPreview(ctx, tx, data, sheet)
	if err != nil {
		internal(w, err)
		return
	}
	if !preview.Valid {
		respond(w, 422, map[string]any{"error": map[string]string{"code": "validation_error", "message": "The file has problems; preview it again"}, "preview": preview})
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	importID, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	var policy []byte
	if err = tx.QueryRow(ctx, "SELECT policy FROM credit_settings").Scan(&policy); err != nil {
		internal(w, err)
		return
	}
	sm := preview.Summary
	if _, err = tx.Exec(ctx, "INSERT INTO imports(id,file_sha256,actor_id,created_at,students,guardians_created,guardians_linked,packages,credits) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)",
		importID, hash, s.User.ID, now, sm.Students, sm.GuardiansCreated, sm.GuardiansLinked, sm.Packages, sm.Credits); err != nil {
		internal(w, err)
		return
	}
	guardians := map[string]string{}
	for _, row := range preview.Rows {
		if err = a.importRow(ctx, tx, s.User.ID, importID, row, guardians, policy, now); err != nil {
			internal(w, err)
			return
		}
	}
	if err = audit(ctx, tx, s.User.ID, "import.completed", importID, map[string]any{"fileSha256": hash, "summary": sm}); err != nil {
		internal(w, err)
		return
	}
	respondImport(w, r, tx, importID, 201)
}

func (a *API) importRow(ctx context.Context, tx pgx.Tx, actor, importID string, row ImportRow, guardians map[string]string, policy []byte, now time.Time) error {
	studentID, err := RandomToken()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO students(id,name,level) VALUES($1,$2,$3)", studentID, row.StudentName, row.Level); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "student.imported", studentID, map[string]any{"importId": importID, "row": row.Row, "name": row.StudentName, "level": row.Level}); err != nil {
		return err
	}
	for _, g := range row.Guardians {
		userID, ok := guardians[g.Email]
		if !ok {
			if g.Existing {
				err = tx.QueryRow(ctx, "SELECT id FROM users WHERE email=$1 AND role='parent'", g.Email).Scan(&userID)
			} else if userID, err = RandomToken(); err == nil {
				// The account waits, inactive and without a password, until an administrator sends its invitation.
				invitationID, tokenErr := RandomToken()
				if err = tokenErr; err == nil {
					_, err = tx.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash,active) VALUES($1,$2,$3,'parent',NULL,false)", userID, g.Email, g.Name)
				}
				if err == nil {
					_, err = tx.Exec(ctx, "INSERT INTO invitations(id,user_id,status,expires_at,created_by) VALUES($1,$2,'pending',$3,$4)", invitationID, userID, now, actor)
				}
				if err == nil {
					err = audit(ctx, tx, actor, "invitation.imported", invitationID, map[string]string{"userId": userID, "importId": importID})
				}
			}
			if err != nil {
				return err
			}
			guardians[g.Email] = userID
		}
		if _, err = tx.Exec(ctx, "INSERT INTO student_access(student_id,user_id,kind) VALUES($1,$2,'guardian')", studentID, userID); err != nil {
			return err
		}
	}
	if row.Credits == 0 {
		return nil
	}
	grantID, err := RandomToken()
	packageID, packageErr := RandomToken()
	if err == nil {
		err = packageErr
	}
	if err == nil {
		request := []byte(`{"importId":"` + importID + `","row":` + strconv.Itoa(row.Row) + `}`)
		_, err = tx.Exec(ctx, `INSERT INTO credit_grants(id,student_id,credits,expires_on,reason,actor_id,created_at,idempotency_key,request,kind,import_id)
			VALUES($1,$2,$3,$4,'Migrated balance',$5,$6,$7,$8,'migrated',$9)`, grantID, studentID, row.Credits, row.ExpiresOn, actor, now, "import:"+importID[:32]+":"+strconv.Itoa(row.Row), request, importID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO credit_packages(id,grant_id,student_id,credits,expires_on,policy,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)",
			packageID, grantID, studentID, row.Credits, row.ExpiresOn, policy, now)
	}
	if err == nil {
		_, err = insertMovement(ctx, tx, packageID, "grant:"+grantID, row.Credits, 0, 0, 0, actor, "Migrated balance", now)
	}
	return err
}

func respondImport(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id string, status int) {
	var x ImportResult
	err := tx.QueryRow(r.Context(), "SELECT id,file_sha256,students,guardians_created,guardians_linked,packages,credits,created_at FROM imports WHERE id=$1", id).
		Scan(&x.ID, &x.FileSHA256, &x.Students, &x.GuardiansCreated, &x.GuardiansLinked, &x.Packages, &x.Credits, &x.CreatedAt)
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, status, x)
}
