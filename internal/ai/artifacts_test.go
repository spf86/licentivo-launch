package ai

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestOfficeFilesAreRealOOXMLWithoutActiveContent(t *testing.T) {
	for _, test := range []struct {
		kind, entry string
		in          OfficeInput
	}{
		{"generate_excel", "xl/worksheets/sheet1.xml", OfficeInput{Filename: "report", Sheets: []Sheet{{Name: "数据", Columns: []string{"名称", "数量", "有效"}, Rows: [][]any{{"=HYPERLINK(\"https://evil.invalid\")", float64(12), true}, {"<script>&", nil, false}}}}}},
		{"generate_word", "word/document.xml", OfficeInput{Filename: "guide", Title: "中文 & <Title>", Sections: []Section{{Heading: "一、说明", Paragraphs: []string{"内容 & <text>", "第二段"}}}}},
		{"generate_presentation", "ppt/slides/slide1.xml", OfficeInput{Filename: "slides", Title: "中文介绍", Slides: []Slide{{Title: "产品介绍", Bullets: []string{"可编辑内容", "<script>&"}}}}},
	} {
		data, name, mime, err := Office(test.kind, test.in)
		if err != nil || len(data) < 100 || name == "" || !strings.Contains(mime, "openxmlformats") {
			t.Fatal(test.kind, err)
		}
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range z.File {
			if file.Name == test.entry {
				found = true
			}
			reader, _ := file.Open()
			raw, _ := io.ReadAll(reader)
			reader.Close()
			d := xml.NewDecoder(bytes.NewReader(raw))
			for {
				var token xml.Token
				token, err = d.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(file.Name, "invalid XML", err)
				}
				if file.Name == "xl/workbook.xml" {
					if element, ok := token.(xml.StartElement); ok && element.Name.Local == "sheet" {
						valid := false
						for _, attr := range element.Attr {
							if attr.Name.Local == "id" && attr.Name.Space == strings.TrimSuffix(officeRel, "/") && attr.Value == "rId1" {
								valid = true
							}
						}
						if !valid {
							t.Fatal("workbook sheet relationship is missing or uses an incorrect namespace")
						}
					}
				}
			}
			if bytes.Contains(raw, []byte(`TargetMode="External"`)) || bytes.Contains(raw, []byte("<f>")) || bytes.Contains(raw, []byte("<script>")) {
				t.Fatal("active content in", file.Name)
			}
		}
		if !found {
			t.Fatal("missing document part", test.entry)
		}
	}
}

func TestArtifactLimitsAndStrictArguments(t *testing.T) {
	for _, in := range []OfficeInput{{Filename: "a", Sheets: []Sheet{{Name: "../x", Columns: []string{"x"}}}}, {Filename: "a", Sheets: []Sheet{{Name: "Sheet", Columns: []string{"x"}, Rows: [][]any{{"a", "b"}}}}}} {
		if _, _, _, err := Office("generate_excel", in); err == nil {
			t.Fatal("invalid sheet accepted")
		}
	}
	if _, _, _, err := Office("generate_presentation", OfficeInput{Filename: "x", Slides: []Slide{{Title: "title", Bullets: []string{strings.Repeat("中", 101)}}}}); err == nil {
		t.Fatal("overflowing slide accepted")
	}
	if Decode([]byte(`{"filename":"x","shell":"delete"}`), &OfficeInput{}) == nil {
		t.Fatal("unknown field accepted")
	}
	for _, name := range []string{"", ".", "invalid\r\nname"} {
		if _, err := Filename(name, ".xlsx"); err == nil {
			t.Fatal("invalid filename accepted")
		}
	}
}
