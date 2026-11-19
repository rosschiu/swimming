package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func uploadImport(t *testing.T, h http.Handler, path string, file []byte, contentType string, auth authState) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, bytes.NewReader(file))
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", contentType)
	if auth.cookie != nil {
		r.AddCookie(auth.cookie)
	}
	r.Header.Set("X-CSRF-Token", auth.csrf)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestXLSXRoundTripAndDates(t *testing.T) {
	rows, err := readXLSX(writeXLSX([][]string{{"Student name", "Level"}, {"Ada <&> 泳", "Beginner"}, nil, {"", "", "x"}}))
	if err != nil || len(rows) != 4 || rows[1][0] != "Ada <&> 泳" || rows[3][2] != "x" {
		t.Fatalf("round trip: %q %v", rows, err)
	}
	if _, err := readXLSX([]byte("not a zip")); err == nil {
		t.Fatal("garbage must not parse")
	}
	// Workbooks saved by spreadsheet programs use shared strings, rich-text runs, numeric date cells and absolute paths.
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId7"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId7" Target="/xl/worksheets/data.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Student name</t></si><si><r><t>Le</t></r><r><t>vel</t></r></si><si><t>Credits expire on</t></si><si><t>Bo</t></si></sst>`,
		"xl/worksheets/data.xml":     `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="2"><c r="A2" t="s"><v>0</v></c><c r="B2" t="s"><v>1</v></c><c r="D2" t="s"><v>2</v></c></row><row r="3"><c r="A3" t="s"><v>3</v></c><c r="B3" t="str"><v>Beginner</v></c><c r="D3" s="1"><v>46387</v></c></row></sheetData></worksheet>`,
	} {
		f, _ := z.Create(name)
		f.Write([]byte(body))
	}
	z.Close()
	rows, err = readXLSX(buf.Bytes())
	if err != nil || len(rows) != 3 || len(rows[0]) != 0 || rows[1][1] != "Level" || rows[2][0] != "Bo" || rows[2][3] != "46387" {
		t.Fatalf("shared strings workbook: %q %v", rows, err)
	}
	fixture, err := os.ReadFile("testdata/openpyxl-import.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if rows, err = readXLSX(fixture); err != nil || len(rows) != 5 || rows[1][0] != "Real Excel 泳" || importDate(rows[1][7]) != "2027-03-31" || rows[4][7] != "2027-01-15" {
		t.Fatalf("openpyxl workbook: %q %v", rows, err)
	}
	if got := importDate("46387"); got != "2026-12-31" {
		t.Fatalf("Excel serial date: %s", got)
	}
	if got := importDate("2026-12-31"); got != "2026-12-31" {
		t.Fatalf("text date: %s", got)
	}
}

func TestIntegrationMigrationImport(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	future := time.Now().In(hongKong).AddDate(0, 6, 0).Format("2006-01-02")
	past := time.Now().In(hongKong).AddDate(0, 0, -1).Format("2006-01-02")
	header := []string{"Student name", "Level", "Guardian name", "Guardian email", "Second guardian name", "Second guardian email", "Credits", "Credits expire on", "Old ID"}

	expectStatus(t, uploadImport(t, h, "/api/v1/imports/preview", writeXLSX([][]string{header}), xlsxMediaType, parent), 403)
	expectStatus(t, uploadImport(t, h, "/api/v1/imports/preview", []byte("a,b"), "text/csv", admin), 415)
	expectBookingError(t, uploadImport(t, h, "/api/v1/imports/preview", []byte("not a workbook"), xlsxMediaType, admin), 422, "validation_error")

	// The template downloads as a workbook whose headings the importer recognises.
	w := request(t, h, "GET", "/api/v1/imports/template", nil, admin)
	expectStatus(t, w, 200)
	w = uploadImport(t, h, "/api/v1/imports/preview", w.Body.Bytes(), xlsxMediaType, admin)
	expectStatus(t, w, 200)
	if p := decodeBody[ImportPreview](t, w); !p.Valid || len(p.Rows) != 1 || p.Summary.GuardiansCreated != 1 {
		t.Fatalf("template preview: %+v", p)
	}

	// Chinese headings are accepted; missing required columns are reported.
	w = uploadImport(t, h, "/api/v1/imports/preview", writeXLSX([][]string{{"學員姓名", "程度", "剩餘堂數", "堂數到期日"}, {"小明", "初級", "3", future}}), xlsxMediaType, admin)
	expectStatus(t, w, 200)
	if p := decodeBody[ImportPreview](t, w); !p.Valid || p.Rows[0].Credits != 3 || p.Rows[0].ExpiresOn != future {
		t.Fatalf("Chinese headings: %+v", p)
	}
	w = uploadImport(t, h, "/api/v1/imports/preview", writeXLSX([][]string{{"Name"}, {"x"}}), xlsxMediaType, admin)
	if p := decodeBody[ImportPreview](t, w); p.Valid || len(p.Errors) != 1 {
		t.Fatalf("missing columns: %+v", p)
	}

	// Row problems are reported per row, and a sheet with problems cannot be imported.
	bad := writeXLSX([][]string{header,
		{"Bad email", "Beginner", "G", "not-an-email"},
		{"Staff guardian", "Beginner", "", "coach@swim.test"},
		{"No expiry", "Beginner", "", "", "", "", "4", ""},
		{"Past expiry", "Beginner", "", "", "", "", "4", past},
		{"Amy", "Beginner", "", "parent@swim.test"},
		{"Twin", "Beginner", "New G", "new@swim.test"},
		{"Twin", "Beginner", "New G", "new@swim.test"},
		{"Renamed", "Beginner", "Other name", "new@swim.test"},
	})
	w = uploadImport(t, h, "/api/v1/imports/preview", bad, xlsxMediaType, admin)
	expectStatus(t, w, 200)
	p := decodeBody[ImportPreview](t, w)
	if p.Valid || len(p.Rows) != 8 {
		t.Fatalf("bad sheet: %+v", p)
	}
	for i, want := range []string{"valid email", "staff or student", "expiry date", "today or later", "already has a swimmer", "", "Duplicate of row 7", "different guardian name"} {
		got := strings.Join(p.Rows[i].Errors, " ")
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Fatalf("row %d: want %q, got %q", p.Rows[i].Row, want, got)
		}
	}
	expectStatus(t, uploadImport(t, h, "/api/v1/imports?fileSha256="+p.FileSHA256, bad, xlsxMediaType, admin), 422)

	// A valid migration: siblings share a new guardian, an existing guardian is linked, balances become migrated packages.
	good := writeXLSX([][]string{header,
		{"Mei Wong", "Beginner", "Grace Wong", "grace@swim.test", "", "parent@swim.test", "10", "46387", "A-1"},
		{"Kit Wong", "Improver", "Grace Wong", "Grace@Swim.test", "", "", "3", future},
		{"Lok Chan", "Beginner", "", "", "", "", "0", ""},
	})
	sum := sha256.Sum256(good)
	hash := hex.EncodeToString(sum[:])
	expectBookingError(t, uploadImport(t, h, "/api/v1/imports?fileSha256="+strings.Repeat("0", 64), good, xlsxMediaType, admin), 409, "file_changed")
	var purchasesBefore, receiptsBefore int
	if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM purchases),(SELECT count(*) FROM receipts)").Scan(&purchasesBefore, &receiptsBefore); err != nil {
		t.Fatal(err)
	}
	w = uploadImport(t, h, "/api/v1/imports?fileSha256="+hash, good, xlsxMediaType, admin)
	expectStatus(t, w, 201)
	result := decodeBody[ImportResult](t, w)
	if result.Students != 3 || result.GuardiansCreated != 1 || result.GuardiansLinked != 1 || result.Packages != 2 || result.Credits != 13 {
		t.Fatalf("import result: %+v", result)
	}
	w = uploadImport(t, h, "/api/v1/imports?fileSha256="+hash, good, xlsxMediaType, admin)
	expectStatus(t, w, 200)
	if replay := decodeBody[ImportResult](t, w); replay.ID != result.ID {
		t.Fatalf("replay created another import: %+v", replay)
	}
	var students, purchases, receipts, audits int
	var active bool
	var password *string
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM students WHERE name IN ('Mei Wong','Kit Wong','Lok Chan')),(SELECT count(*) FROM purchases),(SELECT count(*) FROM receipts),
		(SELECT count(*) FROM audit_events WHERE action IN ('import.completed','student.imported','invitation.imported')),u.active,u.password_hash FROM users u WHERE u.email='grace@swim.test'`).
		Scan(&students, &purchases, &receipts, &audits, &active, &password); err != nil {
		t.Fatal(err)
	}
	if students != 3 || purchases != purchasesBefore || receipts != receiptsBefore || audits != 5 || active || password != nil {
		t.Fatalf("imported state: students=%d purchases=%d receipts=%d audits=%d active=%v", students, purchases, receipts, audits, active)
	}

	// The new guardian's invitation waits unsent until an administrator sends it.
	w = request(t, h, "GET", "/api/v1/invitations", nil, admin)
	expectStatus(t, w, 200)
	var invitation Invitation
	for _, item := range decodeBody[struct{ Items []Invitation }](t, w).Items {
		if item.Email == "grace@swim.test" {
			invitation = item
		}
	}
	if invitation.Status != "pending" || invitation.DeliveryStatus != "not_sent" {
		t.Fatalf("imported invitation: %+v", invitation)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations/"+invitation.ID+"/resend", nil, admin), 200)

	// The existing guardian sees the linked swimmer and the migrated balance, labelled as migrated.
	var mei string
	if err := db.QueryRow(ctx, "SELECT id FROM students WHERE name='Mei Wong'").Scan(&mei); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", "/api/v1/students/"+mei+"/credit-history", nil, parent)
	expectStatus(t, w, 200)
	history := decodeBody[struct{ Items []CreditHistoryPackage }](t, w).Items
	if len(history) != 1 || !history[0].Migrated || history[0].Complimentary || history[0].Available != 10 || *history[0].ExpiresOn != "2026-12-31" {
		t.Fatalf("migrated balance: %+v", history)
	}
	if _, err := db.Exec(ctx, "DELETE FROM imports"); err == nil {
		t.Fatal("import history must be immutable")
	}
}
