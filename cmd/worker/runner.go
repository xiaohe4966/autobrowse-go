package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
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
	u := launcher.New().Bin("").Headless(true).NoSandbox(true).MustLaunch()
	b := rod.New().ControlURL(u).MustConnect()
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
	LogChan     chan LogEntry
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

// runStep executes a single step and returns an error if it fails.
func runStep(ctx context.Context, page *rod.Page, step *models.Step, rc *RunnerContext) error {
	switch step.Type {
	case "open":
		return runOpen(ctx, page, step)
	case "click":
		return runClick(ctx, page, step)
	case "input":
		return runInput(ctx, page, step)
	case "delay":
		return runDelay(step)
	case "waitSelector":
		return runWaitSelector(ctx, page, step)
	case "hasText":
		return runHasText(ctx, page, step, rc)
	case "extract":
		return runExtract(ctx, page, step, rc)
	case "log":
		runLog(step, rc)
		return nil
	case "condition":
		return runCondition(ctx, page, step, rc)
	case "loop":
		return runLoop(ctx, page, step, rc)
	case "screenshot":
		return runScreenshot(ctx, page, step, rc)
	case "getSource":
		return runGetSource(ctx, page, step, rc)
	case "js":
		return runJS(ctx, page, step)
	case "setCookie":
		return runSetCookie(ctx, page, step)
	case "setHeader":
		return runSetHeader(page, step)
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
}

// --- Step implementations ---

func runOpen(ctx context.Context, page *rod.Page, step *models.Step) error {
	if step.URL == "" {
		return fmt.Errorf("open step requires url")
	}
	_ = ctx
	page.Timeout(time.Duration(step.TimeoutSec)*time.Second).MustNavigate(step.URL)
	return nil
}

func runClick(ctx context.Context, page *rod.Page, step *models.Step) error {
	if step.Selector == "" {
		return fmt.Errorf("click step requires selector")
	}
	_ = ctx
	page.Timeout(time.Duration(step.TimeoutSec)*time.Second).MustElement(step.Selector).MustClick()
	return nil
}

func runInput(ctx context.Context, page *rod.Page, step *models.Step) error {
	if step.Selector == "" {
		return fmt.Errorf("input step requires selector")
	}
	el := page.Timeout(time.Duration(step.TimeoutSec)*time.Second).MustElement(step.Selector)
	if step.Clear {
		el.MustSelectAllText().MustEval(`(el) => el.value = ''`)
	}
	if step.CharDelayMs > 0 {
		for _, ch := range step.Text {
			el.Input(string(ch))
			time.Sleep(time.Duration(step.CharDelayMs) * time.Millisecond)
		}
	} else {
		el.Input(step.Text)
	}
	return nil
}

func runDelay(step *models.Step) error {
	if step.Sec > 0 {
		time.Sleep(time.Duration(step.Sec) * time.Second)
	} else if step.MinSec > 0 || step.MaxSec > 0 {
		min := step.MinSec
		max := step.MaxSec
		if max < min {
			max = min
		}
		delay := min + rand.Intn(max-min+1)
		time.Sleep(time.Duration(delay) * time.Second)
	} else {
		time.Sleep(1 * time.Second)
	}
	return nil
}

func runWaitSelector(ctx context.Context, page *rod.Page, step *models.Step) error {
	if step.Selector == "" {
		return fmt.Errorf("waitSelector step requires selector")
	}
	_ = ctx
	page.Timeout(time.Duration(step.TimeoutSec)*time.Second).MustElement(step.Selector)
	return nil
}

func runHasText(ctx context.Context, page *rod.Page, step *models.Step, rc *RunnerContext) error {
	if step.Selector == "" {
		return fmt.Errorf("hasText step requires selector")
	}
	text := step.Text
	if text == "" {
		text = step.Text2
	}
	if text == "" {
		return fmt.Errorf("hasText step requires text or text2")
	}
	_ = ctx
	page.Timeout(time.Duration(step.TimeoutSec)*time.Second).MustElement(step.Selector)
	elText := page.MustElement(step.Selector).MustText()
	matched := strings.Contains(elText, text)
	rc.LogChan <- LogEntry{Level: "INFO", Message: fmt.Sprintf("hasText(%s, %q) = %v (actual: %q)", step.Selector, text, matched, elText)}
	if !matched {
		return fmt.Errorf("text not found: %q in %s", text, step.Selector)
	}
	return nil
}

func runExtract(ctx context.Context, page *rod.Page, step *models.Step, rc *RunnerContext) error {
	if step.Selector == "" {
		return fmt.Errorf("extract step requires selector")
	}
	if step.Var == "" {
		return fmt.Errorf("extract step requires var name")
	}
	_ = ctx
	el := page.Timeout(time.Duration(step.TimeoutSec)*time.Second).MustElement(step.Selector)
	var value string
	if step.Attr != "" {
		if v, _ := el.Attribute(step.Attr); v != nil {
			value = *v
		}
	} else {
		value = el.MustText()
	}
	rc.Variables[step.Var] = value
	rc.LogChan <- LogEntry{Level: "INFO", Message: fmt.Sprintf("extract %s=%s", step.Var, value)}
	return nil
}

func runLog(step *models.Step, rc *RunnerContext) {
	msg := step.Message
	for name, val := range rc.Variables {
		msg = strings.ReplaceAll(msg, "{{"+name+"}}", val)
	}
	rc.LogChan <- LogEntry{Level: "INFO", Message: msg}
}

func runCondition(ctx context.Context, page *rod.Page, step *models.Step, rc *RunnerContext) error {
	if step.If == nil {
		return fmt.Errorf("condition step requires if block")
	}
	err := runStep(ctx, page, step.If, rc)
	if err != nil {
		rc.LogChan <- LogEntry{Level: "INFO", Message: "condition: false, executing else branch"}
		return runSteps(ctx, page, step.Else, rc)
	}
	rc.LogChan <- LogEntry{Level: "INFO", Message: "condition: true, executing then branch"}
	return runSteps(ctx, page, step.Then, rc)
}

func runLoop(ctx context.Context, page *rod.Page, step *models.Step, rc *RunnerContext) error {
	if len(step.Steps) == 0 {
		return fmt.Errorf("loop step requires steps")
	}
	if step.Count > 0 {
		for i := 0; i < step.Count; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			rc.LogChan <- LogEntry{Level: "INFO", Message: fmt.Sprintf("loop iteration %d/%d", i+1, step.Count)}
			if err := runSteps(ctx, page, step.Steps, rc); err != nil {
				return err
			}
		}
	}
	return nil
}

func runScreenshot(ctx context.Context, page *rod.Page, step *models.Step, rc *RunnerContext) error {
	path := step.Path
	if path == "" {
		path = fmt.Sprintf("screenshot_%d.png", time.Now().Unix())
	}
	data, err := page.Screenshot(step.FullPage, &proto.PageCaptureScreenshot{})
	if err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}
	rc.LogChan <- LogEntry{Level: "INFO", Message: fmt.Sprintf("screenshot saved: %s (%d bytes)", path, len(data))}
	if rc.UploadFunc != nil {
		_ = rc.UploadFunc(ctx, rc.ExecutionID, "screenshot", path, data)
	}
	return nil
}

func runGetSource(ctx context.Context, page *rod.Page, step *models.Step, rc *RunnerContext) error {
	_ = ctx
	var html string
	if step.Selector2 != "" {
		els := page.MustElements(step.Selector2)
		if len(els) == 0 {
			return fmt.Errorf("selector not found: %s", step.Selector2)
		}
		html, _ = els[0].HTML()
	} else {
		html = page.MustHTML()
	}
	rc.LogChan <- LogEntry{Level: "INFO", Message: fmt.Sprintf("getSource: %d chars", len(html))}
	if rc.UploadFunc != nil {
		_ = rc.UploadFunc(ctx, rc.ExecutionID, "source", "source.html", []byte(html))
	}
	return nil
}

func runJS(ctx context.Context, page *rod.Page, step *models.Step) error {
	if step.Script == "" {
		return fmt.Errorf("js step requires script")
	}
	page.Timeout(time.Duration(step.TimeoutSec)*time.Second).MustEval(step.Script)
	return nil
}

func runSetCookie(ctx context.Context, page *rod.Page, step *models.Step) error {
	for _, c := range step.Cookies {
		params := proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
		}
		if c.Expires > 0 {
			params.Expires = proto.TimeSinceEpoch(c.Expires)
		}
		switch c.SameSite {
		case "Strict":
			params.SameSite = proto.NetworkCookieSameSiteStrict
		case "Lax":
			params.SameSite = proto.NetworkCookieSameSiteLax
		case "None":
			params.SameSite = proto.NetworkCookieSameSiteNone
		}
		page.Browser().SetCookies([]*proto.NetworkCookieParam{&params})
	}
	return nil
}

func runSetHeader(page *rod.Page, step *models.Step) error {
	if len(step.Headers) == 0 {
		return nil
	}
	headerJSON := "{"
	for i, h := range step.Headers {
		if i > 0 {
			headerJSON += ", "
		}
		headerJSON += fmt.Sprintf(`"%s": "%s"`, h.Name, h.Value)
	}
	headerJSON += "}"
	page.MustEval(fmt.Sprintf(`() => {
		const extraHeaders = %s;
		const origFetch = window.fetch;
		window.fetch = function(url, options) {
			options = options || {};
			options.headers = { ...options.headers, ...extraHeaders };
			return origFetch(url, options);
		};
		const origXHR = window.XMLHttpRequest;
		window.XMLHttpRequest = function() {
			const xhr = new origXHR();
			const origOpen = xhr.open;
			xhr.open = function(method, url, ...args) {
				for (const [k, v] of Object.entries(extraHeaders)) {
					xhr.setRequestHeader(k, v);
				}
				return origOpen.call(this, method, url, ...args);
			};
			return xhr;
		};
	}`, headerJSON))
	return nil
}

// runSteps executes a list of steps in order.
func runSteps(ctx context.Context, page *rod.Page, steps []models.Step, rc *RunnerContext) error {
	for i, step := range steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := runStep(ctx, page, &step, rc); err != nil {
			return fmt.Errorf("step %d (%s): %w", i, step.Type, err)
		}
	}
	return nil
}

