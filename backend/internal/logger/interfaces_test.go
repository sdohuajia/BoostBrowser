package logger

import (
	"encoding/json"
	"testing"
	"time"
)

// TestLogEntryJSONSerialization 测试 LogEntry JSON 序列化
func TestLogEntryJSONSerialization(t *testing.T) {
	// 创建测试时间
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// 创建完整的 LogEntry
	entry := &LogEntry{
		Timestamp:  testTime,
		Level:      INFO,
		Component:  "TestComponent",
		Message:    "Test message",
		Fields:     map[string]interface{}{"key1": "value1", "key2": 123},
		RequestID:  "req-12345",
		Method:     "TestMethod",
		Duration:   150,
		CallerFile: "test.go",
		CallerLine: 42,
		Error:      "",
	}

	// 序列化
	data, err := entry.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// 验证是有效的 JSON
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// 验证必需字段存在
	requiredFields := []string{"timestamp", "level", "component", "message"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("Required field %q missing from JSON output", field)
		}
	}

	// 验证 Level 以字符串形式输出
	if level, ok := result["level"].(string); !ok || level != "INFO" {
		t.Errorf("Level should be string 'INFO', got %v", result["level"])
	}

	// 验证 Component
	if component, ok := result["component"].(string); !ok || component != "TestComponent" {
		t.Errorf("Component should be 'TestComponent', got %v", result["component"])
	}

	// 验证 Message
	if message, ok := result["message"].(string); !ok || message != "Test message" {
		t.Errorf("Message should be 'Test message', got %v", result["message"])
	}

	// 验证 RequestID
	if requestID, ok := result["request_id"].(string); !ok || requestID != "req-12345" {
		t.Errorf("RequestID should be 'req-12345', got %v", result["request_id"])
	}

	// 验证 Method
	if method, ok := result["method"].(string); !ok || method != "TestMethod" {
		t.Errorf("Method should be 'TestMethod', got %v", result["method"])
	}

	// 验证 Duration
	if duration, ok := result["duration_ms"].(float64); !ok || duration != 150 {
		t.Errorf("Duration should be 150, got %v", result["duration_ms"])
	}
}

// TestLogEntryJSONSerializationAllLevels 测试所有日志级别的序列化
func TestLogEntryJSONSerializationAllLevels(t *testing.T) {
	levels := []struct {
		level    Level
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
	}

	for _, tc := range levels {
		t.Run(tc.expected, func(t *testing.T) {
			entry := NewLogEntry(tc.level, "TestComponent", "Test message")
			data, err := entry.ToJSON()
			if err != nil {
				t.Fatalf("ToJSON failed: %v", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("JSON unmarshal failed: %v", err)
			}

			if level, ok := result["level"].(string); !ok || level != tc.expected {
				t.Errorf("Level should be %q, got %v", tc.expected, result["level"])
			}
		})
	}
}

// TestLogEntryOmitEmptyFields 测试空字段不输出
func TestLogEntryOmitEmptyFields(t *testing.T) {
	// 创建只有必需字段的 LogEntry
	entry := NewLogEntry(INFO, "TestComponent", "Test message")

	data, err := entry.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// 验证可选字段不存在（omitempty）
	optionalFields := []string{"fields", "request_id", "method", "error"}
	for _, field := range optionalFields {
		if val, ok := result[field]; ok && val != "" {
			t.Errorf("Optional field %q should be omitted when empty, got %v", field, val)
		}
	}
}

// TestLogEntryWithMethods 测试链式方法
func TestLogEntryWithMethods(t *testing.T) {
	entry := NewLogEntry(INFO, "TestComponent", "Test message").
		WithRequestID("req-123").
		WithMethod("TestMethod").
		WithDuration(100).
		WithCaller("test.go", 10).
		WithFields(map[string]interface{}{"key": "value"})

	if entry.RequestID != "req-123" {
		t.Errorf("RequestID should be 'req-123', got %q", entry.RequestID)
	}
	if entry.Method != "TestMethod" {
		t.Errorf("Method should be 'TestMethod', got %q", entry.Method)
	}
	if entry.Duration != 100 {
		t.Errorf("Duration should be 100, got %d", entry.Duration)
	}
	if entry.CallerFile != "test.go" {
		t.Errorf("CallerFile should be 'test.go', got %q", entry.CallerFile)
	}
	if entry.CallerLine != 10 {
		t.Errorf("CallerLine should be 10, got %d", entry.CallerLine)
	}
	if entry.Fields["key"] != "value" {
		t.Errorf("Fields['key'] should be 'value', got %v", entry.Fields["key"])
	}
}
