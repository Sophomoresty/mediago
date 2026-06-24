package bilibili

import (
	"bytes"
	"encoding/json"
	"net/url"
	"path"
	"strings"
)

type biliStringID string

func (v *biliStringID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*v = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*v = biliStringID(strings.TrimSpace(s))
		return nil
	}
	*v = biliStringID(strings.TrimSpace(string(b)))
	return nil
}

func (v biliStringID) String() string { return string(v) }

func biliFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func biliPickFormat(rawURL, hint string) string {
	if h := strings.Trim(strings.TrimSpace(hint), "."); h != "" {
		return strings.ToLower(h)
	}
	p := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		p = u.Path
	}
	if ext := strings.TrimPrefix(strings.ToLower(path.Ext(p)), "."); ext != "" {
		return ext
	}
	return "bin"
}
