package app

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// A minimal reader for the first worksheet of an Excel (.xlsx) workbook: enough for a school's migration sheet,
// without a third-party dependency. Cells come back as text; numbers keep Excel's stored form.

var errWorkbook = errors.New("not a readable .xlsx workbook")

const maxWorkbookPart = 20 << 20

func readWorkbookPart(files map[string]*zip.File, name string) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, errWorkbook
	}
	if f.UncompressedSize64 > maxWorkbookPart {
		return nil, errWorkbook
	}
	rc, err := f.Open()
	if err != nil {
		return nil, errWorkbook
	}
	defer rc.Close()
	// The declared size can lie, so the read itself is bounded too.
	data, err := io.ReadAll(io.LimitReader(rc, maxWorkbookPart+1))
	if err != nil || len(data) > maxWorkbookPart {
		return nil, errWorkbook
	}
	return data, nil
}

func readXLSX(data []byte) ([][]string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errWorkbook
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	part, err := readWorkbookPart(files, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var workbook struct {
		Sheets []struct {
			ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(part, &workbook) != nil || len(workbook.Sheets) == 0 {
		return nil, errWorkbook
	}
	if part, err = readWorkbookPart(files, "xl/_rels/workbook.xml.rels"); err != nil {
		return nil, err
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(part, &rels) != nil {
		return nil, errWorkbook
	}
	sheet := ""
	for _, rel := range rels.Items {
		if rel.ID == workbook.Sheets[0].ID {
			sheet = strings.TrimPrefix(rel.Target, "/")
			if !strings.HasPrefix(sheet, "xl/") {
				sheet = path.Join("xl", sheet)
			}
		}
	}
	var shared []string
	if _, ok := files["xl/sharedStrings.xml"]; ok {
		if part, err = readWorkbookPart(files, "xl/sharedStrings.xml"); err != nil {
			return nil, err
		}
		var sst struct {
			Items []struct {
				T string `xml:"t"`
				R []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if xml.Unmarshal(part, &sst) != nil {
			return nil, errWorkbook
		}
		for _, si := range sst.Items {
			text := si.T
			for _, run := range si.R {
				text += run.T
			}
			shared = append(shared, text)
		}
	}
	if part, err = readWorkbookPart(files, sheet); err != nil {
		return nil, err
	}
	var ws struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				Ref    string `xml:"r,attr"`
				Type   string `xml:"t,attr"`
				Value  string `xml:"v"`
				Inline struct {
					T string `xml:"t"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if xml.Unmarshal(part, &ws) != nil {
		return nil, errWorkbook
	}
	var out [][]string
	for i, row := range ws.Rows {
		index := row.R - 1
		if row.R == 0 {
			index = i
		}
		if index < 0 || index >= maxImportRows+1 {
			return nil, errImportTooLarge
		}
		for len(out) <= index {
			out = append(out, nil)
		}
		for j, c := range row.Cells {
			col := j
			if c.Ref != "" {
				if col = columnIndex(c.Ref); col < 0 || col > 50 {
					continue
				}
			}
			value := c.Value
			switch c.Type {
			case "s":
				n, err := strconv.Atoi(c.Value)
				if err != nil || n < 0 || n >= len(shared) {
					return nil, errWorkbook
				}
				value = shared[n]
			case "inlineStr":
				value = c.Inline.T
			}
			for len(out[index]) <= col {
				out[index] = append(out[index], "")
			}
			out[index][col] = value
		}
	}
	return out, nil
}

func columnIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

func columnName(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

// writeXLSX builds a one-sheet workbook with inline text cells, used for the import template and tests.
func writeXLSX(rows [][]string) []byte {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, _ := z.Create(name)
		io.WriteString(w, xml.Header+body)
	}
	add("[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`)
	add("_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	add("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Students" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`)
	var sheet strings.Builder
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for i, row := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, i+1)
		for j, value := range row {
			var text strings.Builder
			xml.EscapeText(&text, []byte(value))
			fmt.Fprintf(&sheet, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, columnName(j), i+1, text.String())
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	add("xl/worksheets/sheet1.xml", sheet.String())
	z.Close()
	return buf.Bytes()
}
