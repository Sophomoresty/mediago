package cctalk

import (
	"net/url"
	"strings"
	"testing"
)

func TestEntriesFromMapBuildsArticleAndFile(t *testing.T) {
	item := map[string]any{
		"articleInfo": map[string]any{
			"articleId":   "art-1",
			"articleName": "图文课",
			"content":     "<p>正文</p>",
		},
		"materials": []any{
			map[string]any{
				"fileName": "资料.pdf",
				"fileUrl":  "https://cdn.example.com/files/资料.pdf",
				"fileSize": "2048",
			},
		},
	}
	entries := entriesFromMap(&apiClient{headers: baseHeaders()}, item, "课时1")
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Streams == nil || entries[0].Streams["document"].Format != "html" {
		t.Fatalf("article stream = %#v", entries[0].Streams)
	}
	if got := entries[1].Streams["file"]; got.Format != "pdf" || got.URLs[0] != "https://cdn.example.com/files/资料.pdf" {
		t.Fatalf("file stream = %#v", got)
	}
}

func TestBuildOCSV55StreamRewritesPlaylistAndKey(t *testing.T) {
	item := map[string]any{
		"m3u8s": []any{
			map[string]any{
				"resourceId": "board-1",
				"content":    "#EXTM3U\n#EXTINF:10,\nseg0.ts\n#EXTINF:10,\nseg1.ts\n",
				"key":        "AQIDBAUGBwgJCgsMDQ4PEA==",
				"iv":         "0102030405060708090a0b0c0d0e0f10",
			},
		},
		"cdnHosts": []any{"https://cdn.example.com/root"},
	}
	stream, extra, ok := buildEmbeddedOCSStream(item, extractCoursewareInfo(map[string]any{"coursewareId": "cw-1"}))
	if !ok {
		t.Fatal("buildEmbeddedOCSStream returned false")
	}
	if stream.Format != "m3u8" || len(stream.URLs) != 1 || !strings.HasPrefix(stream.URLs[0], "data:application/vnd.apple.mpegurl") {
		t.Fatalf("stream = %#v", stream)
	}
	playlist, err := url.PathUnescape(strings.SplitN(stream.URLs[0], ",", 2)[1])
	if err != nil {
		t.Fatalf("decode playlist: %v", err)
	}
	if !strings.Contains(playlist, "https://cdn.example.com/root/seg0.ts") || !strings.Contains(playlist, "#EXT-X-KEY:METHOD=AES-128") {
		t.Fatalf("playlist not rewritten: %s", playlist)
	}
	if extra["mode"] != "v55" || extra["m3u8_resource_id"] != "board-1" {
		t.Fatalf("extra = %#v", extra)
	}
}
