package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"panshow/backend/internal/config"

	"github.com/gin-gonic/gin"
)

const activeLogFileName = "panshow.log"

func setupLogging(cfg config.Config) (io.Closer, error) {
	if strings.TrimSpace(cfg.LogDir) == "" {
		return nil, nil
	}
	writer, err := newRotatingLogWriter(cfg)
	if err != nil {
		return nil, err
	}

	logWriter := io.MultiWriter(os.Stdout, writer)
	errorWriter := io.MultiWriter(os.Stderr, writer)
	log.SetOutput(logWriter)
	gin.DefaultWriter = logWriter
	gin.DefaultErrorWriter = errorWriter
	log.Printf("PanShow logging to %s", writer.activePath)
	return writer, nil
}

type rotatingLogWriter struct {
	mu           sync.Mutex
	dir          string
	archiveDir   string
	activePath   string
	file         *os.File
	size         int64
	openedDay    string
	maxSizeBytes int64
	maxBackups   int
	maxAge       time.Duration
}

func newRotatingLogWriter(cfg config.Config) (*rotatingLogWriter, error) {
	maxSizeMB := cfg.LogMaxSizeMB
	if maxSizeMB <= 0 {
		maxSizeMB = 50
	}
	maxBackups := cfg.LogMaxBackups
	if maxBackups < 0 {
		maxBackups = 0
	}
	maxAgeDays := cfg.LogMaxAgeDays
	if maxAgeDays < 0 {
		maxAgeDays = 0
	}

	writer := &rotatingLogWriter{
		dir:          cfg.LogDir,
		archiveDir:   filepath.Join(cfg.LogDir, "archive"),
		activePath:   filepath.Join(cfg.LogDir, activeLogFileName),
		maxSizeBytes: int64(maxSizeMB) * 1024 * 1024,
		maxBackups:   maxBackups,
		maxAge:       time.Duration(maxAgeDays) * 24 * time.Hour,
	}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	now := time.Now()
	if w.shouldRotate(len(p), now) {
		if err := w.rotate(now); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingLogWriter) open() error {
	if err := os.MkdirAll(w.dir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(w.archiveDir, 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(w.activePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	if w.size > 0 {
		w.openedDay = dayKey(info.ModTime())
	} else {
		w.openedDay = dayKey(time.Now())
	}
	return nil
}

func (w *rotatingLogWriter) shouldRotate(incoming int, now time.Time) bool {
	if w.size <= 0 {
		return false
	}
	if w.openedDay != "" && w.openedDay != dayKey(now) {
		return true
	}
	return w.maxSizeBytes > 0 && w.size+int64(incoming) > w.maxSizeBytes
}

func (w *rotatingLogWriter) rotate(now time.Time) error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}

	if info, err := os.Stat(w.activePath); err == nil && info.Size() > 0 {
		if err := os.Rename(w.activePath, w.archivePath(now)); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := w.open(); err != nil {
		return err
	}
	return w.cleanupArchives(now)
}

func (w *rotatingLogWriter) archivePath(now time.Time) string {
	base := fmt.Sprintf("panshow-%s.log", now.Format("20060102-150405"))
	path := filepath.Join(w.archiveDir, base)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	for i := 1; ; i++ {
		candidate := filepath.Join(w.archiveDir, fmt.Sprintf("panshow-%s-%03d.log", now.Format("20060102-150405"), i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (w *rotatingLogWriter) cleanupArchives(now time.Time) error {
	entries, err := os.ReadDir(w.archiveDir)
	if err != nil {
		return err
	}

	archives := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "panshow-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		archives = append(archives, info)
	}

	cutoff := time.Time{}
	if w.maxAge > 0 {
		cutoff = now.Add(-w.maxAge)
	}
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].ModTime().After(archives[j].ModTime())
	})
	for i, archive := range archives {
		tooMany := w.maxBackups > 0 && i >= w.maxBackups
		tooOld := !cutoff.IsZero() && archive.ModTime().Before(cutoff)
		if !tooMany && !tooOld {
			continue
		}
		if err := os.Remove(filepath.Join(w.archiveDir, archive.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func dayKey(t time.Time) string {
	return t.Format("2006-01-02")
}
