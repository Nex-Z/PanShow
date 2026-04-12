package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"panshow/backend/internal/config"
)

func TestRotatingLogWriterRotatesOldActiveLog(t *testing.T) {
	logDir := t.TempDir()
	activePath := filepath.Join(logDir, activeLogFileName)
	if err := os.WriteFile(activePath, []byte("old\n"), 0644); err != nil {
		t.Fatalf("write active log: %v", err)
	}
	yesterday := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(activePath, yesterday, yesterday); err != nil {
		t.Fatalf("set active log time: %v", err)
	}

	writer, err := newRotatingLogWriter(config.Config{
		LogDir:        logDir,
		LogMaxSizeMB:  50,
		LogMaxBackups: 14,
		LogMaxAgeDays: 30,
	})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer writer.Close()

	if _, err := writer.Write([]byte("new\n")); err != nil {
		t.Fatalf("write log: %v", err)
	}
	active, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read active log: %v", err)
	}
	if string(active) != "new\n" {
		t.Fatalf("active log = %q, want only new entry", string(active))
	}

	archives, err := os.ReadDir(filepath.Join(logDir, "archive"))
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("archive count = %d, want 1", len(archives))
	}
	archived, err := os.ReadFile(filepath.Join(logDir, "archive", archives[0].Name()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if string(archived) != "old\n" {
		t.Fatalf("archive log = %q, want old entry", string(archived))
	}
}
