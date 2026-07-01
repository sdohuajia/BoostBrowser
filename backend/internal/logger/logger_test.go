package logger

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// MockWriter 用于测试的模拟写入器
type MockWriter struct {
	entries []*LogEntry
	mu      sync.Mutex
}

func NewMockWriter() *MockWriter {
	return &MockWriter{
		entries: make([]*LogEntry, 0),
	}
}

func (w *MockWriter) Write(entry *LogEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, entry)
	return nil
}

func (w *MockWriter) Close() error {
	return nil
}

func (w *MockWriter) GetEntries() []*LogEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]*LogEntry, len(w.entries))
	copy(result, w.entries)
	return result
}

func (w *MockWriter) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = make([]*LogEntry, 0)
}

// createTestLogger 创建用于测试的 Logger
func createTestLogger(level Level, writer Writer) *Logger {
	return &Logger{
		level:         level,
		component:     "test",
		writers:       []Writer{writer},
		consoleWriter: writer,
	}
}

// TestProperty5_LogLevelFiltering 属性测试：日志级别过滤
// Property 5: Log Level Filtering
// *For any* configured log level L, all log entries with level below L SHALL NOT be written
// to any output, and all entries with level >= L SHALL be written.
// **Validates: Requirements 3.2**
func TestProperty5_LogLevelFiltering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// 生成日志级别 (0-3: DEBUG, INFO, WARN, ERROR)
	levelGen := gen.IntRange(0, 3).Map(func(i int) Level {
		return Level(i)
	})

	// Property: 低于配置级别的日志不应被写入
	properties.Property("logs below configured level are not written", prop.ForAll(
		func(configuredLevel Level, entryLevel Level) bool {
			mockWriter := NewMockWriter()
			logger := createTestLogger(configuredLevel, mockWriter)

			// 根据 entryLevel 调用相应的日志方法
			switch entryLevel {
			case DEBUG:
				logger.Debug("test message")
			case INFO:
				logger.Info("test message")
			case WARN:
				logger.Warn("test message")
			case ERROR:
				logger.Error("test message")
			}

			entries := mockWriter.GetEntries()

			// 如果 entryLevel < configuredLevel，不应该有日志写入
			if entryLevel < configuredLevel {
				return len(entries) == 0
			}
			// 如果 entryLevel >= configuredLevel，应该有日志写入
			return len(entries) == 1 && entries[0].Level == entryLevel
		},
		levelGen,
		levelGen,
	))

	// Property: 等于或高于配置级别的日志应被写入
	properties.Property("logs at or above configured level are written", prop.ForAll(
		func(configuredLevel Level) bool {
			mockWriter := NewMockWriter()
			logger := createTestLogger(configuredLevel, mockWriter)

			// 写入所有级别的日志
			logger.Debug("debug message")
			logger.Info("info message")
			logger.Warn("warn message")
			logger.Error("error message")

			entries := mockWriter.GetEntries()

			// 计算应该写入的日志数量
			expectedCount := 0
			for level := DEBUG; level <= ERROR; level++ {
				if level >= configuredLevel {
					expectedCount++
				}
			}

			if len(entries) != expectedCount {
				return false
			}

			// 验证所有写入的日志级别都 >= configuredLevel
			for _, entry := range entries {
				if entry.Level < configuredLevel {
					return false
				}
			}

			return true
		},
		levelGen,
	))

	// Property: 动态修改级别后过滤行为正确
	properties.Property("dynamic level change affects filtering correctly", prop.ForAll(
		func(initialLevel Level, newLevel Level) bool {
			mockWriter := NewMockWriter()
			logger := createTestLogger(initialLevel, mockWriter)

			// 使用初始级别写入日志
			logger.Info("initial info")
			initialEntries := mockWriter.GetEntries()

			// 验证初始级别过滤
			initialExpected := INFO >= initialLevel
			if initialExpected && len(initialEntries) != 1 {
				return false
			}
			if !initialExpected && len(initialEntries) != 0 {
				return false
			}

			// 动态修改级别
			mockWriter.Clear()
			logger.SetLevel(newLevel)

			// 使用新级别写入日志
			logger.Info("new info")
			newEntries := mockWriter.GetEntries()

			// 验证新级别过滤
			newExpected := INFO >= newLevel
			if newExpected && len(newEntries) != 1 {
				return false
			}
			if !newExpected && len(newEntries) != 0 {
				return false
			}

			return true
		},
		levelGen,
		levelGen,
	))

	properties.TestingRun(t)
}

// TestLoggerBasic 基础功能测试
func TestLoggerBasic(t *testing.T) {
	mockWriter := NewMockWriter()
	logger := createTestLogger(DEBUG, mockWriter)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	entries := mockWriter.GetEntries()
	if len(entries) != 4 {
		t.Errorf("expected 4 entries, got %d", len(entries))
	}
}

// TestLoggerLevelFiltering 级别过滤测试
func TestLoggerLevelFiltering(t *testing.T) {
	tests := []struct {
		name          string
		configLevel   Level
		expectedCount int
	}{
		{"DEBUG level logs all", DEBUG, 4},
		{"INFO level filters DEBUG", INFO, 3},
		{"WARN level filters DEBUG and INFO", WARN, 2},
		{"ERROR level filters all except ERROR", ERROR, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockWriter := NewMockWriter()
			logger := createTestLogger(tt.configLevel, mockWriter)

			logger.Debug("debug")
			logger.Info("info")
			logger.Warn("warn")
			logger.Error("error")

			entries := mockWriter.GetEntries()
			if len(entries) != tt.expectedCount {
				t.Errorf("expected %d entries, got %d", tt.expectedCount, len(entries))
			}
		})
	}
}

// TestLoggerSetLevel 动态级别修改测试
func TestLoggerSetLevel(t *testing.T) {
	mockWriter := NewMockWriter()
	logger := createTestLogger(DEBUG, mockWriter)

	// 初始级别为 DEBUG，所有日志都应该写入
	logger.Debug("debug1")
	if len(mockWriter.GetEntries()) != 1 {
		t.Error("DEBUG log should be written at DEBUG level")
	}

	// 修改级别为 ERROR
	mockWriter.Clear()
	logger.SetLevel(ERROR)

	logger.Debug("debug2")
	logger.Info("info2")
	logger.Warn("warn2")
	logger.Error("error2")

	entries := mockWriter.GetEntries()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry at ERROR level, got %d", len(entries))
	}
	if entries[0].Level != ERROR {
		t.Errorf("expected ERROR level, got %s", entries[0].Level.String())
	}
}

// TestLoggerGetLevel 获取级别测试
func TestLoggerGetLevel(t *testing.T) {
	mockWriter := NewMockWriter()
	logger := createTestLogger(WARN, mockWriter)

	if logger.GetLevel() != WARN {
		t.Errorf("expected WARN level, got %s", logger.GetLevel().String())
	}

	logger.SetLevel(DEBUG)
	if logger.GetLevel() != DEBUG {
		t.Errorf("expected DEBUG level after SetLevel, got %s", logger.GetLevel().String())
	}
}

// TestLoggerShouldLog 检查是否应该记录测试
func TestLoggerShouldLog(t *testing.T) {
	mockWriter := NewMockWriter()
	logger := createTestLogger(INFO, mockWriter)

	if logger.ShouldLog(DEBUG) {
		t.Error("DEBUG should not be logged at INFO level")
	}
	if !logger.ShouldLog(INFO) {
		t.Error("INFO should be logged at INFO level")
	}
	if !logger.ShouldLog(WARN) {
		t.Error("WARN should be logged at INFO level")
	}
	if !logger.ShouldLog(ERROR) {
		t.Error("ERROR should be logged at INFO level")
	}
}

// TestLoggerWithFields 带字段的日志测试
func TestLoggerWithFields(t *testing.T) {
	mockWriter := NewMockWriter()
	logger := createTestLogger(DEBUG, mockWriter)

	logger.Info("test message", F("key1", "value1"), F("key2", 123))

	entries := mockWriter.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Fields == nil {
		t.Fatal("expected fields to be set")
	}
	if entry.Fields["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %v", entry.Fields["key1"])
	}
	if entry.Fields["key2"] != 123 {
		t.Errorf("expected key2=123, got %v", entry.Fields["key2"])
	}
}

// TestParseLevel 级别解析测试
func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", DEBUG},
		{"DEBUG", DEBUG},
		{"info", INFO},
		{"INFO", INFO},
		{"warn", WARN},
		{"WARN", WARN},
		{"warning", WARN},
		{"error", ERROR},
		{"ERROR", ERROR},
		{"invalid", INFO}, // 默认为 INFO
		{"", INFO},        // 空字符串默认为 INFO
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestLoggerInit 初始化测试
func TestLoggerInit(t *testing.T) {
	// 保存原始全局 logger
	originalLogger := globalLogger
	defer func() {
		globalLogger = originalLogger
	}()

	ctx := context.Background()
	Init(ctx, "debug")

	logger := New("test-component")
	if logger.GetLevel() != DEBUG {
		t.Errorf("expected DEBUG level, got %s", logger.GetLevel().String())
	}
	if logger.component != "test-component" {
		t.Errorf("expected component 'test-component', got %s", logger.component)
	}
}

// TestLoggerConcurrency 并发安全测试
func TestLoggerConcurrency(t *testing.T) {
	mockWriter := NewMockWriter()
	logger := createTestLogger(DEBUG, mockWriter)

	var wg sync.WaitGroup
	iterations := 100

	// 并发写入日志
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			logger.Info("concurrent message", F("iteration", n))
		}(i)
	}

	// 并发修改级别
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			level := Level(n % 4)
			logger.SetLevel(level)
		}(i)
	}

	wg.Wait()

	// 验证没有 panic 发生，日志数量可能因级别变化而不同
	entries := mockWriter.GetEntries()
	t.Logf("Concurrent test wrote %d entries", len(entries))
}

// BufferWriter 用于捕获输出的写入器
type BufferWriter struct {
	buffer *bytes.Buffer
	mu     sync.Mutex
}

func NewBufferWriter() *BufferWriter {
	return &BufferWriter{
		buffer: new(bytes.Buffer),
	}
}

func (w *BufferWriter) Write(entry *LogEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	formatter := NewTextFormatter()
	data, err := formatter.Format(entry)
	if err != nil {
		return err
	}
	w.buffer.Write(data)
	return nil
}

func (w *BufferWriter) Close() error {
	return nil
}

func (w *BufferWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}
