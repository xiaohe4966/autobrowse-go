package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"auto-take-go/internal/models"
)

// Runner executes tasks using rod browser.
type Runner struct {
	client  *Client
	browser *rod.Browser
	execCtx map[string]context.CancelFunc
	execMu  sync.RWMutex
}

// NewRunner creates a new task runner.
func NewRunner(client *Client) (*Runner, error) {
	b := rod.New().Headless(true).NoSandbox(true).MustConnect()
	return &Runner{
		client:  client,
		browser: b,
		execCtx: make(map[string]context.CancelFunc),
	}, nil
}

// Run executes a task in a goroutine.
func (r *Runner) Run(execID string, definitionJSON string) {
	// Parse definition
	var def models.TaskDefinition
	if err := json.Unmarshal([]byte(definitionJSON), &def); err != nil {
		r.submitResult(execID, "failed", "", nil, fmt.Sprintf("parse definition: %v", err))
		return
	}

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	r.execMu.Lock()
	r.execCtx[execID] = cancel
	r.execMu.Unlock()

	// Run in goroutine
	go func() {
		defer func() {
			r.execMu.Lock()
			delete(r.execCtx, execID)
			r.execMu.Unlock()
			cancel()
		}()

		// Create browser context for this task (isolated)
		browserCtx := r.browser.MustIncognito()
		defer browserCtx.Close()

		// Create page
		page := browserCtx.MustPage()
		defer page.Close()

		// Setup runner context
		runnerCtx := &RunnerContext{
			ExecutionID: execID,
			Variables:   make(map[string]string),
			LogChan:     make(chan LogEntry, 100),
			UploadFunc:  r.client.UploadFile,
		}

		// Start log collector
		var logBuilder stringsBuilder
		go func() {
			for entry := range runnerCtx.LogChan {
				logBuilder.WriteString(fmt.Sprintf("[%s] %s\n", entry.Level, entry.Message))
			}
		}()

		// Execute steps
		totalSteps := len(def.Steps)
		for i, step := range def.Steps {
			// Check for cancellation
			select {
			case <-ctx.Done():
				runnerCtx.LogChan <- LogEntry{Level: "INFO", Message: "Task stopped by user"}
				close(runnerCtx.LogChan)
				r.submitResult(execID, "stopped", logBuilder.String(), runnerCtx.Variables, "")
				return
			default:
			}

			// Report progress
			if err := r.client.ReportProgress(context.Background(), &ProgressRequest{
				ExecutionID: execID,
				StepIndex:   i + 1,
				TotalSteps:  totalSteps,
				StepType:    step.Type,
			}); err != nil {
				log.Printf("[runner] report progress failed: %v", err)
			}

			// Execute step
			if err := runStep(ctx, page, &step, runnerCtx); err != nil {
				runnerCtx.LogChan <- LogEntry{Level: "ERROR", Message: fmt.Sprintf("Step %d (%s) failed: %v", i+1, step.Type, err)}
				close(runnerCtx.LogChan)

				// Handle failure: screenshot if enabled
				if def.OnFailure != nil && def.OnFailure.Screenshot {
					if data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{}); err == nil {
						_ = r.client.UploadFile(context.Background(), execID, "screenshot", "failure.png", data)
					}
				}

				r.submitResult(execID, "failed", logBuilder.String(), runnerCtx.Variables, err.Error())
				return
			}

			runnerCtx.LogChan <- LogEntry{Level: "INFO", Message: fmt.Sprintf("Step %d (%s) completed", i+1, step.Type)}
		}

		close(runnerCtx.LogChan)
		r.submitResult(execID, "success", logBuilder.String(), runnerCtx.Variables, "")
	}()
}

// Stop cancels a running task.
func (r *Runner) Stop(execID string) {
	r.execMu.Lock()
	defer r.execMu.Unlock()
	if cancel, ok := r.execCtx[execID]; ok {
		cancel()
	}
}

// Close shuts down the runner and closes the browser.
func (r *Runner) Close() error {
	r.execMu.Lock()
	for _, cancel := range r.execCtx {
		cancel()
	}
	r.execMu.Unlock()
	return r.browser.Close()
}

// submitResult submits the task result to the server.
func (r *Runner) submitResult(execID, status, logStr string, vars map[string]string, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &ResultRequest{
		ExecutionID: execID,
		Status:      status,
		Log:         logStr,
		Variables:   vars,
		ErrorMsg:    errMsg,
	}

	if err := r.client.SubmitResult(ctx, req); err != nil {
		log.Printf("[runner] submit result failed: %v", err)
	}
}

// RunnerContext holds execution context for a task.
type RunnerContext struct {
	ExecutionID string
	Variables   map[string]string
	LogChan     chan<- LogEntry
	UploadFunc  func(ctx context.Context, execID, fileType, filename string, data []byte) error
}

// LogEntry represents a log entry.
type LogEntry struct {
	Level   string
	Message string
}

// stringsBuilder is a simple string builder wrapper.
type stringsBuilder struct {
	data []string
}

func (b *stringsBuilder) WriteString(s string) {
	b.data = append(b.data, s)
}

func (b *stringsBuilder) String() string {
	var result string
	for _, s := range b.data {
		result += s
	}
	return result
}
