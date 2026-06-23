package cookie

import (
	"os"
	"testing"
)

func TestParseNetscapeFile(t *testing.T) {
	content := `# Netscape HTTP Cookie File
.bilibili.com	TRUE	/	FALSE	1700000000	SESSDATA	abc123def456
.bilibili.com	TRUE	/	TRUE	1700000000	bili_jct	token789
`
	tmp, _ := os.CreateTemp("", "cookies*.txt")
	tmp.WriteString(content)
	tmp.Close()
	defer os.Remove(tmp.Name())

	cookies, err := ParseNetscapeFile(tmp.Name())
	if err != nil {
		t.Fatalf("ParseNetscapeFile error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
	if cookies[0].Name != "SESSDATA" || cookies[0].Value != "abc123def456" {
		t.Errorf("unexpected first cookie: %+v", cookies[0])
	}
	if cookies[1].Domain != "bilibili.com" {
		t.Errorf("expected domain bilibili.com, got %s", cookies[1].Domain)
	}
}

func TestNewStore(t *testing.T) {
	store := NewStore()
	if store.Jar() == nil {
		t.Error("Jar() returned nil")
	}
}
