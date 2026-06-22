package logger

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TextFormatter 文本格式化器
// 将日志条目格式化为结构化文本格式
type TextFormatter struct {
	// TimestampFormat 时间戳格式，默认为 "2006-01-02 15:04:05.000"
	TimestampFormat string
}

// NewTextFormatter 创建新的文本格式化器
func NewTextFormatter() *TextFormatter {
	return &TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05.000",
	}
}

// Format 格式化日志条目为文本格式
// 输出格式: [timestamp] [level] [component] message | field1=value1 field2=value2
func (f *TextFormatter) Format(entry *LogEntry) ([]byte, error) {
	if entry == nil {
		return nil, fmt.Errorf("log entry is nil")
	}

	var sb strings.Builder

	// 时间戳
	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = "2006-01-02 15:04:05.000"
	}
	sb.WriteString("[")
	sb.WriteString(entry.Timestamp.Format(timestampFormat))
	sb.WriteString("] ")

	// 级别
	sb.WriteString("[")
	sb.WriteString(entry.Level.String())
	sb.WriteString("] ")

	// 组件
	sb.WriteString("[")
	if entry.Component != "" {
		sb.WriteString(entry.Component)
	} else {
		sb.WriteString("-")
	}
	sb.WriteString("] ")

	// 消息
	sb.WriteString(entry.Message)

	// 收集所有额外字段
	extraFields := make([]string, 0)

	// 请求ID
	if entry.RequestID != "" {
		extraFields = append(extraFields, fmt.Sprintf("request_id=%s", entry.RequestID))
	}

	// 方法名
	if entry.Method != "" {
		extraFields = append(extraFields, fmt.Sprintf("method=%s", entry.Method))
	}

	// 执行耗时
	if entry.Duration > 0 {
		extraFields = append(extraFields, fmt.Sprintf("duration_ms=%d", entry.Duration))
	}

	// 调用位置
	if entry.CallerFile != "" {
		caller := entry.CallerFile
		if entry.CallerLine > 0 {
			caller = fmt.Sprintf("%s:%d", entry.CallerFile, entry.CallerLine)
		}
		extraFields = append(extraFields, fmt.Sprintf("caller=%s", caller))
	}

	// 错误信息
	if entry.Error != "" {
		extraFields = append(extraFields, fmt.Sprintf("error=%s", entry.Error))
	}

	// 扩展字段（按key排序以保证输出稳定）
	if len(entry.Fields) > 0 {
		keys := make([]string, 0, len(entry.Fields))
		for k := range entry.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			extraFields = append(extraFields, fmt.Sprintf("%s=%v", k, entry.Fields[k]))
		}
	}

	// 如果有额外字段，添加分隔符和字段
	if len(extraFields) > 0 {
		sb.WriteString(" | ")
		sb.WriteString(strings.Join(extraFields, " "))
	}

	// 添加换行符
	sb.WriteString("\n")

	return []byte(sb.String()), nil
}

// JSONFormatter JSON格式化器
// 将日志条目格式化为JSON格式
type JSONFormatter struct {
	// PrettyPrint 是否美化输出（带缩进）
	PrettyPrint bool
}

// NewJSONFormatter 创建新的JSON格式化器
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{
		PrettyPrint: false,
	}
}

// jsonLogEntry 用于JSON序列化的内部结构
// 确保字段顺序和格式符合要求
type jsonLogEntry struct {
	Timestamp  string                 `json:"timestamp"`
	Level      string                 `json:"level"`
	Component  string                 `json:"component"`
	Message    string                 `json:"message"`
	RequestID  string                 `json:"request_id,omitempty"`
	Method     string                 `json:"method,omitempty"`
	DurationMs int64                  `json:"duration_ms,omitempty"`
	Caller     string                 `json:"caller,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Fields     map[string]interface{} `json:"fields,omitempty"`
}

// Format 格式化日志条目为JSON格式
func (f *JSONFormatter) Format(entry *LogEntry) ([]byte, error) {
	if entry == nil {
		return nil, fmt.Errorf("log entry is nil")
	}

	// 构建调用位置字符串
	var caller string
	if entry.CallerFile != "" {
		if entry.CallerLine > 0 {
			caller = fmt.Sprintf("%s:%d", entry.CallerFile, entry.CallerLine)
		} else {
			caller = entry.CallerFile
		}
	}

	// 创建JSON结构
	jsonEntry := jsonLogEntry{
		Timestamp:  entry.Timestamp.Format("2006-01-02T15:04:05.000Z07:00"),
		Level:      entry.Level.String(),
		Component:  entry.Component,
		Message:    entry.Message,
		RequestID:  entry.RequestID,
		Method:     entry.Method,
		DurationMs: entry.Duration,
		Caller:     caller,
		Error:      entry.Error,
		Fields:     entry.Fields,
	}

	var data []byte
	var err error

	if f.PrettyPrint {
		data, err = json.MarshalIndent(jsonEntry, "", "  ")
	} else {
		data, err = json.Marshal(jsonEntry)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to marshal log entry to JSON: %w", err)
	}

	// 添加换行符
	data = append(data, '\n')

	return data, nil
}
