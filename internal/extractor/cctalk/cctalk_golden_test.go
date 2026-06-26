package cctalk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestExtractMock(t *testing.T) {
	fixture := readGoldenFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	assertFixtureServed(t, srv.URL, fixture)

	ext, err := extractor.Match("https://www.cctalk.com/m/group/123456")
	if err != nil {
		t.Fatalf("extractor pattern should match fixture URL: %v", err)
	}
	info, err := ext.Extract("https://www.cctalk.com/m/group/123456", nil)
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
