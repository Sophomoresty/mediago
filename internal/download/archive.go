package download

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

// Archive tracks downloaded items to avoid re-downloading.
// Format: one "<site> <id>" per line, compatible with yt-dlp.
type Archive struct {
	path    string
	entries map[string]struct{}
	mu      sync.Mutex
}

// LoadArchive reads an existing archive file or creates an empty Archive
// if the file doesn't exist.
func LoadArchive(path string) (*Archive, error) {
	a := &Archive{
		path:    path,
		entries: make(map[string]struct{}),
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return a, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			a.entries[line] = struct{}{}
		}
	}
	return a, scanner.Err()
}

// Contains checks if the given ID is already in the archive.
func (a *Archive) Contains(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.entries[id]
	return ok
}

// Add records a new ID in the archive file.
func (a *Archive) Add(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.entries[id]; ok {
		return nil
	}

	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, id); err != nil {
		return err
	}
	a.entries[id] = struct{}{}
	return nil
}

// MakeArchiveID constructs an archive ID from a MediaInfo.
// Format: "<site> <hash>" where hash is derived from the title or first stream URL.
func MakeArchiveID(info *extractor.MediaInfo) string {
	site := info.Site
	if site == "" {
		site = "unknown"
	}

	// Use the first stream URL if available, otherwise hash the title
	var source string
	for _, s := range info.Streams {
		if len(s.URLs) > 0 {
			source = s.URLs[0]
			break
		}
	}
	if source == "" {
		source = info.Title
	}

	h := sha256.Sum256([]byte(source))
	return fmt.Sprintf("%s %x", site, h[:8])
}
