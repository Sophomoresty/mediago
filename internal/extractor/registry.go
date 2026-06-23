package extractor

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
)

var (
	mu         sync.RWMutex
	extractors []registeredExtractor
	sites      []SiteInfo
)

type registeredExtractor struct {
	patterns []*regexp.Regexp
	ext      Extractor
}

func Register(ext Extractor, info SiteInfo) {
	mu.Lock()
	defer mu.Unlock()

	var compiled []*regexp.Regexp
	for _, p := range ext.Patterns() {
		compiled = append(compiled, regexp.MustCompile(p))
	}
	extractors = append(extractors, registeredExtractor{
		patterns: compiled,
		ext:      ext,
	})
	sites = append(sites, info)
}

func Match(url string) (Extractor, error) {
	mu.RLock()
	defer mu.RUnlock()

	for _, re := range extractors {
		for _, p := range re.patterns {
			if p.MatchString(url) {
				return re.ext, nil
			}
		}
	}
	return nil, fmt.Errorf("no extractor found for URL: %s", url)
}

func ListSites() []SiteInfo {
	mu.RLock()
	defer mu.RUnlock()

	result := make([]SiteInfo, len(sites))
	copy(result, sites)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}
