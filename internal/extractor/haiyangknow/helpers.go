package haiyangknow

import (
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var tokenKeys = []string{"Admin-Token", "admin_token", "token", "Token", "access_token", "accessToken", "Authorization", "authorization"}

func cookieHeader(jar http.CookieJar, origins []string) string {
	seen := map[string]bool{}
	var parts []string
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil {
			continue
		}
		for _, c := range jar.Cookies(u) {
			if c.Name == "" || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func extractToken(value any) string {
	switch t := value.(type) {
	case map[string]any:
		for _, k := range tokenKeys {
			if v := extractToken(t[k]); v != "" {
				return v
			}
		}
		for _, v := range t {
			if tok := extractToken(v); tok != "" {
				return tok
			}
		}
	case []any:
		for _, v := range t {
			if tok := extractToken(v); tok != "" {
				return tok
			}
		}
	case string:
		s := strings.Trim(strings.TrimSpace(t), "'\"")
		if strings.HasPrefix(strings.ToLower(s), "bearer ") {
			return strings.TrimSpace(s[7:])
		}
		for _, k := range tokenKeys {
			re := regexp.MustCompile(`(?i)(?:^|[?&;,\s])` + regexp.QuoteMeta(k) + `\s*[:=]\s*"?([^";,\s]+)`)
			if m := re.FindStringSubmatch(s); len(m) == 2 {
				return strings.TrimPrefix(m[1], "Bearer ")
			}
		}
		if !strings.Contains(s, "=") && len(s) >= 20 {
			return s
		}
	}
	return ""
}

func extractRecords(v any) []map[string]any {
	if list, ok := v.([]any); ok {
		return listMaps(list)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range []string{"records", "list", "rows", "data", "result", "groupList", "chapterList", "courseGroupList", "courseChapterList"} {
		child := m[k]
		if list, ok := child.([]any); ok {
			return listMaps(list)
		}
		if mm, ok := child.(map[string]any); ok {
			if rows := extractRecords(mm); len(rows) > 0 {
				return rows
			}
		}
	}
	return nil
}

func listMaps(list []any) []map[string]any {
	out := []map[string]any{}
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func anyList(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case nil:
		return nil
	default:
		return []any{t}
	}
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}
func firstExisting(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && str(v) != "" {
			return v
		}
	}
	return nil
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func uniqueNonEmpty(vals ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func cloneStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if math.Trunc(t) == t {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func intVal(v any) int {
	s := str(v)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return int(f)
}
func truthy(v any) bool {
	s := strings.ToLower(str(v))
	return s != "" && s != "0" && s != "false" && s != "none" && s != "no"
}
func isOKCode(v any) bool { s := str(v); return s == "" || s == "0" || s == "200" }

func walkStrings(v any) []string {
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			for _, v := range t {
				walk(v)
			}
		case []any:
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(v)
	return out
}

func parseSizeMB(value string) int {
	if value == "" {
		return 0
	}
	re := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*([GMK]?)`)
	m := re.FindStringSubmatch(strings.ReplaceAll(value, ",", ""))
	if len(m) < 2 {
		return 0
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	unit := ""
	if len(m) > 2 {
		unit = strings.ToUpper(m[2])
	}
	switch unit {
	case "G":
		f *= 1024
	case "K":
		f /= 1024
	case "M":
	default:
		if f > 1048576 {
			f /= 1048576
		} else if f > 1024 {
			f /= 1024
		}
	}
	return int(f)
}

func coursePrice(course map[string]any) float64 {
	for _, k := range []string{"priceCent", "priceCents", "price_cent", "price_cents", "priceFen", "price_fen", "moneyCent", "moneyCents", "money_cent", "money_cents", "moneyFen", "money_fen", "amountCent", "amountCents", "amount_cent", "amount_cents", "amountFen", "amount_fen", "payMoneyCent", "payMoneyCents", "pay_money_cent", "pay_money_cents", "salePriceCent", "sale_price_cent", "actualPriceCent", "actual_price_cent"} {
		if p := normalizePrice(course[k], true); p > 0 {
			return p
		}
	}
	for _, k := range []string{"payMoney", "pay_money", "salePrice", "sale_price", "actualPrice", "actual_price", "activityPrice", "activity_price", "discountPrice", "discount_price", "purchasePrice", "purchase_price", "coursePrice", "course_price", "originalPrice", "original_price", "price", "amount", "money"} {
		if p := normalizePrice(course[k], false); p > 0 {
			return p
		}
	}
	return 0
}

func normalizePrice(v any, cents bool) float64 {
	s := str(v)
	if s == "" {
		return 0
	}
	re := regexp.MustCompile(`-?\d+(?:\.\d+)?`)
	n := re.FindString(strings.ReplaceAll(s, ",", ""))
	if n == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(n, 64)
	if f <= 0 {
		return 0
	}
	if cents || (!strings.Contains(n, ".") && f >= 1000) {
		f /= 100
	}
	return math.Round(f*100) / 100
}
