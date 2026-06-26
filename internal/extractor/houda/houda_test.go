package houda

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/util"
)

func TestRequestHoudaFallsBackToWrappedData(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		bodies = append(bodies, r.Form.Encode())
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("data") == `{"classId":"C1"}` {
			fmt.Fprint(w, `{"code":"1","data":{"ok":true}}`)
			return
		}
		fmt.Fprint(w, `not-json`)
	}))
	defer srv.Close()

	got, err := requestHouda(util.NewClient(), srv.URL, map[string]string{"classId": "C1"}, nil)
	if err != nil {
		t.Fatalf("requestHouda returned error: %v", err)
	}
	if stringValue(got["code"]) != "1" {
		t.Fatalf("code = %#v, want 1", got["code"])
	}
	if len(bodies) != 2 {
		t.Fatalf("request count = %d, want 2; bodies=%#v", len(bodies), bodies)
	}
	if bodies[0] != "classId=C1" {
		t.Fatalf("raw form body = %q, want classId=C1", bodies[0])
	}
	if !strings.HasPrefix(bodies[1], "data=") {
		t.Fatalf("wrapped form body = %q, want data=...", bodies[1])
	}
}

func TestParseHoudaCourseListSkipsMaterialTabs(t *testing.T) {
	root := map[string]any{
		"code": "1",
		"data": map[string]any{
			"tabList": []any{
				map[string]any{
					"name": "已购课程",
					"dataList": []any{
						map[string]any{"id": "101", "title": "民法精讲"},
						map[string]any{"classId": "102", "courseName": "刑法精讲"},
					},
				},
				map[string]any{
					"name":     "资料",
					"code":     "ZL",
					"dataList": []any{map[string]any{"id": "M1", "title": "讲义"}},
				},
			},
		},
	}

	got := parseHoudaCourseList(root)
	if len(got) != 2 {
		t.Fatalf("course count = %d, want 2: %#v", len(got), got)
	}
	if got[0].ID != "101" || got[0].Title != "民法精讲" {
		t.Fatalf("first course = %#v, want id=101 title=民法精讲", got[0])
	}
	if got[1].ID != "102" || got[1].Title != "刑法精讲" {
		t.Fatalf("second course = %#v, want id=102 title=刑法精讲", got[1])
	}
	if got[0].Raw["tab_name"] != "已购课程" {
		t.Fatalf("tab_name = %#v, want 已购课程", got[0].Raw["tab_name"])
	}
}

func TestParseHoudaLessonsReadsLiveList(t *testing.T) {
	root := map[string]any{
		"code": "1",
		"data": map[string]any{
			"liveList": []any{
				map[string]any{
					"id":          "L1",
					"title":       "第一讲",
					"ccLiveId":    "room-1",
					"recordId":    "record-1",
					"playbackMp4": "https://example.com/a.mp4",
				},
			},
		},
	}

	got := parseHoudaLessons(root)
	if len(got) != 1 {
		t.Fatalf("lesson count = %d, want 1", len(got))
	}
	if firstText(got[0].ID) != "L1" || firstText(got[0].Title) != "第一讲" {
		t.Fatalf("lesson = %#v, want id/title preserved", got[0])
	}
	if firstText(got[0].RoomID, got[0].CCLiveID) != "room-1" {
		t.Fatalf("room id = %q, want room-1", firstText(got[0].RoomID, got[0].CCLiveID))
	}
}

func TestHoudaMaterialHelpersMatchSourceBranches(t *testing.T) {
	laws := collectHoudaLawRefs(map[string]any{
		"lawList": []any{
			map[string]any{"id": "law-1", "name": "民法"},
			map[string]any{"id": "law-1", "name": "重复"},
			map[string]any{"lawId": "law-2", "lawName": "刑法"},
		},
	})
	if len(laws) != 2 || laws[0].ID != "law-1" || laws[1].ID != "law-2" {
		t.Fatalf("laws = %#v, want unique law-1/law-2", laws)
	}

	item := map[string]any{"downLoadUrl": "/files/lecture.pdf", "title": "配套讲义"}
	if key := houdaMaterialKey(item); key != "url:http://www.houdask.com/files/lecture.pdf" {
		t.Fatalf("material key = %q, want normalized URL key", key)
	}

	entry := buildHoudaMaterialEntry(1, item, nil)
	if entry == nil {
		t.Fatalf("material entry is nil")
	}
	stream, ok := entry.Streams["file"]
	if !ok {
		t.Fatalf("file stream missing: %#v", entry.Streams)
	}
	if len(stream.URLs) != 1 || stream.URLs[0] != "http://www.houdask.com/files/lecture.pdf" {
		t.Fatalf("material URL = %#v, want normalized absolute URL", stream.URLs)
	}
	if stream.Format != "pdf" {
		t.Fatalf("format = %q, want pdf", stream.Format)
	}
}
