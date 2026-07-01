package logger

import (
	"encoding/json"
	"os"
	"time"
)

// Writer 日志写入器接口
// 负责将日志写入不同目标（控制台、文件等）
type Writer interface {
	// Write 写入日志条目
	Write(entry *LogEntry) error
	// Close 关闭写入器，释放资源
	Close() error
}

// Formatter 日志格式化器接口
// 负责将日志条目格式化为字节数组
type Formatter interface {
	// Format 格式化日志条目
	Format(entry *LogEntry) ([]byte, error)
}

// RotationPolicy 日志分片策略接口
// 定义何时触发日志文件分片
type RotationPolicy interface {
	// ShouldRotate 判断是否应该触发分片
	ShouldRotate(fileInfo os.FileInfo, entry *LogEntry) bool
	// GetRotatedFileName 获取分片后的文件名
	GetRotatedFileName(baseName string, timestamp time.Time) string
}

// LogEntry 日志条目
// 包含日志记录的所有必要信息
type LogEntry struct {
	// Timestamp 日志时间戳
	Timestamp time.Time `json:"timestamp"`
	// Level 日志级别
	Level Level `json:"level"`
	// Component 组件名称
	Component string `json:"component"`
	// Message 日志消息
	Message string `json:"message"`
	// Fields 扩展字段
	Fields map[string]interface{} `json:"fields,omitempty"`
	// RequestID 请求ID，用于链路追踪
	RequestID string `json:"request_id,omitempty"`
	// Method 方法名（方法调用日志）
	Method string `json:"method,omitempty"`
	// Duration 执行耗时（毫秒）
	Duration int64 `json:"duration_ms,omitempty"`
	// CallerFile 调用者文件
	CallerFile string `json:"caller_file,omitempty"`
	// CallerLine 调用者行号
	CallerLine int `json:"caller_line,omitempty"`
	// Error 错误信息
	Error string `json:"error,omitempty"`
}

// Caller 返回格式化的调用位置字符串
func (e *LogEntry) Caller() string {
	if e.CallerFile == "" {
		return ""
	}
	if e.CallerLine > 0 {
		return e.CallerFile + ":" + string(rune(e.CallerLine+'0'))
	}
	return e.CallerFile
}

// ToJSON 将日志条目序列化为JSON字节数组
func (e *LogEntry) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// MarshalJSON 自定义JSON序列化，确保Level以字符串形式输出
func (e *LogEntry) MarshalJSON() ([]byte, error) {
	type Alias LogEntry
	return json.Marshal(&struct {
		Level string `json:"level"`
		*Alias
	}{
		Level: e.Level.String(),
		Alias: (*Alias)(e),
	})
}

// NewLogEntry 创建新的日志条目
func NewLogEntry(level Level, component, message string) *LogEntry {
	return &LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Component: component,
		Message:   message,
	}
}

// WithFields 添加扩展字段
func (e *LogEntry) WithFields(fields map[string]interface{}) *LogEntry {
	e.Fields = fields
	return e
}

// WithRequestID 添加请求ID
func (e *LogEntry) WithRequestID(requestID string) *LogEntry {
	e.RequestID = requestID
	return e
}

// WithMethod 添加方法名
func (e *LogEntry) WithMethod(method string) *LogEntry {
	e.Method = method
	return e
}

// WithDuration 添加执行耗时
func (e *LogEntry) WithDuration(duration int64) *LogEntry {
	e.Duration = duration
	return e
}

// WithCaller 添加调用位置
func (e *LogEntry) WithCaller(file string, line int) *LogEntry {
	e.CallerFile = file
	e.CallerLine = line
	return e
}

// WithError 添加错误信息
func (e *LogEntry) WithError(err string) *LogEntry {
	e.Error = err
	return e
}
