// Created by nc.ai 0.9.1 on new-admin-pc 2026-10-28 11:01:01
package agent

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// newDataTag возвращает случайное имя тега для недоверенного контента. Случайность
// на каждый прогон не даёт атакующему закрыть тег заранее заготовленным </…>.
func newDataTag() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "data-untrusted"
	}
	return "data-" + hex.EncodeToString(b[:])
}

// maxAttachmentExtractBytes caps the raw container size we unpack into the
// prompt. Office files beyond this are left as a name-only hint: a 5 MB sheet
// is already far past "glance at it".
const maxAttachmentExtractBytes = 5 << 20

// decodeDataURLBytes turns a data:...;base64,... URL into raw bytes.
func decodeDataURLBytes(dataURL string) ([]byte, error) {
	comma := strings.Index(dataURL, ",")
	if comma < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if !strings.Contains(dataURL[:comma], "base64") {
		return nil, io.ErrUnexpectedEOF
	}
	return base64.StdEncoding.DecodeString(dataURL[comma+1:])
}

// extractAttachmentText unpacks a text-bearing container (Office Open XML and
// ODF) into a compact plain-text form the model can read. It returns "" when the
// file is unsupported, unreadable, or larger than maxAttachmentExtractBytes.
func extractAttachmentText(name string, data []byte) string {
	if len(data) == 0 || len(data) > maxAttachmentExtractBytes {
		return ""
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx", ".xlsm":
		return extractSpreadsheet(data)
	case ".docx":
		return extractWord(data)
	case ".pptx":
		return extractSlides(data)
	case ".odt", ".ods", ".odp", ".fodt", ".fods", ".fodp":
		return extractODF(data)
	case ".pdf":
		return extractPDF(data)
	}
	return ""
}

// openZip opens a zip that may be a whole file (Office) or, for flat ODF, nil.
func openZip(data []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}

func zipFile(r *zip.Reader, name string) *zip.File {
	for _, f := range r.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// ---- xlsx ----

func extractSpreadsheet(data []byte) string {
	zr, err := openZip(data)
	if err != nil {
		return ""
	}
	shared := readSharedStrings(zr)
	var sheets []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheets = append(sheets, f.Name)
		}
	}
	sort.Strings(sheets)
	var b strings.Builder
	for _, sh := range sheets {
		f := zipFile(zr, sh)
		if f == nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		rows := extractSheetRows(rc, shared)
		rc.Close()
		if s := strings.TrimSpace(rows); s != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(s)
		}
	}
	return strings.TrimSpace(b.String())
}

// readSharedStrings maps xl/sharedStrings.xml si entries (rich text included) to
// their joined text, ignoring phonetic pronunciation runs.
func readSharedStrings(zr *zip.Reader) []string {
	f := zipFile(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	var out []string
	var cur strings.Builder
	var inSI, inT, inPh bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI, cur = true, strings.Builder{}
			case "t":
				inT = true
			case "rPh":
				inPh = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				inSI = false
				out = append(out, cur.String())
			case "t":
				inT = false
			case "rPh":
				inPh = false
			}
		case xml.CharData:
			if inSI && inT && !inPh {
				cur.Write(t)
			}
		}
	}
	return out
}

// extractSheetRows turns one worksheet XML into tab-separated rows: each <row>
// becomes a line, each <c> with a value becomes a column. Shared strings are
// resolved through shared, inline strings are read from <is>.
func extractSheetRows(r io.Reader, shared []string) string {
	dec := xml.NewDecoder(r)
	var b strings.Builder
	var cellType string
	var cellVal, inlineVal strings.Builder
	var inV, inIS bool
	var firstCell bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				firstCell = true
			case "c":
				cellType = ""
				cellVal.Reset()
				inlineVal.Reset()
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellType = a.Value
					}
				}
			case "v":
				inV = true
			case "is":
				inIS = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "row":
				b.WriteString("\n")
			case "c":
				val := strings.TrimSpace(cellVal.String())
				if cellType == "s" {
					if i, err := strconv.Atoi(val); err == nil && i >= 0 && i < len(shared) {
						val = shared[i]
					} else {
						val = ""
					}
				} else if cellType == "inlineStr" {
					val = strings.TrimSpace(inlineVal.String())
				}
				if val != "" {
					if !firstCell {
						b.WriteString("\t")
					}
					b.WriteString(val)
					firstCell = false
				}
			case "v":
				inV = false
			case "is":
				inIS = false
			}
		case xml.CharData:
			if inV {
				cellVal.Write(t)
			}
			if inIS {
				inlineVal.Write(t)
			}
		}
	}
	return b.String()
}

// ---- docx ----

func extractWord(data []byte) string {
	zr, err := openZip(data)
	if err != nil {
		return ""
	}
	f := zipFile(zr, "word/document.xml")
	if f == nil {
		return ""
	}
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	var b, para strings.Builder
	var inT bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				para.Reset()
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				if s := strings.TrimSpace(para.String()); s != "" {
					b.WriteString(s)
					b.WriteString("\n")
				}
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				para.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// ---- pptx ----

func extractSlides(data []byte) string {
	zr, err := openZip(data)
	if err != nil {
		return ""
	}
	var slides []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slides = append(slides, f.Name)
		}
	}
	sort.Strings(slides)
	var b strings.Builder
	for _, name := range slides {
		f := zipFile(zr, name)
		if f == nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		text := extractDrawingText(rc)
		rc.Close()
		if s := strings.TrimSpace(text); s != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(s)
		}
	}
	return strings.TrimSpace(b.String())
}

// extractDrawingText reads <a:p> paragraphs and their <a:t> runs from a slide XML.
func extractDrawingText(r io.Reader) string {
	dec := xml.NewDecoder(r)
	var b, para strings.Builder
	var inT bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				para.Reset()
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				if s := strings.TrimSpace(para.String()); s != "" {
					b.WriteString(s)
					b.WriteString("\n")
				}
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				para.Write(t)
			}
		}
	}
	return b.String()
}

// ---- ODF ----

func extractODF(data []byte) string {
	var r io.Reader
	if bytes.HasPrefix(data, []byte("PK")) {
		zr, err := openZip(data)
		if err != nil {
			return ""
		}
		f := zipFile(zr, "content.xml")
		if f == nil {
			return ""
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		defer rc.Close()
		r = rc
	} else {
		r = bytes.NewReader(data)
	}
	return extractODFParagraphs(r)
}

// extractODFParagraphs reads <text:p>/<text:h> paragraphs from an ODF content.xml
// (or a flat .fod* file) and joins them with newlines.
func extractODFParagraphs(r io.Reader) string {
	dec := xml.NewDecoder(r)
	var b, para strings.Builder
	var inPara bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p", "h":
				inPara = true
				para.Reset()
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p", "h":
				inPara = false
				if s := strings.TrimSpace(para.String()); s != "" {
					b.WriteString(s)
					b.WriteString("\n")
				}
			}
		case xml.CharData:
			if inPara {
				para.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// ---- pdf ----

// extractPDF достаёт текстовый слой PDF. Файл-скан (одни картинки, без текста)
// вернёт "" — тогда вызывающий оставит вложение как «только имя».
func extractPDF(data []byte) string {
	return strings.TrimSpace(pdfPlainText(data))
}

// pdfPlainText гасит паники сторонней библиотеки: битый PDF не должен ронять
// прогон агента — на выходе будет просто пустой текст.
func pdfPlainText(data []byte) (out string) {
	defer func() {
		if recover() != nil {
			out = ""
		}
	}()
	rd, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	text, err := rd.GetPlainText()
	if err != nil {
		return ""
	}
	const maxPDFText = 4 << 20 // 4 МБ текста — с запасом даже для крупного документа
	b, err := io.ReadAll(io.LimitReader(text, int64(maxPDFText)+1))
	if err != nil {
		return ""
	}
	if len(b) > maxPDFText {
		return string(b[:maxPDFText]) + "\n…(truncated; PDF text limited to 4 MB)"
	}
	return string(b)
}
