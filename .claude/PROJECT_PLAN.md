# 录音转写服务 - 项目实施计划

## 项目概述
实现一个简化版的"录音转写与智能摘要"后端服务,支持音频文件上传、异步转写、LLM智能摘要和结果查询。

## 技术栈
- **语言**: Go 1.21+
- **Web框架**: Gin
- **数据库**: MySQL 8.0
- **ORM**: GORM
- **LLM API**: Agnes AI (https://api.agnes-ai.cn/v1)
- **异步处理**: Goroutine + Channel

## 数据库配置
- Host: localhost
- Port: 3306
- Username: root
- Password: 940588775jia
- Database: audio_transcription

## 项目结构
```
audio-transcription-service/
├── cmd/
│   └── server/
│       └── main.go              # 应用入口
├── internal/
│   ├── config/
│   │   └── config.go            # 配置管理
│   ├── models/
│   │   ├── recording.go         # 录音模型
│   │   └── task.go              # 任务模型
│   ├── handlers/
│   │   ├── recording.go         # 录音接口
│   │   └── task.go              # 任务接口
│   ├── services/
│   │   ├── storage.go           # 文件存储
│   │   ├── transcription.go     # 转写服务(Mock)
│   │   ├── summarization.go     # LLM摘要服务
│   │   └── processor.go         # 异步任务处理器
│   └── database/
│       └── db.go                # 数据库连接
├── migrations/
│   └── init.sql                 # 数据库初始化脚本
├── uploads/                     # 上传文件存储目录
├── api/
│   └── requests.http            # API测试文件
├── go.mod
├── go.sum
├── .gitignore
└── README.md
```

## 数据库表设计

### recordings 表
```sql
CREATE TABLE recordings (
    id VARCHAR(36) PRIMARY KEY,
    filename VARCHAR(255) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    file_size BIGINT NOT NULL,
    format VARCHAR(10) NOT NULL,
    transcript TEXT,
    summary JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### tasks 表
```sql
CREATE TABLE tasks (
    id VARCHAR(36) PRIMARY KEY,
    recording_id VARCHAR(36) NOT NULL,
    status ENUM('pending', 'transcribing', 'summarizing', 'done', 'failed') DEFAULT 'pending',
    current_stage VARCHAR(50),
    error_message TEXT,
    retry_count INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (recording_id) REFERENCES recordings(id) ON DELETE CASCADE,
    INDEX idx_status (status),
    INDEX idx_recording_id (recording_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

## API接口设计

### 1. 上传录音
- **POST** `/v1/recordings`
- Content-Type: multipart/form-data
- 请求参数: file (文件)
- 响应: `{"recording_id": "...", "task_id": "...", "status": "pending"}`

### 2. 查询任务状态
- **GET** `/v1/tasks/{task_id}`
- 响应: 任务详情(id, status, current_stage, error_message等)

### 3. 录音列表
- **GET** `/v1/recordings?page=1&page_size=10`
- 响应: 分页录音列表,按创建时间倒序

### 4. 录音详情
- **GET** `/v1/recordings/{id}`
- 响应: 录音详情(含transcript和summary)

### 5. 任务重试
- **POST** `/v1/tasks/{task_id}/retry`
- 仅failed状态可重试

### 6. 删除录音
- **DELETE** `/v1/recordings/{id}`
- 删除录音记录和关联文件

## 状态机流转
```
pending → transcribing → summarizing → done
              ↓              ↓
           failed ←──────────┘
```

## 异步处理方案
- 使用Goroutine处理每个任务
- 启动固定数量的worker goroutine从任务队列消费
- 使用channel作为任务队列
- 服务重启时,扫描pending/transcribing/summarizing状态的任务重新入队

## LLM摘要Prompt
```
请对以下录音转写文本生成结构化摘要,返回JSON格式:
{
  "summary": "一句话总结录音内容",
  "key_points": ["要点1", "要点2", "要点3"],
  "todos": ["待办事项1", "待办事项2"]
}

转写文本:
{transcript}
```

## 开发阶段与Commit计划

### 阶段1: 项目初始化 ✓
- 初始化Go模块
- 配置依赖
- 创建项目结构
- **Commit**: "chore: initialize project structure"

### 阶段2: 数据库设计
- 创建数据库迁移脚本
- 实现数据库连接
- **Commit**: "feat: add database schema and connection"

### 阶段3: 核心模型层
- 实现Recording和Task模型
- 定义GORM模型
- **Commit**: "feat: add recording and task models"

### 阶段4: 文件上传接口
- 实现文件验证
- 实现文件存储
- 实现POST /v1/recordings接口
- **Commit**: "feat: implement file upload endpoint"

### 阶段5: Mock转写服务
- 实现模拟转写(5-15秒,20%失败率)
- **Commit**: "feat: add mock transcription service"

### 阶段6: LLM摘要服务
- 集成Agnes AI API
- 实现摘要生成
- 处理超时和错误
- **Commit**: "feat: integrate LLM summarization service"

### 阶段7: 异步任务处理器
- 实现任务队列和worker pool
- 实现状态机流转
- **Commit**: "feat: implement async task processor"

### 阶段8: 查询接口
- 实现任务查询接口
- 实现录音列表接口
- 实现录音详情接口
- **Commit**: "feat: add query endpoints"

### 阶段9: 其他功能接口
- 实现任务重试接口
- 实现录音删除接口
- **Commit**: "feat: add retry and delete endpoints"

### 阶段10: 错误处理和日志
- 统一错误响应格式
- 添加日志记录
- **Commit**: "feat: add error handling and logging"

### 阶段11: 文档和测试
- 编写README.md
- 创建API测试文件
- 绘制架构图
- **Commit**: "docs: add README and API documentation"

## 测试验证清单
- [ ] 文件上传验证(大小、格式)
- [ ] 任务状态正确流转
- [ ] Mock转写随机延迟和失败
- [ ] LLM调用成功生成结构化摘要
- [ ] 分页查询正常工作
- [ ] 重试接口仅对failed任务生效
- [ ] 删除操作同时删除文件和数据库记录
- [ ] 错误场景返回正确HTTP状态码
- [ ] 日志能够追踪任务完整生命周期

## 已知约束
- 不实现用户认证
- 不实现前端页面
- 文件存储在本地磁盘(不对接OSS)
- 转写功能使用Mock模拟
- 服务重启时未完成任务会重新入队继续执行
