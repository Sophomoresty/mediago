// BokeCC (p.bokecc.com / cc.video) helpers — used by Qihang, Jingtongxue, etc.
//
// BokeCC playback chain (from Qihang_Course.pyc):
//   GET https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}
//
// The response is XML containing <copy>0</copy>, <playurl>...</playurl>, and a
// <quality>NN</quality> per quality block. siteid is per-tenant, hardcoded by
// each parent site (Qihang uses A183AC83A2983CCC).
package shared

import (
	"encoding/xml"
	"fmt"
	"net/url"

	"github.com/nichuanfang/medigo/internal/util"
)

const BokeCCGetVideoFileURL = "https://p.bokecc.com/servlet/getvideofile"

// BokeCCVideo represents one quality variant from getvideofile.
type BokeCCVideo struct {
	Quality int    `xml:"quality"`
	PlayURL string `xml:"playurl"`
}

// BokeCCResponse is the root envelope of the XML response.
type BokeCCResponse struct {
	XMLName xml.Name      `xml:"video"`
	Videos  []BokeCCVideo `xml:"copy"`
}

// BokeCCResolve fetches getvideofile?vid&siteid and returns the highest-quality
// playable mp4/m3u8 URL.
func BokeCCResolve(c *util.Client, vid, siteid string, headers map[string]string) (string, error) {
	if vid == "" || siteid == "" {
		return "", fmt.Errorf("bokecc: missing vid or siteid")
	}
	apiURL := fmt.Sprintf("%s?vid=%s&siteid=%s",
		BokeCCGetVideoFileURL, url.QueryEscape(vid), url.QueryEscape(siteid))
	body, err := c.GetBytes(apiURL, headers)
	if err != nil {
		return "", fmt.Errorf("bokecc fetch: %w", err)
	}
	var resp BokeCCResponse
	if err := xml.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("bokecc parse XML: %w", err)
	}
	if len(resp.Videos) == 0 {
		return "", fmt.Errorf("bokecc: no quality variants in response")
	}
	best := resp.Videos[0]
	for _, v := range resp.Videos[1:] {
		if v.Quality > best.Quality && v.PlayURL != "" {
			best = v
		}
	}
	if best.PlayURL == "" {
		return "", fmt.Errorf("bokecc: best variant has empty playurl")
	}
	return best.PlayURL, nil
}
