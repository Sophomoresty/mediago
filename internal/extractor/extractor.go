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
}

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
