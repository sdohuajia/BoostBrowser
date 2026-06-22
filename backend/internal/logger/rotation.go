package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// TimeInterval 时间分片间隔类型
type TimeInterval string

const (
	// Daily 每天分片
	Daily TimeInterval = "daily"
	// Hourly 每小时分片
	Hourly TimeInterval = "hourly"
)

// TimeRotationPolicy 按时间分片策略
// 支持按天或按小时分片
type TimeRotationPolicy struct {
	interval   TimeInterval
	lastRotate time.Time
	mu         sync.RWMutex
}

// NewTimeRotationPolicy 创建时间分片策略
func NewTimeRotationPolicy(interval TimeInterval) *TimeRotationPolicy {
	return &TimeRotationPolicy{
		interval:   interval,
		lastRotate: time.Time{}, // 零值，首次检查时会初始化
	}
}

// ShouldRotate 判断是否应该触发时间分片
func (p *TimeRotationPolicy) ShouldRotate(fileInfo os.FileInfo, entry *LogEntry) bool {
	if fileInfo == nil || entry == nil {
		return false
	}

	p.mu.RLock()
	lastRotate := p.lastRotate
	p.mu.RUnlock()

	entryTime := entry.Timestamp
	if entryTime.IsZero() {
		entryTime = time.Now()
	}

	// 首次检查，使用文件修改时间作为基准
	if lastRotate.IsZero() {
		p.mu.Lock()
		p.lastRotate = fileInfo.ModTime()
		p.mu.Unlock()
		lastRotate = fileInfo.ModTime()
	}

	switch p.interval {
	case Daily:
		// 检查是否跨天
		return !sameDay(lastRotate, entryTime)
	case Hourly:
		// 检查是否跨小时
		return !sameHour(lastRotate, entryTime)
	default:
		// 默认按天
		return !sameDay(lastRotate, entryTime)
	}
}

// GetRotatedFileName 获取分片后的文件名
func (p *TimeRotationPolicy) GetRotatedFileName(baseName string, timestamp time.Time) string {
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	if ext == "" {
		ext = ".log"
	}

	switch p.interval {
	case Hourly:
		// 格式: app.2024-01-15-14.log
		return fmt.Sprintf("%s.%s%s", nameWithoutExt, timestamp.Format("2006-01-02-15"), ext)
	default:
		// 格式: app.2024-01-15.log
		return fmt.Sprintf("%s.%s%s", nameWithoutExt, timestamp.Format("2006-01-02"), ext)
	}
}

// UpdateLastRotate 更新最后分片时间
func (p *TimeRotationPolicy) UpdateLastRotate(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRotate = t
}

// sameDay 判断两个时间是否在同一天
func sameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// sameHour 判断两个时间是否在同一小时
func sameHour(t1, t2 time.Time) bool {
	return sameDay(t1, t2) && t1.Hour() == t2.Hour()
}

// SizeRotationPolicy 按大小分片策略
// 当文件大小超过指定阈值时触发分片
type SizeRotationPolicy struct {
	maxSize  int64 // 最大文件大小（字节）
	sequence int   // 当前序号（同一天内多次分片）
	mu       sync.RWMutex
}

// NewSizeRotationPolicy 创建大小分片策略
// maxSizeBytes: 最大文件大小（字节）
func NewSizeRotationPolicy(maxSizeBytes int64) *SizeRotationPolicy {
	return &SizeRotationPolicy{
		maxSize:  maxSizeBytes,
		sequence: 0,
	}
}

// NewSizeRotationPolicyMB 创建大小分片策略（MB为单位）
// maxSizeMB: 最大文件大小（MB）
func NewSizeRotationPolicyMB(maxSizeMB int) *SizeRotationPolicy {
	return NewSizeRotationPolicy(int64(maxSizeMB) * 1024 * 1024)
}

// ShouldRotate 判断是否应该触发大小分片
func (p *SizeRotationPolicy) ShouldRotate(fileInfo os.FileInfo, entry *LogEntry) bool {
	if fileInfo == nil {
		return false
	}
	return fileInfo.Size() >= p.maxSize
}

// GetRotatedFileName 获取分片后的文件名
func (p *SizeRotationPolicy) GetRotatedFileName(baseName string, timestamp time.Time) string {
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	if ext == "" {
		ext = ".log"
	}

	p.mu.Lock()
	p.sequence++
	seq := p.sequence
	p.mu.Unlock()

	// 格式: app.2024-01-15.1.log
	return fmt.Sprintf("%s.%s.%d%s", nameWithoutExt, timestamp.Format("2006-01-02"), seq, ext)
}

// ResetSequence 重置序号（通常在日期变化时调用）
func (p *SizeRotationPolicy) ResetSequence() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sequence = 0
}

// GetMaxSize 获取最大文件大小
func (p *SizeRotationPolicy) GetMaxSize() int64 {
	return p.maxSize
}

// CompositeRotationPolicy 组合分片策略
// 任一子策略满足条件即触发分片
type CompositeRotationPolicy struct {
	policies []RotationPolicy
	mu       sync.RWMutex
}

// NewCompositeRotationPolicy 创建组合分片策略
func NewCompositeRotationPolicy(policies ...RotationPolicy) *CompositeRotationPolicy {
	return &CompositeRotationPolicy{
		policies: policies,
	}
}

// ShouldRotate 判断是否应该触发分片
// 任一子策略返回 true 即触发
func (p *CompositeRotationPolicy) ShouldRotate(fileInfo os.FileInfo, entry *LogEntry) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, policy := range p.policies {
		if policy.ShouldRotate(fileInfo, entry) {
			return true
		}
	}
	return false
}

// GetRotatedFileName 获取分片后的文件名
// 使用第一个策略的命名规则
func (p *CompositeRotationPolicy) GetRotatedFileName(baseName string, timestamp time.Time) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.policies) > 0 {
		return p.policies[0].GetRotatedFileName(baseName, timestamp)
	}

	// 默认命名
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	if ext == "" {
		ext = ".log"
	}
	return fmt.Sprintf("%s.%s%s", nameWithoutExt, timestamp.Format("2006-01-02"), ext)
}

// AddPolicy 添加子策略
func (p *CompositeRotationPolicy) AddPolicy(policy RotationPolicy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.policies = append(p.policies, policy)
}

// GetPolicies 获取所有子策略
func (p *CompositeRotationPolicy) GetPolicies() []RotationPolicy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]RotationPolicy, len(p.policies))
	copy(result, p.policies)
	return result
}

// RotationManagerConfig 分片管理器配置
type RotationManagerConfig struct {
	BasePath   string         // 基础日志文件路径
	MaxBackups int            // 最大保留文件数
	MaxAge     int            // 最大保留天数
	Policy     RotationPolicy // 分片策略
}

// RotationManager 日志分片管理器
// 负责执行分片操作和清理历史文件
type RotationManager struct {
	config     RotationManagerConfig
	mu         sync.Mutex
	currentSeq int // 当前序号
}

// NewRotationManager 创建分片管理器
func NewRotationManager(config RotationManagerConfig) *RotationManager {
	if config.MaxBackups <= 0 {
		config.MaxBackups = 5
	}
	return &RotationManager{
		config:     config,
		currentSeq: 0,
	}
}

// ShouldRotate 检查是否需要分片
func (m *RotationManager) ShouldRotate(fileInfo os.FileInfo, entry *LogEntry) bool {
	if m.config.Policy == nil {
		return false
	}
	return m.config.Policy.ShouldRotate(fileInfo, entry)
}

// Rotate 执行分片操作
// 返回新的日志文件路径
func (m *RotationManager) Rotate(currentFile *os.File) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if currentFile == nil {
		return "", fmt.Errorf("current file is nil")
	}

	// 获取当前文件信息
	basePath := m.config.BasePath
	timestamp := time.Now()

	// 生成分片文件名
	rotatedName := m.generateRotatedFileName(basePath, timestamp)

	// 关闭当前文件
	if err := currentFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close current file: %w", err)
	}

	// 重命名当前文件为分片文件
	if err := os.Rename(basePath, rotatedName); err != nil {
		return "", fmt.Errorf("failed to rename file: %w", err)
	}

	// 清理历史文件
	if err := m.cleanupOldFiles(); err != nil {
		// 清理失败不影响主流程，只记录错误
		fmt.Fprintf(os.Stderr, "failed to cleanup old files: %v\n", err)
	}

	return rotatedName, nil
}

// generateRotatedFileName 生成分片文件名
// 格式: {basename}.{timestamp}[.{sequence}].log
func (m *RotationManager) generateRotatedFileName(basePath string, timestamp time.Time) string {
	ext := filepath.Ext(basePath)
	nameWithoutExt := strings.TrimSuffix(basePath, ext)
	if ext == "" {
		ext = ".log"
	}

	dateStr := timestamp.Format("2006-01-02")

	// 检查是否已存在同日期的文件，确定序号
	seq := m.findNextSequence(nameWithoutExt, dateStr, ext)

	if seq > 0 {
		// 格式: app.2024-01-15.1.log
		return fmt.Sprintf("%s.%s.%d%s", nameWithoutExt, dateStr, seq, ext)
	}
	// 格式: app.2024-01-15.log
	return fmt.Sprintf("%s.%s%s", nameWithoutExt, dateStr, ext)
}

// findNextSequence 查找下一个可用序号
func (m *RotationManager) findNextSequence(nameWithoutExt, dateStr, ext string) int {
	dir := filepath.Dir(nameWithoutExt)
	if dir == "" {
		dir = "."
	}
	baseName := filepath.Base(nameWithoutExt)

	// 查找已存在的同日期文件
	pattern := fmt.Sprintf("%s.%s*%s", baseName, dateStr, ext)
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) == 0 {
		return 0
	}

	// 找到最大序号
	maxSeq := 0
	seqPattern := regexp.MustCompile(fmt.Sprintf(`%s\.%s(?:\.(\d+))?%s$`,
		regexp.QuoteMeta(baseName),
		regexp.QuoteMeta(dateStr),
		regexp.QuoteMeta(ext)))

	for _, match := range matches {
		fileName := filepath.Base(match)
		if submatches := seqPattern.FindStringSubmatch(fileName); submatches != nil {
			if len(submatches) > 1 && submatches[1] != "" {
				var seq int
				fmt.Sscanf(submatches[1], "%d", &seq)
				if seq > maxSeq {
					maxSeq = seq
				}
			} else {
				// 无序号的文件存在，下一个从1开始
				if maxSeq == 0 {
					maxSeq = 0
				}
			}
		}
	}

	return maxSeq + 1
}

// cleanupOldFiles 清理历史文件
func (m *RotationManager) cleanupOldFiles() error {
	files, err := m.listRotatedFiles()
	if err != nil {
		return err
	}

	// 按修改时间排序（最新的在前）
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})

	// 删除超出数量限制的文件
	if len(files) > m.config.MaxBackups {
		for _, f := range files[m.config.MaxBackups:] {
			if err := os.Remove(f.Path); err != nil {
				return fmt.Errorf("failed to remove old file %s: %w", f.Path, err)
			}
		}
	}

	// 删除超出时间限制的文件
	if m.config.MaxAge > 0 {
		cutoff := time.Now().AddDate(0, 0, -m.config.MaxAge)
		for _, f := range files {
			if f.ModTime.Before(cutoff) {
				if err := os.Remove(f.Path); err != nil {
					return fmt.Errorf("failed to remove old file %s: %w", f.Path, err)
				}
			}
		}
	}

	return nil
}

// rotatedFileInfo 分片文件信息
type rotatedFileInfo struct {
	Path    string
	ModTime time.Time
}

// listRotatedFiles 列出所有分片文件
func (m *RotationManager) listRotatedFiles() ([]rotatedFileInfo, error) {
	basePath := m.config.BasePath
	dir := filepath.Dir(basePath)
	if dir == "" {
		dir = "."
	}

	ext := filepath.Ext(basePath)
	nameWithoutExt := filepath.Base(strings.TrimSuffix(basePath, ext))
	if ext == "" {
		ext = ".log"
	}

	// 匹配模式: app.YYYY-MM-DD*.log
	pattern := fmt.Sprintf("%s.[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]*%s", nameWithoutExt, ext)
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob files: %w", err)
	}

	var files []rotatedFileInfo
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		files = append(files, rotatedFileInfo{
			Path:    match,
			ModTime: info.ModTime(),
		})
	}

	return files, nil
}

// GetRotatedFileCount 获取当前分片文件数量
func (m *RotationManager) GetRotatedFileCount() (int, error) {
	files, err := m.listRotatedFiles()
	if err != nil {
		return 0, err
	}
	return len(files), nil
}

// GetConfig 获取配置
func (m *RotationManager) GetConfig() RotationManagerConfig {
	return m.config
}

// ValidateRotatedFileName 验证文件名是否符合分片命名格式
// 格式: {basename}.{timestamp}[.{sequence}].log
func ValidateRotatedFileName(fileName string) bool {
	// 匹配模式: name.YYYY-MM-DD.log 或 name.YYYY-MM-DD.N.log 或 name.YYYY-MM-DD-HH.log
	patterns := []string{
		`^.+\.\d{4}-\d{2}-\d{2}\.log$`,            // app.2024-01-15.log
		`^.+\.\d{4}-\d{2}-\d{2}\.\d+\.log$`,       // app.2024-01-15.1.log
		`^.+\.\d{4}-\d{2}-\d{2}-\d{2}\.log$`,      // app.2024-01-15-14.log (hourly)
		`^.+\.\d{4}-\d{2}-\d{2}-\d{2}\.\d+\.log$`, // app.2024-01-15-14.1.log
	}

	for _, p := range patterns {
		matched, _ := regexp.MatchString(p, fileName)
		if matched {
			return true
		}
	}
	return false
}

// ParseRotatedFileName 解析分片文件名
// 返回基础名、时间戳、序号
func ParseRotatedFileName(fileName string) (baseName string, timestamp time.Time, sequence int, err error) {
	ext := filepath.Ext(fileName)
	nameWithoutExt := strings.TrimSuffix(fileName, ext)

	// 尝试匹配带序号的格式: app.2024-01-15.1
	seqPattern := regexp.MustCompile(`^(.+)\.(\d{4}-\d{2}-\d{2}(?:-\d{2})?)\.(\d+)$`)
	if matches := seqPattern.FindStringSubmatch(nameWithoutExt); matches != nil {
		baseName = matches[1]
		timestamp, err = parseTimestamp(matches[2])
		if err != nil {
			return "", time.Time{}, 0, err
		}
		fmt.Sscanf(matches[3], "%d", &sequence)
		return baseName, timestamp, sequence, nil
	}

	// 尝试匹配不带序号的格式: app.2024-01-15
	noSeqPattern := regexp.MustCompile(`^(.+)\.(\d{4}-\d{2}-\d{2}(?:-\d{2})?)$`)
	if matches := noSeqPattern.FindStringSubmatch(nameWithoutExt); matches != nil {
		baseName = matches[1]
		timestamp, err = parseTimestamp(matches[2])
		if err != nil {
			return "", time.Time{}, 0, err
		}
		return baseName, timestamp, 0, nil
	}

	return "", time.Time{}, 0, fmt.Errorf("invalid rotated file name format: %s", fileName)
}

// parseTimestamp 解析时间戳字符串
func parseTimestamp(s string) (time.Time, error) {
	// 尝试小时格式
	if t, err := time.Parse("2006-01-02-15", s); err == nil {
		return t, nil
	}
	// 尝试日期格式
	return time.Parse("2006-01-02", s)
}
