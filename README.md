# Audio Transcription Service

一个简化版的录音转写与智能摘要后端服务,支持音频文件上传、异步转写(Mock)和 LLM 智能摘要。

## 技术栈

- **语言**: Go 1.26+
- **Web 框架**: Gin
- **数据库**: MySQL 8.0
- **ORM**: GORM
- **LLM API**: Agnes AI (https://api.agnes-ai.cn)
- **异步处理**: Goroutine + Channel

## 功能特性

### 核心功能 (P0)

- ✅ **音频文件上传** - 支持 wav/mp3/m4a/aac 格式,最大 50MB
- ✅ **异步转写** - Mock 模拟转写(5-15秒,20%失败率)
- ✅ **LLM 智能摘要** - 调用 Agnes AI 生成结构化摘要
- ✅ **任务状态查询** - 实时查看处理进度
- ✅ **录音列表** - 分页查询,按时间倒序
- ✅ **任务重试** - 支持失败任务重新处理
- ✅ **删除录音** - 同步删除文件和数据库记录
- ✅ **统一错误处理** - 合理的 HTTP 状态码
- ✅ **完整日志** - 可追踪任务生命周期

## 快速开始

### 前置要求

- Go 1.26 或更高版本
- MySQL 8.0 或更高版本
- Git

### 1. 克隆仓库

```bash
git clone https://github.com/monsterbiter/Audio-Transcription-Service.git
cd Audio-Transcription-Service
```

### 2. 配置数据库

确保 MySQL 服务已启动,然后执行初始化脚本:

```bash
mysql -u root -p < migrations/init.sql
```

或者手动创建数据库和表:

```sql
CREATE DATABASE audio_transcription CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

然后导入 `migrations/init.sql`。

### 3. 配置环境变量(可选)

默认配置已内置在代码中,如需修改可设置环境变量:

```bash
# 数据库配置
export DB_HOST=localhost
export DB_PORT=3306
export DB_USER=root
export DB_PASSWORD=your_password
export DB_NAME=audio_transcription

# 服务端口
export SERVER_PORT=8080

# LLM API 配置
export LLM_BASE_URL=https://api.agnes-ai.cn/v1
export LLM_API_KEY=your_api_key
export LLM_MODEL=agnes-25-flash

# 文件存储目录
export UPLOAD_DIR=./uploads
```

### 4. 安装依赖

```bash
go mod download
```

### 5. 启动服务

```bash
go run cmd/server/main.go
```

服务将在 `http://localhost:8080` 启动。

## API 文档

### 1. 上传录音

```http
POST /v1/recordings
Content-Type: multipart/form-data
```

**请求参数:**
- `file`: 音频文件 (wav/mp3/m4a/aac, ≤50MB)

**响应示例:**
```json
{
  "recording_id": "550e8400-e29b-41d4-a716-446655440000",
  "task_id": "660e8400-e29b-41d4-a716-446655440001",
  "status": "pending"
}
```

### 2. 查询任务状态

```http
GET /v1/tasks/{task_id}
```

**响应示例:**
```json
{
  "id": "660e8400-e29b-41d4-a716-446655440001",
  "recording_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "summarizing",
  "current_stage": "starting summarization",
  "created_at": "2026-09-11T12:00:00Z",
  "updated_at": "2026-09-11T12:00:15Z"
}
```

**状态说明:**
- `pending` - 等待处理
- `transcribing` - 转写中
- `summarizing` - 摘要生成中
- `done` - 完成
- `failed` - 失败

### 3. 获取录音列表

```http
GET /v1/recordings?page=1&page_size=10
```

**响应示例:**
```json
{
  "recordings": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "filename": "meeting.mp3",
      "file_size": 1048576,
      "format": "mp3",
      "task_status": "done",
      "task_id": "660e8400-e29b-41d4-a716-446655440001",
      "created_at": "2026-09-11T12:00:00Z"
    }
  ],
  "total": 100,
  "page": 1,
  "page_size": 10
}
```

### 4. 获取录音详情

```http
GET /v1/recordings/{recording_id}
```

**响应示例:**
```json
{
  "recording": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "filename": "meeting.mp3",
    "transcript": "这是一段关于产品发布会的录音...",
    "summary": {
      "summary": "产品发布会讨论新版本发布计划",
      "key_points": [
        "下个月推出新版本",
        "需要准备市场宣传材料",
        "编写用户文档"
      ],
      "todos": [
        "准备宣传材料",
        "编写用户文档"
      ]
    }
  },
  "task": {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "status": "done"
  }
}
```

### 5. 重试失败任务

```http
POST /v1/tasks/{task_id}/retry
```

**响应示例:**
```json
{
  "message": "task retry queued",
  "task_id": "660e8400-e29b-41d4-a716-446655440001",
  "status": "pending",
  "retry_count": 1
}
```

### 6. 删除录音

```http
DELETE /v1/recordings/{recording_id}
```

**响应示例:**
```json
{
  "message": "recording deleted successfully"
}
```

### 7. 健康检查

```http
GET /health
```

**响应示例:**
```json
{
  "status": "ok",
  "time": "2026-09-11T12:00:00Z"
}
```

## 架构说明

### 系统架构图

```
Client → Gin Router → Handlers → Services → Database/LLM
                                    ↓
                            Worker Pool (Async)
                                    ↓
                          Task Queue (Channel)
```

### 数据库表结构

#### recordings 表
| 字段 | 类型 | 说明 |
|-----|------|------|
| id | VARCHAR(36) | 主键,UUID |
| filename | VARCHAR(255) | 原始文件名 |
| file_path | VARCHAR(512) | 文件存储路径 |
| file_size | BIGINT | 文件大小(字节) |
| format | VARCHAR(10) | 文件格式 |
| transcript | TEXT | 转写文本 |
| summary | JSON | 摘要结果 |
| created_at | TIMESTAMP | 创建时间 |
| updated_at | TIMESTAMP | 更新时间 |

#### tasks 表
| 字段 | 类型 | 说明 |
|-----|------|------|
| id | VARCHAR(36) | 主键,UUID |
| recording_id | VARCHAR(36) | 关联录音ID |
| status | ENUM | 任务状态 |
| current_stage | VARCHAR(50) | 当前阶段 |
| error_message | TEXT | 错误信息 |
| retry_count | INT | 重试次数 |
| created_at | TIMESTAMP | 创建时间 |
| updated_at | TIMESTAMP | 更新时间 |

### 状态机流转

```
pending → transcribing → summarizing → done
             ↓              ↓
          failed ←──────────┘
```

### 异步处理方案

- 使用 **Goroutine Worker Pool** 处理任务
- **Channel** 作为任务队列
- **3个并发 Worker** 同时处理任务
- 服务重启时,扫描 `pending/transcribing/summarizing` 状态的任务重新入队

## 技术取舍

### 1. 为什么选择 Goroutine + Channel?

- **轻量级**: 相比 Redis 队列,无需额外依赖
- **简单**: Go 原生并发原语,易于实现和调试
- **性能**: 对于中小规模任务,性能足够
- **权衡**: 服务重启时任务会丢失(已通过数据库状态恢复)

### 2. Mock 转写 vs 真实 ASR

- 按需求要求,转写阶段使用 Mock 模拟
- 模拟真实行为: 5-15秒延迟,20%失败率
- 生成随机中文文本作为转写结果
- 易于切换为真实 ASR API

### 3. 服务重启恢复机制

当服务重启时:
1. 启动时扫描数据库中 `pending/transcribing/summarizing` 状态的任务
2. 将这些任务重新加入队列
3. Worker 继续处理

**实现位置**: 可在 `main.go` 启动时添加恢复逻辑。

## 测试

### 使用 curl 测试

#### 1. 上传音频文件
```bash
curl -X POST http://localhost:8080/v1/recordings \
  -F "file=@test-audio.mp3"
```

#### 2. 查询任务状态
```bash
curl http://localhost:8080/v1/tasks/{task_id}
```

#### 3. 获取录音列表
```bash
curl "http://localhost:8080/v1/recordings?page=1&page_size=10"
```

### 使用 VSCode REST Client

打开 `api/requests.http` 文件,点击请求上方的 "Send Request" 按钮。

## 已知问题与未完成项

### 已知问题
- 服务重启时,正在处理的任务会中断,需要手动重试
- 大文件上传可能超时(建议增加 Nginx 超时配置)

### 未完成项(加分项)
- [ ] 失败自动重试(指数退避)
- [ ] 服务重启后自动恢复进行中的任务
- [ ] LLM 流式输出(SSE)
- [ ] 上传幂等(文件哈希去重)
- [ ] 并发控制(限制最大并发数)
- [ ] 单元测试和集成测试
- [ ] Docker 部署

## 开发日志

### Commit 历史

1. `90aad0a` - 项目初始化和依赖安装
2. `9332654` - 数据库设计和模型实现
3. `832d8e9` - 核心服务和异步处理器
4. (下一次) - 完整功能和文档

## License

MIT

## 联系方式

- GitHub: [@monsterbiter](https://github.com/monsterbiter)
- Repository: [Audio-Transcription-Service](https://github.com/monsterbiter/Audio-Transcription-Service)
