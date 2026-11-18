package ai

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Generate OOXML from escaped text, never from model-provided XML/HTML, macros,
// relationships or filesystem paths. All relationships remain inside the ZIP.
func officeText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 32 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF) {
			return r
		}
		return -1
	}, s)
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
const relNS = `http://schemas.openxmlformats.org/package/2006/relationships`
const officeRel = `http://schemas.openxmlformats.org/officeDocument/2006/relationships/`

func officeZip(parts map[string]string) ([]byte, error) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	names := make([]string, 0, len(parts))
	for n := range parts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := z.Create(n)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(xmlHeader + parts[n])); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func officeRoot(target string) string {
	return `<Relationships xmlns="` + relNS + `"><Relationship Id="rId1" Type="` + officeRel + `officeDocument" Target="` + target + `"/></Relationships>`
}
func contentTypes(overrides string) string {
	return `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` + overrides + `</Types>`
}

func wordParagraph(text, style string) string {
	properties := ""
	if style != "" {
		properties += `<w:pStyle w:val="` + style + `"/>`
	}
	properties += `<w:spacing w:after="160" w:line="300" w:lineRule="auto"/>`
	var runs strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			runs.WriteString(`<w:r><w:br/></w:r>`)
		}
		runs.WriteString(`<w:r><w:t xml:space="preserve">` + officeText(line) + `</w:t></w:r>`)
	}
	return `<w:p><w:pPr>` + properties + `</w:pPr>` + runs.String() + `</w:p>`
}
func buildWord(title string, sections []Section) ([]byte, error) {
	if len(title) > 500 || len(sections) < 1 || len(sections) > 50 {
		return nil, errors.New("Word需要1至50节，标题不超过500字节")
	}
	body := wordParagraph(title, "Title")
	for _, section := range sections {
		if len(section.Heading) > 500 || len(section.Paragraphs) < 1 || len(section.Paragraphs) > 30 {
			return nil, errors.New("章节标题或段落数量无效")
		}
		if section.Heading != "" {
			body += wordParagraph(section.Heading, "Heading1")
		}
		for _, p := range section.Paragraphs {
			if len(p) > 10000 {
				return nil, errors.New("单段正文过长")
			}
			body += wordParagraph(p, "")
		}
	}
	return officeZip(map[string]string{
		"[Content_Types].xml":          contentTypes(`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`),
		"_rels/.rels":                  officeRoot("word/document.xml"),
		"word/_rels/document.xml.rels": `<Relationships xmlns="` + relNS + `"><Relationship Id="rId1" Type="` + officeRel + `styles" Target="styles.xml"/></Relationships>`,
		"word/document.xml":            `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body + `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`,
		"word/styles.xml":              `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Microsoft YaHei"/><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:color w:val="243B53"/><w:sz w:val="40"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:color w:val="243B53"/><w:sz w:val="28"/></w:rPr></w:style></w:styles>`,
	})
}

const presentationNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
const slideGroup = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`

func slideText(id int, title string, lines []string, x, y, cx, cy, font int, color string) string {
	var body strings.Builder
	for _, line := range lines {
		body.WriteString(fmt.Sprintf(`<a:p><a:pPr marL="0" indent="0"><a:spcAft><a:spcPts val="1200"/></a:spcAft></a:pPr><a:r><a:rPr lang="zh-CN" sz="%d"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Arial"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r><a:endParaRPr lang="zh-CN"/></a:p>`, font, color, officeText(line)))
	}
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/><a:ln><a:noFill/></a:ln></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="0" rIns="0" tIns="0" bIns="0"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`, id, officeText(title), x, y, cx, cy, body.String())
}
func buildPresentation(title string, slides []Slide) ([]byte, error) {
	if len(title) > 500 || len(slides) < 1 || len(slides) > 30 {
		return nil, errors.New("PPT需要1至30页")
	}
	parts := map[string]string{"_rels/.rels": officeRoot("ppt/presentation.xml")}
	types := `<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/><Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/><Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/><Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>`
	rels := `<Relationship Id="rId1" Type="` + officeRel + `slideMaster" Target="slideMasters/slideMaster1.xml"/>`
	ids := ""
	for i, slide := range slides {
		if utf8.RuneCountInString(slide.Title) > 60 || len(slide.Bullets) < 1 || len(slide.Bullets) > 8 {
			return nil, errors.New("每页标题最多60字、需要1至8个要点")
		}
		for _, b := range slide.Bullets {
			if utf8.RuneCountInString(b) > 100 {
				return nil, errors.New("每个幻灯片要点最多100字，请拆页")
			}
		}
		n := i + 1
		types += fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, n)
		ids += fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 255+n, n+1)
		rels += fmt.Sprintf(`<Relationship Id="rId%d" Type="%sslide" Target="slides/slide%d.xml"/>`, n+1, officeRel, n)
		content := slideText(2, "Title", []string{slide.Title}, 640000, 360000, 10900000, 1200000, 3000, "243B53") + slideText(3, "Content", slide.Bullets, 720000, 1700000, 10600000, 4300000, 2200, "334E68") + slideText(4, "Footer", []string{fmt.Sprintf("%s  ·  %d / %d", title, n, len(slides))}, 720000, 6300000, 10600000, 350000, 1000, "627D98")
		parts[fmt.Sprintf("ppt/slides/slide%d.xml", n)] = `<p:sld ` + presentationNS + `><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>` + slideGroup + content + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
		parts[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n)] = `<Relationships xmlns="` + relNS + `"><Relationship Id="rId1" Type="` + officeRel + `slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`
	}
	parts["[Content_Types].xml"] = contentTypes(types)
	parts["ppt/presentation.xml"] = `<p:presentation ` + presentationNS + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>` + ids + `</p:sldIdLst><p:sldSz cx="12192000" cy="6858000" type="screen16x9"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`
	parts["ppt/_rels/presentation.xml.rels"] = `<Relationships xmlns="` + relNS + `">` + rels + `</Relationships>`
	parts["ppt/slideMasters/slideMaster1.xml"] = `<p:sldMaster ` + presentationNS + `><p:cSld><p:spTree>` + slideGroup + `</p:spTree></p:cSld><p:clrMap accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" bg1="lt1" bg2="lt2" folHlink="folHlink" hlink="hlink" tx1="dk1" tx2="dk2"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`
	parts["ppt/slideMasters/_rels/slideMaster1.xml.rels"] = `<Relationships xmlns="` + relNS + `"><Relationship Id="rId1" Type="` + officeRel + `slideLayout" Target="../slideLayouts/slideLayout1.xml"/><Relationship Id="rId2" Type="` + officeRel + `theme" Target="../theme/theme1.xml"/></Relationships>`
	parts["ppt/slideLayouts/slideLayout1.xml"] = `<p:sldLayout ` + presentationNS + ` type="blank" preserve="1"><p:cSld name="Blank"><p:spTree>` + slideGroup + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`
	parts["ppt/slideLayouts/_rels/slideLayout1.xml.rels"] = `<Relationships xmlns="` + relNS + `"><Relationship Id="rId1" Type="` + officeRel + `slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`
	parts["ppt/theme/theme1.xml"] = `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Licentivo Launch"><a:themeElements><a:clrScheme name="Licentivo Launch"><a:dk1><a:srgbClr val="243B53"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="334E68"/></a:dk2><a:lt2><a:srgbClr val="F0F4F8"/></a:lt2><a:accent1><a:srgbClr val="2680C2"/></a:accent1><a:accent2><a:srgbClr val="199473"/></a:accent2><a:accent3><a:srgbClr val="F0B429"/></a:accent3><a:accent4><a:srgbClr val="D64545"/></a:accent4><a:accent5><a:srgbClr val="627D98"/></a:accent5><a:accent6><a:srgbClr val="7B93DB"/></a:accent6><a:hlink><a:srgbClr val="2680C2"/></a:hlink><a:folHlink><a:srgbClr val="7B93DB"/></a:folHlink></a:clrScheme><a:fontScheme name="Licentivo Launch"><a:majorFont><a:latin typeface="Arial"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface="Arial"/></a:majorFont><a:minorFont><a:latin typeface="Arial"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface="Arial"/></a:minorFont></a:fontScheme><a:fmtScheme name="Licentivo Launch"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="25400"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="38100"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`
	return officeZip(parts)
}
