package csv

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
)

func testCtx() context.Context {
	return utils.WithLogger(context.Background(), logrus.New())
}

// writeXlsxWithSheetName writes a minimal valid XLSX whose single worksheet
// carries the given name verbatim. excelize's writer API sanitises sheet
// names, but a parser must cope with whatever a hostile producer embeds in
// xl/workbook.xml, so the zip is assembled by hand.
func writeXlsxWithSheetName(t *testing.T, path, sheetName string) {
	t.Helper()

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + sheetName + `" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create xlsx: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

// A hostile downloaded workbook must not be able to write (or truncate) files
// outside the download directory via its sheet names, and the scratch CSV
// must not linger afterwards.
func TestParseFromExcelTraversalSheetName(t *testing.T) {
	dir := t.TempDir()
	traversalName := "../../../../audit-escape"
	download := filepath.Join(dir, "statement.xlsx")
	writeXlsxWithSheetName(t, download, traversalName)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	escapePath := filepath.Join(cwd, traversalName+".csv")
	if _, err := os.Stat(escapePath); err == nil {
		t.Fatalf("precondition: %s already exists, refusing to test", escapePath)
	}

	p := NewParser(testCtx(), nil, &FieldConfig{}, false)
	_, _ = p.ParseFromExcel(download, 0, "")

	if _, err := os.Stat(escapePath); err == nil {
		t.Errorf("sheet name escaped the download directory: %s was created", escapePath)
		os.Remove(escapePath)
	}

	leftovers, _ := filepath.Glob(filepath.Join(dir, "firefly-sheet-*.csv"))
	if len(leftovers) > 0 {
		t.Errorf("scratch CSVs left behind: %v", leftovers)
	}
}

// A config-derived worksheet index beyond what the downloaded file actually
// contains must surface as an error, not a panic that kills the sync run.
func TestParseFromExcelWorksheetIndexOutOfRange(t *testing.T) {
	dir := t.TempDir()
	download := filepath.Join(dir, "statement.xlsx")
	writeXlsxWithSheetName(t, download, "Only")

	p := NewParser(testCtx(), nil, &FieldConfig{}, false)

	for _, idx := range []int{-1, 1, 99} {
		_, err := p.ParseFromExcel(download, idx, "")
		if err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("index %d: got err %v, want out-of-range error", idx, err)
		}
	}
}

// A corrupt workbook served by the remote must return an error, not panic.
func TestParseFromExcelCorruptFile(t *testing.T) {
	dir := t.TempDir()
	download := filepath.Join(dir, "statement.xlsx")
	if err := os.WriteFile(download, []byte("this is not a zip file"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	p := NewParser(testCtx(), nil, &FieldConfig{}, false)
	if _, err := p.ParseFromExcel(download, 0, ""); err == nil {
		t.Error("corrupt workbook: got nil error, want failure")
	}
}
