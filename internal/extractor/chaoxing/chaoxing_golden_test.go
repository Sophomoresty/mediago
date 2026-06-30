package chaoxing

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func TestCollectChaoxingChapters(t *testing.T) {
	html := `
<div class="chapter_td">
  <div class="chapter_unit">
    <div class="chapter_item"><div class="catalog_name"><span title="第一章"></span></div></div>
    <div class="catalog_level"><ul>
      <li><div class="chapter_item" title="1.1 视频课" onclick="toOld('1','101','3')"></div></li>
      <li><a href="/mycourse/studentstudy?courseId=1&clazzid=2&chapterId=202">1.2 文档课</a></li>
      <li><div class="chapter_item" title="重复章节" onclick="toOld('1','101','3')"></div></li>
    </ul></div>
  </div>
</div>`

	chapters := collectChaoxingChapters(html)
	if len(chapters) != 2 {
		t.Fatalf("expected 2 unique chapters, got %d: %#v", len(chapters), chapters)
	}
	if chapters[0].ID != "101" || chapters[0].Title != "1.1 视频课" || chapters[0].Index != 1 {
		t.Fatalf("unexpected first chapter: %#v", chapters[0])
	}
	if chapters[1].ID != "202" || chapters[1].Title != "1.2 文档课" || chapters[1].Index != 2 {
		t.Fatalf("unexpected second chapter: %#v", chapters[1])
	}
}

func TestParseCardCountAndKnowledgeID(t *testing.T) {
	if n, kid := parseCardCountAndKnowledgeID(`getClazzDetail('3','456','1','1','')`, "fallback"); n != 3 || kid != "456" {
		t.Fatalf("onclick form parsed to (%d, %q), want (3, 456)", n, kid)
	}

	html := `<input id="cardcount" type="hidden" value="2"><a href="/knowledge/cards?clazzid=1&courseid=2&knowledgeid=789&num=0&cpi=4">card</a>`
	if n, kid := parseCardCountAndKnowledgeID(html, "fallback"); n != 2 || kid != "789" {
		t.Fatalf("hidden/cards form parsed to (%d, %q), want (2, 789)", n, kid)
	}

	if n, kid := parseCardCountAndKnowledgeID(`<input id="cardcount" type="hidden" value="1">`, "888"); n != 1 || kid != "888" {
		t.Fatalf("hidden fallback parsed to (%d, %q), want (1, 888)", n, kid)
	}
}

func TestCollectChaoxingResources(t *testing.T) {
	cards := []string{
		`<script>mArg = {"attachments":[{"property":{"name":"Video.mp4","objectid":"oid1","mid":"mid1","type":".mp4"}}]};</script>`,
		`<div class="ans-job-icon" data="{&quot;title&quot;:&quot;Live Room&quot;,&quot;liveId&quot;:&quot;live1&quot;,&quot;jobid&quot;:&quot;job1&quot;}"></div>`,
		`<div data="{&quot;title&quot;:&quot;Audio&quot;,&quot;url&quot;:&quot;https://appswh.chaoxing.com/vclass/page/view/uuid1&quot;}"></div>`,
		`<script>mArg = {"attachments":[{"property":{"name":"Doc.pdf","objectid":"oid2","type":".pdf"}}]};</script>`,
	}

	resources := collectChaoxingResources(cards, "fallback")
	assertResource(t, resources, func(r chaoxingResource) bool {
		return r.Kind == "video" && r.ObjectID == "oid1" && r.Mid == "mid1" && r.Title == "Video.mp4"
	})
	assertResource(t, resources, func(r chaoxingResource) bool {
		return r.Kind == "live" && r.LiveID == "live1" && r.JobID == "job1" && r.Title == "Live Room"
	})
	assertResource(t, resources, func(r chaoxingResource) bool {
		return r.Kind == "audio" && r.UUID == "uuid1" && r.Title == "Audio"
	})
	assertResource(t, resources, func(r chaoxingResource) bool {
		return r.Kind == "file" && r.ObjectID == "oid2" && r.Ext == "pdf" && r.Title == "Doc.pdf"
	})
}

func TestResolveCourseTraversesAjaxCardsAndResources(t *testing.T) {
	mux := http.NewServeMux()
	coursePage := `
<html><head><title>Course Alpha</title></head><body>
<input id="courseId" value="1"><input id="clazzid" value="2"><input id="enc" value="abc"><input id="cpi" value="9">
<div class="chapter_td"><div class="chapter_unit">
  <div class="chapter_item"><div class="catalog_name"><span title="第一章"></span></div></div>
  <div class="catalog_level"><ul>
    <li><div class="chapter_item" title="Lesson One" onclick="toOld('1','101','3')"></div></li>
  </ul></div>
</div></div>
</body></html>`

	mux.HandleFunc("/entry", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(coursePage))
	})
	mux.HandleFunc("/mycourse/studentcourse", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "1" || r.URL.Query().Get("clazzid") != "2" || r.URL.Query().Get("enc") != "abc" {
			t.Fatalf("unexpected studentcourse query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(coursePage))
	})
	mux.HandleFunc("/mycourse/studentstudyAjax", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("studentstudyAjax method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := url.Values{"chapterId": {"101"}, "clazzid": {"2"}, "courseId": {"1"}, "cpi": {"9"}}
		for k, v := range want {
			if got := r.Form.Get(k); got != v[0] {
				t.Fatalf("form %s = %q, want %q", k, got, v[0])
			}
		}
		w.Write([]byte(`getClazzDetail('2','101','1','1','')`))
	})
	mux.HandleFunc("/knowledge/cards", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("knowledgeid") != "101" || r.URL.Query().Get("cpi") != "9" {
			t.Fatalf("unexpected cards query: %s", r.URL.RawQuery)
		}
		switch r.URL.Query().Get("num") {
		case "0":
			w.Write([]byte(`<script>mArg = {"attachments":[{"property":{"name":"Video.mp4","objectid":"oid-video","mid":"mid1","type":".mp4"}},{"property":{"name":"Doc.pdf","objectid":"oid-doc","type":".pdf"}}]};</script>`))
		case "1":
			w.Write([]byte(`<div data="{&quot;title&quot;:&quot;Live Room&quot;,&quot;liveId&quot;:&quot;live1&quot;,&quot;jobid&quot;:&quot;job1&quot;}"></div>`))
		default:
			t.Fatalf("unexpected card num: %s", r.URL.Query().Get("num"))
		}
	})
	mux.HandleFunc("/ananas/status/oid-video", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"filename":"Video.mp4","download":"https://cdn.example/video.mp4","httphd":"https://cdn.example/video-hd.mp4"}`))
	})
	mux.HandleFunc("/ananas/status/oid-doc", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"filename":"Doc.pdf","download":"https://cdn.example/doc.pdf"}`))
	})
	mux.HandleFunc("/richvideo/subtitle", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mid") != "mid1" {
			t.Fatalf("subtitle mid = %q, want mid1", r.URL.Query().Get("mid"))
		}
		w.Write([]byte(`[{"url":"https://cdn.example/sub.srt"}]`))
	})
	mux.HandleFunc("/ananas/live/liveinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("liveid") != "live1" || r.URL.Query().Get("jobid") != "job1" {
			t.Fatalf("unexpected liveinfo query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"temp":{"data":{"mp4Url":"https://cdn.example/live.mp4"}}}`))
	})
	mux.HandleFunc("/meet-review", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:             util.NewClient(),
		courseURL:     srv.URL,
		meetReviewURL: srv.URL + "/meet-review?uuid=%s",
		yunFileURL:    srv.URL + "/yun-file?objectId=%s",
		headers:       map[string]string{"Referer": srv.URL + "/"},
	}
	info, pageObjectID, err := ctx.resolveCourse(srv.URL + "/entry?courseId=1&clazzid=2&enc=abc&cpi=9")
	if err != nil {
		t.Fatalf("resolveCourse returned error: %v", err)
	}
	if pageObjectID != "" {
		t.Fatalf("pageObjectID = %q, want empty", pageObjectID)
	}
	if info.Title != "Course Alpha" {
		t.Fatalf("course title = %q, want Course Alpha", info.Title)
	}
	if len(info.Entries) != 3 {
		t.Fatalf("entries = %d, want 3: %#v", len(info.Entries), info.Entries)
	}
	assertEntryURL(t, info.Entries, "https://cdn.example/video.mp4")
	assertEntryURL(t, info.Entries, "https://cdn.example/doc.pdf")
	assertEntryURL(t, info.Entries, "https://cdn.example/live.mp4")
	if !hasSubtitle(info.Entries, "https://cdn.example/sub.srt") {
		t.Fatalf("expected video subtitle URL to be preserved")
	}
}

func assertResource(t *testing.T, resources []chaoxingResource, pred func(chaoxingResource) bool) {
	t.Helper()
	for _, r := range resources {
		if pred(r) {
			return
		}
	}
	t.Fatalf("resource not found in %#v", resources)
}

func assertEntryURL(t *testing.T, entries []*extractor.MediaInfo, want string) {
	t.Helper()
	for _, entry := range entries {
		for _, stream := range entry.Streams {
			for _, got := range stream.URLs {
				if got == want {
					if strings.TrimSpace(entry.Title) == "" {
						t.Fatalf("entry for %s has empty title", want)
					}
					return
				}
			}
		}
	}
	t.Fatalf("entry URL %s not found in %#v", want, entries)
}

func hasSubtitle(entries []*extractor.MediaInfo, want string) bool {
	for _, entry := range entries {
		for _, sub := range entry.Subtitles {
			if sub.URL == want {
				return true
			}
		}
	}
	return false
}

func TestExtractMock(t *testing.T) {
	fixture := readChaoxingGoldenFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	assertChaoxingFixtureServed(t, srv.URL, fixture)

	testURL := "https://mooc1.chaoxing.com/mycourse/studentstudy?courseId=1001&clazzid=2002&cpi=3003"
	ext, err := extractor.Match(testURL)
	if err != nil {
		t.Fatalf("extractor pattern should match fixture URL: %v", err)
	}
	info, err := ext.Extract(testURL, nil)
	if err == nil {
		t.Fatalf("expected login-cookie error, got info: %#v", info)
	}
	if info != nil {
		t.Fatalf("expected nil MediaInfo on auth error, got %#v", info)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "requires login cookies") {
		t.Fatalf("expected explicit auth error, got %v", err)
	}
}

func readChaoxingGoldenFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("fixture is not valid JSON: %s", b)
	}
	return b
}

func assertChaoxingFixtureServed(t *testing.T, baseURL string, want []byte) {
	t.Helper()
	resp, err := http.Get(baseURL + "/fixture")
	if err != nil {
		t.Fatalf("fetch fixture from mock server: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read fixture response: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("mock fixture mismatch: got %s want %s", got, want)
	}
}

func TestResolvePortalCourseResources(t *testing.T) {
	mux := http.NewServeMux()
	portalPage := `<html><head><title>课程门户首页</title></head><body>
<input id="courseId" value="1"><input id="courseEnc" value="ce"><input id="portalEnc" value="pe"><input id="t" value="123">
</body></html>`
	mux.HandleFunc("/course-ans/courseportal/portal/1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(portalPage))
	})
	mux.HandleFunc("/course-ans/courseportal/portal-basic-info", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "1" || r.URL.Query().Get("courseEnc") != "ce" || r.URL.Query().Get("t") != "123" {
			t.Fatalf("unexpected portal basic query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"dataInfo":{"courseName":"Portal Course"}}`))
	})
	mux.HandleFunc("/course-ans/courseportal/portal-node-resource", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "1" || r.URL.Query().Get("showResourceCount") != "true" {
			t.Fatalf("unexpected portal resource query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"fileArray":[{"nodeId":"n1","fileName":"Portal Doc.pdf","fileExtension":"pdf","statusUrl":"/status/doc"}]}`))
	})
	mux.HandleFunc("/status/doc", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"download":"https://cdn.example/portal-doc.pdf"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{c: util.NewClient(), courseURL: srv.URL, headers: map[string]string{"Referer": srv.URL + "/"}, downpath: "https://cs-ans.chaoxing.com"}
	info, _, err := ctx.resolveCourse(srv.URL + "/course-ans/courseportal/portal/1?courseid=1")
	if err != nil {
		t.Fatalf("resolveCourse returned error: %v", err)
	}
	if info.Title != "Portal Course" {
		t.Fatalf("course title = %q, want Portal Course", info.Title)
	}
	if len(info.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(info.Entries), info.Entries)
	}
	assertEntryURL(t, info.Entries, "https://cdn.example/portal-doc.pdf")
}

func TestResolvePublicCourseFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/course/222529124.html", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>Public Course</title></head><body>公开课</body></html>`))
	})
	mux.HandleFunc("/course-ans/moocstatistics/chapterlist", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "222529124" {
			t.Fatalf("chapterlist courseId = %q, want 222529124", r.URL.Query().Get("courseId"))
		}
		w.Write([]byte(`
<ul class="chapter-list">
  <li class="chapter"><div><a>第一章</a></div><ul class="section-list">
    <li><p onclick="jumpKnowledge(101)"><a>1.1 公开视频</a></p></li>
  </ul></li>
</ul>`))
	})
	mux.HandleFunc("/nodedetailcontroller/visitnodedetail", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "222529124" || r.URL.Query().Get("knowledgeId") != "101" {
			t.Fatalf("unexpected public knowledge query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`<script>mArg = {"attachments":[{"property":{"name":"Public.mp4","objectid":"oid-public","type":".mp4"}}]};</script>`))
	})
	mux.HandleFunc("/ananas/status/oid-public", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"filename":"Public.mp4","download":"https://cdn.example/public.mp4"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:               util.NewClient(),
		courseURL:       srv.URL,
		newCourseURL:    srv.URL,
		publicCourseURL: srv.URL,
		headers:         map[string]string{"Referer": srv.URL + "/"},
		downpath:        "https://cs-ans.chaoxing.com",
	}
	info, _, err := ctx.resolveCourse(srv.URL + "/course/222529124.html")
	if err != nil {
		t.Fatalf("resolveCourse returned error: %v", err)
	}
	if info.Title != "Public Course" {
		t.Fatalf("course title = %q, want Public Course", info.Title)
	}
	if len(info.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(info.Entries), info.Entries)
	}
	assertEntryURL(t, info.Entries, "https://cdn.example/public.mp4")
	if got := info.Entries[0].Extra["source"]; got != "public-course" {
		t.Fatalf("entry source = %#v, want public-course", got)
	}
}

func TestResolveMoocAnsPublicCourseFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mooc-ans/course/207139102.html", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>Mooc Ans Public</title></head><body>公开课</body></html>`))
	})
	mux.HandleFunc("/course-ans/moocstatistics/chapterlist", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "207139102" {
			t.Fatalf("chapterlist courseId = %q, want 207139102", r.URL.Query().Get("courseId"))
		}
		w.Write([]byte(`<ul class="chapter-list"><li><p onclick="jumpKnowledge(101)"><a>1.1 公开视频</a></p></li></ul>`))
	})
	mux.HandleFunc("/nodedetailcontroller/visitnodedetail", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("knowledgeId") != "101" {
			t.Fatalf("unexpected public knowledge query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`<script>mArg = {"attachments":[{"property":{"name":"MoocAns.mp4","objectid":"oid-mooc-ans","type":".mp4"}}]};</script>`))
	})
	mux.HandleFunc("/ananas/status/oid-mooc-ans", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"filename":"MoocAns.mp4","download":"https://cdn.example/mooc-ans.mp4"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:               util.NewClient(),
		courseURL:       srv.URL,
		newCourseURL:    srv.URL,
		publicCourseURL: srv.URL,
		headers:         map[string]string{"Referer": srv.URL + "/"},
		downpath:        "https://cs-ans.chaoxing.com",
	}
	info, _, err := ctx.resolveCourse(srv.URL + "/mooc-ans/course/207139102.html")
	if err != nil {
		t.Fatalf("resolveCourse returned error: %v", err)
	}
	if ctx.newCourse {
		t.Fatalf("mooc-ans public course page must not be treated as mooc2 joined course")
	}
	if info.Title != "Mooc Ans Public" {
		t.Fatalf("title = %q, want Mooc Ans Public", info.Title)
	}
	assertEntryURL(t, info.Entries, "https://cdn.example/mooc-ans.mp4")
}

func TestCollectChaoxingChaptersIncludesInnerKnowledgeLinks(t *testing.T) {
	html := `
<span onclick="goChapter('/nodedetailcontroller/visitnodedetail?courseId=1&knowledgeId=201')">1.1 Inner A</span>
<a href="/nodedetailcontroller/visitnodedetail?courseId=1&knowledgeId=202">1.2 Inner B</a>`
	chapters := collectChaoxingChapters(html)
	if len(chapters) != 2 {
		t.Fatalf("inner knowledge chapters = %d, want 2: %#v", len(chapters), chapters)
	}
	if chapters[0].ID != "202" || chapters[1].ID != "201" {
		t.Fatalf("unexpected inner knowledge ids: %#v", chapters)
	}
}

func TestResolveSpaceIndexCourses(t *testing.T) {
	mux := http.NewServeMux()
	coursePage := `
<html><head><title>Space Course</title></head><body>
<input id="courseId" value="1"><input id="clazzid" value="2"><input id="enc" value="abc"><input id="cpi" value="9">
<div class="chapter_item" title="Lesson One" onclick="toOld('1','101','3')"></div>
</body></html>`
	mux.HandleFunc("/space/index", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><p class="personalName">Alice</p><a href="/mycourse/studentcourse?courseId=1&clazzid=2&enc=abc&cpi=9">Space Course</a></body></html>`))
	})
	mux.HandleFunc("/mycourse/studentcourse", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "1" || r.URL.Query().Get("clazzid") != "2" || r.URL.Query().Get("enc") != "abc" {
			t.Fatalf("unexpected space studentcourse query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(coursePage))
	})
	mux.HandleFunc("/mycourse/studentstudyAjax", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("chapterId") != "101" || r.Form.Get("courseId") != "1" || r.Form.Get("clazzid") != "2" {
			t.Fatalf("unexpected ajax form: %#v", r.Form)
		}
		w.Write([]byte(`getClazzDetail('1','101','1','1','')`))
	})
	mux.HandleFunc("/knowledge/cards", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<script>mArg = {"attachments":[{"property":{"name":"Space.mp4","objectid":"oid-space","type":".mp4"}}]};</script>`))
	})
	mux.HandleFunc("/ananas/status/oid-space", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"filename":"Space.mp4","download":"https://cdn.example/space.mp4"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:               util.NewClient(),
		courseURL:       srv.URL,
		newCourseURL:    srv.URL,
		publicCourseURL: srv.URL,
		downpath:        "https://cs-ans.chaoxing.com",
		headers:         map[string]string{"Referer": srv.URL + "/"},
	}
	info, err := ctx.resolveSpaceIndex(srv.URL + "/space/index")
	if err != nil {
		t.Fatalf("resolveSpaceIndex returned error: %v", err)
	}
	if info.Title != "chaoxing_space_courses" {
		t.Fatalf("space title = %q, want chaoxing_space_courses", info.Title)
	}
	if len(info.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(info.Entries), info.Entries)
	}
	assertEntryURL(t, info.Entries, "https://cdn.example/space.mp4")
	if got := info.Entries[0].Extra["source"]; got != "i.mooc.space" {
		t.Fatalf("entry source = %#v, want i.mooc.space", got)
	}
}

func TestResolveSpaceIndexCoursesFromEmbeddedMoocJSON(t *testing.T) {
	mux := http.NewServeMux()
	coursePage := `
<html><head><title>Embedded Space Course</title></head><body>
<input id="courseId" value="1"><input id="clazzid" value="2"><input id="enc" value="abc"><input id="cpi" value="9">
<div class="chapter_item" title="Lesson One" onclick="toOld('1','101','3')"></div>
</body></html>`
	mux.HandleFunc("/space/index", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><p class="personalName">Alice</p>
<script>window.courseList=[{"courseId":"1","clazzid":"2","enc":"abc","cpi":"9","courseName":"Embedded Space Course"}];</script>
</body></html>`))
	})
	mux.HandleFunc("/mycourse/studentcourse", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("courseId") != "1" || r.URL.Query().Get("clazzid") != "2" || r.URL.Query().Get("enc") != "abc" || r.URL.Query().Get("cpi") != "9" {
			t.Fatalf("unexpected embedded space studentcourse query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(coursePage))
	})
	mux.HandleFunc("/mycourse/studentstudyAjax", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("chapterId") != "101" || r.Form.Get("courseId") != "1" || r.Form.Get("clazzid") != "2" {
			t.Fatalf("unexpected ajax form: %#v", r.Form)
		}
		w.Write([]byte(`getClazzDetail('1','101','1','1','')`))
	})
	mux.HandleFunc("/knowledge/cards", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<script>mArg = {"attachments":[{"property":{"name":"Embedded.mp4","objectid":"oid-embedded","type":".mp4"}}]};</script>`))
	})
	mux.HandleFunc("/ananas/status/oid-embedded", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"filename":"Embedded.mp4","download":"https://cdn.example/embedded.mp4"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:               util.NewClient(),
		courseURL:       srv.URL,
		newCourseURL:    srv.URL,
		publicCourseURL: srv.URL,
		downpath:        "https://cs-ans.chaoxing.com",
		headers:         map[string]string{"Referer": srv.URL + "/"},
	}
	links := collectChaoxingSpaceCourseLinks(`<script>window.courseList=[{"courseId":"1","clazzid":"2","enc":"abc","cpi":"9","courseName":"Embedded Space Course"}];</script>`, srv.URL+"/space/index")
	if len(links) != 1 {
		t.Fatalf("embedded links = %d, want 1: %#v", len(links), links)
	}
	if links[0].Title != "Embedded Space Course" {
		t.Fatalf("embedded link title = %q, want Embedded Space Course", links[0].Title)
	}
	attrLinks := collectChaoxingSpaceCourseLinks(`<div data-courseid="1" data-clazzid="2" data-enc="abc" data-cpi="9" data-coursename="Attr Course"></div>`, srv.URL+"/space/index")
	if len(attrLinks) != 1 || attrLinks[0].Title != "Attr Course" {
		t.Fatalf("attribute course links = %#v, want one Attr Course link", attrLinks)
	}
	looseLinks := collectChaoxingSpaceCourseLinks(`<script>window.courseList=[{courseId:'1', clazzid:'2', enc:'abc', cpi:'9', courseName:'Loose Course'}];</script>`, srv.URL+"/space/index")
	if len(looseLinks) != 1 || looseLinks[0].Title != "Loose Course" {
		t.Fatalf("loose object course links = %#v, want one Loose Course link", looseLinks)
	}
	info, err := ctx.resolveSpaceIndex(srv.URL + "/space/index")
	if err != nil {
		t.Fatalf("resolveSpaceIndex returned error: %v", err)
	}
	if len(info.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(info.Entries), info.Entries)
	}
	assertEntryURL(t, info.Entries, "https://cdn.example/embedded.mp4")
}

func TestResolveCourseProbesOpencAndIncludesMaterials(t *testing.T) {
	mux := http.NewServeMux()
	coursePage := `
<html><head><title>Material Course</title></head><body>
<input id="courseId" value="1"><input id="clazzid" value="2"><input id="enc" value="abc"><input id="cpi" value="9">
<div class="chapter_item" title="Material Lesson" onclick="toOld('1','101','3')"></div>
</body></html>`
	mux.HandleFunc("/entry", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(coursePage))
	})
	mux.HandleFunc("/mycourse/studentcourse", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(coursePage))
	})
	mux.HandleFunc("/mycourse/studentstudyAjax", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`getClazzDetail('0','101','1','1','')`))
	})
	mux.HandleFunc("/mycourse/transfer", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("moocId") != "1" || r.URL.Query().Get("clazzid") != "2" || !strings.Contains(r.URL.Query().Get("refer"), "/mycourse/studentstudy") {
			t.Fatalf("unexpected transfer query: %s", r.URL.RawQuery)
		}
		http.Redirect(w, r, "/transfer-final?openc=OPEN1", http.StatusFound)
	})
	mux.HandleFunc("/transfer-final", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<input id="openc" value="OPEN1">`))
	})
	mux.HandleFunc("/coursedata", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("openc") != "OPEN1" || r.URL.Query().Get("courseId") != "1" || r.URL.Query().Get("classId") != "2" || r.URL.Query().Get("enc") != "abc" {
			t.Fatalf("unexpected coursedata query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("pages") == "" {
			w.Write([]byte(`page.showPage(1, 1, "course_change", "首页", "尾页")`))
			return
		}
		w.Write([]byte(`<tbody id="tableId02">
<tr type="pdf" id="501"><td><input type="text" value="Material.pdf"></td></tr>
</tbody>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:               util.NewClient(),
		courseURL:       srv.URL,
		newCourseURL:    srv.URL,
		publicCourseURL: srv.URL,
		downpath:        "https://cs-ans.chaoxing.com",
		headers:         map[string]string{"Referer": srv.URL + "/"},
	}
	info, _, err := ctx.resolveCourse(srv.URL + "/entry?courseId=1&clazzid=2&enc=abc&cpi=9")
	if err != nil {
		t.Fatalf("resolveCourse returned error: %v", err)
	}
	if ctx.openc != "OPEN1" {
		t.Fatalf("openc = %q, want OPEN1", ctx.openc)
	}
	assertEntryURL(t, info.Entries, srv.URL+"/coursedata/downloadData?classId=2&courseId=1&dataId=501")
}

func TestResolveZhiboLivePrefersMeetReview(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zhibo/123", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta content="Nice Live" itemprop="name" /></head></html>`))
	})
	mux.HandleFunc("/ananas/live/liveinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("liveid") != "123" {
			t.Fatalf("liveid = %q, want 123", r.URL.Query().Get("liveid"))
		}
		w.Write([]byte(`{"temp":{"data":{"mp4Url":"https://cdn.example/live-direct.mp4"}}}`))
	})
	mux.HandleFunc("/meet", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uuid") != "uuid-abc" {
			t.Fatalf("uuid = %q, want uuid-abc", r.URL.Query().Get("uuid"))
		}
		w.Write([]byte(`{"data":[{"objectId":"oid-review"}]}`))
	})
	mux.HandleFunc("/yun", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("objectId") != "oid-review" {
			t.Fatalf("objectId = %q, want oid-review", r.URL.Query().Get("objectId"))
		}
		w.Write([]byte(`{"data":{"download":"https://cdn.example/live-review.mp4","http":"https://cdn.example/live-http.mp4"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:             util.NewClient(),
		courseURL:     srv.URL,
		livePageURL:   srv.URL + "/zhibo",
		meetReviewURL: srv.URL + "/meet?crossOrigin=true&uuid=%s",
		yunFileURL:    srv.URL + "/yun?crossOrigin=true&objectId=%s&key=",
		headers:       map[string]string{"Referer": srv.URL + "/"},
	}
	entry, err := ctx.resolveZhiboLiveEntry("123", "uuid-abc")
	if err != nil {
		t.Fatalf("resolveZhiboLiveEntry returned error: %v", err)
	}
	if entry.Title != "Nice Live" {
		t.Fatalf("entry title = %q, want Nice Live", entry.Title)
	}
	assertEntryURL(t, []*extractor.MediaInfo{entry}, "https://cdn.example/live-review.mp4")
}

func TestResolveZhiboLiveIDWithoutUUIDUsesLivePlayback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zhibo/123", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta itemprop="name" content="Direct Live" /></head></html>`))
	})
	mux.HandleFunc("/ananas/live/liveinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("liveid") != "123" {
			t.Fatalf("liveid = %q, want 123", r.URL.Query().Get("liveid"))
		}
		w.Write([]byte(`{"temp":{"data":{"mp4Url":"https://cdn.example/live-direct.mp4"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:           util.NewClient(),
		courseURL:   srv.URL,
		livePageURL: srv.URL + "/zhibo",
		headers:     map[string]string{"Referer": srv.URL + "/"},
	}
	if uuid := extractZhiboReviewUUID("https://zhibo.chaoxing.com/live/watch?liveid=123"); uuid != "" {
		t.Fatalf("zhibo liveid must not be reused as review uuid, got %q", uuid)
	}
	entry, err := ctx.resolveZhiboLiveEntry("123", "")
	if err != nil {
		t.Fatalf("resolveZhiboLiveEntry returned error: %v", err)
	}
	if entry.Title != "Direct Live" {
		t.Fatalf("entry title = %q, want Direct Live", entry.Title)
	}
	assertEntryURL(t, []*extractor.MediaInfo{entry}, "https://cdn.example/live-direct.mp4")
}

func TestExtractSpaceIndexEndToEnd(t *testing.T) {
	jar := chaoxingTestCookieJar(t)
	installChaoxingMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "i.mooc.chaoxing.com" && r.URL.Path == "/space/index":
			w.Write([]byte(`<html><body><p class="personalName">Alice</p><a href="https://mooc1.chaoxing.com/mycourse/studentcourse?courseId=1&clazzid=2&enc=abc&cpi=9">Space Course</a></body></html>`))
		case r.Host == "www.xueyinonline.com" && r.URL.Path == "/portal/new-header":
			w.Write([]byte(`<a id="logout">logout</a>`))
		case r.Host == "mooc1.chaoxing.com" && r.URL.Path == "/mycourse/studentcourse":
			w.Write([]byte(`<html><head><title>Space Course</title></head><body>
<input id="courseId" value="1"><input id="clazzid" value="2"><input id="enc" value="abc"><input id="cpi" value="9">
<div class="chapter_item" title="Lesson One" onclick="toOld('1','101','3')"></div>
</body></html>`))
		case r.Host == "mooc1.chaoxing.com" && r.URL.Path == "/mycourse/studentstudyAjax":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("chapterId") != "101" {
				t.Fatalf("chapterId = %q, want 101", r.Form.Get("chapterId"))
			}
			w.Write([]byte(`getClazzDetail('1','101','1','1','')`))
		case r.Host == "mooc1.chaoxing.com" && r.URL.Path == "/knowledge/cards":
			w.Write([]byte(`<script>mArg = {"attachments":[{"property":{"name":"Space.mp4","objectid":"oid-space-e2e","type":".mp4"}}]};</script>`))
		case r.Host == "mooc1.chaoxing.com" && r.URL.Path == "/ananas/status/oid-space-e2e":
			w.Write([]byte(`{"filename":"Space.mp4","download":"https://cdn.example/space-e2e.mp4"}`))
		case r.Host == "mooc1.chaoxing.com" && r.URL.Path == "/mycourse/transfer":
			w.Write([]byte(`no openc for this fixture`))
		default:
			t.Fatalf("unexpected request: host=%s path=%s query=%s", r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	info, err := (&Chaoxing{}).Extract("https://i.mooc.chaoxing.com/space/index", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	assertEntryURL(t, info.Entries, "https://cdn.example/space-e2e.mp4")
}

func TestExtractZhiboEndToEnd(t *testing.T) {
	jar := chaoxingTestCookieJar(t)
	installChaoxingMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "zhibo.chaoxing.com" && r.URL.Path == "/123":
			w.Write([]byte(`<html><head><meta content="End Live" itemprop="name" /></head></html>`))
		case r.Host == "mooc1.chaoxing.com" && r.URL.Path == "/ananas/live/liveinfo":
			if r.URL.Query().Get("liveid") != "123" {
				t.Fatalf("liveid = %q, want 123", r.URL.Query().Get("liveid"))
			}
			w.Write([]byte(`{"temp":{"data":{"mp4Url":"https://cdn.example/zhibo-e2e.mp4"}}}`))
		default:
			t.Fatalf("unexpected request: host=%s path=%s query=%s", r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	info, err := (&Chaoxing{}).Extract("https://zhibo.chaoxing.com/123", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if info.Title != "End Live" {
		t.Fatalf("title = %q, want End Live", info.Title)
	}
	assertEntryURL(t, []*extractor.MediaInfo{info}, "https://cdn.example/zhibo-e2e.mp4")
}

func TestResolveZhiboUUIDOnlyMeetReview(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/meet", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uuid") != "uuid-abc" {
			t.Fatalf("uuid = %q, want uuid-abc", r.URL.Query().Get("uuid"))
		}
		w.Write([]byte(`{"data":[{"objectId":"oid-review"}]}`))
	})
	mux.HandleFunc("/yun", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("objectId") != "oid-review" {
			t.Fatalf("objectId = %q, want oid-review", r.URL.Query().Get("objectId"))
		}
		w.Write([]byte(`{"data":{"download":"https://cdn.example/uuid-review.mp4"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := &chaoxingContext{
		c:             util.NewClient(),
		courseURL:     srv.URL,
		livePageURL:   srv.URL + "/zhibo",
		meetReviewURL: srv.URL + "/meet?crossOrigin=true&uuid=%s",
		yunFileURL:    srv.URL + "/yun?crossOrigin=true&objectId=%s&key=",
		headers:       map[string]string{"Referer": srv.URL + "/"},
	}
	entry, err := ctx.resolveZhiboLiveEntry("", "uuid-abc")
	if err != nil {
		t.Fatalf("resolveZhiboLiveEntry returned error: %v", err)
	}
	if entry.Title != "超星直播_uuid-abc" {
		t.Fatalf("entry title = %q, want 超星直播_uuid-abc", entry.Title)
	}
	assertEntryURL(t, []*extractor.MediaInfo{entry}, "https://cdn.example/uuid-review.mp4")
}

func TestExtractZhiboLiveIDRequiresNumericID(t *testing.T) {
	if got := extractZhiboLiveID("https://zhibo.chaoxing.com/12345?x=1"); got != "12345" {
		t.Fatalf("live id = %q, want 12345", got)
	}
	if got := extractZhiboLiveID("https://zhibo.chaoxing.com/live/watch?liveid=12345&uuid=uuid-abc"); got != "12345" {
		t.Fatalf("query live id = %q, want 12345", got)
	}
	if !isZhiboChaoxingURL("https://zhibo.chaoxing.com/live/watch?uuid=uuid-abc") {
		t.Fatalf("expected zhibo uuid-only URL to be recognized")
	}
	if got := extractZhiboReviewUUID("https://zhibo.chaoxing.com/live/watch?liveid=12345&uuid=uuid-abc"); got != "uuid-abc" {
		t.Fatalf("review uuid = %q, want uuid-abc", got)
	}
	if got := extractZhiboLiveID("https://zhibo.chaoxing.com/not-a-live-id"); got != "" {
		t.Fatalf("non-numeric live id = %q, want empty", got)
	}
}

func TestChaoxingCookieHeaderFromJar(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for raw, cookies := range map[string][]*http.Cookie{
		"https://i.mooc.chaoxing.com/": {{Name: "UID", Value: "100"}},
		"https://k.chaoxing.com/":      {{Name: "_uid", Value: "200"}},
		"https://zhibo.chaoxing.com/":  {{Name: "fid", Value: "300"}},
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		jar.SetCookies(u, cookies)
	}
	header := chaoxingCookieHeader(jar)
	for _, want := range []string{"UID=100", "_uid=200", "fid=300"} {
		if !strings.Contains(header, want) {
			t.Fatalf("cookie header %q missing %s", header, want)
		}
	}
}

func chaoxingTestCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"https://i.mooc.chaoxing.com/",
		"https://mooc1.chaoxing.com/",
		"https://zhibo.chaoxing.com/",
		"https://www.xueyinonline.com/",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		jar.SetCookies(u, []*http.Cookie{{Name: "UID", Value: "100"}})
	}
	return jar
}

func installChaoxingMockTransport(t *testing.T, handler http.Handler) {
	t.Helper()
	httpsSrv := httptest.NewTLSServer(handler)
	t.Cleanup(httpsSrv.Close)

	oldDefault := http.DefaultTransport
	base, ok := oldDefault.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is %T, want *http.Transport", oldDefault)
	}
	tr := base.Clone()
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, httpsSrv.Listener.Addr().String())
	}
	http.DefaultTransport = tr
	t.Cleanup(func() {
		http.DefaultTransport = oldDefault
	})
}
