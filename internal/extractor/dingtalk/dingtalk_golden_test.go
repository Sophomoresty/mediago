package dingtalk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nichuanfang/medigo/internal/extractor"
)

func TestExtractMock(t *testing.T) {
	fixture := readGoldenFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	assertFixtureServed(t, srv.URL, fixture)

	ext, err := extractor.Match("https://n.dingtalk.com/dingding/live-room/index.html?roomId=1001&liveUuid=2002")
	if err != nil {
		t.Fatalf("extractor pattern should match fixture URL: %v", err)
	}
	info, err := ext.Extract("https://n.dingtalk.com/dingding/live-room/index.html?roomId=1001&liveUuid=2002", nil)
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

func TestExtractLiveIDs(t *testing.T) {
	tests := []struct {
		url      string
		roomID   string
		encCid   string
		liveUUID string
	}{
		{
			url:      "https://n.dingtalk.com/dingding/live-room/index.html?roomId=abc123&liveUuid=def456",
			roomID:   "abc123",
			liveUUID: "def456",
		},
		{
			url:      "https://h5.dingtalk.com/group-live-share/index.htm?encCid=enc789&liveUuid=uuid012",
			encCid:   "enc789",
			liveUUID: "uuid012",
		},
		{
			url:      "https://h5.dingtalk.com/group-live-share/index.htm?encCid=enc789&liveUuid=uuid012&pcCode=pc345",
			encCid:   "enc789",
			liveUUID: "uuid012",
		},
	}

	for _, tt := range tests {
		roomID, encCid, liveUUID, _ := extractLiveIDs(tt.url)
		if roomID != tt.roomID {
			t.Errorf("extractLiveIDs(%q): roomID=%q, want %q", tt.url, roomID, tt.roomID)
		}
		if encCid != tt.encCid {
			t.Errorf("extractLiveIDs(%q): encCid=%q, want %q", tt.url, encCid, tt.encCid)
		}
		if liveUUID != tt.liveUUID {
			t.Errorf("extractLiveIDs(%q): liveUUID=%q, want %q", tt.url, liveUUID, tt.liveUUID)
		}
	}
}

func TestExtractTranscribeUUID(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://shanji.dingtalk.com/app/transcribes/abc-123-def", "abc-123-def"},
		{"https://example.com/other", ""},
	}
	for _, tt := range tests {
		got := extractTranscribeUUID(tt.url)
		if got != tt.want {
			t.Errorf("extractTranscribeUUID(%q)=%q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestCookieParsing(t *testing.T) {
	cookie := "account=mytoken123; deviceid=dev456; other=val"
	token := extractTokenFromCookie(cookie)
	if token != "mytoken123" {
		t.Errorf("extractTokenFromCookie: got %q, want %q", token, "mytoken123")
	}
	devID := extractCookieValue(cookie, "deviceid")
	if devID != "dev456" {
		t.Errorf("extractCookieValue(deviceid): got %q, want %q", devID, "dev456")
	}
}

func TestAbsolutizeM3U8(t *testing.T) {
	content := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000
playlist_1.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000
playlist_2.m3u8`

	result := absolutizeM3U8(content, "https://cdn.example.com/live/abc/master.m3u8")
	if !strings.Contains(result, "https://cdn.example.com/live/abc/playlist_1.m3u8") {
		t.Errorf("expected absolutized URL, got:\n%s", result)
	}
	if !strings.Contains(result, "https://cdn.example.com/live/abc/playlist_2.m3u8") {
		t.Errorf("expected absolutized URL, got:\n%s", result)
	}
}

func TestMakeDingToken(t *testing.T) {
	token := makeDingToken("https://cdn.example.com/live/abc-123/segment_001.ts?foo=bar", "secrettoken")
	if token == "" {
		t.Fatal("expected non-empty ding token")
	}
	parts := strings.SplitN(token, "-", 2)
	if len(parts) != 2 {
		t.Fatalf("expected timestamp-hash format, got %q", token)
	}
}

func readGoldenFixture(t *testing.T) []byte {
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

func assertFixtureServed(t *testing.T, baseURL string, want []byte) {
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
