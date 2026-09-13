# 音频分段转写设计文档

**日期**: 2026-09-13  
**作者**: 用户 + Claude Code  
**状态**: 已批准

## 问题陈述

当前实现将整个音频文件一次性发送给科大讯飞语音听写API进行转写。这导致:

1. **可靠性问题**: 长音频(>5分钟)转写容易超时或失败
2. **用户体验差**: 长时间等待无进度反馈
3. **错误恢复难**: 失败需要重新转写整个文件

用户需要一个更可靠的方案来处理任意长度的音频文件。

## 目标

- ✅ 支持任意长度音频文件的可靠转写
- ✅ 提供实时进度反馈
- ✅ 提高转写成功率
- ✅ 加快处理速度(并行处理)
- ✅ 符合项目加分项(失败自动重试)

## 设计方案

### 核心思路

将长音频分割成多个60秒片段,使用worker pool并行转写,最后合并结果。

### 架构流程

```
上传音频 (任意长度)
    ↓
格式转换 (FFmpeg: → 16kHz, mono, 16bit PCM)
    ↓
音频分割 (FFmpeg: → N个60秒片段)
    ↓
Worker Pool (3个并发worker)
    ├─ Worker1: 转写 chunk_000.wav (失败重试最多3次)
    ├─ Worker2: 转写 chunk_001.wav (失败重试最多3次)
    └─ Worker3: 转写 chunk_002.wav (失败重试最多3次)
    ↓
按序合并转写文本
    ↓
Agnes AI 生成摘要 (使用完整转写文本)
    ↓
完成并清理临时文件
```

### 技术细节

#### 1. 音频分割策略

**选择**: 固定60秒分段  
**理由**:
- 科大讯飞对60秒音频处理稳定
- 简单可靠,易于实现
- 便于并行处理和进度追踪

**实现**: 使用FFmpeg的segment功能
```bash
ffmpeg -i input.wav -f segment -segment_time 60 -c copy output_%03d.wav
```

输出示例:
```
output_000.wav  (0-60秒)
output_001.wav  (60-120秒)
output_002.wav  (120-180秒)
...
```

#### 2. 并行处理策略

**选择**: 限制并发数为3  
**理由**:
- 避免触发API速率限制
- 平衡处理速度与资源消耗
- 复用项目现有的3-worker架构

**实现**: 使用Goroutine + Channel的Worker Pool模式
```go
type chunkJob struct {
    index int
    path  string
}

type chunkResult struct {
    index int
    text  string
    err   error
}

// Worker pool处理
jobChan := make(chan chunkJob, numChunks)
resultChan := make(chan chunkResult, numChunks)

// 启动3个worker
for i := 0; i < 3; i++ {
    go worker(jobChan, resultChan)
}

// 发送任务
for i, chunk := range chunks {
    jobChan <- chunkJob{index: i, path: chunk}
}
close(jobChan)

// 收集结果
results := make([]string, numChunks)
for i := 0; i < numChunks; i++ {
    result := <-resultChan
    if result.err != nil {
        return "", result.err
    }
    results[result.index] = result.text
}

// 按序合并
transcript := strings.Join(results, " ")
```

#### 3. 进度追踪

**选择**: 详细进度显示  
**理由**:
- 用户体验更好,知道还需等待多久
- 便于调试和监控
- 实现成本低

**实现**: 更新`tasks`表的`current_stage`字段
```go
// 处理过程中更新
current_stage = "transcribing (3/10)"

// 用户查询时返回
{
  "status": "transcribing",
  "current_stage": "transcribing (3/10)",
  "progress": 30  // 可选:百分比
}
```

#### 4. 错误处理与重试

**选择**: 单段重试,最多3次,指数退避  
**理由**:
- 最大化成功率
- 网络抖动不会导致整个任务失败
- 符合项目加分项要求

**实现**: 每段转写的重试逻辑
```go
func (s *TranscriptionService) transcribeChunkWithRetry(chunkPath string, maxRetries int) (string, error) {
    var lastErr error
    
    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 {
            // 指数退避: 2s, 4s, 8s
            backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
            log.Printf("[Retry] Attempt %d after %v", attempt, backoff)
            time.Sleep(backoff)
        }
        
        text, err := s.transcribeChunk(chunkPath)
        if err == nil {
            return text, nil
        }
        
        lastErr = err
        log.Printf("[Retry] Chunk %s failed (attempt %d/%d): %v", 
            chunkPath, attempt+1, maxRetries+1, err)
    }
    
    return "", fmt.Errorf("chunk transcription failed after %d retries: %w", 
        maxRetries+1, lastErr)
}
```

### 代码改动

#### 修改文件

**1. `internal/services/transcription.go`**

新增函数:
- `splitAudioIntoChunks(inputPath string) ([]string, error)`  
  使用FFmpeg将音频分割成60秒片段,返回片段路径列表

- `transcribeChunk(chunkPath string) (string, error)`  
  转写单个音频片段(复用现有WebSocket逻辑)

- `transcribeChunkWithRetry(chunkPath string, maxRetries int) (string, error)`  
  带重试的单段转写

修改函数:
- `Transcribe(filePath string) (string, error)`  
  主流程改为:
  1. 转换音频格式
  2. 分割成片段
  3. 使用worker pool并行转写
  4. 合并结果
  5. 清理临时文件

**2. `internal/services/processor.go`**

修改:
- `processTranscription()` 中更新进度的逻辑
- 添加进度回调函数传递给`Transcribe()`

```go
// 添加进度回调
type ProgressCallback func(current, total int)

func (s *TranscriptionService) Transcribe(filePath string, onProgress ProgressCallback) (string, error) {
    // 在worker完成时调用
    onProgress(completedCount, totalChunks)
}

// Processor中使用
transcript, err := p.transcriptionSvc.Transcribe(convertedPath, func(current, total int) {
    stage := fmt.Sprintf("transcribing (%d/%d)", current, total)
    p.updateTaskStage(task.ID, stage)
})
```

#### 不修改

- ✅ `internal/services/summarization.go` - 摘要逻辑不变
- ✅ `internal/models/*.go` - 数据库结构不变
- ✅ `internal/handlers/*.go` - API接口不变
- ✅ 前端文件 - UI不需要改动

### 临时文件管理

**问题**: 分段会产生大量临时文件  
**解决**: 
1. 所有分段保存在临时目录: `/tmp/transcription-{taskID}/`
2. 转写完成后立即清理整个目录
3. 使用`defer`确保即使失败也会清理

```go
tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("transcription-%s", taskID))
os.MkdirAll(tempDir, 0755)
defer os.RemoveAll(tempDir)
```

### 边界情况处理

1. **音频短于60秒**: 不分段,直接转写
2. **最后一段不足60秒**: 正常处理,FFmpeg自动处理
3. **某段完全静音**: 科大讯飞返回空文本,接受
4. **所有段都失败**: 标记任务为failed,记录错误
5. **部分段失败**: 整个任务失败(不接受不完整转写)

## 性能分析

### 时间复杂度

假设音频长度为 T 秒:
- 分段数: N = ⌈T / 60⌉
- 串行转写时间: O(N × t_transcribe)
- 并行转写时间: O(⌈N / 3⌉ × t_transcribe)

**加速比**: ~3倍 (理论值)

### 示例: 10分钟音频

| 阶段 | 耗时 |
|------|------|
| 格式转换 | ~2秒 |
| 音频分割 | ~1秒 |
| 并行转写(10段÷3并发) | ~40秒 |
| 文本合并 | <1秒 |
| 总耗时 | ~44秒 |

对比原方案(整体转写): ~60秒 + 更高失败率

## 风险与缓解

### 风险1: 段边界截断单词

**影响**: 中等  
**可能性**: 低  
**缓解**: 60秒足够长,截断影响可忽略;如需完美,可添加段间重叠(如前后各加2秒)

### 风险2: 临时文件占用磁盘

**影响**: 低  
**可能性**: 中  
**缓解**: 及时清理;磁盘空间检查;定期清理遗留文件

### 风险3: API限流

**影响**: 高  
**可能性**: 低  
**缓解**: 限制并发数为3;添加重试逻辑;监控API响应

## 测试计划

### 单元测试

1. `splitAudioIntoChunks()` - 验证分段数量和时长
2. `transcribeChunkWithRetry()` - 验证重试逻辑
3. Worker pool - 验证并发控制

### 集成测试

1. 短音频(<60秒) - 验证不分段逻辑
2. 标准音频(2-5分钟) - 验证分段+并发
3. 长音频(>10分钟) - 验证大规模分段
4. 网络失败模拟 - 验证重试机制
5. 并发任务 - 验证多任务同时处理

### 手动测试

1. 上传不同格式音频(mp3/wav/m4a/aac)
2. 观察进度更新是否实时
3. 检查临时文件是否清理
4. 验证最终转写文本完整性

## 符合项目加分项

✅ **失败自动重试**: 单段重试最多3次,指数退避  
✅ **并发控制**: 限制同时处理3段,复用worker架构  
✅ **关键路径日志**: 记录每段转写状态、重试、进度  

## 未来优化方向

1. **智能分段**: 基于静音检测切分,避免截断句子
2. **段间重叠**: 前后各加2秒,提高边界准确性
3. **流式返回**: 每完成一段立即返回部分结果
4. **缓存机制**: 相同音频文件不重复转写(基于文件哈希)

## 总结

本设计通过音频分段+并行转写的方式,解决了长音频转写的可靠性问题:

- **可靠性**: 1分钟片段失败率极低,单段重试进一步提升成功率
- **性能**: 3倍并行加速
- **可扩展性**: 支持任意长度音频
- **用户体验**: 实时进度反馈
- **代码质量**: 符合项目加分项要求,日志完善

**实现复杂度**: 中等  
**风险**: 低  
**收益**: 高

---

**批准状态**: ✅ 用户已批准 (2026-09-13)
