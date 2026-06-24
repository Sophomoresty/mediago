package bilibili

import (
	"encoding/json"
	"testing"
)

func TestBiliStringIDAcceptsNumberAndString(t *testing.T) {
	var got struct {
		Number biliStringID `json:"number"`
		String biliStringID `json:"string"`
	}
	if err := json.Unmarshal([]byte(`{"number":12345,"string":"67890"}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Number.String() != "12345" || got.String.String() != "67890" {
		t.Fatalf("ids = %q/%q", got.Number.String(), got.String.String())
	}
}

func TestCheeseSeasonEpisodesCollectsSectionsAndDedupes(t *testing.T) {
	season := cheeseSeason{
		Episodes: []cheeseRawEpisode{{ID: "100", Title: "intro"}},
		Sections: []struct {
			Title    string             `json:"title"`
			Episodes []cheeseRawEpisode `json:"episodes"`
		}{
			{Title: "section", Episodes: []cheeseRawEpisode{{ID: "100", Title: "dup"}, {EPID: "101", LongTitle: "lesson"}}},
		},
	}
	episodes := season.episodes()
	if len(episodes) != 2 {
		t.Fatalf("episodes = %d, want 2", len(episodes))
	}
	if episodes[0].ID != "100" || episodes[0].Title != "intro" {
		t.Fatalf("first episode = %#v", episodes[0])
	}
	if episodes[1].ID != "101" || episodes[1].Title != "lesson" || episodes[1].SectionTitle != "section" {
		t.Fatalf("second episode = %#v", episodes[1])
	}
}

func TestGongfangParseDownloadData(t *testing.T) {
	if got := parseGongfangDownloadData(json.RawMessage(`{"url":"https://example.test/file.zip"}`)); got != "https://example.test/file.zip" {
		t.Fatalf("object url = %q", got)
	}
	if got := parseGongfangDownloadData(json.RawMessage(`"https://example.test/raw.mp4"`)); got != "https://example.test/raw.mp4" {
		t.Fatalf("string url = %q", got)
	}
}

func TestGongfangEntryTitleTrimsContentType(t *testing.T) {
	got := gongfangEntryTitle(2, "课程资料.pdf", ".pdf", "abc")
	if got != "[2]--课程资料" {
		t.Fatalf("title = %q", got)
	}
}
