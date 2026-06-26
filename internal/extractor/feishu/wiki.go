package feishu

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type wikiInfo struct {
	Token    string
	ObjToken string
	ObjType  string
	Title    string
}

func (w wikiInfo) isDoc() bool {
	switch strings.ToLower(w.ObjType) {
	case "2", "22", "doc", "docs", "docx", "docx_v2":
		return true
	}
	return false
}

func (w wikiInfo) isFile() bool {
	switch strings.ToLower(w.ObjType) {
	case "12", "file", "file_token":
		return true
	}
	return false
}

func (w wikiInfo) docPath() string {
	switch strings.ToLower(w.ObjType) {
	case "2", "doc", "docs":
		return "docs"
	default:
		return "docx"
	}
}

func feishuWikiTokenInfos(c *util.Client, origin, token string, h map[string]string) []wikiInfo {
	q := neturl.QueryEscape(token)
	apiURLs := []string{
		fmt.Sprintf("%s/space/api/wiki/v2/tree/get_node/?wiki_token=%s&expand_shortcut=true", origin, q),
		fmt.Sprintf("%s/space/api/wiki/v2/tree/get_node?wiki_token=%s&expand_shortcut=true", origin, q),
		fmt.Sprintf("%s/space/api/wiki/tree/get_node?token=%s", origin, q),
		fmt.Sprintf("%s/space/api/wiki/v2/tree/get_node?token=%s", origin, q),
	}
	jsonHeaders := cloneHeaders(h)
	jsonHeaders["Accept"] = "application/json, text/plain, */*"
	jsonHeaders["x-lgw-os-type"] = "1"
	jsonHeaders["x-lgw-terminal-type"] = "2"
	jsonHeaders["doc-os"] = "windows"
	jsonHeaders["doc-platform"] = "web"

	seen := map[string]bool{}
	var infos []wikiInfo
	for _, apiURL := range apiURLs {
		body, err := c.GetString(apiURL, jsonHeaders)
		if err != nil {
			continue
		}
		var root any
		if err := json.Unmarshal([]byte(body), &root); err != nil {
			continue
		}
		if code := jsonFindFirst(root, "code"); code != "" && code != "0" {
			continue
		}
		info := wikiInfo{
			Token:    token,
			ObjToken: jsonFindFirst(root, "obj_token", "objToken", "origin_token", "originToken"),
			ObjType:  jsonFindFirst(root, "obj_type", "objType", "origin_type", "originType", "type"),
			Title:    jsonFindFirst(root, "title", "name"),
		}
		if info.ObjToken == "" {
			info.ObjToken = jsonFindFirst(root, "token")
		}
		if info.ObjToken == "" && info.ObjType == "" {
			continue
		}
		key := info.ObjType + ":" + info.ObjToken
		if seen[key] {
			continue
		}
		seen[key] = true
		infos = append(infos, info)
	}
	if len(infos) == 0 {
		infos = append(infos, wikiInfo{Token: token, ObjToken: token, ObjType: "docx"})
	}
	return infos
}

func prefixEntries(prefix string, entries []*extractor.MediaInfo) []*extractor.MediaInfo {
	prefix = util.SanitizeFilename(prefix)
	if prefix == "" || len(entries) <= 1 {
		return entries
	}
	out := make([]*extractor.MediaInfo, 0, len(entries))
	for _, entry := range entries {
		cp := *entry
		cp.Title = util.SanitizeFilename(prefix + "_" + entry.Title)
		out = append(out, &cp)
	}
	return out
}

func flattenEntries(info *extractor.MediaInfo) []*extractor.MediaInfo {
	if info == nil {
		return nil
	}
	if len(info.Entries) == 0 {
		return []*extractor.MediaInfo{info}
	}
	return info.Entries
}
