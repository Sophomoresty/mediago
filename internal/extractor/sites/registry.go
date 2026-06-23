// Package sites contained generic HTML-extraction stubs for sites without
// dedicated extractors. As of cleanup-generic, every named site in the upstream
// decompiled source has its own dedicated extractor under
// internal/extractor/<name>/. This file stays for future generic-pattern needs
// (e.g., Tencent Classroom-style legacy URLs) but registers no extractors today.
package sites

// allSites is intentionally empty — every site previously listed here now has
// a source-aligned dedicated extractor package.
var allSites = []siteSpec{}

type siteSpec struct {
	name        string
	domain      string
	patterns    []string
	needAuth    bool
	apiTemplate string
}

func init() {
	// No-op: all sites have dedicated extractors. See internal/extractor/*/.
	for range allSites {
	}
}
