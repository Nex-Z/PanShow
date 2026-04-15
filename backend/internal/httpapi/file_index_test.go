package httpapi

import (
	"testing"
	"time"

	"panshow/backend/internal/config"
	"panshow/backend/internal/storage"
)

func TestTodayIndexPathUsesConfiguredTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Date(2026, 4, 14, 16, 30, 0, 0, time.UTC)

	got := todayIndexPath(now, location)
	if got != "/2026/04/15" {
		t.Fatalf("today path = %q, want /2026/04/15", got)
	}
}

func TestFileIndexRowsUseOnlyRequestedParentDirectory(t *testing.T) {
	modified := time.Date(2026, 4, 14, 8, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 4, 14, 9, 0, 0, 0, time.UTC)

	rows := fileIndexRowsFromEntries("/2026/04/14", []storage.FileEntry{
		{
			Name:         "a.txt",
			Path:         "/2026/04/14/a.txt",
			Size:         123,
			LastModified: &modified,
			ContentType:  "text/plain",
		},
	}, indexedAt)

	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.ParentPath != "/2026/04/14" {
		t.Fatalf("parent path = %q, want /2026/04/14", row.ParentPath)
	}
	if row.Path != "/2026/04/14/a.txt" || row.Name != "a.txt" || row.Size != 123 {
		t.Fatalf("row = %+v, want indexed file fields", row)
	}
	if row.LastModified == nil || !row.LastModified.Equal(modified) {
		t.Fatalf("lastModified = %v, want %v", row.LastModified, modified)
	}
	if !row.IndexedAt.Equal(indexedAt) {
		t.Fatalf("indexedAt = %v, want %v", row.IndexedAt, indexedAt)
	}
}

func TestSyntheticDirectoryEntriesBuildParentsFromIndexedDayDirs(t *testing.T) {
	indexedDirs := []string{
		"/2025/03/02",
		"/2025/03/15",
		"/2025/04/01",
		"/2026/01/01",
	}

	root := syntheticDirectoryEntries("/", indexedDirs)
	if got := entryPaths(root); !sameStrings(got, []string{"/2025", "/2026"}) {
		t.Fatalf("root entries = %v, want /2025 and /2026", got)
	}

	year := syntheticDirectoryEntries("/2025", indexedDirs)
	if got := entryPaths(year); !sameStrings(got, []string{"/2025/03", "/2025/04"}) {
		t.Fatalf("year entries = %v, want /2025/03 and /2025/04", got)
	}

	month := syntheticDirectoryEntries("/2025/03", indexedDirs)
	if got := entryPaths(month); !sameStrings(got, []string{"/2025/03/02", "/2025/03/15"}) {
		t.Fatalf("month entries = %v, want /2025/03/02 and /2025/03/15", got)
	}
}

func entryPaths(entries []storage.FileEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
		if !entry.IsDir {
			paths = append(paths, "not-dir:"+entry.Path)
		}
	}
	return paths
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestIndexBackfillOptionsCapToTodayAndAcceptPathDates(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	skipIndexed := false
	api := &API{cfg: config.Config{
		IndexTimezone:            "Asia/Shanghai",
		IndexBackfillConcurrency: 4,
	}}

	options, err := api.buildIndexBackfillOptions(fileIndexBackfillRequest{
		From:        "/2026/04/14",
		To:          "2030-01-01",
		Concurrency: 2,
		SkipIndexed: &skipIndexed,
	}, time.Date(2026, 4, 15, 10, 0, 0, 0, location))
	if err != nil {
		t.Fatalf("build options: %v", err)
	}
	if options.fromDate != "2026-04-14" || options.toDate != "2026-04-15" {
		t.Fatalf("date range = %s..%s, want 2026-04-14..2026-04-15", options.fromDate, options.toDate)
	}
	if len(options.dates) != 2 {
		t.Fatalf("date count = %d, want 2", len(options.dates))
	}
	if got := indexBackfillPath(options.dates[0]); got != "/2026/04/14" {
		t.Fatalf("first path = %q, want /2026/04/14", got)
	}
	if options.concurrency != 2 {
		t.Fatalf("concurrency = %d, want 2", options.concurrency)
	}
	if options.skipIndexed {
		t.Fatalf("skipIndexed = true, want false")
	}
}

func TestIndexBackfillOptionsDefaultRangeAndLimit(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	api := &API{cfg: config.Config{
		IndexTimezone:            "Asia/Shanghai",
		IndexBackfillConcurrency: 4,
	}}

	options, err := api.buildIndexBackfillOptions(fileIndexBackfillRequest{LimitDays: 2}, time.Date(2026, 4, 15, 10, 0, 0, 0, location))
	if err != nil {
		t.Fatalf("build options: %v", err)
	}
	if options.fromDate != "2025-01-01" || options.toDate != "2026-04-15" {
		t.Fatalf("date range = %s..%s, want 2025-01-01..2026-04-15", options.fromDate, options.toDate)
	}
	if len(options.dates) != 2 {
		t.Fatalf("date count = %d, want limit 2", len(options.dates))
	}
	if got := indexBackfillPath(options.dates[1]); got != "/2025/01/02" {
		t.Fatalf("second path = %q, want /2025/01/02", got)
	}
	if options.concurrency != 4 {
		t.Fatalf("concurrency = %d, want 4", options.concurrency)
	}
	if !options.skipIndexed {
		t.Fatalf("skipIndexed = false, want true")
	}
}
