package services

import (
	"fmt"
	"math/rand"
	"time"
)

// TranscriptionService handles audio transcription (Mock)
type TranscriptionService struct {
}

// NewTranscriptionService creates a new transcription service
func NewTranscriptionService() *TranscriptionService {
	return &TranscriptionService{}
}

// Transcribe performs mock transcription
// Simulates 5-15 seconds processing time with 20% failure rate
func (s *TranscriptionService) Transcribe(filePath string) (string, error) {
	// Random delay: 5-15 seconds
	delaySeconds := rand.Intn(11) + 5 // 5 to 15
	time.Sleep(time.Duration(delaySeconds) * time.Second)

	// 20% failure rate
	if rand.Intn(100) < 20 {
		return "", fmt.Errorf("mock transcription failed (simulated failure)")
	}

	// Generate mock transcript
	transcripts := []string{
		"这是一段关于产品发布会的录音。我们计划在下个月推出新版本,需要准备市场宣传材料和用户文档。",
		"今天的会议讨论了项目进度,目前开发工作已经完成80%,测试团队正在进行集成测试。预计下周可以进入UAT阶段。",
		"客户反馈了几个重要的问题,包括系统响应速度慢和界面不够友好。我们需要在下一个迭代中优先解决这些问题。",
		"财务部门报告本季度收入同比增长25%,利润率提升了3个百分点。但是运营成本也在上升,需要控制费用。",
		"技术架构评审会上,大家讨论了微服务迁移方案。需要评估风险和收益,制定详细的迁移计划和回滚策略。",
	}

	// Random select a transcript
	transcript := transcripts[rand.Intn(len(transcripts))]

	return transcript, nil
}
