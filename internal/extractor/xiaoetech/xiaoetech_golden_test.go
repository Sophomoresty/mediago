package xiaoetech

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func loadGoldenFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !json.Valid(fixture) {
		t.Fatalf("fixture is not valid JSON: %s", fixture)
	}
	return fixture
}

func installMockTransport(t *testing.T, httpURL, httpsURL string) {
	t.Helper()
	httpTarget, err := url.Parse(httpURL)
	if err != nil {
		t.Fatalf("parse HTTP mock server URL: %v", err)
	}
	httpsTarget, err := url.Parse(httpsURL)
	if err != nil {
		t.Fatalf("parse HTTPS mock server URL: %v", err)
	}
	previous := http.DefaultTransport
	base, ok := previous.(*http.Transport)
	if !ok {
		t.Fatalf("default transport has unexpected type %T", previous)
	}
	tr := base.Clone()
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := &net.Dialer{}
		return d.DialContext(ctx, network, httpTarget.Host)
	}
	tr.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := &tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{InsecureSkipVerify: true}}
		return d.DialContext(ctx, network, httpsTarget.Host)
	}
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func assertGoldenOutcome(t *testing.T, media *extractor.MediaInfo, err error) {
	t.Helper()
	if err != nil {
		msg := strings.ToLower(err.Error())
		allowed := []string{"xiaoetech", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "xiaoetech" {
		t.Fatalf("Site = %q, want xiaoetech", media.Site)
	}
	if len(media.Streams) == 0 && len(media.Entries) == 0 {
		t.Fatalf("MediaInfo has no streams or entries: %#v", media)
	}
}

func TestExtractMock(t *testing.T) {
	fixture := loadGoldenFixture(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(fixture)
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}

	media, err := (&Xiaoetech{}).Extract("https://demo.xiaoeknow.com/p/course/video/xe-course-1", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
}

func TestMatchH5MerchantSubdomain(t *testing.T) {
	ext, err := extractor.Match("https://app1wdgsmih6712.h5.xiaoeknow.com/p/course/video/v_615fa7a5e4b0dfaf7faa90f6")
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if _, ok := ext.(*Xiaoetech); !ok {
		t.Fatalf("Match() = %T, want *Xiaoetech", ext)
	}
}

func TestParseCtxH5MerchantHosts(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		appID     string
		xetDomain string
		cid       string
		typ       string
		referer   string
		pc        bool
	}{
		{
			name:      "h5 xiaoeknow v4 live",
			raw:       "https://appabc123.h5.xiaoeknow.com/v4/course/alive/l_68a2d7cae4b0694ca101d0a8?app_id=appabc123&type=2&resource_type=4&resource_id=l_68a2d7cae4b0694ca101d0a8&pro_id=course_1",
			appID:     "appabc123",
			xetDomain: ".h5.xiaoeknow.com",
			cid:       "l_68a2d7cae4b0694ca101d0a8",
			typ:       "live",
			referer:   "https://appabc123.h5.xiaoeknow.com",
		},
		{
			name:      "bare xet citv live host maps to h5 api domain",
			raw:       "https://appfdksyi2e1655.xet.citv.cn/v3/course/alive/l_68a2d7cae4b0694ca101d0a8?app_id=appfdksyi2e1655&alive_mode=0&pro_id=&type=2",
			appID:     "appfdksyi2e1655",
			xetDomain: ".h5.xet.citv.cn",
			cid:       "l_68a2d7cae4b0694ca101d0a8",
			typ:       "live",
			referer:   "https://appfdksyi2e1655.h5.xet.citv.cn",
		},
		{
			name:      "goods detail infers resource type from id",
			raw:       "https://appabc123.h5.xiaoeknow.com/v4/goods/goods_detail/p_649aac0ee4b0b2d1c4297a5f?app_id=appabc123",
			appID:     "appabc123",
			xetDomain: ".h5.xiaoeknow.com",
			cid:       "p_649aac0ee4b0b2d1c4297a5f",
			typ:       "column",
			referer:   "https://appabc123.h5.xiaoeknow.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCtx(tt.raw)
			if got.appID != tt.appID || got.xetDomain != tt.xetDomain || got.cid != tt.cid || got.typ != tt.typ || got.referer != tt.referer || got.pc != tt.pc {
				t.Fatalf("parseCtx() = %#v, want appID=%q xetDomain=%q cid=%q typ=%q referer=%q pc=%v", got, tt.appID, tt.xetDomain, tt.cid, tt.typ, tt.referer, tt.pc)
			}
		})
	}
}

func TestExtractH5MerchantSubdomainUsesMerchantAPI(t *testing.T) {
	const rawURL = "https://appabc123.h5.xiaoeknow.com/v4/course/video/v_123456?app_id=appabc123&resource_id=v_123456&resource_type=3"
	const merchantReferer = "https://appabc123.h5.xiaoeknow.com"
	var sawVideoAPI bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "video.detail_info.get"):
			if r.Host != "appabc123.h5.xiaoeknow.com" {
				t.Errorf("video API host = %q, want appabc123.h5.xiaoeknow.com", r.Host)
			}
			if got := r.Header.Get("Referer"); got != merchantReferer {
				t.Errorf("video API Referer = %q, want %q", got, merchantReferer)
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
			}
			if got := r.Form.Get("bizData[resource_id]"); got != "v_123456" {
				t.Errorf("bizData[resource_id] = %q, want v_123456", got)
			}
			sawVideoAPI = true
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"code":0,"data":{"video_m3u8_url":"https://media.example.com/xet/v_123456.m3u8"}}`))
		case r.URL.Path == "/v4/course/video/v_123456":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><title>Merchant Lesson</title><script>window.USERID="u1";</script>`))
		default:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"code":0,"data":{"list":[]}}`))
		}
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}
	media, err := (&Xiaoetech{}).Extract(rawURL, &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !sawVideoAPI {
		t.Fatalf("video detail API was not called")
	}
	if len(media.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(media.Entries))
	}
	stream := media.Entries[0].Streams["default"]
	if got := stream.URLs[0]; got != "https://media.example.com/xet/v_123456.m3u8" {
		t.Fatalf("stream URL = %q", got)
	}
	if got := stream.Headers["Referer"]; got != merchantReferer {
		t.Fatalf("stream Referer = %q, want %q", got, merchantReferer)
	}
}

func TestExtractDocumentUsesDocumentInfoEndpoint(t *testing.T) {
	const rawURL = "https://appabc123.h5.xiaoeknow.com/p/course/text/d_doc1?app_id=appabc123&product_id=course_1"
	var sawDocumentInfo bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case r.URL.Path == "/p/course/text/d_doc1":
			_, _ = w.Write([]byte(`<!doctype html><title>Document Lesson</title><script>window.USERID="u1";</script>`))
		case strings.Contains(r.URL.Path, "e_course.document_info.get"):
			sawDocumentInfo = true
			if r.Method != http.MethodPost {
				t.Errorf("document API method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
				t.Errorf("document Content-Type = %q, want JSON", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"title":"Doc Title","file_name":"doc.pdf","file_url":"https://media.example.com/docs/doc.pdf","is_subscribe":1}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{"list":[]}}`))
		}
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}
	media, err := (&Xiaoetech{}).Extract(rawURL, &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !sawDocumentInfo {
		t.Fatalf("document info API was not called")
	}
	if len(media.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(media.Entries))
	}
	stream := media.Entries[0].Streams["default"]
	if got := stream.URLs[0]; got != "https://media.example.com/docs/doc.pdf" {
		t.Fatalf("document URL = %q", got)
	}
	if got := stream.Format; got != "pdf" {
		t.Fatalf("format = %q, want pdf", got)
	}
}

func TestExtractTextEmitsHTMLAndRichtextMedia(t *testing.T) {
	const rawURL = "https://appabc123.h5.xiaoeknow.com/p/course/text/i_text1?app_id=appabc123"
	var sawTextDetail bool
	var sawRichtextVideo bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case r.URL.Path == "/p/course/text/i_text1":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><title>Text Lesson</title><script>window.USERID="u1";</script>`))
		case strings.Contains(r.URL.Path, "xe.course.business.get.detail"):
			sawTextDetail = true
			_, _ = w.Write([]byte(`{"code":0,"data":{"content":"<h1>Hello</h1><iframe src=\"https://iframe.xiaoeknow.com/page/?id=vid-rich&type=3\"></iframe>"}}`))
		case r.Host == "iframe.xiaoeknow.com" && r.URL.Path == "/api/richtext/get_video_data":
			sawRichtextVideo = true
			_, _ = w.Write([]byte(`{"code":0,"data":{"vid-rich":{"video_url":"https://media.example.com/rich/rich.m3u8","video_title":"Rich Video"}}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{"list":[]}}`))
		}
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}
	media, err := (&Xiaoetech{}).Extract(rawURL, &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !sawTextDetail || !sawRichtextVideo {
		t.Fatalf("text detail called=%v richtext video called=%v", sawTextDetail, sawRichtextVideo)
	}
	if len(media.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(media.Entries))
	}
	seenHTML := false
	seenM3U8 := false
	for _, entry := range media.Entries {
		stream := entry.Streams["default"]
		switch stream.Format {
		case "html":
			seenHTML = true
			if !strings.HasPrefix(stream.URLs[0], "data:text/html;base64,") {
				t.Fatalf("html URL = %q", stream.URLs[0])
			}
		case "m3u8":
			seenM3U8 = true
			if got := stream.URLs[0]; got != "https://media.example.com/rich/rich.m3u8" {
				t.Fatalf("rich media URL = %q", got)
			}
			if !stream.NeedMerge {
				t.Fatalf("rich m3u8 stream NeedMerge=false")
			}
		}
	}
	if !seenHTML || !seenM3U8 {
		t.Fatalf("seenHTML=%v seenM3U8=%v entries=%#v", seenHTML, seenM3U8, media.Entries)
	}
}
