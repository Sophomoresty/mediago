package icourse163

import (
	"strings"
	"testing"
)

func TestParseKaoyanURL(t *testing.T) {
	tests := []struct {
		rawURL string
		cid    string
		termID string
		liveID string
	}{
		{
			rawURL: "https://www.icourse163.org/learn/kaopei-1003304001?tid=1003525006#/learn/announce",
			cid:    "1003304001",
			termID: "1003525006",
		},
		{
			rawURL: "https://kaoyan.icourse163.org/course/terms/12001.htm?courseId=33001",
			cid:    "33001",
			termID: "12001",
		},
		{
			rawURL: "https://www.icourse163.org/live/anything/98765.htm",
			liveID: "98765",
		},
	}
	for _, tt := range tests {
		got, ok := parseKaoyanURL(tt.rawURL)
		if !ok {
			t.Fatalf("parseKaoyanURL(%q) did not match", tt.rawURL)
		}
		if got.cid != tt.cid || got.termID != tt.termID || got.liveID != tt.liveID {
			t.Fatalf("parseKaoyanURL(%q) = %+v, want cid=%q term=%q live=%q", tt.rawURL, got, tt.cid, tt.termID, tt.liveID)
		}
	}
}

func TestParseColumnURL(t *testing.T) {
	for _, rawURL := range []string{
		"https://www.icourse163.org/columns/1205916206.htm",
		"https://www.icourse163.org/column/learn/1205916206.htm",
		"https://www.icourse163.org/column/learn/1205916206/chapter.htm",
	} {
		got, ok := parseColumnURL(rawURL)
		if !ok || got.cid != "1205916206" {
			t.Fatalf("parseColumnURL(%q) = %+v, %v; want cid 1205916206", rawURL, got, ok)
		}
	}
}

func TestParseMocTermJSONChapters(t *testing.T) {
	body := `{"result":{"mocTermDto":{"chapters":[{"id":1,"name":"章","lessons":[{"id":2,"name":"课","units":[{"contentId":3,"contentType":1,"id":4,"name":"视频.mp4"},{"contentId":5,"contentType":3,"id":6,"name":"讲义.pdf"}]}]}]}}}`
	chapters, err := parseMocTermJSONChapters(body)
	if err != nil {
		t.Fatalf("parseMocTermJSONChapters: %v", err)
	}
	if len(chapters) != 1 || len(chapters[0].lessons) != 1 || len(chapters[0].lessons[0].videos) != 2 {
		t.Fatalf("unexpected tree: %#v", chapters)
	}
	v := chapters[0].lessons[0].videos[0]
	if v.contentID != "3" || v.contentType != "1" || v.unitID != "4" || v.name != "视频.mp4" {
		t.Fatalf("video unit = %#v", v)
	}
	p := chapters[0].lessons[0].videos[1]
	if p.contentID != "5" || p.contentType != "3" || p.unitID != "6" || p.name != "讲义.pdf" {
		t.Fatalf("pdf unit = %#v", p)
	}
}

func TestParseColumnLessonsAndUnits(t *testing.T) {
	lessons, err := parseColumnLessons(`{"result":[{"id":123,"name":"lesson"}]}`)
	if err != nil {
		t.Fatalf("parseColumnLessonsForTest: %v", err)
	}
	if len(lessons) != 1 || lessons[0].id != "123" {
		t.Fatalf("lessons = %#v", lessons)
	}
	units, err := parseColumnUnits(`{"result":[{"lessonUnitId":456,"contentType":8,"name":"音频"},{"id":789,"contentType":1,"name":"视频"}]}`)
	if err != nil {
		t.Fatalf("parseColumnUnitsForTest: %v", err)
	}
	if len(units) != 2 || units[0].lessonUnitID != "456" || units[0].contentType != "8" || units[1].lessonUnitID != "789" {
		t.Fatalf("units = %#v", units)
	}
}

func TestTextbookSectionEntryReturnsDownloadableHTMLStream(t *testing.T) {
	entry := textbookSectionEntry("1001", textbookLeaf{title: "原标题", externalID: "ext1"}, map[string]any{"Title": "目录标题"}, `<h1>正文</h1><script>alert(1)</script><img src="//img.example/a.png">`, 2)
	if entry.Title != "目录标题" {
		t.Fatalf("Title = %q, want 目录标题", entry.Title)
	}
	stream, ok := entry.Streams["document"]
	if !ok {
		t.Fatalf("document stream missing: %#v", entry.Streams)
	}
	if stream.Format != "html" || len(stream.URLs) != 1 || !strings.HasPrefix(stream.URLs[0], "data:text/html;charset=utf-8,") {
		t.Fatalf("stream = %#v, want html data URL", stream)
	}
	content, _ := entry.Extra["content"].(string)
	if strings.Contains(content, "<script") || !strings.Contains(content, `src="https://img.example/a.png"`) {
		t.Fatalf("normalized content = %q", content)
	}
}
