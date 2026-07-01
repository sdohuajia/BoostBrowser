package logger

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// genLevel 生成随机日志级别
func genLevel() gopter.Gen {
	return gen.IntRange(0, 3).Map(func(i int) Level {
		return Level(i)
	})
}

// genLogEntry 生成随机 LogEntry
func genLogEntry() gopter.Gen {
	return gopter.CombineGens(
		genLevel(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.Int64Range(0, 10000),
	).Map(func(values []interface{}) *LogEntry {
		level := values[0].(Level)
		component := values[1].(string)
		message := values[2].(string)
		requestID := values[3].(string)
		method := values[4].(string)
		duration := values[5].(int64)

		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     level,
			Component: component,
			Message:   message,
			RequestID: requestID,
			Method:    method,
			Duration:  duration,
		}
		return entry
	})
}

// TestProperty13_JSONFormatValidity 属性测试：JSON 格式有效性
// **Property 13: JSON Format Validity**
// **Validates: Requirements 6.2**
// *For any* log entry when JSON format is configured, the output SHALL be valid JSON
// that can be parsed without error.
func TestProperty13_JSONFormatValidity(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	formatter := NewJSONFormatter()

	properties.Property("JSON output is always valid JSON", prop.ForAll(
		func(entry *LogEntry) bool {
			// Format the entry
			data, err := formatter.Format(entry)
			if err != nil {
				return false
			}

			// Verify it's valid JSON by attempting to unmarshal
			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				return false
			}

			return true
		},
		genLogEntry(),
	))

	properties.TestingRun(t)
}

// TestProperty12_StructuredLogFieldCompleteness 属性测试：结构化日志字段完整性
// **Property 12: Structured Log Field Completeness**
// **Validates: Requirements 6.1**
// *For any* log entry, the output SHALL contain: timestamp (ISO 8601), level
// (DEBUG/INFO/WARN/ERROR), component name, and message.
func TestProperty12_StructuredLogFieldCompleteness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	textFormatter := NewTextFormatter()
	jsonFormatter := NewJSONFormatter()

	// Test TextFormatter field completeness
	properties.Property("TextFormatter output contains all required fields", prop.ForAll(
		func(entry *LogEntry) bool {
			data, err := textFormatter.Format(entry)
			if err != nil {
				return false
			}

			output := string(data)

			// Check timestamp format (YYYY-MM-DD HH:MM:SS.mmm)
			if !strings.Contains(output, "[") || !strings.Contains(output, "]") {
				return false
			}

			// Check level is present (DEBUG/INFO/WARN/ERROR)
			levelStr := entry.Level.String()
			if !strings.Contains(output, "["+levelStr+"]") {
				return false
			}

			// Check component is present (or "-" if empty)
			if entry.Component != "" {
				if !strings.Contains(output, "["+entry.Component+"]") {
					return false
				}
			} else {
				if !strings.Contains(output, "[-]") {
					return false
				}
			}

			// Check message is present
			if !strings.Contains(output, entry.Message) {
				return false
			}

			return true
		},
		genLogEntry(),
	))

	// Test JSONFormatter field completeness
	properties.Property("JSONFormatter output contains all required fields", prop.ForAll(
		func(entry *LogEntry) bool {
			data, err := jsonFormatter.Format(entry)
			if err != nil {
				return false
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				return false
			}

			// Check timestamp exists and is in ISO 8601 format
			timestamp, ok := result["timestamp"].(string)
			if !ok || timestamp == "" {
				return false
			}
			// Verify timestamp can be parsed as ISO 8601
			_, err = time.Parse("2006-01-02T15:04:05.000Z07:00", timestamp)
			if err != nil {
				return false
			}

			// Check level exists and is valid
			level, ok := result["level"].(string)
			if !ok {
				return false
			}
			validLevels := map[string]bool{"DEBUG": true, "INFO": true, "WARN": true, "ERROR": true}
			if !validLevels[level] {
				return false
			}

			// Check component exists
			if _, ok := result["component"]; !ok {
				return false
			}

			// Check message exists
			if _, ok := result["message"]; !ok {
				return false
			}

			return true
		},
		genLogEntry(),
	))

	properties.TestingRun(t)
}

// TestTextFormatterBasic 基础单元测试：TextFormatter
func TestTextFormatterBasic(t *testing.T) {
	formatter := NewTextFormatter()
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	entry := &LogEntry{
		Timestamp: testTime,
		Level:     INFO,
		Component: "TestComponent",
		Message:   "Test message",
	}

	data, err := formatter.Format(entry)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	output := string(data)

	// Verify basic structure
	if !strings.Contains(output, "[2024-01-15 10:30:00.000]") {
		t.Errorf("Timestamp not found in output: %s", output)
	}
	if !strings.Contains(output, "[INFO]") {
		t.Errorf("Level not found in output: %s", output)
	}
	if !strings.Contains(output, "[TestComponent]") {
		t.Errorf("Component not found in output: %s", output)
	}
	if !strings.Contains(output, "Test message") {
		t.Errorf("Message not found in output: %s", output)
	}
}

// TestJSONFormatterBasic 基础单元测试：JSONFormatter
func TestJSONFormatterBasic(t *testing.T) {
	formatter := NewJSONFormatter()
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	entry := &LogEntry{
		Timestamp: testTime,
		Level:     INFO,
		Component: "TestComponent",
		Message:   "Test message",
		RequestID: "req-123",
		Method:    "TestMethod",
		Duration:  150,
	}

	data, err := formatter.Format(entry)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// Verify fields
	if result["level"] != "INFO" {
		t.Errorf("Level should be 'INFO', got %v", result["level"])
	}
	if result["component"] != "TestComponent" {
		t.Errorf("Component should be 'TestComponent', got %v", result["component"])
	}
	if result["message"] != "Test message" {
		t.Errorf("Message should be 'Test message', got %v", result["message"])
	}
	if result["request_id"] != "req-123" {
		t.Errorf("RequestID should be 'req-123', got %v", result["request_id"])
	}
}

// TestFormatterNilEntry 测试 nil entry 处理
func TestFormatterNilEntry(t *testing.T) {
	textFormatter := NewTextFormatter()
	jsonFormatter := NewJSONFormatter()

	_, err := textFormatter.Format(nil)
	if err == nil {
		t.Error("TextFormatter should return error for nil entry")
	}

	_, err = jsonFormatter.Format(nil)
	if err == nil {
		t.Error("JSONFormatter should return error for nil entry")
	}
}
