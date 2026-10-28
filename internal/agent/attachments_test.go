// Created by nc.ai 0.9.1 on new-admin-pc 2026-10-28 11:01:57
package agent

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractXlsx(t *testing.T) {
	data := buildZip(t, map[string]string{
		"xl/sharedStrings.xml": `<?xml version="1.0"?><sst><si><t>Item</t></si><si><r><t>Цена</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet><sheetData><row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row><row><c t="inlineStr"><is><t>100</t></is></c></row></sheetData></worksheet>`,
	})
	got := extractAttachmentText("budget.xlsx", data)
	for _, want := range []string{"Item", "Цена", "100"} {
		if !strings.Contains(got, want) {
			t.Fatalf("extract xlsx: missing %q in:\n%s", want, got)
		}
	}
}

func TestExtractDocx(t *testing.T) {
	data := buildZip(t, map[string]string{
		"word/document.xml": `<w:document><w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p><w:p><w:r><w:t>World</w:t></w:r></w:p></w:body></w:document>`,
	})
	got := extractAttachmentText("note.docx", data)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Fatalf("extract docx: got %q", got)
	}
}

func TestExtractODT(t *testing.T) {
	data := buildZip(t, map[string]string{
		"content.xml": `<office:document-content><office:body><office:text><text:p>Привет</text:p><text:h>Заголовок</text:h></office:text></office:body></office:document-content>`,
	})
	got := extractAttachmentText("doc.odt", data)
	for _, want := range []string{"Привет", "Заголовок"} {
		if !strings.Contains(got, want) {
			t.Fatalf("extract odt: missing %q in:\n%s", want, got)
		}
	}
}

func TestExtractUnsupportedAndOversize(t *testing.T) {
	if got := extractAttachmentText("blob.pdf", []byte("x")); got != "" {
		t.Fatalf("pdf must not be extracted, got %q", got)
	}
	big := make([]byte, maxAttachmentExtractBytes+1)
	if got := extractAttachmentText("big.xlsx", big); got != "" {
		t.Fatalf("oversize file must be skipped, got %q", got)
	}
}
