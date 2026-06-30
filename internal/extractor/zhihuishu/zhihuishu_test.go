package zhihuishu

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/util"
)

func TestExtractCourseHomeID(t *testing.T) {
	tests := map[string]string{
		"https://coursehome.zhihuishu.com/courseHome/1000006263#teachTeam": "1000006263",
		"https://study.zhihuishu.com/path?courseId=20001":                  "20001",
		"https://hikeweb.zhihuishu.com/hike-tch/course/x?proCourseId=42":   "42",
	}
	for rawURL, want := range tests {
		if got := extractCourseHomeID(rawURL); got != want {
			t.Fatalf("extractCourseHomeID(%q) = %q, want %q", rawURL, got, want)
		}
	}
}

func TestCourseHomeTitle(t *testing.T) {
	page := `var courseName = "大学物理"; var schoolName = "测试大学"; var termId = 123;`
	if got := courseHomeTitle(page, "fallback"); got != "大学物理_测试大学" {
		t.Fatalf("courseHomeTitle = %q", got)
	}
}

func TestParseCourseHomeVideos(t *testing.T) {
	body := `
<div class="onlines-sections-list-container">
  <div class="online-sections-wrap">
    <div class="online-item"><div class="online-section-title-text-wrap" title="第一章"></div></div>
    <div class="sections-wrap">
      <div class="section-item" videoid="v100">
        <div class="online-section-title-text-wrap" title="第一节"></div>
      </div>
      <div class="section-childnode-item" videoid="v101">
        <div class="online-section-title-text-wrap" title="小节 A"></div>
      </div>
      <div class="section-childnode-item" videoid="v102">
        <div class="online-section-title-text-wrap" title="小节 B"></div>
      </div>
    </div>
  </div>
</div>`
	videos, err := parseCourseHomeVideos(body)
	if err != nil {
		t.Fatalf("parseCourseHomeVideos: %v", err)
	}
	if len(videos) != 3 {
		t.Fatalf("videos len = %d, want 3: %#v", len(videos), videos)
	}
	want := []courseHomeVideo{
		{Title: "[1.1]--第一节", VideoID: "v100"},
		{Title: "[1.1.1]--小节 A", VideoID: "v101"},
		{Title: "[1.1.2]--小节 B", VideoID: "v102"},
	}
	for i := range want {
		if videos[i] != want[i] {
			t.Fatalf("videos[%d] = %#v, want %#v", i, videos[i], want[i])
		}
	}
}

func TestParseKgCourseTreeExtractsHashResources(t *testing.T) {
	body := `{
	  "data": [{
	    "name": "chapter",
	    "lessons": [{
	      "name": "hash lesson",
	      "idHash": "hash-1",
	      "idStr": "str-1",
	      "smallLessons": [{
	        "name": "small hash",
	        "idHash": "hash-2",
	        "idStr": "str-2"
	      }]
	    }]
	  }]
	}`
	videos, hashes := parseKgCourseTree(body)
	if len(videos) != 0 {
		t.Fatalf("videos len = %d, want 0: %#v", len(videos), videos)
	}
	want := []courseHomeHash{
		{Title: "[1.1]--hash lesson", IDStr: "str-1", IDHash: "hash-1"},
		{Title: "[1.1.1]--small hash", IDStr: "str-2", IDHash: "hash-2"},
	}
	if len(hashes) != len(want) {
		t.Fatalf("hashes len = %d, want %d: %#v", len(hashes), len(want), hashes)
	}
	for i := range want {
		if hashes[i] != want[i] {
			t.Fatalf("hashes[%d] = %#v, want %#v", i, hashes[i], want[i])
		}
	}
}

func TestCollectHashFileEntriesResolvesResources(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/gateway/t/node/queryNodeDescription"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"nodeRespResourceVos": []map[string]any{
						{
							"resourcesSuffix": "mp4",
							"resourcesFileId": "9001",
							"resourcesUrl":    "https://fallback.example.com/video.mp4",
							"resourcesName":   "视频.mp4",
						},
						{
							"resourcesSuffix": "pdf",
							"resourcesUrl":    "https://cdn.example.com/courseware.pdf",
							"resourcesName":   "课件.pdf",
						},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/video/initVideo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uuid":  "uuid-1",
					"lines": []map[string]any{{"lineID": 2}},
				},
			})
		case strings.Contains(r.URL.Path, "/video/changeVideoLine"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": "https://media.example.com/hash-video-hd.mp4",
			})
		default:
			http.NotFound(w, r)
		}
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	ctx := &courseContext{hashItems: []courseHomeHash{{Title: "[1.1]--hash", IDStr: "str-1", IDHash: "hash-1"}}}
	entries := collectHashFileEntries(util.NewClient(), ctx, zhihuishuHeaders("https://coursehome.zhihuishu.com/"), zhsMode{hd: true})
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2: %#v", len(entries), entries)
	}
	if got := goldenFirstPlayableURL(entries[0]); got != "https://media.example.com/hash-video-hd.mp4" {
		t.Fatalf("hash video URL = %q, want resolved HD URL", got)
	}
	if got := goldenFirstPlayableURL(entries[1]); got != "https://cdn.example.com/courseware.pdf" {
		t.Fatalf("hash file URL = %q, want courseware URL", got)
	}
}
