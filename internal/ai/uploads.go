package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"rsc.io/pdf"
)

const MaxUpload = 8 << 20
const MaxExtract = 120000

type Extracted struct {
	Text, MIME string
	Image      bool
}

func Extract(ctx context.Context, name string, data []byte) (out Extracted, err error) {
	defer func() {
		if recover() != nil {
			out = Extracted{}
			err = errors.New("文件无法解析，请重新导出后上传")
		}
	}()
	if len(data) == 0 || len(data) > MaxUpload {
		return out, errors.New("单个附件须为1字节至8MB")
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp":
		out.MIME = http.DetectContentType(data)
		if !strings.HasPrefix(out.MIME, "image/") {
			return out, errors.New("图片内容无效")
		}
		if out.MIME != "image/webp" {
			cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
			if e != nil || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
				return out, errors.New("图片无效或像素过大")
			}
		}
		out.Image = true
		return out, nil
	case ".txt", ".md", ".csv", ".json", ".log", ".xml", ".yaml", ".yml":
		if len(data) >= 2 && (bytes.Equal(data[:2], []byte{0xff, 0xfe}) || bytes.Equal(data[:2], []byte{0xfe, 0xff})) {
			units := []uint16{}
			for i := 2; i+1 < len(data); i += 2 {
				v := binary.LittleEndian.Uint16(data[i : i+2])
				if data[0] == 0xfe {
					v = binary.BigEndian.Uint16(data[i : i+2])
				}
				units = append(units, v)
			}
			out.Text = string(utf16.Decode(units))
		} else if utf8.Valid(data) {
			out.Text = strings.TrimPrefix(string(data), "\ufeff")
		} else {
			decoded, e := simplifiedchinese.GBK.NewDecoder().Bytes(data)
			if e != nil {
				return out, errors.New("文本编码不支持，请另存为 UTF-8")
			}
			out.Text = string(decoded)
		}
		if strings.ContainsRune(out.Text, 0) {
			return out, errors.New("文件不是可分析的文本")
		}
		out.MIME = "text/plain; charset=utf-8"
	case ".docx", ".pptx", ".xlsx":
		out.Text, err = extractOffice(ext, data)
		if err != nil {
			return out, err
		}
		out.MIME = map[string]string{".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation"}[ext]
	case ".pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return out, errors.New("PDF 内容无效")
		}
		if err := preflightPDF(ctx, data); err != nil {
			return out, err
		}
		reader, e := pdf.NewReader(&boundedPDFReader{reader: bytes.NewReader(data), ctx: ctx, remaining: 64 << 20, deadline: time.Now().Add(8 * time.Second)}, int64(len(data)))
		if e != nil {
			return out, errors.New("PDF 无法解析，暂不支持加密文件")
		}
		if reader.NumPage() > 100 {
			return out, errors.New("PDF 最多分析100页，请拆分上传")
		}
		var text strings.Builder
		for i := 1; i <= reader.NumPage(); i++ {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			text.WriteString(fmt.Sprintf("\n第%d页\n", i))
			for _, entry := range reader.Page(i).Content().Text {
				text.WriteString(entry.S)
				text.WriteByte(' ')
				if text.Len() > MaxExtract {
					return out, errors.New("提取文本过长，请拆分上传")
				}
			}
		}
		out.Text = text.String()
		out.MIME = "application/pdf"
	default:
		return out, errors.New("支持 PDF、Word、Excel、PPT、CSV、文本和 PNG/JPEG/WebP 图片")
	}
	if strings.TrimSpace(out.Text) == "" {
		return out, errors.New("文件未提取到文字；扫描文件请转成图片或可复制文字的文档")
	}
	if len(out.Text) > MaxExtract {
		return out, errors.New("提取文本超过120KB，请拆分文件")
	}
	return out, nil
}

func extractOffice(ext string, data []byte) (string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errors.New("Office 文件损坏或格式不受支持")
	}
	parts := map[string][]byte{}
	var inflated uint64
	for _, f := range z.File {
		wanted := ext == ".docx" && f.Name == "word/document.xml" || ext == ".xlsx" && (strings.HasPrefix(f.Name, "xl/worksheets/sheet") || f.Name == "xl/sharedStrings.xml") || ext == ".pptx" && strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml")
		if !wanted {
			continue
		}
		inflated += f.UncompressedSize64
		if inflated > 16<<20 || len(parts) >= 100 {
			return "", errors.New("Office 解压内容过大，请拆分文件")
		}
		r, e := f.Open()
		if e != nil {
			return "", e
		}
		raw, e := io.ReadAll(io.LimitReader(r, 4<<20+1))
		r.Close()
		if e != nil || len(raw) > 4<<20 {
			return "", errors.New("Office 页面过大")
		}
		parts[f.Name] = raw
	}
	if len(parts) == 0 {
		return "", errors.New("未找到可分析的 Office 正文")
	}
	if ext == ".xlsx" {
		var shared struct {
			Items []struct {
				Text string `xml:"t"`
				Runs []struct {
					Text string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if raw := parts["xl/sharedStrings.xml"]; len(raw) > 0 && xml.Unmarshal(raw, &shared) != nil {
			return "", errors.New("表格共享文本无效")
		}
		names := []string{}
		for name := range parts {
			if strings.HasPrefix(name, "xl/worksheets/") {
				names = append(names, name)
			}
		}
		if len(names) > 5 {
			return "", errors.New("最多分析5张工作表，请拆分文件")
		}
		sort.Slice(names, func(i, j int) bool { return officePartNumber(names[i]) < officePartNumber(names[j]) })
		var result strings.Builder
		for n, name := range names {
			if n >= 5 {
				break
			}
			var sheet struct {
				Rows []struct {
					Cells []struct {
						Ref    string `xml:"r,attr"`
						Type   string `xml:"t,attr"`
						Value  string `xml:"v"`
						Inline struct {
							Text string `xml:"t"`
							Runs []struct {
								Text string `xml:"t"`
							} `xml:"r"`
						} `xml:"is"`
					} `xml:"c"`
				} `xml:"sheetData>row"`
			}
			if xml.Unmarshal(parts[name], &sheet) != nil {
				return "", errors.New("工作表格式无效")
			}
			result.WriteString("\n工作表 " + name + "\n")
			if len(sheet.Rows) > 1001 {
				return "", errors.New("每张表最多分析1000行数据")
			}
			for _, row := range sheet.Rows {
				values := make([]string, 30)
				last := 0
				for _, cell := range row.Cells {
					col := 0
					for _, r := range cell.Ref {
						if r < 'A' || r > 'Z' {
							break
						}
						col = col*26 + int(r-'A'+1)
					}
					if col < 1 || col > 30 {
						continue
					}
					v := cell.Value
					if cell.Type == "inlineStr" {
						v = cell.Inline.Text
						for _, run := range cell.Inline.Runs {
							v += run.Text
						}
					}
					if cell.Type == "s" {
						idx := 0
						if _, e := fmt.Sscan(cell.Value, &idx); e == nil && idx >= 0 && idx < len(shared.Items) {
							v = shared.Items[idx].Text
							for _, run := range shared.Items[idx].Runs {
								v += run.Text
							}
						}
					}
					values[col-1] = v
					if col > last {
						last = col
					}
				}
				result.WriteString(strings.Join(values[:last], "\t") + "\n")
				if result.Len() > MaxExtract {
					return "", errors.New("表格文本过长，请拆分上传")
				}
			}
		}
		return result.String(), nil
	}
	names := []string{}
	for name := range parts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return officePartNumber(names[i]) < officePartNumber(names[j]) })
	var text strings.Builder
	for _, name := range names {
		decoder := xml.NewDecoder(bytes.NewReader(parts[name]))
		for {
			token, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", errors.New("Office 正文无效")
			}
			switch token := token.(type) {
			case xml.StartElement:
				if token.Name.Local == "t" {
					var value string
					if decoder.DecodeElement(&value, &token) != nil {
						return "", errors.New("正文文字无效")
					}
					text.WriteString(value)
				}
			case xml.EndElement:
				if token.Name.Local == "p" {
					text.WriteByte('\n')
				}
			}
			if text.Len() > MaxExtract {
				return "", errors.New("文档文字过长，请拆分上传")
			}
		}
	}
	return text.String(), nil
}
func officePartNumber(name string) int {
	name = strings.TrimSuffix(name, ".xml")
	start := len(name)
	for start > 0 && name[start-1] >= '0' && name[start-1] <= '9' {
		start--
	}
	n, _ := strconv.Atoi(name[start:])
	return n
}
