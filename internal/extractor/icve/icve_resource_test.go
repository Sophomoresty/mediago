package icve

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestICVEBearerAuthUsesSSOTokenForDownloadSubdomains(t *testing.T) {
	jar := newTestCookieJar(t, "https://sso.icve.com.cn/", []*http.Cookie{
		{Name: "token", Value: "SSO_TOKEN"},
		{Name: "UNTYXLCOOKIE", Value: "U1"},
	})

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/auth/passLogin":
			if got := r.URL.Query().Get("token"); got != "SSO_TOKEN" {
				t.Fatalf("zyk passLogin token = %q, want SSO_TOKEN", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"access_token": "ZYK_ACCESS"}})
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/system/user/getInfo":
			if got := r.Header.Get("Authorization"); got != "Bearer ZYK_ACCESS" {
				t.Fatalf("zyk Authorization = %q, want Bearer ZYK_ACCESS", got)
			}
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "Token=ZYK_ACCESS") {
				t.Fatalf("zyk cookie %q missing Token=ZYK_ACCESS", cookie)
			}
			writeJSON(t, w, map[string]any{"code": 200})
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/auth/passLogin":
			if got := r.URL.Query().Get("token"); got != "SSO_TOKEN" {
				t.Fatalf("zjy2 passLogin token = %q, want SSO_TOKEN", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"access_token": "ZJY2_ACCESS"}})
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/system/user/getInfo":
			if got := r.Header.Get("Authorization"); got != "Bearer ZJY2_ACCESS" {
				t.Fatalf("zjy2 Authorization = %q, want Bearer ZJY2_ACCESS", got)
			}
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "Token=ZJY2_ACCESS") {
				t.Fatalf("zjy2 cookie %q missing Token=ZJY2_ACCESS", cookie)
			}
			writeJSON(t, w, map[string]any{"code": 200})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	prof := newProfCtx(jar, IS_HD)
	if prof.accessToken != "ZYK_ACCESS" || prof.headers["Authorization"] != "Bearer ZYK_ACCESS" {
		t.Fatalf("profession auth = token %q header %q", prof.accessToken, prof.headers["Authorization"])
	}
	if cookie := prof.headers["cookie"]; !strings.Contains(cookie, "token=SSO_TOKEN") || !strings.Contains(cookie, "Token=ZYK_ACCESS") {
		t.Fatalf("profession cookie = %q, want original token and zyk Token", cookie)
	}

	material := newMaterialCtx(jar, IS_HD)
	if got := material.headers["Authorization"]; got != "Bearer ZYK_ACCESS" {
		t.Fatalf("material Authorization = %q, want Bearer ZYK_ACCESS", got)
	}

	v2 := newProfV2Ctx(jar, IS_HD)
	if got := v2.headers["Authorization"]; got != "Bearer ZJY2_ACCESS" {
		t.Fatalf("profession v2 Authorization = %q, want Bearer ZJY2_ACCESS", got)
	}
}

func TestIcveProfessionRootMIDAndOpenCourseSelection(t *testing.T) {
	jar := newTestCookieJar(t, "https://sso.icve.com.cn/", []*http.Cookie{{Name: "token", Value: "TROOT"}})

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/auth/passLogin":
			writeJSON(t, w, map[string]any{"data": map[string]any{"access_token": "AROOT"}})
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/system/user/getInfo":
			writeJSON(t, w, map[string]any{"code": 200})
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/teacher/courseList/myCourseList":
			switch r.URL.Query().Get("flag") {
			case "1":
				writeJSON(t, w, map[string]any{"rows": []any{map[string]any{
					"courseId":       "ROOT_CID",
					"courseInfoId":   "ROOT_INFO",
					"courseName":     "根课程",
					"courseInfoName": "开课一",
				}}})
			case "2":
				writeJSON(t, w, map[string]any{"rows": []any{}})
			default:
				t.Fatalf("unexpected course list flag %q", r.URL.Query().Get("flag"))
			}
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/teacher/courseContent/MID1":
			writeJSON(t, w, map[string]any{"data": map[string]any{"courseId": "CID_FROM_MID"}})
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/website/course/trust/information":
			if got := r.URL.Query().Get("courseId"); got != "CID_OPEN" {
				t.Fatalf("trust courseId = %q, want CID_OPEN", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"courseVo": map[string]any{"name": "专业课", "schoolName": "示例校"},
				"courseInfo": []any{
					map[string]any{"id": "OC1", "name": "开课一", "courseId": "CID_OPEN"},
					map[string]any{"id": "OC2", "name": "开课二", "courseId": "CID_OPEN"},
				},
			}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	x := newProfCtx(jar, IS_HD)
	cid, openCourse := x.resolveURLCourseID("https://zyk.icve.com.cn/")
	if cid != "ROOT_CID" || openCourse != "" {
		t.Fatalf("root resolve = cid %q openCourse %q", cid, openCourse)
	}

	y := newProfCtx(jar, IS_HD)
	cid, openCourse = y.resolveURLCourseID("https://zyk.icve.com.cn/icve-study?id=MID1")
	if cid != "CID_FROM_MID" || openCourse != "" {
		t.Fatalf("mid resolve = cid %q openCourse %q", cid, openCourse)
	}

	z := newProfCtx(nil, IS_HD)
	z.cid, z.openCourse = z.resolveURLCourseID("https://zyk.icve.com.cn/courseDetailed?id=CID_OPEN&openCourse=OC2")
	if z.cid != "CID_OPEN" || z.openCourse != "OC2" {
		t.Fatalf("openCourse URL resolve = cid %q openCourse %q", z.cid, z.openCourse)
	}
	if err := z.loadTitle(); err != nil {
		t.Fatalf("loadTitle returned error: %v", err)
	}
	if z.courseID != "OC2" {
		t.Fatalf("selected courseID = %q, want OC2", z.courseID)
	}
	if len(z.courseList) != 1 || z.courseList[0].ID != "OC2" {
		t.Fatalf("courseList = %#v, want only OC2", z.courseList)
	}
}

func TestIcveProfessionResourcePayloadResolution(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/teacher/courseContent/VID":
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"fileType": "mp4",
				"fileUrl":  `{"ossGenUrl":"https://cdn.example.com/gen","url":"video/short","ossOriUrl":"https://cdn.example.com/original.mp4?sign=1"}`,
			}})
		case r.Host == "upload.icve.com.cn" && r.URL.Path == "/video/short/status":
			writeJSON(t, w, map[string]any{"args": map[string]any{"720p": true}, "type": "mp4"})
		case r.Host == "cdn.example.com" && r.URL.Path == "/gen/720p.mp4":
			_, _ = w.Write([]byte("ok"))
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/teacher/courseContent/FILE":
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"fileType": "pdf",
				"fileUrl":  `{"courseDesignCellResourceVos":[{"name":"讲义","fileType":"pdf","ossOriUrl":"https://cdn.example.com/vos.pdf?x=1"}]}`,
			}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	x := newProfCtx(nil, IS_HD)
	if got := x.getVideoURL("VID"); got != "https://cdn.example.com/gen/720p.mp4" {
		t.Fatalf("getVideoURL = %q, want transcoded 720p URL", got)
	}
	if got := x.getSourceURL("FILE"); got != "https://cdn.example.com/vos.pdf" {
		t.Fatalf("getSourceURL = %q, want stripped VOS PDF URL", got)
	}
}

func TestIcveMoocLoadTitleJoinsCourse(t *testing.T) {
	joined := false
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "www.icve.com.cn" && r.URL.Path == "/Portal/study/joinStudy":
			assertFormValue(t, r, "courseid", "CID")
			joined = true
			writeJSON(t, w, map[string]any{"code": 1})
		case r.Host == "www.icve.com.cn" && r.URL.Path == "/Portal/courseinfo/getHeadInfo_upgrade":
			if !joined {
				t.Fatal("getHeadInfo_upgrade called before joinStudy")
			}
			assertFormValue(t, r, "courseid", "CID")
			writeJSON(t, w, map[string]any{"list": map[string]any{"Title": "MOOC", "ProjectName": "Project"}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	x := newMoocCtx(nil, IS_HD)
	x.cid = "CID"
	if err := x.loadTitle(); err != nil {
		t.Fatalf("loadTitle returned error: %v", err)
	}
	if !joined {
		t.Fatal("joinStudy was not called")
	}
	if x.title != "MOOC_Project" {
		t.Fatalf("title = %q, want MOOC_Project", x.title)
	}
}

func TestIcveProfessionBuildMediaExpandsMultipleVOSResources(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/teacher/courseContent/VOS":
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"fileType": "文件夹",
				"fileUrl": `{"courseDesignCellResourceVos":[` +
					`{"name":"讲义","fileType":"pdf","ossOriUrl":"https://cdn.example.com/handout.pdf?x=1"},` +
					`{"name":"讲解","fileType":"mp4","ossGenUrl":"https://cdn.example.com/gen","urlShort":"video/vos","ossOriUrl":"https://cdn.example.com/original.mp4?sign=1"}` +
					`]}`,
			}})
		case r.Host == "upload.icve.com.cn" && r.URL.Path == "/video/vos/status":
			writeJSON(t, w, map[string]any{"args": map[string]any{"720p": true}, "type": "mp4"})
		case r.Host == "cdn.example.com" && r.URL.Path == "/gen/720p.mp4":
			_, _ = w.Write([]byte("ok"))
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	x := newProfCtx(nil, IS_HD)
	info, err := x.buildMedia([]profSourceItem{{Name: "(1)--知识点", FileType: "文件夹", FileID: "VOS"}})
	if err != nil {
		t.Fatalf("buildMedia returned error: %v", err)
	}
	urls := collectMediaURLs(info)
	if !containsString(urls, "https://cdn.example.com/handout.pdf") {
		t.Fatalf("resolved URLs missing VOS PDF: %#v", urls)
	}
	if !containsString(urls, "https://cdn.example.com/gen/720p.mp4") {
		t.Fatalf("resolved URLs missing VOS video: %#v", urls)
	}
	if len(info.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(info.Entries))
	}
}

func TestIcveProfessionV2UsesURLCourseInfoAndClassID(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/spoc/courseDesign/study/record":
			if got := r.URL.Query().Get("courseId"); got != "CID" {
				t.Fatalf("courseId = %q, want CID", got)
			}
			if got := r.URL.Query().Get("courseInfoId"); got != "INFO" {
				t.Fatalf("courseInfoId = %q, want INFO", got)
			}
			if got := r.URL.Query().Get("classId"); got != "CLASS" {
				t.Fatalf("classId = %q, want CLASS", got)
			}
			writeJSON(t, w, []any{map[string]any{"id": "SRC", "name": "讲义", "fileType": "pdf"}})
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/spoc/courseDesign/getStudyCellInfo":
			if got := r.URL.Query().Get("classId"); got != "CLASS" {
				t.Fatalf("source classId = %q, want CLASS", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"fileType": "pdf",
				"fileUrl":  `{"ossOriUrl":"https://cdn.example.com/v2.pdf?token=1"}`,
			}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	rawURL := "https://zjy2.icve.com.cn/study/coursePreview/spoccourseIndex/courseware?id=CID&courseInfoId=INFO&classId=CLASS"
	info, err := (&IcveProfessionV2{}).Extract(rawURL, &extractor.ExtractOpts{Quality: "hd"})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	urls := collectMediaURLs(info)
	if !containsString(urls, "https://cdn.example.com/v2.pdf") {
		t.Fatalf("resolved URLs missing v2 PDF: %#v", urls)
	}
}

func TestIcveMaterialNestedVOSResolution(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "zyk.icve.com.cn" && r.URL.Path == "/prod-api/website/resource/detail/info":
			if got := r.URL.Query().Get("id"); got != "MID" {
				t.Fatalf("material id = %q, want MID", got)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"name":     "素材",
				"fileType": "pdf",
				"fileUrl":  `{"resources":[{"ossOriUrl":"https://cdn.example.com/material.pdf?token=1"}]}`,
			}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	info, err := (&IcveMaterial{}).Extract("https://zyk.icve.com.cn/materialDetailed?id=MID", &extractor.ExtractOpts{Quality: "hd"})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	urls := collectMediaURLs(info)
	if !containsString(urls, "https://cdn.example.com/material.pdf") {
		t.Fatalf("resolved URLs missing material PDF: %#v", urls)
	}
}

func TestICVEPDFPageResolutionFromUploadStatus(t *testing.T) {
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "upload.icve.com.cn" && r.URL.Path == "/docs/pdf/status":
			writeJSON(t, w, map[string]any{"args": map[string]any{"page_count": 2}, "type": "pdf"})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	x := newProfCtx(nil, IS_HD)
	entries := buildICVEResourceEntries(x.c, x.headers, IS_HD, map[string]any{
		"fileType":   "pdf",
		"fileGenUrl": "https://cdn.example.com/pdf-pages",
		"urlShort":   "docs/pdf",
	}, "pdf", "讲义", "profession")
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	urls := collectMediaURLs(&extractor.MediaInfo{Entries: entries})
	if !containsString(urls, "https://cdn.example.com/pdf-pages/1.png") || !containsString(urls, "https://cdn.example.com/pdf-pages/2.png") {
		t.Fatalf("PDF page URLs missing: %#v", urls)
	}
}

func TestICVEPDFPageResolutionUsesZJY2GetUrlPngs(t *testing.T) {
	sourcePDF := "https://cdn.example.com/source.pdf?token=1"
	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "zjy2.icve.com.cn" && r.URL.Path == "/prod-api/spoc/oss/getUrlPngs":
			got := r.URL.Query().Get("fileUrl")
			if got != sourcePDF {
				t.Fatalf("getUrlPngs fileUrl = %q, want %q", got, sourcePDF)
			}
			writeJSON(t, w, map[string]any{"data": []any{
				"https://cdn.example.com/png/1.png",
				map[string]any{"url": "https://cdn.example.com/png/2.png"},
			}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Fatalf("unexpected mock request: method=%s host=%s path=%s rawQuery=%s", r.Method, r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	x := newProfV2Ctx(nil, IS_HD)
	pages := fetchICVEPDFPageURLs(x.c, x.headers, sourcePDF)
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2: %#v", len(pages), pages)
	}
	if !containsString(pages, "https://cdn.example.com/png/1.png") || !containsString(pages, "https://cdn.example.com/png/2.png") {
		t.Fatalf("getUrlPngs URLs missing: %#v", pages)
	}
}
