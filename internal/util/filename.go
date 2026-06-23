package util

import (
	"regexp"
	"strings"
)

var unsafeChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func SanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = unsafeChars.ReplaceAllString(name, "_")
	runes := []rune(name)
	if len(runes) > 200 {
		name = string(runes[:200])
	}
	if name == "" {
		name = "untitled"
	}
	return name
}
