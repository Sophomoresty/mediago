package gaotu

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestExtractMock(t *testing.T) {
	fixture := mustReadFixture(t, "testdata/sample.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	testURL := srv.URL + "/mock?target=https://www.gaotu.cn/course?clazz=1&liveId=2"
	if _, err := extractor.Match(testURL); err != nil {
		t.Fatalf("test URL should match extractor pattern: %v", err)
	}

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("fetch fixture server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fixture server status = %d, want 200", resp.StatusCode)
	}

	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode fixture response: %v", err)
	}
	if payload == nil {
		t.Fatal("fixture payload is nil")
	}

	_, err = (&Gaotu{}).Extract(testURL, &extractor.ExtractOpts{})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "requires") {
		t.Fatalf("error = %v, want auth error", err)
	}
}

func mustReadFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("fixture %s is not valid json", path)
	}
	return b
}
