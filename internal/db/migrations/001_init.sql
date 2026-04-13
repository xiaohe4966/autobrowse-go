-- tasks 表
CREATE TABLE IF NOT EXISTS tasks (
  id VARCHAR(36) PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  enabled TINYINT(1) NOT NULL DEFAULT 0,
  definition JSON NOT NULL,
  schedule_type ENUM('none','cron','interval') NOT NULL DEFAULT 'none',
  schedule_cfg JSON,
  timeout_sec INT NOT NULL DEFAULT 300,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_enabled_schedule (enabled, schedule_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- executions 表
CREATE TABLE IF NOT EXISTS executions (
  id VARCHAR(36) PRIMARY KEY,
  task_id VARCHAR(36) NOT NULL,
  status ENUM('pending','running','success','failed','stopped','retry') NOT NULL DEFAULT 'pending',
  worker_id VARCHAR(64),
  start_time DATETIME,
  end_time DATETIME,
  result_summary TEXT,
  result_log LONGTEXT,
  screenshot_path VARCHAR(512),
  source_path VARCHAR(512),
  variables JSON,
  retry_count INT NOT NULL DEFAULT 0,
  error_msg TEXT,
  INDEX idx_task_status (task_id, status),
  INDEX idx_worker_status (worker_id, status),
  INDEX idx_status_start (status, start_time),
  FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- workers 表
CREATE TABLE IF NOT EXISTS workers (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(128),
  last_heartbeat DATETIME,
  status ENUM('idle','busy','offline') NOT NULL DEFAULT 'offline',
  concurrent INT NOT NULL DEFAULT 1,
  current_load INT NOT NULL DEFAULT 0,
  tags JSON,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_status_heartbeat (status, last_heartbeat)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- task_templates 表
CREATE TABLE IF NOT EXISTS task_templates (
  id VARCHAR(36) PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  definition JSON NOT NULL,
  tags JSON,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- migrations 表（记录已执行的迁移）
CREATE TABLE IF NOT EXISTS migrations (
  id INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(255) NOT NULL UNIQUE,
  applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
