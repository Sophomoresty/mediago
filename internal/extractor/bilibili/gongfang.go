package bilibili

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

// API endpoints from decompiled Mooc/Courses/Bilibili/Bilibili_Gongfang.pyc:
//
//	https://mall.bilibili.com/mall-c/order/detail?orderId={cid}
//	https://mall.bilibili.com/mall-c/ship/orderdetails/query
//	https://mall.bilibili.com/mall-c/ship/orderdetails/querydownloadurl
const (
	gongfangOrderDetailURL = "https://mall.bilibili.com/mall-c/order/detail?orderId=%s"
	gongfangInfoURL        = "https://mall.bilibili.com/mall-c/ship/orderdetails/query"
	gongfangDownloadURL    = "https://mall.bilibili.com/mall-c/ship/orderdetails/querydownloadurl"
	gongfangSite           = "bilibili-gongfang"
	// Bilibili_Base.referer; request_get/request_json default header.
	gongfangReferer = "https://www.bilibili.com"
)

var gongfangPatterns = []string{
	`gf\.bilibili\.com/order/hyg-download/\d+`,
}

func init() {
	extractor.Register(&BilibiliGongfang{}, extractor.SiteInfo{
		Name:     "BilibiliGongfang",
		URL:      "gf.bilibili.com/order/hyg-download",
		NeedAuth: true,
	})
}

type BilibiliGongfang struct{}

func (g *BilibiliGongfang) Patterns() []string { return gongfangPatterns }

var gongfangOrderRe = regexp.MustCompile(`/order/hyg-download/(\d+)`)
var gongfangItemsNameRe = regexp.MustCompile(`"itemsName"\s*:\s*"((?:\\.|[^"\\])*)"`)
var gongfangPayTotalRe = regexp.MustCompile(`"payTotalMoney"\s*:\s*(\d+)`)

func (g *BilibiliGongfang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("bilibili gongfang requires login cookies")
	}
	m := gongfangOrderRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, fmt.Errorf("cannot parse gongfang order id from URL: %s", rawURL)
	}
	orderID := m[1]

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)
	headers := gongfangHeaders()

	title, price, _ := fetchGongfangOrderTitle(client, headers, orderID)
	items, err := fetchGongfangItems(client, headers, orderID)
	if err != nil {
		return nil, err
	}

	var entries []*extractor.MediaInfo
	var firstErr error
	for i, item := range items {
		if strings.TrimSpace(item.FileContentType) == "" {
			continue
		}
		sourceID := item.id()
		if sourceID == "" {
			continue
		}
		downloadURL, err := fetchGongfangDownloadURL(client, headers, orderID, sourceID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if downloadURL == "" {
			continue
		}
		format := biliPickFormat(downloadURL, item.FileContentType)
		entryTitle := gongfangEntryTitle(i+1, item.FileName, item.FileContentType, sourceID)
		entries = append(entries, &extractor.MediaInfo{
			Site:  gongfangSite,
			Title: entryTitle,
			Streams: map[string]extractor.Stream{
				"source": {
					Quality: "source",
					URLs:    []string{downloadURL},
					Format:  format,
					Headers: gongfangDownloadHeaders(),
				},
			},
			Extra: map[string]any{
				"order_id":          orderID,
				"ship_detail_id":    sourceID,
				"file_content_type": item.FileContentType,
			},
		})
	}
	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no downloadable gongfang files: %w", firstErr)
		}
		return nil, fmt.Errorf("no downloadable gongfang files")
	}

	extra := map[string]any{"order_id": orderID, "file_count": len(entries)}
	if price != "" {
		extra["pay_total_money"] = price
	}
	return &extractor.MediaInfo{
		Site:    gongfangSite,
		Title:   util.SanitizeFilename(biliFirstNonEmpty(title, "gongfang_"+orderID)),
		Entries: entries,
		Extra:   extra,
	}, nil
}

// gongfangHeaders mirrors Bilibili_Base.__header used by request_get/request_json:
// only a cookie (carried by the client jar) and Referer = https://www.bilibili.com,
// plus a random Chrome User-Agent set by _set_random_user_agent. The source never
// sends an Origin header nor a gf.bilibili.com referer, so neither is added here.
func gongfangHeaders() map[string]string {
	return map[string]string{
		"Referer":    gongfangReferer,
		"User-Agent": util.RandomUA(),
	}
}

// gongfangDownloadHeaders mirrors Bilibili_Base.download_video/download_attach,
// which pass cls.referer = https://www.bilibili.com and the login cookie.
func gongfangDownloadHeaders() map[string]string {
	return map[string]string{
		"Referer":    gongfangReferer,
		"User-Agent": util.RandomUA(),
	}
}

func fetchGongfangOrderTitle(client *util.Client, headers map[string]string, orderID string) (string, string, error) {
	body, err := client.GetString(fmt.Sprintf(gongfangOrderDetailURL, url.QueryEscape(orderID)), headers)
	if err != nil {
		return "", "", fmt.Errorf("gongfang order detail fetch: %w", err)
	}
	title := ""
	if m := gongfangItemsNameRe.FindStringSubmatch(body); m != nil {
		title = strings.TrimSpace(html.UnescapeString(unquoteJSONFragment(m[1])))
	}
	price := ""
	if m := gongfangPayTotalRe.FindStringSubmatch(body); m != nil {
		price = m[1]
	}
	return title, price, nil
}

// gongfangItem keys mirror _get_infos: fileContentType, fileName, shipOrderDetailsId.
type gongfangItem struct {
	FileContentType    string       `json:"fileContentType"`
	FileName           string       `json:"fileName"`
	ShipOrderDetailsID biliStringID `json:"shipOrderDetailsId"`
}

func (g gongfangItem) id() string {
	return g.ShipOrderDetailsID.String()
}

func fetchGongfangItems(client *util.Client, headers map[string]string, orderID string) ([]gongfangItem, error) {
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			ShipOrderDetails []gongfangItem `json:"shipOrderDetails"`
		} `json:"data"`
	}
	if err := postGongfangJSON(client, gongfangInfoURL, map[string]any{"orderId": orderID}, headers, &resp); err != nil {
		return nil, fmt.Errorf("gongfang detail query: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("gongfang detail query returned code=%d message=%q", resp.Code, resp.Message)
	}
	return resp.Data.ShipOrderDetails, nil
}

func fetchGongfangDownloadURL(client *util.Client, headers map[string]string, orderID, sourceID string) (string, error) {
	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	payload := map[string]any{"orderId": orderID, "shipOrderDetailsId": sourceID}
	if err := postGongfangJSON(client, gongfangDownloadURL, payload, headers, &resp); err != nil {
		return "", fmt.Errorf("gongfang download url query source=%s: %w", sourceID, err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("gongfang download url query source=%s returned code=%d message=%q", sourceID, resp.Code, resp.Message)
	}
	return parseGongfangDownloadData(resp.Data), nil
}

// parseGongfangDownloadData mirrors _get_source_url: it reads data.get('url', '').
// The source only ever reads the "url" key from the data object; no other key is
// guessed. A bare-string data payload is also accepted defensively.
func parseGongfangDownloadData(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var data struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	return strings.TrimSpace(data.URL)
}

func postGongfangJSON(client *util.Client, endpoint string, payload map[string]any, headers map[string]string, out any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	h := map[string]string{"Content-Type": "application/json;charset=utf-8"}
	for k, v := range headers {
		h[k] = v
	}
	resp, err := client.Post(endpoint, bytes.NewReader(b), h)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse JSON from %s: %w", endpoint, err)
	}
	return nil
}

func gongfangEntryTitle(idx int, fileName, fileContentType, fallbackID string) string {
	name := strings.TrimSpace(html.UnescapeString(fileName))
	if name == "" {
		name = "source_" + fallbackID
	}
	name = strings.TrimSuffix(name, fileContentType)
	name = strings.TrimSuffix(name, strings.TrimPrefix(fileContentType, "."))
	name = strings.TrimSpace(name)
	if name == "" {
		name = "source_" + fallbackID
	}
	return util.SanitizeFilename(fmt.Sprintf("[%d]--%s", idx, name))
}

func unquoteJSONFragment(s string) string {
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err == nil {
		return out
	}
	return s
}
