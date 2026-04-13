package main

import (
	"context"
	"database/sql"
	"fmt"
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
)

func main() {
	// 加载配置（环境变量 + 命令行参数）
	cfg := config.Load()

	// 验证必要配置
	if cfg.DBDSN == "" {
		log.Fatal("DB DSN is required. Use --db flag or DB environment variable")
	}
	if cfg.JWTSecret == "" {
		log.Println("Warning: JWT_SECRET not set, using default (insecure for production)")
		cfg.JWTSecret = "default-secret-change-in-production"
	}

	// 初始化数据库连接
	db, err := initDB(cfg.DBDSN)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Database connected successfully")

	// 确保上传目录存在
	if err := os.MkdirAll(cfg.UploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}

	// 创建 chi 路由
	r := chi.NewRouter()

	// 中间件
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))

	// API 路由组
	r.Route("/api/v1", func(r chi.Router) {
		// 健康检查
		r.Get("/health", healthHandler)

		// 认证相关
		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", authLoginHandler)
			r.Post("/refresh", authRefreshHandler)
		})

		// 任务管理（需要认证）
		r.Route("/tasks", func(r chi.Router) {
			r.Use(authMiddleware(cfg.JWTSecret))
			r.Get("/", listTasksHandler(db))
			r.Post("/", createTaskHandler(db))
			r.Post("/import", importTaskHandler(db))
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", getTaskHandler(db))
				r.Put("/", updateTaskHandler(db))
				r.Patch("/", patchTaskHandler(db))
				r.Delete("/", deleteTaskHandler(db))
				r.Post("/run", runTaskHandler(db))
				r.Post("/stop", stopTaskHandler(db))
				r.Post("/clone", cloneTaskHandler(db))
				r.Get("/export", exportTaskHandler(db))
			})
		})

		// 执行记录（需要认证）
		r.Route("/executions", func(r chi.Router) {
			r.Use(authMiddleware(cfg.JWTSecret))
			r.Get("/", listExecutionsHandler(db))
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", getExecutionHandler(db))
				r.Get("/screenshot", downloadScreenshotHandler(db, cfg.UploadDir))
				r.Get("/source", downloadSourceHandler(db, cfg.UploadDir))
				r.Get("/stream", streamExecutionHandler(db))
			})
		})

		// 任务模板（需要认证）
		r.Route("/templates", func(r chi.Router) {
			r.Use(authMiddleware(cfg.JWTSecret))
			r.Get("/", listTemplatesHandler(db))
			r.Post("/", createTemplateHandler(db))
			r.Route("/{id}", func(r chi.Router) {
				r.Delete("/", deleteTemplateHandler(db))
				r.Post("/apply", applyTemplateHandler(db))
			})
		})

		// Worker 交互（使用独立认证）
		r.Route("/workers", func(r chi.Router) {
			r.Use(workerAuthMiddleware)
			r.Post("/register", registerWorkerHandler(db))
			r.Post("/heartbeat", heartbeatHandler(db))
			r.Get("/next-task", getNextTaskHandler(db))
			r.Post("/task-result", submitTaskResultHandler(db))
			r.Post("/task-progress", submitTaskProgressHandler(db))
			r.Post("/upload-file", uploadFileHandler(cfg.UploadDir))
		})

		// Worker 管理列表（需要用户认证）
		r.With(authMiddleware(cfg.JWTSecret)).Get("/workers", listWorkersHandler(db))
	})

	// 静态文件服务
	workDir, _ := os.Getwd()
	filesDir := http.Dir(workDir + "/web")
	FileServer(r, "/", filesDir)

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// 启动调度器 goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go schedulerLoop(ctx, db)

	// 优雅关闭
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
		cancel() // 停止调度器
	}()

	log.Printf("Server starting on port %s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// initDB 初始化数据库连接
func initDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn+"?parseTime=true&loc=Local&charset=utf8mb4")
	if err != nil {
		return nil, err
	}

	// 设置连接池参数
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	return db, nil
}

// FileServer 提供静态文件服务，处理 SPA 路由
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

// 健康检查处理器
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// 以下为占位符处理器，实际实现需根据业务逻辑补充

func authLoginHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error":"not implemented"}`))
}

func authRefreshHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error":"not implemented"}`))
}

func authMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO: 实现 JWT 验证
			next.ServeHTTP(w, r)
		})
	}
}

func workerAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: 实现 Worker 认证
		next.ServeHTTP(w, r)
	})
}

func listTasksHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func createTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func importTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func getTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func updateTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func patchTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func deleteTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func runTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func stopTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func cloneTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func exportTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func listExecutionsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func getExecutionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func downloadScreenshotHandler(db *sql.DB, uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func downloadSourceHandler(db *sql.DB, uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func streamExecutionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`data: {"error":"not implemented"}\n\n`))
	}
}

func listTemplatesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func createTemplateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func deleteTemplateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func applyTemplateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func registerWorkerHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func heartbeatHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func getNextTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func submitTaskResultHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func submitTaskProgressHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func uploadFileHandler(uploadDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func listWorkersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"error":"not implemented"}`))
	}
}

func schedulerLoop(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	log.Println("Scheduler started")

	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler stopped")
			return
		case <-ticker.C:
			// TODO: 实现任务调度逻辑
			log.Println("Scheduler tick - checking for schedulable tasks")
		}
	}
}

