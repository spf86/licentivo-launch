package ai

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestOfficeUploadsRoundTrip(t *testing.T) {
	for _, test := range []struct {
		tool  string
		input OfficeInput
		want  string
	}{
		{"generate_excel", OfficeInput{Filename: "报表", Sheets: []Sheet{{Name: "Sales", Columns: []string{"产品", "金额"}, Rows: [][]any{{"Studio", float64(1234.5)}, {"=untrusted", float64(5)}}}}}, "Studio\t1234.5"},
		{"generate_word", OfficeInput{Filename: "指南", Title: "使用指南", Sections: []Section{{Heading: "开始使用", Paragraphs: []string{"安装并登录账号"}}}}, "安装并登录账号"},
		{"generate_presentation", OfficeInput{Filename: "介绍", Title: "产品", Slides: []Slide{{Title: "主要能力", Bullets: []string{"查询真实数据", "生成文件"}}}}, "查询真实数据"},
	} {
		t.Run(test.tool, func(t *testing.T) {
			raw, name, _, err := Office(test.tool, test.input)
			if err != nil {
				t.Fatal(err)
			}
			out, err := Extract(context.Background(), name, raw)
			if err != nil || !strings.Contains(out.Text, test.want) {
				t.Fatal("extract", err, out.Text)
			}
		})
	}
}
func TestUploadLimitsAndTextEncodings(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"facts.csv", []byte("name,value\nStudio,50")}, {"notes.txt", []byte{0xff, 0xfe, 0x41, 0x00, 0x42, 0x00}},
	} {
		out, err := Extract(context.Background(), test.name, test.data)
		if err != nil || out.Text == "" {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"script.exe", []byte("MZ")}, {"fake.png", []byte("not a picture")}, {"binary.txt", []byte{0, 1, 2}}, {"empty.txt", nil}, {"huge.txt", bytes.Repeat([]byte("x"), MaxExtract+1)},
	} {
		if _, err := Extract(context.Background(), test.name, test.data); err == nil {
			t.Fatal("accepted", test.name)
		}
	}
}
func TestPDFPreflightRejectsExpansionAndLargeIndexes(t *testing.T) {
	if preflightPDF(context.Background(), []byte("%PDF-1.7\n/Size 999999999999\n")) == nil {
		t.Fatal("huge index accepted")
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write(bytes.Repeat([]byte("x"), 17<<20))
	writer.Close()
	raw := append([]byte("%PDF-1.7\nstream\n"), compressed.Bytes()...)
	raw = append(raw, []byte("\nendstream")...)
	if preflightPDF(context.Background(), raw) == nil {
		t.Fatal("PDF expansion bomb accepted")
	}
}

func TestTextPDFExtraction(t *testing.T) {
	var file bytes.Buffer
	file.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	content := "BT /F1 12 Tf 50 700 Td (Hello Launch) Tj ET"
	for i, object := range []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)} {
		offsets = append(offsets, file.Len())
		fmt.Fprintf(&file, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := file.Len()
	fmt.Fprintf(&file, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&file, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&file, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	out, err := Extract(context.Background(), "notes.pdf", file.Bytes())
	if err != nil || !strings.Contains(strings.ReplaceAll(out.Text, " ", ""), "HelloLaunch") {
		t.Fatal(out.Text, err)
	}
}
