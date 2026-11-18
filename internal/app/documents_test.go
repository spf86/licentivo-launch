package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocumentsPublishSnapshotsAndKeepDraftPrivate(t *testing.T) {
	v := DocumentLibraryView{}
	change := func(c DocumentCommand) {
		t.Helper()
		if err := applyDocumentCommand(&v, c); err != nil {
			t.Fatal(err)
		}
	}
	change(DocumentCommand{Action: "create_group", Title: "自定义入门目录"})
	g := v.Groups[0].ID
	change(DocumentCommand{Action: "create_article", GroupID: g, Title: "任意标题"})
	id := v.Groups[0].Articles[0].ID
	if err := applyDocumentCommand(&v, DocumentCommand{Action: "publish_article", ID: id}); err == nil {
		t.Fatal("empty body published")
	}
	c := DocumentContent{Title: "Public title", Slug: "getting-started", Body: "## Scope\nPublic body", SEO: DocumentSEO{Title: "Search title", Description: "Published description"}}
	change(DocumentCommand{Action: "save_article", ID: id, Content: c})
	change(DocumentCommand{Action: "publish_article", ID: id})
	c.Title = "PRIVATE_TITLE"
	c.Body = "PRIVATE_BODY"
	c.SEO.Title = "PRIVATE_META"
	c.Slug = "private-draft-link"
	change(DocumentCommand{Action: "save_article", ID: id, Content: c})
	groups, err := publishedDocuments(marshal(v.Groups))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(groups)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "private-draft-link") || groups[0].Articles[0].SEO.Title != "Search title" {
		t.Fatalf("draft leaked: %s", raw)
	}
	change(DocumentCommand{Action: "publish_article", ID: id})
	a := v.Groups[0].Articles[0]
	if len(a.Aliases) != 1 || a.Aliases[0] != "getting-started" {
		t.Fatal("old published slug was not retained", a)
	}
	change(DocumentCommand{Action: "unpublish_article", ID: id})
	groups, err = publishedDocuments(marshal(v.Groups))
	if err != nil || len(groups) != 0 {
		t.Fatal("unpublished article is still public", groups, err)
	}
}

func TestDocumentsReorderAcrossCustomGroupsAndReserveOldSlugs(t *testing.T) {
	v := DocumentLibraryView{}
	change := func(c DocumentCommand) {
		t.Helper()
		if err := applyDocumentCommand(&v, c); err != nil {
			t.Fatal(err)
		}
	}
	change(DocumentCommand{Action: "create_group", Title: "One"})
	one := v.Groups[0].ID
	change(DocumentCommand{Action: "create_group", Title: "Two"})
	two := v.Groups[1].ID
	change(DocumentCommand{Action: "create_article", GroupID: one, Title: "One"})
	id := v.Groups[0].Articles[0].ID
	change(DocumentCommand{Action: "move_article", ID: id, GroupID: two})
	if len(v.Groups[0].Articles) != 0 || v.Groups[1].Articles[0].ID != id {
		t.Fatal(v)
	}
	if err := applyDocumentCommand(&v, DocumentCommand{Action: "delete_group", GroupID: two}); err == nil {
		t.Fatal("non-empty group deleted")
	}
	change(DocumentCommand{Action: "move_group", GroupID: two, BeforeID: one})
	if v.Groups[0].ID != two {
		t.Fatal("group did not move")
	}
	c := DocumentContent{Title: "One", Slug: "first-url", Body: "Body"}
	change(DocumentCommand{Action: "save_article", ID: id, Content: c})
	change(DocumentCommand{Action: "publish_article", ID: id})
	c.Slug = "second-url"
	change(DocumentCommand{Action: "save_article", ID: id, Content: c})
	change(DocumentCommand{Action: "publish_article", ID: id})
	change(DocumentCommand{Action: "create_article", GroupID: one, Title: "Another"})
	other := v.Groups[1].Articles[0].ID
	c.Slug = "first-url"
	if err := applyDocumentCommand(&v, DocumentCommand{Action: "save_article", ID: other, Content: c}); err == nil {
		t.Fatal("a historical slug was reused by another article")
	}
}

func TestDocumentMarkdownSafetyAndRichContent(t *testing.T) {
	body := "## Heading\n\n**Bold** and `inline`\n\n- First\n- Second\n\n| Name | Value |\n| --- | --- |\n| A | B |\n\n```\n<script>alert(1)</script>\n```\n\n<img src=x onerror=alert(1)>\n\n[bad](javascript:alert)\n![img](data:text/html,evil)\n[ok](https://example.com)"
	h := RenderDocumentMarkdown(body)
	for _, want := range []string{`id="section-1"`, "<strong>Bold</strong>", "<ul><li>First</li><li>Second</li></ul>", "<table>", "<pre><code>&lt;script&gt;", `href="https://example.com"`} {
		if !strings.Contains(h, want) {
			t.Fatal("missing markup", want, h)
		}
	}
	for _, bad := range []string{"<script>", "<img src=x", `href="javascript:`, `src="data:`} {
		if strings.Contains(h, bad) {
			t.Fatal("unsafe markup", h)
		}
	}
	if len(DocumentHeadings("```\n## not heading\n```\n## Actual")) != 1 {
		t.Fatal("code block included in outline")
	}
	for _, u := range []string{"javascript:alert(1)", "//evil.test/x", "https://user:pass@example.com/x", "data:text/html,x"} {
		if safeDocumentURL(u, true) {
			t.Fatal("unsafe URL", u)
		}
	}
}
