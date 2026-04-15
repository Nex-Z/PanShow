package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	indexBackfillDefaultStartDate   = "2025-01-01"
	indexBackfillDefaultConcurrency = 4
	indexBackfillMaxConcurrency     = 8
)

type fileIndexBackfillRunner struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	status fileIndexBackfillStatus
}

type fileIndexBackfillRequest struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Concurrency int    `json:"concurrency"`
	LimitDays   int    `json:"limitDays"`
	SkipIndexed *bool  `json:"skipIndexed"`
}

type fileIndexBackfillOptions struct {
	dates       []time.Time
	concurrency int
	fromDate    string
	toDate      string
	skipIndexed bool
}

type fileIndexBackfillStatus struct {
	Running        bool       `json:"running"`
	StopRequested  bool       `json:"stopRequested,omitempty"`
	Canceled       bool       `json:"canceled,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	FromDate       string     `json:"fromDate,omitempty"`
	ToDate         string     `json:"toDate,omitempty"`
	CurrentDate    string     `json:"currentDate,omitempty"`
	CurrentPath    string     `json:"currentPath,omitempty"`
	ActiveWorkers  int        `json:"activeWorkers"`
	TotalDays      int        `json:"totalDays"`
	ProcessedDays  int        `json:"processedDays"`
	IndexedDirs    int        `json:"indexedDirs"`
	EmptyDirs      int        `json:"emptyDirs"`
	SkippedDirs    int        `json:"skippedDirs"`
	FailedDirs     int        `json:"failedDirs"`
	IndexedEntries int        `json:"indexedEntries"`
	Concurrency    int        `json:"concurrency"`
	SkipIndexed    bool       `json:"skipIndexed"`
	LastResult     string     `json:"lastResult,omitempty"`
	LastError      string     `json:"lastError,omitempty"`
}

func newFileIndexBackfillRunner() *fileIndexBackfillRunner {
	return &fileIndexBackfillRunner{}
}

func (runner *fileIndexBackfillRunner) Start(options fileIndexBackfillOptions) (context.Context, fileIndexBackfillStatus, bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()

	if runner.status.Running {
		return nil, runner.status, false
	}

	ctx, cancel := context.WithCancel(context.Background())
	startedAt := time.Now()
	runner.cancel = cancel
	runner.status = fileIndexBackfillStatus{
		Running:     true,
		StartedAt:   &startedAt,
		FromDate:    options.fromDate,
		ToDate:      options.toDate,
		TotalDays:   len(options.dates),
		Concurrency: options.concurrency,
		SkipIndexed: options.skipIndexed,
	}
	return ctx, runner.status, true
}

func (runner *fileIndexBackfillRunner) Status() fileIndexBackfillStatus {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.status
}

func (runner *fileIndexBackfillRunner) Update(update func(*fileIndexBackfillStatus)) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	update(&runner.status)
}

func (runner *fileIndexBackfillRunner) Finish(canceled bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()

	finishedAt := time.Now()
	runner.status.Running = false
	runner.status.StopRequested = false
	runner.status.Canceled = canceled
	runner.status.FinishedAt = &finishedAt
	runner.cancel = nil
}

func (runner *fileIndexBackfillRunner) Cancel() (fileIndexBackfillStatus, bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()

	if !runner.status.Running || runner.cancel == nil {
		return runner.status, false
	}
	runner.status.StopRequested = true
	runner.cancel()
	return runner.status, true
}

func (api *API) indexBackfillStatus(c *gin.Context) {
	writeJSON(c, http.StatusOK, gin.H{"backfill": api.backfill.Status()})
}

func (api *API) startIndexBackfill(c *gin.Context) {
	if !api.indexEnabled() || api.storage == nil {
		writeError(c, http.StatusBadRequest, "index_disabled", "目录索引未启用或 R2 未配置")
		return
	}

	var req fileIndexBackfillRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	options, err := api.buildIndexBackfillOptions(req, time.Now())
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_backfill", err.Error())
		return
	}

	ctx, status, started := api.backfill.Start(options)
	if !started {
		writeJSON(c, http.StatusConflict, gin.H{"ok": false, "backfill": status})
		return
	}

	go api.runIndexBackfill(ctx, options)
	writeJSON(c, http.StatusAccepted, gin.H{"ok": true, "backfill": status})
}

func (api *API) cancelIndexBackfill(c *gin.Context) {
	status, canceled := api.backfill.Cancel()
	writeJSON(c, http.StatusOK, gin.H{"ok": true, "canceled": canceled, "backfill": status})
}

func bindOptionalJSON(c *gin.Context, dest any) bool {
	if c.Request == nil || c.Request.Body == nil {
		return true
	}
	if err := c.ShouldBindJSON(dest); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		writeError(c, http.StatusBadRequest, "invalid_request", "请求参数不合法")
		return false
	}
	return true
}

func (api *API) buildIndexBackfillOptions(req fileIndexBackfillRequest, now time.Time) (fileIndexBackfillOptions, error) {
	location := api.indexLocation()
	today := indexDateOnly(now.In(location), location)

	from, err := parseIndexBackfillDate(req.From, indexBackfillDefaultStartDate, today, location)
	if err != nil {
		return fileIndexBackfillOptions{}, fmt.Errorf("from 日期不合法")
	}
	to, err := parseIndexBackfillDate(req.To, "today", today, location)
	if err != nil {
		return fileIndexBackfillOptions{}, fmt.Errorf("to 日期不合法")
	}
	if to.After(today) {
		to = today
	}
	if from.After(to) {
		return fileIndexBackfillOptions{}, fmt.Errorf("from 不能晚于 to")
	}

	concurrency, err := indexBackfillConcurrency(req.Concurrency, api.cfg.IndexBackfillConcurrency)
	if err != nil {
		return fileIndexBackfillOptions{}, err
	}
	skipIndexed := true
	if req.SkipIndexed != nil {
		skipIndexed = *req.SkipIndexed
	}

	dates := indexBackfillDates(from, to)
	if req.LimitDays > 0 && req.LimitDays < len(dates) {
		dates = dates[:req.LimitDays]
	}
	return fileIndexBackfillOptions{
		dates:       dates,
		concurrency: concurrency,
		fromDate:    formatIndexBackfillDate(from),
		toDate:      formatIndexBackfillDate(to),
		skipIndexed: skipIndexed,
	}, nil
}

func parseIndexBackfillDate(raw, fallback string, today time.Time, location *time.Location) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = fallback
	}
	if strings.EqualFold(value, "today") || strings.EqualFold(value, "now") {
		return today, nil
	}
	if strings.HasPrefix(value, "/") {
		parts := strings.Split(strings.Trim(value, "/"), "/")
		if len(parts) < 3 {
			return time.Time{}, fmt.Errorf("invalid date path")
		}
		value = strings.Join(parts[:3], "-")
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}, err
	}
	return indexDateOnly(parsed, location), nil
}

func indexBackfillConcurrency(raw, fallback int) (int, error) {
	if raw == 0 {
		raw = fallback
	}
	if raw == 0 {
		raw = indexBackfillDefaultConcurrency
	}
	if raw < 0 {
		return 0, fmt.Errorf("concurrency 不能小于 1")
	}
	if raw > indexBackfillMaxConcurrency {
		return 0, fmt.Errorf("concurrency 不能大于 %d", indexBackfillMaxConcurrency)
	}
	return raw, nil
}

func indexBackfillDates(from, to time.Time) []time.Time {
	dates := make([]time.Time, 0, int(to.Sub(from).Hours()/24)+1)
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		dates = append(dates, date)
	}
	return dates
}

func indexDateOnly(date time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.Local
	}
	year, month, day := date.In(location).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

func formatIndexBackfillDate(date time.Time) string {
	return date.Format("2006-01-02")
}

func indexBackfillPath(date time.Time) string {
	return date.Format("/2006/01/02")
}

func (api *API) runIndexBackfill(ctx context.Context, options fileIndexBackfillOptions) {
	log.Printf("file index backfill start from=%s to=%s days=%d concurrency=%d skipIndexed=%t",
		options.fromDate, options.toDate, len(options.dates), options.concurrency, options.skipIndexed)

	defer func() {
		canceled := errors.Is(ctx.Err(), context.Canceled)
		api.backfill.Finish(canceled)
		status := api.backfill.Status()
		log.Printf("file index backfill finish canceled=%t processed=%d indexedDirs=%d emptyDirs=%d skippedDirs=%d failedDirs=%d entries=%d",
			canceled,
			status.ProcessedDays,
			status.IndexedDirs,
			status.EmptyDirs,
			status.SkippedDirs,
			status.FailedDirs,
			status.IndexedEntries,
		)
	}()

	total := len(options.dates)
	if total == 0 {
		return
	}

	workerCount := options.concurrency
	if workerCount <= 0 {
		workerCount = indexBackfillDefaultConcurrency
	}
	if workerCount > total {
		workerCount = total
	}
	jobs := make(chan fileIndexBackfillJob)
	var wg sync.WaitGroup
	for workerID := 1; workerID <= workerCount; workerID++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					api.runIndexBackfillDay(ctx, options, job, workerID, total)
				}
			}
		}(workerID)
	}

	for i, date := range options.dates {
		job := fileIndexBackfillJob{index: i, date: date}
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- job:
		}
	}
	close(jobs)
	wg.Wait()
}

type fileIndexBackfillJob struct {
	index int
	date  time.Time
}

func (api *API) runIndexBackfillDay(ctx context.Context, options fileIndexBackfillOptions, job fileIndexBackfillJob, workerID, total int) {
	if ctx.Err() != nil {
		return
	}
	dir := indexBackfillPath(job.date)
	dateText := formatIndexBackfillDate(job.date)
	progress := job.index + 1
	startedAt := time.Now()
	log.Printf("file index backfill day start worker=%d progress=%d/%d date=%s dir=%q", workerID, progress, total, dateText, dir)
	api.backfill.Update(func(status *fileIndexBackfillStatus) {
		status.ActiveWorkers++
		status.CurrentDate = dateText
		status.CurrentPath = dir
	})
	defer api.backfill.Update(func(status *fileIndexBackfillStatus) {
		if status.ActiveWorkers > 0 {
			status.ActiveWorkers--
		}
	})

	if options.skipIndexed {
		indexed, err := api.fileIndex.DirectoryIndexed(ctx, dir)
		if err != nil {
			api.recordBackfillFailure(progress, total, dateText, dir, startedAt, err)
			return
		}
		if indexed {
			elapsed := time.Since(startedAt)
			log.Printf("file index backfill day skip worker=%d progress=%d/%d date=%s dir=%q reason=already_indexed elapsed=%s",
				workerID, progress, total, dateText, dir, elapsed)
			api.backfill.Update(func(status *fileIndexBackfillStatus) {
				status.ProcessedDays++
				status.SkippedDirs++
				status.LastResult = fmt.Sprintf("%s skipped already indexed", dir)
			})
			return
		}
	}

	indexed, count, err := api.refreshDirectoryIndex(ctx, dir)
	elapsed := time.Since(startedAt)
	if err != nil {
		api.recordBackfillFailure(progress, total, dateText, dir, startedAt, err)
	} else if indexed {
		log.Printf("file index backfill day indexed worker=%d progress=%d/%d date=%s dir=%q entries=%d elapsed=%s",
			workerID, progress, total, dateText, dir, count, elapsed)
		api.backfill.Update(func(status *fileIndexBackfillStatus) {
			status.ProcessedDays++
			status.IndexedDirs++
			status.IndexedEntries += count
			status.LastResult = fmt.Sprintf("%s indexed entries=%d", dir, count)
		})
	} else {
		log.Printf("file index backfill day empty worker=%d progress=%d/%d date=%s dir=%q elapsed=%s",
			workerID, progress, total, dateText, dir, elapsed)
		api.backfill.Update(func(status *fileIndexBackfillStatus) {
			status.ProcessedDays++
			status.EmptyDirs++
			status.LastResult = fmt.Sprintf("%s empty", dir)
		})
	}
}

func (api *API) recordBackfillFailure(progress, total int, dateText, dir string, startedAt time.Time, err error) {
	if err == nil {
		return
	}
	elapsed := time.Since(startedAt)
	log.Printf("file index backfill day failed progress=%d/%d date=%s dir=%q elapsed=%s err=%v",
		progress, total, dateText, dir, elapsed, err)
	api.backfill.Update(func(status *fileIndexBackfillStatus) {
		status.ProcessedDays++
		status.FailedDirs++
		status.LastResult = fmt.Sprintf("%s failed: %v", dir, err)
		status.LastError = fmt.Sprintf("%s: %v", dir, err)
	})
}
