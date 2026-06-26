package extractor_test

import (
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
	_ "github.com/Sophomoresty/mediago/internal/extractor/bilibili"
)

func TestMatchWithSiteReturnsSiteMetadata(t *testing.T) {
	ext, site, err := extractor.MatchWithSite("https://www.bilibili.com/video/BV1GJ411x7h7")
	if err != nil {
		t.Fatalf("MatchWithSite returned error: %v", err)
	}
	if ext == nil {
		t.Fatal("MatchWithSite returned nil extractor")
	}
	if site.Name != "Bilibili" {
		t.Fatalf("site.Name = %q, want Bilibili", site.Name)
	}
}
