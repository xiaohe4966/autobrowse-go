package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/go-sql-driver/mysql"

	"auto-take-go/internal/config"
	"auto-take-go/internal/db"
	"auto-take-go/internal/models"
)

func main() {
	cfg := config.Load()

	if cfg.DB.DSN == "" {
		log.Fatal("DB DSN is required. Set DB env or --db flag")
	}
	if cfg.JWT.Secret == "" {
		log.Println("Warning: JWT_SECRET not set, using default (insecure for production)")
		cfg.JWT.Secret = "default-secret-change-in-production"
	}

	// 用 db.Init 初始化 db 包全局变量
	if err := db.Init(cfg.DB.DSN + "?parseTime=true&loc=Local&charset=utf8mb4"); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Database connected successfully")

	if err := os.MkdirAll(cfg.Upload.Dir, 0755); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", healthHandler)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", authLoginHandler)
			r.Post("/refresh", authRefreshHandler)
		})

		r.Route("/tasks", func(r chi.Router) {
			r.Use(authMiddleware(cfg.JWT.Secret))
			r.Get("/", listTasksHandler())
			r.Post("/", createTaskHandler())
			r.Post("/import", importTaskHandler())
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", getTaskHandler())
				r.Put("/", updateTaskHandler())
				r.Patch("/", patchTaskHandler())
				r.Delete("/", deleteTaskHandler())
				r.Post("/run", runTaskHandler())
				r.Post("/stop", stopTaskHandler())
				r.Post("/clone", cloneTaskHandler())
				r.Get("/export", exportTaskHandler())
			})
		})

		r.Route("/executions", func(r chi.Router) {
			r.Use(authMiddleware(cfg.JWT.Secret))
			r.Get("/", listExecutionsHandler())
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", getExecutionHandler())
				r.Get("/screenshot", downloadScreenshotHandler(cfg.Upload.Dir))
				r.Get("/source", downloadSourceHandler(cfg.Upload.Dir))
				r.Get("/stream", streamExecutionHandler())
			})
		})

		r.Route("/templates", func(r chi.Router) {
			r.Use(authMiddleware(cfg.JWT.Secret))
			r.Get("/", listTemplatesHandler())
			r.Post("/", createTemplateHandler())
			r.Route("/{id}", func(r chi.Router) {
				r.Delete("/", deleteTemplateHandler())
				r.Post("/apply", applyTemplateHandler())
			})
		})

		// Worker 内部接口（独立鉴权）
		r.Route("/workers", func(r chi.Router) {
			r.Use(workerAuthMiddleware)
			r.Post("/register", registerWorkerHandler())
			r.Post("/heartbeat", heartbeatHandler())
			r.Get("/next-task", getNextTaskHandler())
			r.Post("/task-result", submitTaskResultHandler())
			r.Post("/task-progress", submitTaskProgressHandler())
			r.Post("/upload-file", uploadFileHandler(cfg.Upload.Dir))
		})

		// Worker 列表（用户认证）
		r.With(authMiddleware(cfg.JWT.Secret)).Get("/workers", listWorkersHandler())
	})

	workDir, _ := os.Getwd()
	filesDir := http.Dir(workDir + "/web")
	FileServer(r, "/", filesDir)

	srv := &http.Server{Addr: ":" + cfg.Server.Port, Handler: r}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go schedulerLoop(ctx)

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		cancel()
	}()

	log.Printf("Server starting on port %s", cfg.Server.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// FileServer serves static files and handles SPA routing.
func FileServer(r chi.Router, path string, root http.FileSystem) {
	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", http.StatusMovedPermanently).ServeHTTP)
		path += "/"
	}
	path += "*"
	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fs := http.StripPrefix(pathPrefix, http.FileServer(root))
		fs.ServeHTTP(w, r)
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// ── Auth ─────────────────────────────────────────────────────────────────────

func authLoginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	// TODO: 实现真实 JWT 签发
	w.WriteHeader(http.StatusNotImplemented)
}

func authRefreshHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

func authMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO: 解析 JWT Bearer Token
			_ = secret
			next.ServeHTTP(w, r)
		})
	}
}

func workerAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: 验证 X-Worker-Secret header
		next.ServeHTTP(w, r)
	})
}

// ── Task handlers ─────────────────────────────────────────────────────────────

func listTasksHandler() http.HandlerFunc { return notImplemented }
func createTaskHandler() http.HandlerFunc { return notImplemented }
func importTaskHandler() http.HandlerFunc { return notImplemented }
func getTaskHandler() http.HandlerFunc    { return notImplemented }
func updateTaskHandler() http.HandlerFunc { return notImplemented }
func patchTaskHandler() http.HandlerFunc  { return notImplemented }
func deleteTaskHandler() http.HandlerFunc { return notImplemented }
func cloneTaskHandler() http.HandlerFunc  { return notImplemented }
func exportTaskHandler() http.HandlerFunc { return notImplemented }

// POST /tasks/{id}/run — 创建 execution 记录
func runTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "id")
		if taskID == "" {
			http.Error(w, `{"error":"task_id required"}`, http.StatusBadRequest)
			return
		}
		task, err := db.GetByID(taskID)
		if err != nil || task == nil {
			http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
			return
		}
		exec := &models.Execution{
			ID:        newID(),
			TaskID:    taskID,
			Status:    models.ExecutionPending,
			StartTime: nil,
		}
		if err := db.CreateExecution(exec); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"execution_id": exec.ID})
	}
}

// POST /tasks/{id}/stop — 停止 task 下正在运行的 execution
func stopTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "id")
		// 找这个 task 下状态为 running 的最新 execution
		var execID string
		err := db.DB.QueryRow(
			`SELECT id FROM executions WHERE task_id=? AND status='running' ORDER BY start_time DESC LIMIT 1`,
			taskID,
		).Scan(&execID)
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"no running execution found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		if err := db.StopExecution(execID); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}
}

// ── Execution handlers ────────────────────────────────────────────────────────

func listExecutionsHandler() http.HandlerFunc { return notImplemented }
func getExecutionHandler() http.HandlerFunc    { return notImplemented }

func downloadScreenshotHandler(uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		execID := chi.URLParam(r, "id")
		exec, err := db.GetExecutionByID(execID)
		if err != nil || exec == nil {
			http.Error(w, `{"error":"execution not found"}`, http.StatusNotFound)
			return
		}
		if exec.ScreenshotPath == "" {
			http.Error(w, `{"error":"no screenshot"}`, http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, exec.ScreenshotPath)
	}
}

func downloadSourceHandler(uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		execID := chi.URLParam(r, "id")
		exec, err := db.GetExecutionByID(execID)
		if err != nil || exec == nil {
			http.Error(w, `{"error":"execution not found"}`, http.StatusNotFound)
			return
		}
		if exec.SourcePath == "" {
			http.Error(w, `{"error":"no source"}`, http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, exec.SourcePath)
	}
}

func streamExecutionHandler() http.HandlerFunc { return notImplemented }

// ── Template handlers ────────────────────────────────────────────────────────

func listTemplatesHandler()   http.HandlerFunc { return notImplemented }
func createTemplateHandler()  http.HandlerFunc { return notImplemented }
func deleteTemplateHandler()  http.HandlerFunc { return notImplemented }
func applyTemplateHandler()   http.HandlerFunc { return notImplemented }

// ── Worker handlers ──────────────────────────────────────────────────────────

func registerWorkerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Concurrent int    `json:"concurrent"`
			Tags       []byte `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if req.ID == "" {
			http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
			return
		}
		worker := &models.Worker{
			ID:          req.ID,
			Name:        req.Name,
			Concurrent:  req.Concurrent,
			CurrentLoad: 0,
			Status:      models.WorkerIdle,
			Tags:        req.Tags,
		}
		if err := db.RegisterWorker(worker); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func heartbeatHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID   string `json:"id"`
			Load int    `json:"load"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// 随机抽样：10% 概率清理离线 worker，减少数据库压力
		if time.Now().UnixNano()%10 == 0 {
			db.SetOfflineIfExpired(30 * time.Second)
		}

		if err := db.UpdateHeartbeat(req.ID); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		if err := db.UpdateWorkerLoad(req.ID, req.Load); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func getNextTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workerID := r.URL.Query().Get("worker_id")
		if workerID == "" {
			http.Error(w, `{"error":"worker_id is required"}`, http.StatusBadRequest)
			return
		}

		wk, err := db.GetWorkerByID(workerID)
		if err != nil || wk == nil {
			http.Error(w, `{"error":"worker not found"}`, http.StatusNotFound)
			return
		}

		exec, err := db.ClaimTask(workerID, wk.Concurrent)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		if exec == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var definition []byte
		var taskID string
		err = db.DB.QueryRow(
			`SELECT id, definition FROM tasks WHERE id=?`,
			exec.TaskID,
		).Scan(&taskID, &definition)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"execution_id": exec.ID,
			"task_id":      taskID,
			"definition":   json.RawMessage(definition),
		})
	}
}

func submitTaskResultHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ExecutionID string            `json:"execution_id"`
			Status      string            `json:"status"`
			Log         string            `json:"log"`
			Variables   map[string]string `json:"variables"`
			ErrorMsg    string            `json:"error_msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if req.Status == "" {
			req.Status = models.ExecutionSuccess
		}
		if req.Variables != nil {
			b, _ := json.Marshal(req.Variables)
			_, _ = db.DB.Exec(`UPDATE executions SET variables=? WHERE id=?`, b, req.ExecutionID)
		}

		err := db.UpdateExecutionResult(req.ExecutionID, req.Status, "", req.Log, req.ErrorMsg)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func submitTaskProgressHandler() http.HandlerFunc {
	// Worker 进度上报，目前只做空处理，后续可写 execution_logs 表
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func uploadFileHandler(uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		execID := r.FormValue("execution_id")
		fileType := r.FormValue("file_type")
		if execID == "" || fileType == "" {
			http.Error(w, `{"error":"execution_id and file_type are required"}`, http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, `{"error":"no file uploaded"}`, http.StatusBadRequest)
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, `{"error":"failed to read file"}`, http.StatusInternalServerError)
			return
		}

		filename := fmt.Sprintf("%s_%s_%s", execID, fileType, header.Filename)
		fpath := uploadDir + "/" + filename
		if err := os.WriteFile(fpath, data, 0644); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}

		var col string
		switch fileType {
		case "screenshot":
			col = "screenshot_path"
		case "source":
			col = "source_path"
		default:
			http.Error(w, `{"error":"unknown file_type"}`, http.StatusBadRequest)
			return
		}

		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE executions SET %s=? WHERE id=?`, col), fpath, execID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": fpath})
	}
}

func listWorkersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workers, err := db.ListWorkers()
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(workers)
	}
}

// ── Scheduler ────────────────────────────────────────────────────────────────

func schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	log.Println("Scheduler started")
	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler stopped")
			return
		case <-ticker.C:
			// 定时清理离线 worker
			db.SetOfflineIfExpired(30 * time.Second)
			log.Println("Scheduler tick")
		}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func notImplemented(w http.ResponseWriter, r *http.Request) {
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// newID generates a UUID-like ID.
func newID() string {
	b := make([]byte, 16)
	_ = strings.NewReader(string(b[:0]))
	t := time.Now().UnixNano()
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(t>>32), uint16(t>>16), uint16(t),
		uint16(time.Now().Unix()>>48)&0xFFFF,
		time.Now().UnixNano()&0xFFFFFFFFFFFF,
	)
}
