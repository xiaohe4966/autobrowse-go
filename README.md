# 基于 Go + MySQL + 原生 HTML 的 Web 自动化任务编排系统

## 1. 系统概述

本系统提供基于浏览器的自动化任务编排与执行能力。用户通过 Web 界面（纯 HTML + JavaScript + CSS）创建、编辑、删除自动化任务；每个任务由一系列动作组件（打开网页、延迟、点击、检查文本/元素、模拟输入、条件分支、循环等）组成。

后端采用 Go 语言 + MySQL 数据库，负责存储任务定义、调度任务、管理执行端（Worker）。执行端同样使用 Go 编写，基于 rod 库控制 Chrome 浏览器，轮询服务端获取待执行任务，按流程执行并回传结果。

---

## 2. 技术栈

| 组件 | 技术选型 | 说明 |
|------|---------|------|
| 后端语言 | Go 1.21+ | 高性能、并发友好，适合 I/O 密集和浏览器自动化调度 |
| Web 框架 | 标准库 `net/http` + `go-chi/chi` | 轻量、无额外依赖，Router 简洁 |
| 数据库 | MySQL 5.7+ | 存储任务定义、执行记录、Worker 心跳 |
| 前端 | 原生 HTML5 / CSS3 / Vanilla JavaScript | 无构建工具，直接写 HTML/CSS/JS，使用 Fetch API 与后端交互 |
| 浏览器自动化 | [rod](https://github.com/go-rod/rod) | API 友好，支持 JavaScript 运行时和更简单的选择器 |
| 任务队列 | 基于 MySQL（`executions` 表 + 行锁） | 无需独立队列表，用数据库行锁保证并发安全 |
| 调度 | `robfig/cron/v3` | 解析 cron 表达式，支持秒级精度 |
| 实时推送 | Server-Sent Events (SSE) | 替代轮询，简化实现，WebSocket 的更简单替代方案 |

---

## 3. 系统架构

```
                          ┌──────────────────────────┐
                          │       用户浏览器           │
                          │   (原生 HTML / CSS / JS)   │
                          └────────────┬─────────────┘
                                       │ HTTP / SSE
                          ┌────────────▼─────────────┐
                          │      Go 服务端            │
                          │  (REST API + 调度器 + SSE) │
                          └────────────┬─────────────┘
                                       │
                          ┌────────────▼─────────────┐
                          │       MySQL 5.7          │
                          │ tasks / executions /     │
                          │ workers / task_templates │
                          └────────────┬─────────────┘
                                       │ HTTP (轮询任务)
                          ┌────────────▼─────────────┐
                          │   Go Worker (可多实例)    │
                          │   (rod 控制 Chrome)       │
                          └──────────────────────────┘
```

- **服务端**：提供 REST API、静态文件服务、Worker 管理、任务调度、SSE 实时推送
- **Worker 执行端**：独立进程，可部署多实例，通过 HTTP 轮询获取任务并执行浏览器自动化
- **数据库**：存储任务定义、执行记录、Worker 注册信息、任务模板
- **任务领取**：基于 MySQL 行锁（`SELECT ... FOR UPDATE`）原子领取，保证多 Worker 不抢同一任务

---

## 4. 数据库设计

### 4.1 任务表 `tasks`

| 字段名 | 类型 | 说明 |
|--------|------|------|
| id | VARCHAR(36) | 主键，UUID |
| name | VARCHAR(255) | 任务名称 |
| description | TEXT | 任务描述（新增） |
| enabled | TINYINT(1) | 是否启用调度（0 禁用，1 启用） |
| definition | JSON | 步骤定义（动作组件数组，结构见下文） |
| schedule_type | ENUM('none', 'cron', 'interval') | 调度类型 |
| schedule_cfg | JSON | 调度配置：cron 表达式 / 间隔毫秒数 / 重复次数（新增 retry 字段） |
| timeout_sec | INT | 任务整体超时秒数，默认 300（新增） |
| created_at | DATETIME | |
| updated_at | DATETIME | |

**索引**：`idx_enabled_schedule` ON (`enabled`, `schedule_type`)

### 4.2 执行实例表 `executions`

| 字段名 | 类型 | 说明 |
|--------|------|------|
| id | VARCHAR(36) | 主键 UUID |
| task_id | VARCHAR(36) | 关联 tasks.id |
| status | ENUM('pending', 'running', 'success', 'failed', 'stopped', 'retry') | 执行状态 |
| worker_id | VARCHAR(64) | 执行该任务的 Worker 标识 |
| start_time | DATETIME | |
| end_time | DATETIME | |
| result_summary | TEXT | 执行结果摘要 |
| result_log | LONGTEXT | 完整执行日志 |
| screenshot_path | VARCHAR(512) | 失败时截图路径（新增） |
| source_path | VARCHAR(512) | 渲染后 HTML 源码路径（新增） |
| variables | JSON | 执行时的变量快照 |
| retry_count | INT | 当前重试次数（新增） |
| error_msg | TEXT | 错误信息（失败时） |

**索引**：`idx_task_status` ON (`task_id`, `status`)，`idx_worker_status` ON (`worker_id`, `status`)，`idx_status_start` ON (`status`, `start_time`)

### 4.3 Worker 注册表 `workers`

| 字段名 | 类型 | 说明 |
|--------|------|------|
| id | VARCHAR(64) | Worker 唯一标识（启动时由启动参数指定，或自动生成 UUID） |
| name | VARCHAR(128) | Worker 显示名称（新增） |
| last_heartbeat | DATETIME | 最后心跳时间 |
| status | ENUM('idle', 'busy', 'offline') | |
| concurrent | INT | 最大并发任务数（默认 1） |
| current_load | INT | 当前正在执行的任务数（由服务端维护，新增） |
| tags | JSON | 标签列表，用于任务亲和性（新增） |
| created_at | DATETIME | |

**索引**：`idx_status_heartbeat` ON (`status`, `last_heartbeat`)

### 4.4 任务模板表 `task_templates`（新增）

| 字段名 | 类型 | 说明 |
|--------|------|------|
| id | VARCHAR(36) | 主键 UUID |
| name | VARCHAR(255) | 模板名称 |
| description | TEXT | 模板描述 |
| definition | JSON | 模板步骤定义 |
| tags | JSON | 标签 |
| created_at | DATETIME | |

### 4.5 步骤定义 `definition` JSON 结构

```json
{
  "steps": [
    { "type": "open", "url": "https://example.com/login" },
    { "type": "input", "selector": "#username", "text": "admin", "charDelayMs": 30 },
    { "type": "click", "selector": "button[type=submit]", "timeoutSec": 10 },
    { "type": "condition",
      "if": { "type": "hasText", "selector": "body", "text": "欢迎" },
      "then": [ { "type": "delay", "minSec": 1, "maxSec": 2 } ],
      "else": [ { "type": "delay", "sec": 5 } ]
    },
    { "type": "extract", "selector": "#order-id", "var": "orderId", "attr": "value" },
    { "type": "log", "message": "提取到订单号: {{orderId}}" },
    { "type": "loop", "count": 3, "steps": [
        { "type": "click", "selector": ".next-page" }
    ]},
    { "type": "waitSelector", "selector": "#result", "timeoutSec": 30 },
    { "type": "screenshot", "path": "result.png", "fullPage": true },
    { "type": "getSource", "selector": "#main-content" }
  ],
  "retry": {
    "maxAttempts": 3,
    "delaySec": 30,
    "onErrors": ["selector_not_found", "timeout", "navigation_failed"]
  },
  "onFailure": {
    "screenshot": true,
    "logVariables": true
  }
}
```

**支持的动作组件类型**：

| type | 说明 | 特有字段 |
|------|------|---------|
| `open` | 打开网页 | `url`, `timeoutSec` |
| `click` | 点击元素 | `selector`, `timeoutSec` |
| `input` | 输入文本 | `selector`, `text`, `charDelayMs`, `clear` |
| `delay` | 等待 | `sec` 或 `minSec`/`maxSec`（随机） |
| `hasText` | 检查文本是否存在 | `selector`, `text` |
| `waitSelector` | 等待元素出现 | `selector`, `timeoutSec` |
| `extract` | 提取数据到变量 | `selector`, `var`, `attr`（可选，提取属性而非文本） |
| `log` | 输出日志 | `message`（支持 `{{varName}}` 插值） |
| `condition` | 条件分支 | `if`, `then[]`, `else[]` |
| `loop` | 循环执行 | `count` 或 `until`（条件） |
| `screenshot` | 截图当前页面并上传服务器 | `path`（可选，默认自动命名），`fullPage`（可选，true 截整页） |
| `getSource` | 获取渲染后的完整 HTML 源码并上传服务器 | `selector`（可选，截取指定元素的 HTML，不填则取整页） |

---

## 5. 服务端 API 设计

所有接口返回 JSON，路径前缀 `/api/v1`。

### 5.1 认证（新增）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/auth/login` | 用户登录，返回 JWT Token |
| POST | `/auth/refresh` | 刷新 Token |

> 后续所有 API（除 Worker 相关接口）均需携带 `Authorization: Bearer <token>` 请求头。

### 5.2 任务管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/tasks` | 分页获取任务列表（支持 search、enabled 过滤） |
| POST | `/tasks` | 创建任务 |
| GET | `/tasks/:id` | 获取单个任务详情 |
| PUT | `/tasks/:id` | 更新任务（全量替换） |
| PATCH | `/tasks/:id` | 部分更新（支持仅启用/禁用调度） |
| DELETE | `/tasks/:id` | 删除任务 |
| POST | `/tasks/:id/run` | 手动立即执行一次任务 |
| POST | `/tasks/:id/stop` | 停止正在运行的任务实例 |
| POST | `/tasks/:id/clone` | 克隆任务（新增） |
| GET | `/tasks/:id/export` | 导出任务为 JSON 文件（新增） |

### 5.3 执行记录

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/executions` | 查询执行历史（支持 task_id、status、dateRange 过滤） |
| GET | `/executions/:id` | 获取执行详情（含完整日志） |
| GET | `/executions/:id/screenshot` | 下载失败截图（新增） |
| GET | `/executions/:id/source` | 下载渲染后 HTML 源码（新增） |
| GET | `/executions/:id/stream` | SSE 流，实时获取执行日志（新增） |

### 5.4 任务模板（新增）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/templates` | 获取模板列表 |
| POST | `/templates` | 创建模板 |
| POST | `/templates/:id/apply` | 基于模板创建任务 |
| DELETE | `/templates/:id` | 删除模板 |

### 5.5 Worker 交互

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/workers/register` | Worker 启动时注册，上报 ID、名称、并发数、标签 |
| POST | `/workers/heartbeat` | 心跳，更新 last_heartbeat 和 current_load |
| GET | `/workers/next-task` | Worker 轮询获取待执行任务（原子领取，行锁保证不重复） |
| POST | `/workers/task-result` | 汇报任务执行结果（成功/失败，日志） |
| POST | `/workers/task-progress` | 实时上报步骤进度（用于 SSE 推送） |
| POST | `/workers/upload-file` | Worker 上传截图或源码文件到服务端（新增） |
| GET | `/workers` | 获取 Worker 列表（供管理页面查看，新增） |

### 5.6 系统

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| POST | `/tasks/import` | 导入任务 JSON（新增） |

### 5.7 任务领取原子操作（关键实现）

Worker 获取任务时使用以下 SQL，保证多实例不抢同一任务：

```sql
-- 1. 在 pending 状态的执行记录中，原子领取一个
UPDATE executions
SET status = 'running', worker_id = ?, start_time = NOW()
WHERE id = (
    SELECT id FROM (
        SELECT id FROM executions
        WHERE status = 'pending' AND task_id IN (
            SELECT id FROM tasks WHERE enabled = 1
        )
        ORDER BY start_time ASC
        LIMIT 1
    ) AS t
)
AND status = 'pending';

-- 2. 查询被领取的记录
SELECT * FROM executions WHERE worker_id = ? AND status = 'running' ORDER BY start_time DESC LIMIT 1;
```

结合 Worker 的 `current_load < concurrent` 限制，做到：
- 同一任务不会被两个 Worker 领取
- 同一 Worker 不会超负载运行

---

## 6. 前端页面设计

采用单页应用风格（SPA），原生 HTML/JS 实现，通过 fetch + SSE 与后端交互。

### 6.1 页面结构

| 页面 | 路由 | 说明 |
|------|------|------|
| 仪表盘 | `/` | 概览：任务总数、执行次数、成功率、Worker 状态 |
| 任务列表 | `/tasks` | 表格展示所有任务，支持搜索、过滤 |
| 任务编辑器 | `/tasks/new`、`/tasks/:id/edit` | 表单 + JSON 编辑 + 步骤预览 |
| 执行历史 | `/executions` | 执行记录列表，点击展开日志 |
| 模板市场 | `/templates` | 预置模板列表，一键应用创建任务 |
| Worker 管理 | `/workers` | Worker 列表、心跳状态、当前负载（新增） |

### 6.2 任务编辑器

提供两种编辑模式：

1. **表单模式（推荐）**：动态表单生成，每个步骤类型对应一个表单卡片，支持拖拽排序
2. **JSON 源码模式**：CodeMirror 编辑器，带语法高亮和实时校验

**辅助功能**：
- 右侧快捷工具栏：点按插入常用步骤 JSON 片段（打开网页、点击、输入、延迟等）
- 左上角模式切换：表单 ↔ JSON，一键同步
- 步骤预览：显示简化流程图
- 变量高亮：`{{varName}}` 语法糖，实时提示可用变量

### 6.3 实时日志展示

执行详情页使用 SSE 订阅 `/api/v1/executions/:id/stream`，实时显示：
- 步骤开始/结束
- 变量提取结果
- 条件分支走向
- 错误信息和截图链接

失败时自动展示截图，点击可放大。

---

## 7. 执行端（Worker）设计

### 7.1 工作流程

```go
func main() {
    workerID := flag.String("id", "", "Worker ID")
    workerName := flag.String("name", "", "Worker display name")
    serverURL := flag.String("server", "http://localhost:8099", "Server URL")
    concurrent := flag.Int("concurrent", 1, "Max concurrent tasks")
    tags := flag.String("tags", "", "Worker tags (comma separated)")
    flag.Parse()

    // 启动时注册
    registerWorker(*serverURL, *workerID, *workerName, *concurrent, *tags)

    // 心跳 goroutine
    go heartbeatLoop(*serverURL, *workerID, *concurrent)

    // 任务执行循环
    for {
        task := pollNextTask(*serverURL, *workerID, *concurrent)
        if task == nil {
            time.Sleep(5 * time.Second)
            continue
        }
        go runTask(task, *serverURL) // 支持并发
    }
}
```

### 7.2 浏览器实例管理

```
一个 Worker 进程
├── 一个浏览器实例（rod.New().MustConnect()）
│   └── 多个浏览器上下文（browser.NewContext()），每个任务一个
│       └── 一个页面（page := context.MustPage()）
```

- 浏览器以 **headless** 模式启动，减少资源占用
- 每个任务使用独立的 Browser Context，避免 Cookie 和缓存相互干扰
- 任务完成后销毁 Context，释放资源

### 7.3 步骤执行引擎

```go
func runSteps(steps []Step, ctx context.Context, execCtx context.Context) error {
    browser := rod.New().Headless(true).MustConnect()
    defer browser.MustClose()

    for i, step := range steps {
        select {
        case <-execCtx.Done():
            return errors.New("stopped by user")
        default:
        }

        ctx, cancel := context.WithTimeout(ctx, time.Duration(step.TimeoutSec)*time.Second)
        defer cancel()

        if err := executeStep(step, ctx); err != nil {
            // 按错误类型判断是否重试
            if isRetryable(err) && step.CanRetry() {
                return err // 上层逻辑处理重试
            }
            return err
        }

        reportProgress(execCtx, i+1, len(steps), step.Type)
    }
    return nil
}
```

### 7.4 停止信号

- 服务端维护 `map[executionID]context.CancelFunc`
- Worker 在每个步骤前检查 `ctx.Err()`
- 用户调用 `/tasks/:id/stop` → 服务端调用 CancelFunc → Worker 感知到并优雅退出

### 7.5 失败截图

任务失败时自动截图并上传到服务端指定目录：

```go
page.MustScreenshot("failure.png")
uploadScreenshot(executionID, "failure.png")
```

---

## 8. 调度与重复执行

### 8.1 调度器

服务端启动一个 goroutine，每分钟扫描一次 tasks 表：

```go
func scheduleLoop() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        tasks := db.GetSchedulableTasks()
        for _, task := range tasks {
            if shouldSchedule(task) {
                jitter := rand.Int63n(int64(task.JitterSeconds)) // 错峰 jitter
                time.AfterFunc(time.Duration(jitter)*time.Second, func() {
                    createExecution(task.ID)
                })
            }
        }
    }
}
```

### 8.2 随机抖动（Jitter）

为避免同一时刻所有定时任务同时触发造成瞬时并发峰值，在调度触发时增加 **随机延迟**：

- 在 `tasks.schedule_cfg` 中增加 `jitterSec` 字段，默认 `60`
- cron 触发时，先等待 `[0, jitterSec]` 秒内的随机时间再创建执行实例

### 8.3 任务整体超时

`tasks.timeout_sec` 字段控制单个任务的最大执行时长，Worker 侧维护最外层超时 Context。

---

## 9. 部署与运行

### 9.1 环境要求

- Go 1.21+
- MySQL 5.7+
- Chrome/Chromium（Worker 所在机器，需与 rod 版本对应的 ChromeDriver）

### 9.2 数据库初始化

```sql
CREATE DATABASE auto_task CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
-- 执行 migrations/ 目录下的 SQL 文件
```

### 9.3 编译与启动

```bash
# 服务端
go build -o server ./cmd/server
./server --port 8099 --db "root:password@tcp(localhost:3306)/auto_task"

# Worker
go build -o worker ./cmd/worker
./worker --server=http://localhost:8099 --id=worker-1 --name="Worker-1" --concurrent=3
```

### 9.4 Docker 部署

```yaml
version: '3'
services:
  mysql:
    image: mysql:8
    environment:
      MYSQL_ROOT_PASSWORD: rootpass
      MYSQL_DATABASE: auto_task
    volumes:
      - mysql_data:/var/lib/mysql
      - ./migrations:/docker-entrypoint-initdb.d
    ports:
      - "3306:3306"

  server:
    build: ./server
    ports:
      - "8099:8099"
    depends_on:
      - mysql
    environment:
      - DB=root:rootpass@tcp(mysql:3306)/auto_task
      - PORT=8099
    volumes:
      - ./screenshots:/app/screenshots

  worker:
    build: ./worker
    depends_on:
      - server
    environment:
      - SERVER_URL=http://server:8099
      - WORKER_ID=worker-1
      - WORKER_CONCURRENT=3
    volumes:
      - ./screenshots:/app/screenshots

volumes:
  mysql_data:
```

---

## 10. 安全性设计

### 10.1 用户认证

- 服务端 API 使用 **JWT** 认证，Token 有效期 24 小时，支持刷新
- Worker 使用独立的预共享密钥（`WORKER_SECRET`）进行注册认证，与用户 Token 分离

### 10.2 输入校验

- 服务端对所有 JSON 请求做 **schema 校验**，拒绝不合法的 step definition
- 限制 `loop.count` 最大值（默认 100），防止无限循环
- 限制 `charDelayMs` 最大值，防止单字符输入延迟过长
- 步骤执行时设置全局超时，防止资源耗尽

### 10.3 XSS 防护

- 前端对用户输入内容做 HTML 转义再展示
- 执行日志中如有敏感信息（密码等），后端输出时做脱敏处理

### 10.4 Worker 隔离

- 每个 Worker 使用独立的 Chrome 用户数据目录（Context 隔离）
- 生产环境强制 headless 模式

---

## 11. 开发计划

| 周次 | 里程碑 | 交付物 |
|------|--------|--------|
| 第 1 周 | 服务端基础架构 | 路由框架、数据库连接、Migrations、任务 CRUD API、静态文件服务 |
| 第 2 周 | 前端基础页面 | 任务列表页、仪表盘、API 联调 |
| 第 3 周 | 任务编辑器 | JSON 编辑器、表单模式、快捷模板插入 |
| 第 4 周 | Worker 框架 | 注册、心跳、轮询任务、基本步骤执行（open/click/input/delay） |
| 第 5 周 | 复杂组件 | 条件分支、循环、数据提取、变量插值、失败截图 |
| 第 6 周 | 停止 + 进度 | 停止信号、SSE 实时日志 |
| 第 7 周 | 调度器 | cron / interval 调度、随机抖动、超时控制、重试机制 |
| 第 8 周 | 认证 + Worker 管理 | JWT 认证、Worker 列表页、任务导入导出、模板系统 |
| 第 9 周 | 测试 + 优化 | 单元测试、集成测试、性能优化、Docker 部署文档 |

---

## 12. 扩展建议（后续迭代）

- **分布式调度**：多服务端实例时，用 MySQL 行锁或 Redis 分布式锁确保同一任务不重复调度
- **选择器测试器**：前端提供一个"测试选择器"功能，代理到 Worker 实时验证选择器是否命中
- **通知集成**：任务失败时，通过 Webhook / 邮件 / 钉钉推送通知
- **任务依赖**：支持定义任务间的依赖关系（DAG），一个任务完成后触发下一个
- **Playwright 支持**：在 rod 之外增加 Playwright 作为可选引擎，扩展兼容性
- **录制成套件**：提供 Chrome 插件录制用户操作，自动生成 steps JSON，降低入门门槛
