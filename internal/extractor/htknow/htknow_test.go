package htknow

import (
	"net/url"
	"strings"
	"testing"
)

func TestAnswerEndpointConstantsMatchSource(t *testing.T) {
	want := map[string]string{
		"answerTagURL":         "https://saas.clientapi.htknow.com/pc_view/quest/get_quest_tag_list",
		"answerNumURL":         "https://saas.clientapi.htknow.com/pc_view/quest/get_quest_num_list",
		"answerListURL":        "https://saas.clientapi.htknow.com/pc_view/quest/get_quest_list",
		"answerCreatePaperURL": "https://saas.clientapi.htknow.com/pc_view/quest/create_question_paper",
	}
	got := map[string]string{
		"answerTagURL":         answerTagURL,
		"answerNumURL":         answerNumURL,
		"answerListURL":        answerListURL,
		"answerCreatePaperURL": answerCreatePaperURL,
	}
	for name, wantURL := range want {
		if got[name] != wantURL {
			t.Fatalf("%s = %q, want %q", name, got[name], wantURL)
		}
	}
}

func TestMediaFromSourcesKeepsHTMLOnlyEntry(t *testing.T) {
	html := "<h1>图文内容</h1>"
	mi, err := mediaFromSources("课程", []source{{name: "图文章节", kind: "图文", html: html}})
	if err != nil {
		t.Fatalf("mediaFromSources returned error: %v", err)
	}
	if mi.Title != "图文章节" {
		t.Fatalf("title = %q, want %q", mi.Title, "图文章节")
	}
	stream, ok := mi.Streams["document"]
	if !ok {
		t.Fatalf("document stream missing: %#v", mi.Streams)
	}
	if stream.Format != "html" {
		t.Fatalf("format = %q, want html", stream.Format)
	}
	if len(stream.URLs) != 1 || !strings.HasPrefix(stream.URLs[0], "data:text/html;charset=utf-8,") {
		t.Fatalf("document URL = %#v, want data:text/html", stream.URLs)
	}
	escaped := strings.TrimPrefix(stream.URLs[0], "data:text/html;charset=utf-8,")
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		t.Fatalf("decode html data URL: %v", err)
	}
	if decoded != html {
		t.Fatalf("decoded html = %q, want %q", decoded, html)
	}
	if mi.Extra["html_content"] != html {
		t.Fatalf("html_content extra = %#v, want %q", mi.Extra["html_content"], html)
	}
}

func TestMediaFromSourcesKeepsMixedVideoAndHTML(t *testing.T) {
	mi, err := mediaFromSources("课程", []source{
		{name: "视频", kind: "视频", url: "https://example.com/video.mp4"},
		{name: "图文", kind: "图文", html: "<p>content</p>"},
	})
	if err != nil {
		t.Fatalf("mediaFromSources returned error: %v", err)
	}
	if len(mi.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(mi.Entries))
	}
	if _, ok := mi.Entries[0].Streams["default"]; !ok {
		t.Fatalf("video default stream missing: %#v", mi.Entries[0].Streams)
	}
	if _, ok := mi.Entries[1].Streams["document"]; !ok {
		t.Fatalf("html document stream missing: %#v", mi.Entries[1].Streams)
	}
}

func TestBuildAnswerHTMLRendersQuestions(t *testing.T) {
	doc := buildAnswerHTML("答题课", map[string]any{"pay_content": "<p>课程介绍</p>"}, map[string]any{"quest_total": 1}, map[string]any{"id": "log1"}, nil, nil, []map[string]any{
		{
			"id":             "q1",
			"title":          "1+1 等于几?",
			"type":           1,
			"options":        []any{map[string]any{"key": "A", "content": "2"}, map[string]any{"key": "B", "content": "3"}},
			"correct_answer": "A",
			"analysis":       "基础加法",
		},
	})
	for _, want := range []string{"答题课", "1+1 等于几?", "单选题", "基础加法"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("answer html missing %q: %s", want, doc)
		}
	}
}

func TestMediaFromSourcesKeepsAnswerHTML(t *testing.T) {
	html := buildAnswerHTML("答题课", nil, nil, nil, nil, nil, nil)
	mi, err := mediaFromSources("课程", []source{{name: "答题课", kind: "答题HTML", answerHTML: html, extra: map[string]any{"product_type": "9"}}})
	if err != nil {
		t.Fatalf("mediaFromSources returned error: %v", err)
	}
	stream, ok := mi.Streams["document"]
	if !ok {
		t.Fatalf("document stream missing: %#v", mi.Streams)
	}
	if stream.Format != "html" || !strings.HasPrefix(stream.URLs[0], "data:text/html;charset=utf-8,") {
		t.Fatalf("stream = %#v, want html data URL", stream)
	}
	if mi.Extra["answer_html"] != true || mi.Extra["product_type"] != "9" {
		t.Fatalf("answer extra = %#v", mi.Extra)
	}
}
