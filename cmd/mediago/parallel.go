package main

import (
	"context"
	"sync"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

const extractionWorkers = 4

// extractionResult holds a re-extracted MediaInfo and its original index.
type extractionResult struct {
	index int
	info  *extractor.MediaInfo
	err   error
}

// parallelExtractEntries re-extracts playlist entries concurrently using a worker pool.
// Entries that already have streams are passed through without re-extraction.
// This parallelizes the slow API calls while keeping downloads sequential.
func parallelExtractEntries(ctx context.Context, entries []*extractor.MediaInfo, opts *extractor.ExtractOpts) []*extractor.MediaInfo {
	if len(entries) == 0 {
		return entries
	}

	// Check if any entries actually need extraction (have no streams but have a URL in Extra)
	needsExtraction := false
	for _, e := range entries {
		if e != nil && len(e.Streams) == 0 && e.Extra != nil {
			if _, ok := e.Extra["url"]; ok {
				needsExtraction = true
				break
			}
		}
	}
	if !needsExtraction {
		return entries
	}

	results := make([]*extractor.MediaInfo, len(entries))
	jobs := make(chan int, len(entries))
	var wg sync.WaitGroup

	// Start worker pool
	for w := 0; w < extractionWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx.Err() != nil {
					return
				}
				entry := entries[idx]
				if entry == nil {
					results[idx] = nil
					continue
				}
				// If entry already has streams, no need to re-extract
				if len(entry.Streams) > 0 {
					results[idx] = entry
					continue
				}
				// Try to re-extract using the URL stored in Extra
				rawURL, ok := entry.Extra["url"].(string)
				if !ok || rawURL == "" {
					results[idx] = entry
					continue
				}
				ext, err := extractor.Match(rawURL)
				if err != nil {
					// Can't re-extract, pass through
					results[idx] = entry
					continue
				}
				reExtracted, err := ext.Extract(rawURL, opts)
				if err != nil {
					warnf("parallel extraction failed for %s: %v", entry.Title, err)
					results[idx] = entry
					continue
				}
				results[idx] = reExtracted
			}
		}()
	}

	// Send jobs
	for i := range entries {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	return results
}
