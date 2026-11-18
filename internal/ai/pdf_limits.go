package ai

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var pdfNameEscape = regexp.MustCompile(`#[0-9a-fA-F]{2}`)
var pdfAllocation = regexp.MustCompile(`/(Size|Columns)\s+([+-]?[0-9]+)(\s+[0-9]+\s+R)?`)
var pdfWidths = regexp.MustCompile(`/W\s*\[([^\]]{0,100})\]`)
var pdfXref = regexp.MustCompile(`(?m)(?:^|[\r\n])xref\s+([0-9]+)\s+([0-9]+)`)
var pdfXrefChunk = regexp.MustCompile(`(?m)(?:^|[\r\n])([+-]?[0-9]+)[ \t]+([+-]?[0-9]+)[ \t]*[\r\n]+[+-]?[0-9]+[ \t]+[+-]?[0-9]+[ \t]+[fn]`)

// Bound expansion and the allocation metadata before passing a PDF to the
// parser. ReaderAt also has a read/time budget, including cyclic references.
func preflightPDF(ctx context.Context, data []byte) error {
	var expanded int64
	var inspect func([]byte, int) error
	inspect = func(raw []byte, depth int) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if depth > 3 {
			return errors.New("PDF 压缩层数过多，请重新导出")
		}
		if err := pdfNesting(raw); err != nil {
			return err
		}
		normalized := pdfNameEscape.ReplaceAllFunc(raw, func(v []byte) []byte { n, _ := strconv.ParseUint(string(v[1:]), 16, 8); return []byte{byte(n)} })
		for _, match := range pdfAllocation.FindAllSubmatch(normalized, -1) {
			n, e := strconv.ParseInt(string(match[2]), 10, 64)
			if e != nil || n < 0 || n > 100000 || len(match[3]) > 0 {
				return errors.New("PDF 结构过大或不受支持，请重新导出")
			}
		}
		for _, match := range pdfXref.FindAllSubmatch(normalized, -1) {
			start, _ := strconv.ParseInt(string(match[1]), 10, 64)
			count, _ := strconv.ParseInt(string(match[2]), 10, 64)
			if start < 0 || count < 0 || start+count > 100000 {
				return errors.New("PDF 索引过大")
			}
		}
		for _, match := range pdfXrefChunk.FindAllSubmatch(normalized, -1) {
			start, e1 := strconv.ParseInt(string(match[1]), 10, 64)
			count, e2 := strconv.ParseInt(string(match[2]), 10, 64)
			if e1 != nil || e2 != nil || start < 0 || count < 0 || start > 100000 || count > 100000 || start+count > 100000 {
				return errors.New("PDF 索引范围过大")
			}
		}
		for _, match := range pdfWidths.FindAllSubmatch(normalized, -1) {
			fields := strings.Fields(string(match[1]))
			if len(fields) != 3 {
				return errors.New("PDF 索引格式不支持")
			}
			for _, f := range fields {
				n, e := strconv.Atoi(f)
				if e != nil || n < 0 || n > 8 {
					return errors.New("PDF 索引宽度无效")
				}
			}
		}
		for offset := 0; offset < len(raw); {
			at := bytes.Index(raw[offset:], []byte("stream"))
			if at < 0 {
				break
			}
			at += offset + 6
			offset = at
			if at >= len(raw) {
				break
			}
			if raw[at] == '\r' {
				at++
			}
			if at >= len(raw) || raw[at] != '\n' {
				continue
			}
			at++
			end := bytes.Index(raw[at:], []byte("endstream"))
			if end < 0 {
				break
			}
			stream := raw[at : at+end]
			offset = at + end + 9
			if len(stream) < 2 || stream[0]&15 != 8 || (int(stream[0])*256+int(stream[1]))%31 != 0 {
				continue
			}
			reader, e := zlib.NewReader(bytes.NewReader(stream))
			if e != nil {
				return errors.New("PDF 压缩内容无效")
			}
			decoded, e := io.ReadAll(io.LimitReader(reader, (16<<20)-expanded+1))
			reader.Close()
			expanded += int64(len(decoded))
			if e != nil || expanded > 16<<20 {
				return errors.New("PDF 解压内容过大，请拆分上传")
			}
			if err := inspect(decoded, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return inspect(data, 0)
}

func pdfNesting(raw []byte) error {
	depth := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '%' {
			for i < len(raw) && raw[i] != '\n' && raw[i] != '\r' {
				i++
			}
			continue
		}
		if raw[i] == '(' {
			level := 1
			for i++; i < len(raw) && level > 0; i++ {
				if raw[i] == '\\' {
					i++
					continue
				}
				if raw[i] == '(' {
					level++
				}
				if raw[i] == ')' {
					level--
				}
				if level > 128 {
					return errors.New("PDF 文字嵌套过深")
				}
			}
			i--
			continue
		}
		if bytes.HasPrefix(raw[i:], []byte("stream\n")) || bytes.HasPrefix(raw[i:], []byte("stream\r\n")) {
			end := bytes.Index(raw[i+6:], []byte("endstream"))
			if end < 0 {
				break
			}
			i += 6 + end + 8
			continue
		}
		switch raw[i] {
		case '[':
			depth++
		case ']':
			depth--
		case '<':
			if i+1 < len(raw) && raw[i+1] == '<' {
				depth++
				i++
			} else {
				for i < len(raw) && raw[i] != '>' {
					i++
				}
			}
		case '>':
			if i+1 < len(raw) && raw[i+1] == '>' {
				depth--
				i++
			}
		}
		if depth > 128 {
			return errors.New("PDF 结构嵌套过深，请重新导出")
		}
		if depth < 0 {
			depth = 0
		}
	}
	return nil
}

type boundedPDFReader struct {
	reader    *bytes.Reader
	ctx       context.Context
	remaining int64
	deadline  time.Time
}

func (r *boundedPDFReader) ReadAt(p []byte, offset int64) (int, error) {
	r.remaining -= int64(len(p))
	if r.remaining < 0 || r.ctx.Err() != nil || time.Now().After(r.deadline) {
		return 0, errors.New("PDF 解析达到限制，请拆分文件")
	}
	return r.reader.ReadAt(p, offset)
}
