package executor

import (
	"auto-take-go/internal/db"
	"auto-take-go/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Executor 任务执行引擎
type Executor struct {
	serverURL string
	uploadDir string
	browserPool *BrowserPool
}

// New 创建新的执行器
func New(serverURL, uploadDir string) (*Executor, error) {
	// 确保上传目录存在
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// 创建浏览器池
	pool, err := NewBrowserPool()
	if err != nil {
		return nil, fmt.Errorf("failed to create browser pool: %w", err)
	}

	return &Executor{
		serverURL:   serverURL,
		uploadDir:   uploadDir,
		browserPool: pool,
	}, nil
}

// Close 关闭执行器
func (e *Executor) Close() error {
	if e.browserPool != nil {
		return e.browserPool.Close()
	}
	return nil
}

// Run 执行一个任务，返回 error 表示失败
func (e *Executor) Run(ctx context.Context, exec *models.Execution, taskDef *models.TaskDefinition) error {
	// 更新执行状态为运行中
	if err := db.UpdateExecutionStatus(exec.ID, models.ExecutionRunning, "", ""); err != nil {
		return fmt.Errorf("failed to update execution status: %w", err)
	}

	// 创建日志通道
	logChan := make(chan LogEntry, 100)
	defer close(logChan)

	// 启动日志收集器
	var logs []LogEntry
	go func() {
		for entry := range logChan {
			logs = append(logs, entry)
		}
	}()

	// 创建执行上下文
	execCtx := &ExecutionContext{
		ExecutionID: exec.ID,
		Variables:   make(map[string]string),
		LogChan:     logChan,
		UploadDir:   e.uploadDir,
		CancelFunc:  func() {},
	}

	// 加载已保存的变量（如果有）
	if len(exec.Variables) > 0 {
		var savedVars map[string]string
		if err := json.Unmarshal(exec.Variables, &savedVars); err == nil {
			for k, v := range savedVars {
				execCtx.Variables[k] = v
			}
		}
	}

	// 创建浏览器上下文
	browserCtx, err := e.browserPool.NewContext()
	if err != nil {
		e.finishExecution(exec, taskDef, logs, err, "failed to create browser context")
		return err
	}
	defer browserCtx.Close()

	// 创建页面
	page := browserCtx.MustPage()
	defer page.Close()

	// 执行所有步骤
	startTime := time.Now()
	execCtx.Log("开始执行任务，共 %d 个步骤", len(taskDef.Steps))

	var lastErr error
	for i, step := range taskDef.Steps {
		// 检查是否被取消
		select {
		case <-ctx.Done():
			lastErr = ErrStopped
			execCtx.Log("任务被用户停止")
			goto finish
		default:
		}

		// 设置步骤超时
		stepTimeout := time.Duration(step.TimeoutSec) * time.Second
		if stepTimeout <= 0 {
			stepTimeout = 30 * time.Second // 默认 30 秒
		}

		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		execCtx.CancelFunc = cancel

		execCtx.Log("执行步骤 %d/%d: %s", i+1, len(taskDef.Steps), step.Type)
		if err := RunStep(stepCtx, &step, page, execCtx); err != nil {
			cancel()
			lastErr = err
			execCtx.LogError(err, "步骤 %d 执行失败", i+1)

			// 检查是否需要重试
			if taskDef.Retry != nil && exec.RetryCount < taskDef.Retry.MaxAttempts {
				if e.isRetryableError(err, taskDef.Retry.OnErrors) {
					execCtx.Log("将在 %d 秒后重试 (%d/%d)", taskDef.Retry.DelaySec, exec.RetryCount+1, taskDef.Retry.MaxAttempts)
					time.Sleep(time.Duration(taskDef.Retry.DelaySec) * time.Second)
					exec.RetryCount++
					// 重新执行
					i = -1 // 重置步骤索引
					lastErr = nil
					continue
				}
			}

			goto finish
		}
		cancel()
	}

finish:
	duration := time.Since(startTime)
	execCtx.Log("任务执行完成，耗时: %v", duration)

	return e.finishExecution(exec, taskDef, logs, lastErr, "")
}

// finishExecution 完成执行，更新状态
func (e *Executor) finishExecution(exec *models.Execution, taskDef *models.TaskDefinition, logs []LogEntry, execErr error, summary string) error {
	// 序列化日志
	logData, _ := json.Marshal(logs)

	// 序列化变量
	varData, _ := json.Marshal(exec.Variables)

	var status string
	var errMsg string

	if execErr != nil {
		status = models.ExecutionFailed
		errMsg = execErr.Error()
		if summary != "" {
			errMsg = summary + ": " + errMsg
		}

		// 失败处理
		if taskDef.OnFailure != nil {
			if taskDef.OnFailure.Screenshot {
				// 截图已在 RunStep 中处理
			}
		}
	} else {
		status = models.ExecutionSuccess
		summary = "任务执行成功"
	}

	// 更新执行记录
	if err := db.UpdateExecutionComplete(exec.ID, status, summary, string(logData), string(varData), errMsg); err != nil {
		return fmt.Errorf("failed to update execution: %w", err)
	}

	return execErr
}

// isRetryableError 检查错误是否可重试
func (e *Executor) isRetryableError(err error, retryableErrors []string) bool {
	if len(retryableErrors) == 0 {
		return true // 默认所有错误都可重试
	}

	errStr := err.Error()
	for _, retryable := range retryableErrors {
		if errStr == retryable {
			return true
		}
	}
	return false
}

// uploadFile 上传文件到服务器
func (e *Executor) uploadFile(execID string, filePath string) (string, error) {
	// 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// 生成目标路径
	filename := filepath.Base(filePath)
	targetPath := filepath.Join(e.uploadDir, execID, filename)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// 保存文件
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return targetPath, nil
}
