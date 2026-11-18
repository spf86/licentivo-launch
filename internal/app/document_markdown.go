package app

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func safeDocumentURL(raw string, image bool) bool {
	if raw == "" || strings.ContainsAny(raw, "\x00\r\n\t \\") || strings.HasPrefix(raw, "//") {
		return false
	}
	if strings.HasPrefix(raw, "/api/v1/product-document-images/") {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}

var documentInline = regexp.MustCompile("!\\[([^\\]\\n]*)\\]\\(([^\\s)]+)\\)|\\[([^\\]\\n]+)\\]\\(([^\\s)]+)\\)|\\*\\*([^\\n]+?)\\*\\*|`([^`\\n]+)`|\\*([^*\\n]+)\\*")

func documentInlineHTML(value string) string {
	var out strings.Builder
	at := 0
	for _, m := range documentInline.FindAllStringSubmatchIndex(value, -1) {
		out.WriteString(html.EscapeString(value[at:m[0]]))
		part := func(n int) string {
			if m[n*2] < 0 {
				return ""
			}
			return value[m[n*2]:m[n*2+1]]
		}
		switch {
		case m[2] >= 0:
			if safeDocumentURL(part(2), true) {
				out.WriteString(`<img loading="lazy" src="` + html.EscapeString(part(2)) + `" alt="` + html.EscapeString(part(1)) + `">`)
			} else {
				out.WriteString(html.EscapeString(value[m[0]:m[1]]))
			}
		case m[6] >= 0:
			if safeDocumentURL(part(4), false) {
				out.WriteString(`<a rel="noopener noreferrer" href="` + html.EscapeString(part(4)) + `">` + html.EscapeString(part(3)) + `</a>`)
			} else {
				out.WriteString(html.EscapeString(value[m[0]:m[1]]))
			}
		case m[10] >= 0:
			out.WriteString("<strong>" + html.EscapeString(part(5)) + "</strong>")
		case m[12] >= 0:
			out.WriteString("<code>" + html.EscapeString(part(6)) + "</code>")
		default:
			out.WriteString("<em>" + html.EscapeString(part(7)) + "</em>")
		}
		at = m[1]
	}
	out.WriteString(html.EscapeString(value[at:]))
	return out.String()
}

var documentHeading = regexp.MustCompile(`^(#{1,3})\s+(.+)$`)
var documentList = regexp.MustCompile(`^([-*]|[0-9]+\.)\s+(.+)$`)
var documentTableRule = regexp.MustCompile(`^\|?[ :\-]+\|[ |:\-]*$`)

type DocumentHeading struct{ ID, Title string }

func DocumentHeadings(body string) []DocumentHeading {
	out := []DocumentHeading{}
	code := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			code = !code
			continue
		}
		if !code {
			if m := documentHeading.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				out = append(out, DocumentHeading{"section-" + strconv.Itoa(len(out)+1), m[2]})
			}
		}
	}
	return out
}

// RenderDocumentMarkdown intentionally accepts a small Markdown vocabulary,
// never raw HTML, inline styles, embeds or executable URL schemes.
func RenderDocumentMarkdown(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var out strings.Builder
	heading := 0
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}
		if strings.HasPrefix(line, "```") {
			i++
			code := []string{}
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			out.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>")
			continue
		}
		if m := documentHeading.FindStringSubmatch(line); m != nil {
			heading++
			level := strconv.Itoa(len(m[1]))
			out.WriteString(`<h` + level + ` id="section-` + strconv.Itoa(heading) + `">` + documentInlineHTML(m[2]) + `</h` + level + `>`)
			i++
			continue
		}
		if i+1 < len(lines) && strings.Contains(line, "|") && documentTableRule.MatchString(strings.TrimSpace(lines[i+1])) {
			cells := func(s string) []string { return strings.Split(strings.Trim(strings.TrimSpace(s), "|"), "|") }
			headers := cells(line)
			out.WriteString("<div class=\"document-table-wrap\"><table><thead><tr>")
			for _, c := range headers {
				out.WriteString("<th>" + documentInlineHTML(strings.TrimSpace(c)) + "</th>")
			}
			out.WriteString("</tr></thead><tbody>")
			i += 2
			for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				out.WriteString("<tr>")
				for _, c := range cells(lines[i]) {
					out.WriteString("<td>" + documentInlineHTML(strings.TrimSpace(c)) + "</td>")
				}
				out.WriteString("</tr>")
				i++
			}
			out.WriteString("</tbody></table></div>")
			continue
		}
		if m := documentList.FindStringSubmatch(line); m != nil {
			tag := "ul"
			if m[1] != "-" && m[1] != "*" {
				tag = "ol"
			}
			out.WriteString("<" + tag + ">")
			for i < len(lines) {
				m = documentList.FindStringSubmatch(strings.TrimSpace(lines[i]))
				if m == nil {
					break
				}
				next := "ul"
				if m[1] != "-" && m[1] != "*" {
					next = "ol"
				}
				if next != tag {
					break
				}
				out.WriteString("<li>" + documentInlineHTML(m[2]) + "</li>")
				i++
			}
			out.WriteString("</" + tag + ">")
			continue
		}
		if strings.HasPrefix(line, "> ") {
			out.WriteString("<blockquote>" + documentInlineHTML(strings.TrimPrefix(line, "> ")) + "</blockquote>")
			i++
			continue
		}
		out.WriteString("<p>" + documentInlineHTML(line) + "</p>")
		i++
	}
	return out.String()
}

func DocumentDescription(c DocumentContent) string {
	if c.SEO.Description != "" {
		return c.SEO.Description
	}
	body := regexp.MustCompile("[\\x60#*|>\\[\\]]").ReplaceAllString(c.Body, "")
	runes := []rune(strings.Join(strings.Fields(body), " "))
	if len(runes) > 160 {
		runes = runes[:160]
	}
	return string(runes)
}
