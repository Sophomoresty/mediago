package extractor

import "net/http"

type MediaInfo struct {
	Site      string            `json:"site"`
	Title     string            `json:"title"`
	Artist    string            `json:"artist"`
	Streams   map[string]Stream `json:"streams"`
	Chapters  []Chapter         `json:"chapters,omitempty"`
	Subtitles []Subtitle        `json:"subtitles,omitempty"`
	Extra     map[string]any    `json:"extra,omitempty"`

	// Entries holds child media items when this MediaInfo represents a
	// playlist/course (multi-video). Each entry is a standalone downloadable
	// item with its own Streams. When Entries is non-empty the top-level
	// Streams map is typically empty and Title is the course/playlist title.
	Entries []*MediaInfo `json:"entries,omitempty"`
}

// IsPlaylist reports whether this MediaInfo is a multi-item course/playlist.
func (m *MediaInfo) IsPlaylist() bool { return len(m.Entries) > 0 }

type Stream struct {
	Quality   string            `json:"quality"`
	URLs      []string          `json:"urls"`
	Format    string            `json:"format"`
	Size      int64             `json:"size"`
	NeedMerge bool              `json:"need_merge"`
	AudioURL  string            `json:"audio_url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type Chapter struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Index int    `json:"index"`
}

type Subtitle struct {
	Language string `json:"language"`
	URL      string `json:"url"`
	Format   string `json:"format"`
}

type ExtractOpts struct {
	Cookies  http.CookieJar
	Quality  string
	ListOnly bool
}

type Extractor interface {
	Patterns() []string
	Extract(url string, opts *ExtractOpts) (*MediaInfo, error)
}

type SiteInfo struct {
	Name     string
	URL      string
	NeedAuth bool
}
