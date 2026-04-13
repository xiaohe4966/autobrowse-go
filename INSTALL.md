# AutoTakeGo 安装与使用指南

> 基于 Go + MySQL + Rod 浏览器自动化的任务编排系统

---

## 目录

- [环境准备](#环境准备)
- [数据库初始化](#数据库初始化)
- [编译项目](#编译项目)
- [配置与启动](#配置与启动)
- [本地 Demo 测试](#本地-demo-测试)
- [Docker 部署](#docker-部署)
- [常见问题](#常见问题)

---

## 环境准备

### 1. 安装 Go

项目需要 Go 1.21 或更高版本。

**macOS（Intel）**

```bash
# 使用 Homebrew 安装
brew install go

# 或者下载安装包
# 下载地址：https://go.dev/dl/go1.21.6.darwin-amd64.pkg
sudo installer -pkg /tmp/go1.21.6.darwin-amd64.pkg -target /
```

**macOS（Apple Silicon）**

```bash
# Homebrew
brew install go

# 或下载 ARM 版：https://go.dev/dl/go1.21.6.darwin-arm64.pkg
```

**Linux（Ubuntu/Debian）**

```bash
wget https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

**验证安装**

```bash
go version
# go version go1.21.6 darwin/amd64
```

### 2. 安装 MySQL

项目需要 MySQL 5.7 或更高版本（推荐 MySQL 8）。

**macOS**

```bash
brew install mysql
brew services start mysql

# 设置 root 密码
mysql_secure_installation
```

**Ubuntu/Debian**

```bash
sudo apt update
sudo apt install mysql-server
sudo systemctl start mysql
sudo mysql_secure_installation
```

**验证安装**

```bash
mysql --version
# mysql  Ver 8.x.x for macosxXX on x86_64
```

### 3. 安装 Chrome/Chromium

Worker 所在机器需要安装 Chrome 或 Chromium（Rod 控制浏览器使用）。

**macOS**

```bash
# 直接下载 Chrome：https://www.google.com/chrome/
# 或使用 Chromium
brew install --cask chromium
```

**Ubuntu/Debian**

```bash
sudo apt install chromium-browser
# 或
sudo apt install google-chrome-stable
```

**验证**

```bash
# macOS Chromium 路径
/Applications/Chromium.app/Contents/MacOS/Chromium --version

# Linux
chromium-browser --version
google-chrome --version
```

> ⚠️ 如果 Chrome/Chromium 不在默认路径，可以通过 `GOOGLE_CHROME_BIN` 环境变量指定：
> ```bash
> export GOOGLE_CHROME_BIN=/Applications/Chromium.app/Contents/MacOS/Chromium
> ```

---

## 数据库初始化

### 1. 创建数据库

```bash
mysql -u root -p

# 进入 MySQL 后执行：
```

```sql
CREATE DATABASE auto_task CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 2. 执行数据库迁移

迁移脚本位于 `internal/db/migrations/001_init.sql`，手动执行：

```bash
mysql -u root -p auto_task < internal/db/migrations/001_init.sql
```

或登录 MySQL 后：

```sql
USE auto_task;
SOURCE internal/db/migrations/001_init.sql;
```

### 3. 验证表结构

```sql
USE auto_task;
SHOW TABLES;
-- 应该看到：tasks, executions, workers, task_templates, migrations
```

---

## 编译项目

### 1. 克隆项目

```bash
git clone https://github.com/xiaohe4966/autobrowse-go.git
cd autobrowse-go
```

### 2. 下载依赖

```bash
go mod tidy
```

### 3. 编译

```bash
# 编译服务端
go build -o bin/server ./cmd/server

# 编译 Worker
go build -o bin/worker ./cmd/worker

# 编译本地 Demo（可选）
go build -o bin/demo ./cmd/demo
```

---

## 配置与启动

### 1. 服务端配置

通过环境变量或命令行参数配置：

| 环境变量 | 命令行参数 | 默认值 | 说明 |
|---------|-----------|--------|------|
| `PORT` | `--port` | `8099` | HTTP 服务端口 |
| `DB` | `--db` | - | MySQL DSN（必填） |
| `JWT_SECRET` | `--jwt-secret` | `change-me-in-production` | JWT 密钥（生产必改） |
| `WORKER_SECRET` | `--worker-secret` | - | Worker 认证密钥 |
| `UPLOAD_DIR` | `--upload-dir` | `./uploads` | 截图/源码存储目录 |

**示例（使用环境变量）**

```bash
export DB="root:yourpassword@tcp(localhost:3306)/auto_task?parseTime=true&charset=utf8mb4"
export JWT_SECRET="your-super-secret-jwt-key-change-in-production"
export PORT="8099"
./bin/server
```

**示例（使用命令行参数）**

```bash
./bin/server --port 8099 \
  --db "root:password@tcp(localhost:3306)/auto_task?parseTime=true&charset=utf8mb4" \
  --jwt-secret "change-me-in-production"
```

### 2. 启动服务端

```bash
./bin/server
# 输出：Server starting on port 8099
```

访问 Web 界面：`http://localhost:8099`

### 3. 启动 Worker

| 参数 | 说明 | 示例 |
|------|------|------|
| `--server` | 服务端地址 | `http://localhost:8099` |
| `--id` | Worker 唯一标识 | `worker-1` |
| `--name` | 显示名称 | `Worker-1` |
| `--concurrent` | 最大并发任务数 | `3` |
| `--tags` | 标签（逗号分隔） | `chrome,linux` |
| `--secret` | Worker 认证密钥 | 与服务端 `WORKER_SECRET` 一致 |

**示例**

```bash
./bin/worker \
  --server=http://localhost:8099 \
  --id=worker-1 \
  --name="Worker-1" \
  --concurrent=3 \
  --secret=your-worker-secret
```

**也可以使用环境变量**

```bash
export WORKER_SECRET="your-worker-secret"
./bin/worker --server=http://localhost:8099 --id=worker-1 --concurrent=3
```

### 4. 多 Worker 部署

同一服务端可部署多个 Worker 实例，分布在不同机器上：

```bash
# 机器 A
./bin/worker --server=http://your-server:8099 --id=worker-1 --name="Linux-Worker" --concurrent=5 --tags=linux,chrome

# 机器 B
./bin/worker --server=http://your-server:8099 --id=worker-2 --name="Mac-Worker" --concurrent=3 --tags=macos,chromium
```

---

## 本地 Demo 测试

无需启动服务端和数据库，可直接运行本地浏览器自动化 Demo。

### 1. 编译 Demo

```bash
go build -o bin/demo ./cmd/demo
```

### 2. 运行

```bash
./bin/demo
```

Demo 将：
- 启动一个可见 Chrome 浏览器窗口
- 访问 `httpbin.org/cookies` 验证自定义 Cookie 设置
- 访问百度并执行搜索
- 保存截图到 `/tmp/demo_result.png` 和 `/tmp/baidu_demo.png`
- 10 秒后自动关闭

### 3. 自定义测试配置

编辑 `cmd/demo/main.go` 中的 `TaskConfig` 部分：

```go
config := TaskConfig{
    URL:      "https://example.com",       // 修改目标 URL
    Headless: false,                        // true=无头模式 false=显示窗口
    Cookies: []Cookie{
        {"Name": "session", "Value": "xxx", "Domain": "example.com", "Path": "/"},
    },
    Headers: []Header{
        {"Name": "Authorization", "Value": "Bearer xxx"},
    },
}
```

---

## Docker 部署

### 1. 构建镜像

```bash
docker build -t autobrowse-go .
```

### 2. 使用 docker-compose 启动

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
    ports:
      - "3306:3306"
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 10s
      timeout: 5s
      retries: 5

  server:
    image: autobrowse-go
    command: ./server --port 8099 --db "root:rootpass@tcp(mysql:3306)/auto_task"
    ports:
      - "8099:8099"
    depends_on:
      mysql:
        condition: service_healthy
    volumes:
      - ./uploads:/app/uploads

  worker:
    image: autobrowse-go
    command: ./worker --server=http://server:8099 --id=worker-1 --concurrent=3
    depends_on:
      - server
    volumes:
      - ./uploads:/app/uploads

volumes:
  mysql_data:
```

```bash
docker-compose up -d
```

---

## 常见问题

### Q: Rod 连接 Chrome 失败？

**macOS Chrome 路径问题**

```bash
export GOOGLE_CHROME_BIN=/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome
```

**Linux 权限问题**

```bash
chromium-browser --no-sandbox
# 或
google-chrome --no-sandbox
```

### Q: MySQL 连接被拒绝？

检查 MySQL 是否允许远程连接：

```sql
ALTER USER 'root'@'localhost' IDENTIFIED WITH mysql_native_password BY 'yourpassword';
CREATE USER 'root'@'%' IDENTIFIED BY 'yourpassword';
GRANT ALL PRIVILEGES ON auto_task.* TO 'root'@'%';
FLUSH PRIVILEGES;
```

### Q: Worker 无法注册到服务端？

确保：
1. 服务端已启动并监听
2. `WORKER_SECRET` 与服务端配置一致
3. 网络互通（防火墙放行端口）

### Q: 截图/源码文件上传失败？

确保 `UPLOAD_DIR` 目录存在且有写权限：

```bash
mkdir -p uploads
chmod 755 uploads
```

### Q: Go 模块下载慢？

设置 Go 代理：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=off
```

---

## 项目结构

```
auto-take-go/
├── cmd/
│   ├── server/          # 服务端入口
│   ├── worker/          # Worker 入口
│   └── demo/            # 本地 Demo
├── internal/
│   ├── config/          # 配置加载
│   ├── db/              # 数据库操作
│   │   └── migrations/  # 迁移 SQL
│   ├── executor/        # 浏览器执行引擎
│   ├── models/          # 数据模型
│   └── scheduler/       # 任务调度器
├── web/                 # 前端静态文件
│   └── static/
│       ├── api.js      # 前端 API 调用
│       └── app.css     # 样式
├── bin/                 # 编译输出目录
├── INSTALL.md           # 本文件
└── README.md            # 项目说明文档
```

---

## 下一步

- 查看 [README.md](README.md) 了解系统架构和 API 文档
- 查看 [Web 界面](../web/) 了解前端功能
- 参考 `cmd/demo/main.go` 编写自定义自动化任务
