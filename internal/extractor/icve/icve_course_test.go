package icve

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestIcveCourseCheckCookieUsesSSOAndZJY2(t *testing.T) {
	jar := newTestCookieJar(t, "https://sso.icve.com.cn/", []*http.Cookie{
		{Name: "token", Value: "T1"},
		{Name: "UNTYXLCOOKIE", Value: "U1"},
	})

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "sso.icve.com.cn" && r.Method == http.MethodPost && r.URL.Path == "/api/user/userInfo":
			if got := r.URL.Query().Get("token"); got != "T1" {
				t.Fatalf("sso token = %q, want T1", got)
			}
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "UNTYXLCOOKIE=U1") {
				t.Fatalf("sso cookie %q missing UNTYXLCOOKIE", cookie)
			}
			writeJSON(t, w, map[string]any{"code": 200, "msg": "ok"})
		case r.Host == "zjy2.icve.com.cn" && r.Method == http.MethodGet && r.URL.Path == "/prod-api/auth/passLogin":
			if got := r.URL.Query().Get("token"); got != "T1" {
				t.Fatalf("passLogin token = %q, want T1", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"access_token": "A1"}})
		case r.Host == "zjy2.icve.com.cn" && r.Method == http.MethodGet && r.URL.Path == "/prod-api/system/user/getInfo":
			if got := r.Header.Get("Authorization"); got != "Bearer A1" {
				t.Fatalf("Authorization = %q, want Bearer A1", got)
			}
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "Token=A1") {
				t.Fatalf("zjy2 cookie %q missing Token=A1", cookie)
			}
			writeJSON(t, w, map[string]any{"code": 200})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	x := newCourseCtx(jar, IS_HD)
	if !x.checkCookie("icve", x.headers["cookie"]) {
		t.Fatal("checkCookie returned false")
	}
	if !x.logined {
		t.Fatal("checkCookie did not mark context as logged in")
	}
	if x.accessToken != "A1" {
		t.Fatalf("accessToken = %q, want A1", x.accessToken)
	}
	if got := x.headers["Authorization"]; got != "Bearer A1" {
		t.Fatalf("stored Authorization = %q, want Bearer A1", got)
	}
	if got := x.headers["cookie"]; !strings.Contains(got, "Token=A1") {
		t.Fatalf("stored cookie %q missing Token=A1", got)
	}
}

func TestIcveCourseURLIDParsing(t *testing.T) {
	cid, classCode, classID, moocRoot := parseCourseURLIDs("https://mooc-old.icve.com.cn/cms/courseDetails/index.htm?cid=qcdzzs013shy833")
	if cid != "" || classCode != "qcdzzs013shy833" || classID != "" || moocRoot {
		t.Fatalf("cid URL parsed as cid=%q classCode=%q classID=%q moocRoot=%v", cid, classCode, classID, moocRoot)
	}

	cid, classCode, classID, moocRoot = parseCourseURLIDs("https://mooc-old.icve.com.cn/cms/courseDetails/index.htm?classId=kfznbz033xch519")
	if cid != "" || classCode != "" || classID != "kfznbz033xch519" || moocRoot {
		t.Fatalf("class URL parsed as cid=%q classCode=%q classID=%q moocRoot=%v", cid, classCode, classID, moocRoot)
	}

	cid, classCode, classID, moocRoot = parseCourseURLIDs("https://mooc-old.icve.com.cn/")
	if cid != "" || classCode != "" || classID != "" || !moocRoot {
		t.Fatalf("mooc root parsed as cid=%q classCode=%q classID=%q moocRoot=%v", cid, classCode, classID, moocRoot)
	}

	for _, rawURL := range []string{
		"https://sso.icve.com.cn/",
		"https://sso.icve.com.cn/login?redirect=user",
		"https://user.icve.com.cn/",
		"https://user.icve.com.cn/learning/u/myCourse",
	} {
		cid, classCode, classID, moocRoot = parseCourseURLIDs(rawURL)
		if cid != "" || classCode != "" || classID != "" || !moocRoot {
			t.Fatalf("%s parsed as cid=%q classCode=%q classID=%q moocRoot=%v", rawURL, cid, classCode, classID, moocRoot)
		}
	}

	if got := parseCourseCID("https://mooc-old.icve.com.cn/"); got != "" {
		t.Fatalf("parseCourseCID(mooc root) = %q, want empty", got)
	}
}

func TestIcveCourseLoginSubdomainPatterns(t *testing.T) {
	for _, rawURL := range []string{
		"https://sso.icve.com.cn/",
		"https://sso.icve.com.cn/api/user/userInfo?token=T1",
		"https://user.icve.com.cn/",
		"https://user.icve.com.cn/learning/u/myCourse",
	} {
		_, site, err := extractor.MatchWithSite(rawURL)
		if err != nil {
			t.Fatalf("MatchWithSite(%q) returned error: %v", rawURL, err)
		}
		if site.Name != "IcveCourse" {
			t.Fatalf("MatchWithSite(%q) site = %q, want IcveCourse", rawURL, site.Name)
		}
	}
}

func TestIcveCourseGetCourseListUsesMoocOldAndUserSubdomains(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "mooc-old.icve.com.cn" && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/studentMooc_selectMoocCourse.action"):
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse mooc-old form: %v", err)
			}
			if got := r.Form.Get("token"); got != "TOKEN" {
				t.Fatalf("mooc-old token = %q, want TOKEN", got)
			}
			if got := r.Form.Get("pageSize"); got != courseDefaultPageSize {
				t.Fatalf("mooc-old pageSize = %q, want %s", got, courseDefaultPageSize)
			}
			if got := r.Form.Get("selectType"); got != "0" {
				t.Fatalf("mooc-old selectType = %q, want 0", got)
			}
			switch r.Form.Get("curPage") {
			case "1":
				writeJSON(t, w, map[string]any{"data": []any{
					[]any{"NoIDCourse", "", "", "", "", "", "", "", "NoIDSchool"},
					[]any{"CourseA", "", "", "", "", "", "CID_A", "", "SchoolA"},
				}})
			case "2":
				writeJSON(t, w, map[string]any{"data": []any{}})
			default:
				t.Fatalf("unexpected mooc-old page %q", r.Form.Get("curPage"))
			}
		case r.Host == "user.icve.com.cn" && r.Method == http.MethodPost && r.URL.Path == "/learning/u/userDefinedSql/getBySqlCode.json":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse user form: %v", err)
			}
			if got := r.Form.Get("page.searchItem.queryId"); got != "getNewStuCourseInfoById" {
				t.Fatalf("user queryId = %q", got)
			}
			if got := r.Form.Get("page.curPage"); got != "1" {
				t.Fatalf("user curPage = %q", got)
			}
			writeJSON(t, w, map[string]any{"page": map[string]any{"items": []any{map[string]any{"info": []any{
				map[string]any{"ext9": "", "ext1": "NoIDUserCourse"},
				map[string]any{"ext9": "CID_B", "ext1": "CourseB"},
			}}}}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s", r.Method, r.Host, r.URL.Path)
		}
	}))

	x := newCourseCtx(nil, IS_HD)
	x.token = "TOKEN"
	courses := x.getCourseList()
	if len(courses) != 2 {
		t.Fatalf("len(courses) = %d, want 2: %#v", len(courses), courses)
	}
	if courses[0] != (courseListItem{CourseID: "CID_A", Title: "CourseA_SchoolA"}) {
		t.Fatalf("mooc-old course = %#v", courses[0])
	}
	if courses[1] != (courseListItem{CourseID: "CID_B", Title: "CourseB"}) {
		t.Fatalf("user course = %#v", courses[1])
	}
}

func TestIcveCourseExtractFromLoginRootsUsesUserCourseList(t *testing.T) {
	jar := newTestCookieJar(t, "https://sso.icve.com.cn/", []*http.Cookie{
		{Name: "token", Value: "T1"},
		{Name: "UNTYXLCOOKIE", Value: "U1"},
	})

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "sso.icve.com.cn" && r.Method == http.MethodPost && r.URL.Path == "/api/user/userInfo":
			if got := r.URL.Query().Get("token"); got != "T1" {
				t.Fatalf("sso token = %q, want T1", got)
			}
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "UNTYXLCOOKIE=U1") {
				t.Fatalf("sso cookie %q missing UNTYXLCOOKIE", cookie)
			}
			writeJSON(t, w, map[string]any{"code": 200, "msg": "ok"})
		case r.Host == "zjy2.icve.com.cn" && r.Method == http.MethodGet && r.URL.Path == "/prod-api/auth/passLogin":
			if got := r.URL.Query().Get("token"); got != "T1" {
				t.Fatalf("passLogin token = %q, want T1", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"access_token": "A1"}})
		case r.Host == "zjy2.icve.com.cn" && r.Method == http.MethodGet && r.URL.Path == "/prod-api/system/user/getInfo":
			if got := r.Header.Get("Authorization"); got != "Bearer A1" {
				t.Fatalf("Authorization = %q, want Bearer A1", got)
			}
			writeJSON(t, w, map[string]any{"code": 200})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/islogin.action"):
			_, _ = w.Write([]byte("token:'TOKEN2';siteCode:'SITE';loginId:'LOGIN';"))
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/studentMooc_selectMoocCourse.action"):
			assertFormValue(t, r, "token", "TOKEN2")
			assertFormValue(t, r, "curPage", "1")
			writeJSON(t, w, map[string]any{"data": []any{}})
		case r.Host == "user.icve.com.cn" && r.Method == http.MethodPost && r.URL.Path == "/learning/u/userDefinedSql/getBySqlCode.json":
			assertFormValue(t, r, "page.searchItem.queryId", "getNewStuCourseInfoById")
			writeJSON(t, w, map[string]any{"page": map[string]any{"items": []any{map[string]any{"info": []any{map[string]any{"ext9": "CIDUSER", "ext1": "用户课程"}}}}}})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_addCourseFormStudent.action"):
			assertFormValue(t, r, "courseId", "CIDUSER")
			assertFormValue(t, r, "token", "TOKEN2")
			_, _ = w.Write([]byte(`{"errorCode":"200"}`))
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_selectCourseDetails.action"):
			assertFormValue(t, r, "courseId", "CIDUSER")
			writeJSON(t, w, map[string]any{"data": map[string]any{"className": "用户课程", "schoolName": "示例校", "startTime": "2000-01-01 00:00:00"}, "courseData": []any{}})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/dataCheck.action"):
			assertFormValue(t, r, "courseId", "CIDUSER")
			writeJSON(t, w, map[string]any{"data": "learn-url"})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_getCourseOutline.action"):
			writeJSON(t, w, map[string]any{"data": []any{map[string]any{"title": "章一", "childItem": []any{map[string]any{"title": "节一", "childItem": []any{map[string]any{"id": "VIDUSER", "title": "用户视频.mp4", "type": "video", "resource": "media/user.mp4"}}}}}}})
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/courseware_index.action"):
			_, _ = w.Write([]byte(""))
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/content_video.action"):
			if got := r.URL.Query().Get("params.courseId"); got != "CIDUSER" {
				t.Fatalf("content courseId = %q, want CIDUSER", got)
			}
			if got := r.URL.Query().Get("params.itemId"); got != "VIDUSER" {
				t.Fatalf("content itemId = %q, want VIDUSER", got)
			}
			_, _ = w.Write([]byte(`{"HD":"https://cdn.example.com/user-video-hd.mp4"}`))
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	for _, rawURL := range []string{
		"https://sso.icve.com.cn/",
		"https://user.icve.com.cn/learning/u/myCourse",
	} {
		t.Run(rawURL, func(t *testing.T) {
			info, err := (&IcveCourse{}).Extract(rawURL, &extractor.ExtractOpts{Cookies: jar, Quality: "hd"})
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			urls := collectMediaURLs(info)
			if !containsString(urls, "https://cdn.example.com/user-video-hd.mp4") {
				t.Fatalf("resolved URLs missing user-course video: %#v", urls)
			}
		})
	}
}

func TestIcveCourseVideoFileAndYunpanResolution(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "course.icve.com.cn" && r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content_video.action"):
			if got := r.URL.Query().Get("params.courseId"); got != "CID" {
				t.Fatalf("courseId = %q, want CID", got)
			}
			if got := r.URL.Query().Get("params.itemId"); got != "VID" {
				t.Fatalf("itemId = %q, want VID", got)
			}
			_, _ = w.Write([]byte(`{"FHD":"https://cdn.example.com/fhd.mp4","HD":"https://cdn.example.com/hd.mp4","SD":"https://cdn.example.com/sd.mp4","LD":"https://cdn.example.com/ld.mp4"}`))
		case r.Host == "course.icve.com.cn" && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/getItemResourceDownloadUrl.json"):
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse resource form: %v", err)
			}
			switch r.Form.Get("params.itemId") {
			case "FILE":
				writeJSON(t, w, map[string]any{"data": map[string]any{"downloadUrl": "https://file.icve.com.cn/doc.pdf?sign=1"}})
			case "FALLBACK":
				writeJSON(t, w, map[string]any{"data": map[string]any{"downloadUrl": "null"}})
			default:
				t.Fatalf("unexpected resource item %q", r.Form.Get("params.itemId"))
			}
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s", r.Method, r.Host, r.URL.Path)
		}
	}))

	hd := newCourseCtx(nil, IS_HD)
	hd.cid = "CID"
	u, ext := hd.getVideoURL("VID", "")
	if u != "https://cdn.example.com/fhd.mp4" || ext != ".mp4" {
		t.Fatalf("HD getVideoURL = (%q,%q)", u, ext)
	}

	sd := newCourseCtx(nil, IS_SD)
	sd.cid = "CID"
	u, ext = sd.getVideoURL("VID", "")
	if u != "https://cdn.example.com/ld.mp4" || ext != ".mp4" {
		t.Fatalf("SD getVideoURL = (%q,%q)", u, ext)
	}

	u, ext = hd.getFileURL("FILE", "")
	if u != "https://file.icve.com.cn/doc.pdf?sign=1" || ext != ".pdf" {
		t.Fatalf("getFileURL direct = (%q,%q)", u, ext)
	}

	u, ext = hd.getFileURL("FALLBACK", "resource/path/slides.pptx")
	if u != "https://file.icve.com.cn/resource/path/slides.pptx" || ext != ".pptx" {
		t.Fatalf("getFileURL fallback = (%q,%q)", u, ext)
	}

	got := hd.getYunpanFileURL("META 1", map[string]any{"cloudUserName": "user name", "cloudSiteCode": "site/code"})
	want := "https://spoc-yunpan-sdk.icve.com.cn/api/downloadbyte?token=user+name/site%2Fcode&isView=true&metaId=META+1"
	if got != want {
		t.Fatalf("getYunpanFileURL = %q, want %q", got, want)
	}

	if got := mediaURLFromAny(`{"HD":"https://cdn.example.com/json-hd.mp4"}`); got != "https://cdn.example.com/json-hd.mp4" {
		t.Fatalf("mediaURLFromAny JSON string = %q", got)
	}
}

func TestIcveCourseLoadInfosFromCoursewareHTML(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/dataCheck.action"):
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{"data": "learn-url"})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_getCourseOutline.action"):
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{"data": []any{}})
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/courseware_index.action"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`
<div class="s_learnlist">
  <div class="s_chapter" title="HTML章"></div>
  <div class="s_sectionlist">
    <div class="s_section" title="HTML节">
      <div class="s_pointwrap" itemtype="video" onclick="openLearnResItem('VIDHTML')">
        <span class="s_pointti" title="HTML视频.mp4"></span>
      </div>
      <div class="s_point_hassub" itemtype="doc" onclick="openLearnResItem('FILEHTML')">
        <span class="s_pointti" title="HTML资料.pdf"></span>
      </div>
    </div>
  </div>
</div>`))
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s", r.Method, r.Host, r.URL.Path)
		}
	}))

	x := newCourseCtx(nil, IS_HD)
	x.cid = "CID"
	if err := x.loadInfos(); err != nil {
		t.Fatalf("loadInfos returned error: %v", err)
	}
	items := x.flattenInfoItems()
	if len(items) != 2 {
		t.Fatalf("flattenInfoItems length = %d, want 2: %#v", len(items), items)
	}
	if items[0].ItemID != "VIDHTML" || items[0].Kind != "video" {
		t.Fatalf("video item = %#v", items[0])
	}
	if items[1].ItemID != "FILEHTML" || items[1].Kind != "file" {
		t.Fatalf("file item = %#v", items[1])
	}
}

func TestIcveCourseLoadInfosFallbackToZJY2(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/dataCheck.action"):
			writeJSON(t, w, map[string]any{"data": "learn-url"})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_getCourseOutline.action"):
			writeJSON(t, w, map[string]any{"data": []any{}})
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/courseware_index.action"):
			_, _ = w.Write([]byte(""))
		case r.Host == "zjy2.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/getModuleList/CID"):
			writeJSON(t, w, map[string]any{"data": []any{map[string]any{"id": "M1", "moduleName": "模块一"}}})
		case r.Host == "zjy2.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/getTopicList"):
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topic payload: %v", err)
			}
			if payload["moduleId"] != "M1" || payload["courseOpenId"] != "CID" {
				t.Fatalf("topic payload = %#v", payload)
			}
			writeJSON(t, w, map[string]any{"data": []any{map[string]any{"topicId": "T1", "topicName": "主题一"}}})
		case r.Host == "zjy2.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/getCellList"):
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode cell payload: %v", err)
			}
			if payload["moduleId"] != "M1" || payload["topicId"] != "T1" || payload["courseOpenId"] != "CID" {
				t.Fatalf("cell payload = %#v", payload)
			}
			writeJSON(t, w, map[string]any{"data": []any{map[string]any{"id": "ZVID", "cellName": "Z视频", "categoryName": "视频", "resourceUrl": "z/video.mp4"}, map[string]any{"id": "ZFILE", "cellName": "Z文件", "categoryName": "文档", "resourceUrl": "z/doc.pdf"}}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s", r.Method, r.Host, r.URL.Path)
		}
	}))

	x := newCourseCtx(nil, IS_HD)
	x.cid = "CID"
	if err := x.loadInfos(); err != nil {
		t.Fatalf("loadInfos returned error: %v", err)
	}
	items := x.flattenInfoItems()
	if len(items) != 2 {
		t.Fatalf("flattenInfoItems length = %d, want 2: %#v", len(items), items)
	}
	if items[0].ItemID != "ZVID" || items[0].Kind != "video" {
		t.Fatalf("zjy2 video item = %#v", items[0])
	}
	if items[1].ItemID != "ZFILE" || items[1].Kind != "file" {
		t.Fatalf("zjy2 file item = %#v", items[1])
	}
}

func TestIcveCourseExtractEndToEndOldMoocFlow(t *testing.T) {
	jar := newTestCookieJar(t, "https://sso.icve.com.cn/", []*http.Cookie{
		{Name: "token", Value: "T1"},
		{Name: "UNTYXLCOOKIE", Value: "U1"},
	})

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "sso.icve.com.cn" && r.URL.Path == "/api/user/userInfo":
			writeJSON(t, w, map[string]any{"code": 200, "msg": "ok"})
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/auth/passLogin":
			writeJSON(t, w, map[string]any{"data": map[string]any{"access_token": "A1"}})
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/system/user/getInfo":
			writeJSON(t, w, map[string]any{"code": 200})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/islogin.action"):
			_, _ = w.Write([]byte("token:'TOKEN2';siteCode:'SITE';loginId:'LOGIN';"))
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_addCourseFormStudent.action"):
			assertFormValue(t, r, "courseId", "CID")
			assertFormValue(t, r, "token", "TOKEN2")
			_, _ = w.Write([]byte(`{"errorCode":"200"}`))
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_selectCourseDetails.action"):
			assertFormValue(t, r, "courseId", "CID")
			writeJSON(t, w, map[string]any{"data": map[string]any{"className": "示例课", "schoolName": "示例校", "startTime": "2000-01-01 00:00:00"}, "courseData": []any{}})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/dataCheck.action"):
			assertFormValue(t, r, "courseId", "CID")
			assertFormValue(t, r, "checkType", "1")
			writeJSON(t, w, map[string]any{"data": "learn-url"})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_getCourseOutline.action"):
			writeJSON(t, w, map[string]any{"data": []any{map[string]any{"title": "章一", "childItem": []any{map[string]any{"title": "节一", "childItem": []any{map[string]any{"id": "VID1", "title": "视频一.mp4", "type": "video", "resource": "media/video.mp4"}, map[string]any{"id": "FILE1", "title": "课件一", "type": "pdf", "resource": "docs/file.pdf"}}}}}}})
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/courseware_index.action"):
			_, _ = w.Write([]byte(""))
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/content_video.action"):
			_, _ = w.Write([]byte(`{"HD":"https://cdn.example.com/video-hd.mp4","LD":"https://cdn.example.com/video-ld.mp4"}`))
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/getItemResourceDownloadUrl.json"):
			assertFormValue(t, r, "params.itemId", "FILE1")
			writeJSON(t, w, map[string]any{"data": map[string]any{"downloadUrl": "https://file.icve.com.cn/file.pdf"}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	info, err := (&IcveCourse{}).Extract("https://course.icve.com.cn/learnspace/learn/learn/templateeight/courseware_index.action?params.courseId=CID", &extractor.ExtractOpts{Cookies: jar, Quality: "hd"})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	urls := collectMediaURLs(info)
	if !containsString(urls, "https://cdn.example.com/video-hd.mp4") {
		t.Fatalf("resolved URLs missing video: %#v", urls)
	}
	if !containsString(urls, "https://file.icve.com.cn/file.pdf") {
		t.Fatalf("resolved URLs missing file: %#v", urls)
	}
}

func TestIcveCourseListOnlyStillLoadsInfos(t *testing.T) {
	jar := newTestCookieJar(t, "https://sso.icve.com.cn/", []*http.Cookie{
		{Name: "token", Value: "T1"},
		{Name: "UNTYXLCOOKIE", Value: "U1"},
	})

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "sso.icve.com.cn" && r.URL.Path == "/api/user/userInfo":
			writeJSON(t, w, map[string]any{"code": 200, "msg": "ok"})
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/auth/passLogin":
			writeJSON(t, w, map[string]any{"data": map[string]any{"access_token": "A1"}})
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/system/user/getInfo":
			writeJSON(t, w, map[string]any{"code": 200})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/islogin.action"):
			_, _ = w.Write([]byte("token:'TOKEN2';siteCode:'SITE';loginId:'LOGIN';"))
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_addCourseFormStudent.action"):
			_, _ = w.Write([]byte(`{"errorCode":"200"}`))
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_selectCourseDetails.action"):
			writeJSON(t, w, map[string]any{"data": map[string]any{"className": "示例课", "schoolName": "示例校", "startTime": "2000-01-01 00:00:00"}, "courseData": []any{}})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/dataCheck.action"):
			writeJSON(t, w, map[string]any{"data": "learn-url"})
		case r.Host == "mooc-old.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/portalMooc_getCourseOutline.action"):
			writeJSON(t, w, map[string]any{"data": []any{map[string]any{"title": "章一", "childItem": []any{map[string]any{"title": "节一", "childItem": []any{map[string]any{"id": "VID1", "title": "视频一.mp4", "type": "video", "resource": "media/video.mp4"}}}}}}})
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/courseware_index.action"):
			_, _ = w.Write([]byte(""))
		case r.Host == "course.icve.com.cn" && strings.HasSuffix(r.URL.Path, "/content_video.action"):
			_, _ = w.Write([]byte(`{"HD":"https://cdn.example.com/video-hd.mp4"}`))
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	info, err := (&IcveCourse{}).Extract(
		"https://course.icve.com.cn/learnspace/learn/learn/templateeight/courseware_index.action?params.courseId=CID",
		&extractor.ExtractOpts{Cookies: jar, Quality: "hd", ListOnly: true},
	)
	if err != nil {
		t.Fatalf("ListOnly Extract returned error: %v", err)
	}
	if urls := collectMediaURLs(info); !containsString(urls, "https://cdn.example.com/video-hd.mp4") {
		t.Fatalf("ListOnly URLs missing video: %#v", urls)
	}
}

func newTestCookieJar(t *testing.T, rawURL string, cookies []*http.Cookie) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", rawURL, err)
	}
	jar.SetCookies(u, cookies)
	return jar
}

func assertFormValue(t *testing.T, r *http.Request, key, want string) {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatalf("parse form for %s: %v", key, err)
	}
	if got := r.Form.Get(key); got != want {
		t.Fatalf("form[%s] = %q, want %q", key, got, want)
	}
}

func collectMediaURLs(info *extractor.MediaInfo) []string {
	if info == nil {
		return nil
	}
	var out []string
	for _, stream := range info.Streams {
		out = append(out, stream.URLs...)
	}
	for _, child := range info.Entries {
		out = append(out, collectMediaURLs(child)...)
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
