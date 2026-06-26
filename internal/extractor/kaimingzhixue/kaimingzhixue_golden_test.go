package kaimingzhixue

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestExtractMock(t *testing.T) {
	fixture := loadSampleFixture(t)
	assertValidJSONFixture(t, fixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	assertServerReturnsFixture(t, srv, fixture)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	opts := &extractor.ExtractOpts{Cookies: jar}
	info, err := (&Kaimingzhixue{}).Extract("https://www.lckmzx.com/course/detail/1001", opts)
	if err == nil {
		if info == nil {
			t.Fatal("Extract returned nil MediaInfo")
		}
		if info.Site == "" || (len(info.Streams) == 0 && len(info.Entries) == 0) {
			t.Fatalf("Extract returned incomplete MediaInfo: %#v", info)
		}
		return
	}
	assertAuthLikeError(t, err)
}

func loadSampleFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sample.json"))
	if err != nil {
		t.Fatalf("read sample fixture: %v", err)
	}
	return data
}

func assertValidJSONFixture(t *testing.T, data []byte) {
	t.Helper()
	if !json.Valid(data) {
		t.Fatalf("sample fixture is not valid JSON: %s", data)
	}
}

func assertServerReturnsFixture(t *testing.T, srv *httptest.Server, fixture []byte) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/fixture")
	if err != nil {
		t.Fatalf("fetch mock fixture: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read mock fixture: %v", err)
	}
	if !bytes.Equal(body, fixture) {
		t.Fatalf("mock fixture mismatch: got %s", body)
	}
}

func assertAuthLikeError(t *testing.T, err error) {
	t.Helper()
	msg := strings.ToLower(err.Error())
	for _, want := range []string{"requires", "cookie", "login", "token", "missing", "auth"} {
		if strings.Contains(msg, want) {
			return
		}
	}
	t.Fatalf("unexpected Extract error: %v", err)
}
