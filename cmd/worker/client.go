package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Client is the HTTP client for communicating with the server.
type Client struct {
	BaseURL string
	Secret  string
	http.Client
}

// NewClient creates a new Worker HTTP client.
func NewClient(baseURL, secret string) *Client {
	return &Client{
		BaseURL: baseURL,
		Secret:  secret,
		Client: http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Request/Response types

type RegisterRequest struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Concurrent int      `json:"concurrent"`
	Tags       []string `json:"tags"`
}

type HeartbeatRequest struct {
	ID   string `json:"id"`
	Load int    `json:"load"`
}

type TaskResponse struct {
	ExecutionID string `json:"execution_id"`
	TaskID      string `json:"task_id"`
	Definition  string `json:"definition"`
}

type ResultRequest struct {
	ExecutionID string            `json:"execution_id"`
	Status      string            `json:"status"`
	Log         string            `json:"log"`
	Variables   map[string]string `json:"variables,omitempty"`
	ErrorMsg    string            `json:"error_msg,omitempty"`
}

type ProgressRequest struct {
	ExecutionID string `json:"execution_id"`
	StepIndex   int    `json:"step_index"`
	TotalSteps  int    `json:"total_steps"`
	StepType    string `json:"step_type"`
	Message     string `json:"message,omitempty"`
}

// API response wrapper
type apiResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// doRequest sends an authenticated HTTP request and decodes the response.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*apiResponse, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api/v1"+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.Secret != "" {
		req.Header.Set("X-Worker-Secret", c.Secret)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return &apiResponse{Success: true}, nil
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &apiResp, fmt.Errorf("server error %d: %s", resp.StatusCode, apiResp.Message)
	}

	return &apiResp, nil
}

// Register registers the worker with the server.
func (c *Client) Register(ctx context.Context, req *RegisterRequest) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/workers/register", req)
	return err
}

// Heartbeat sends a heartbeat to the server.
func (c *Client) Heartbeat(ctx context.Context, req *HeartbeatRequest) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/workers/heartbeat", req)
	return err
}

// PollTask polls the server for the next available task.
// Returns nil if no task is available.
func (c *Client) PollTask(ctx context.Context, workerID string) (*TaskResponse, error) {
	path := fmt.Sprintf("/workers/next-task?worker_id=%s", workerID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if resp.Data == nil {
		return nil, nil
	}

	var task TaskResponse
	if err := json.Unmarshal(resp.Data, &task); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}

	return &task, nil
}

// SubmitResult submits a task execution result to the server.
func (c *Client) SubmitResult(ctx context.Context, req *ResultRequest) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/workers/task-result", req)
	return err
}

// ReportProgress reports step execution progress to the server.
func (c *Client) ReportProgress(ctx context.Context, req *ProgressRequest) error {
	_, err := c.doRequest(ctx, http.MethodPost, "/workers/task-progress", req)
	return err
}

// UploadFile uploads a file (screenshot, source, etc.) to the server.
func (c *Client) UploadFile(ctx context.Context, execID, fileType string, filename string, data []byte) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add form fields
	_ = writer.WriteField("execution_id", execID)
	_ = writer.WriteField("file_type", fileType)

	// Add file
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("write file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/workers/upload-file", &buf)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.Secret != "" {
		req.Header.Set("X-Worker-Secret", c.Secret)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
