package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Sheet struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}
type Section struct {
	Heading    string   `json:"heading"`
	Paragraphs []string `json:"paragraphs"`
}
type Slide struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}
type OfficeInput struct {
	Filename string    `json:"filename"`
	Title    string    `json:"title"`
	Sheets   []Sheet   `json:"sheets,omitempty"`
	Sections []Section `json:"sections,omitempty"`
	Slides   []Slide   `json:"slides,omitempty"`
}

func Decode(raw []byte, out any) error {
	if len(raw) > 512<<10 {
		return errors.New("内容过长，请拆分任务")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(&struct{}{}) != io.EOF {
		return errors.New("工具参数格式无效")
	}
	return nil
}

func Filename(name, ext string) (string, error) {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" || name == "." || len(name) > 180 || strings.ContainsAny(name, "\x00\r\n<>:\"|?*") {
		return "", errors.New("请提供有效文件名")
	}
	return name + ext, nil
}

func Office(kind string, in OfficeInput) (data []byte, name, mime string, err error) {
	var ext string
	switch kind {
	case "generate_excel":
		ext = ".xlsx"
		mime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		data, err = buildExcel(in.Sheets)
	case "generate_word":
		ext = ".docx"
		mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		data, err = buildWord(in.Title, in.Sections)
	case "generate_presentation":
		ext = ".pptx"
		mime = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
		data, err = buildPresentation(in.Title, in.Slides)
	default:
		err = errors.New("不支持的文件类型")
		return
	}
	if err == nil {
		name, err = Filename(in.Filename, ext)
	}
	return
}

func buildExcel(sheets []Sheet) ([]byte, error) {
	if len(sheets) < 1 || len(sheets) > 5 {
		return nil, errors.New("Excel需要1至5张工作表")
	}
	parts := map[string]string{"_rels/.rels": officeRoot("xl/workbook.xml")}
	styles := `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF176B5B"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`
	parts["xl/styles.xml"] = styles
	types := `<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`
	var wb, rels strings.Builder
	seen := map[string]bool{}
	rels.WriteString(`<Relationship Id="styles" Type="` + officeRel + `styles" Target="styles.xml"/>`)
	for i, s := range sheets {
		if s.Name == "" || utf8.RuneCountInString(s.Name) > 31 || strings.ContainsAny(s.Name, "[]:*?/\\") || strings.HasPrefix(s.Name, "'") || strings.HasSuffix(s.Name, "'") || seen[strings.ToLower(s.Name)] {
			return nil, errors.New("工作表名称无效或重复")
		}
		seen[strings.ToLower(s.Name)] = true
		if len(s.Columns) < 1 || len(s.Columns) > 30 || len(s.Rows) > 1000 {
			return nil, errors.New("每张表需要1至30列、最多1000行")
		}
		var body strings.Builder
		for r := 0; r <= len(s.Rows); r++ {
			row := []any{}
			if r == 0 {
				for _, c := range s.Columns {
					row = append(row, c)
				}
			} else {
				row = s.Rows[r-1]
			}
			if len(row) != len(s.Columns) {
				return nil, errors.New("行列数与表头不一致")
			}
			body.WriteString(fmt.Sprintf(`<row r="%d">`, r+1))
			for j, v := range row {
				col := string(rune('A' + j%26))
				if j >= 26 {
					col = "A" + col
				}
				ref := fmt.Sprintf("%s%d", col, r+1)
				style := "0"
				if r == 0 {
					style = "1"
				}
				switch val := v.(type) {
				case nil:
					body.WriteString(`<c r="` + ref + `"/>`)
				case string:
					if utf8.RuneCountInString(val) > 32767 {
						return nil, errors.New("单元格文本过长")
					}
					body.WriteString(`<c r="` + ref + `" s="` + style + `" t="inlineStr"><is><t xml:space="preserve">` + officeText(val) + `</t></is></c>`)
				case float64:
					if math.IsInf(val, 0) || math.IsNaN(val) {
						return nil, errors.New("单元格数值无效")
					}
					body.WriteString(`<c r="` + ref + `"><v>` + strconv.FormatFloat(val, 'g', -1, 64) + `</v></c>`)
				case bool:
					n := 0
					if val {
						n = 1
					}
					body.WriteString(fmt.Sprintf(`<c r="%s" t="b"><v>%d</v></c>`, ref, n))
				default:
					return nil, errors.New("单元格仅支持文本、数字、布尔或空值")
				}
			}
			body.WriteString(`</row>`)
		}
		n := i + 1
		path := fmt.Sprintf("xl/worksheets/sheet%d.xml", n)
		parts[path] = `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="30" width="24" customWidth="1"/></cols><sheetData>` + body.String() + `</sheetData></worksheet>`
		wb.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, officeText(s.Name), n, n))
		rels.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="%sworksheet" Target="worksheets/sheet%d.xml"/>`, n, officeRel, n))
		types += `<Override PartName="/` + path + `" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`
	}
	parts["xl/workbook.xml"] = `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="` + strings.TrimSuffix(officeRel, "/") + `"><sheets>` + wb.String() + `</sheets></workbook>`
	parts["xl/_rels/workbook.xml.rels"] = `<Relationships xmlns="` + relNS + `">` + rels.String() + `</Relationships>`
	parts["[Content_Types].xml"] = contentTypes(types)
	return officeZip(parts)
}
