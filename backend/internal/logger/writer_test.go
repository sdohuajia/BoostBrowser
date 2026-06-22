package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestConsoleWriter_Write tests basic console writer functionality
func TestConsoleWriter_Write(t *testing.T) {
	formatter := NewTextFormatter()
	writer := NewConsoleWriter(formatter)
	defer writer.Close()

	entry := NewLogEntry(INFO, "test", "test message")
	err := writer.Write(entry)
	if err != nil {
		t.Errorf("ConsoleWriter.Write() error = %v", err)
	}
}

// TestFileWriter_Write tests basic file writer functionality
func TestFileWriter_Write(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	config := DefaultFileWriterConfig(logPath)
	formatter := NewTextFormatter()

	writer, err := NewFileWriter(config, formatter)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}
	defer writer.Close()

	entry := NewLogEntry(INFO, "test", "test message")
	err = writer.Write(entry)
	if err != nil {
		t.Errorf("FileWriter.Write() error = %v", err)
	}

	// Flush and verify file exists
	writer.Flush()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("Log file was not created")
	}
}

// TestFileWriter_CreateDirectory tests automatic directory creation
func TestFileWriter_CreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "subdir", "nested", "test.log")

	config := DefaultFileWriterConfig(logPath)
	formatter := NewTextFormatter()

	writer, err := NewFileWriter(config, formatter)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}
	defer writer.Close()

	// Verify directory was created
	dir := filepath.Dir(logPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Directory was not created automatically")
	}
}

// TestAsyncFileWriter_NonBlocking tests that async writes are non-blocking
func TestAsyncFileWriter_NonBlocking(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "async_test.log")

	config := FileWriterConfig{
		FilePath:       logPath,
		BufferSize:     4 * 1024,
		FlushInterval:  100 * time.Millisecond,
		AsyncQueueSize: 100,
	}
	formatter := NewTextFormatter()

	writer, err := NewAsyncFileWriter(config, formatter)
	if err != nil {
		t.Fatalf("NewAsyncFileWriter() error = %v", err)
	}
	defer writer.Close()

	// Write should return quickly
	entry := NewLogEntry(INFO, "test", "test message")
	start := time.Now()
	err = writer.Write(entry)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("AsyncFileWriter.Write() error = %v", err)
	}

	// Should complete in less than 1ms (non-blocking)
	if elapsed > time.Millisecond {
		t.Errorf("Async write took too long: %v", elapsed)
	}
}

// Property 10: Async Write Non-Blocking
// *For any* log write operation, the call SHALL return within a bounded time (< 1ms typical)
// regardless of file I/O latency.
// **Validates: Requirements 5.2**
func TestProperty10_AsyncWriteNonBlocking(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	properties.Property("async write returns within bounded time", prop.ForAll(
		func(level int, component string, message string) bool {
			// Create temp file for each test
			tmpDir := os.TempDir()
			logPath := filepath.Join(tmpDir, "pbt_async_test.log")
			defer os.Remove(logPath)

			config := FileWriterConfig{
				FilePath:       logPath,
				BufferSize:     4 * 1024,
				FlushInterval:  time.Second,
				AsyncQueueSize: 1000,
			}
			formatter := NewTextFormatter()

			writer, err := NewAsyncFileWriter(config, formatter)
			if err != nil {
				return false
			}
			defer writer.Close()

			// Create log entry from generated data
			logLevel := Level(level % 4) // Ensure valid level 0-3
			entry := NewLogEntry(logLevel, component, message)

			// Measure write time
			start := time.Now()
			_ = writer.Write(entry)
			elapsed := time.Since(start)

			// Property: write should complete within 1ms (non-blocking)
			// Using 5ms as upper bound to account for system variance
			return elapsed < 5*time.Millisecond
		},
		gen.IntRange(0, 3),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// TestMultiWriter tests writing to multiple destinations
func TestMultiWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "multi_test.log")

	// Create console and file writers
	consoleWriter := NewConsoleWriter(NewTextFormatter())
	fileConfig := DefaultFileWriterConfig(logPath)
	fileWriter, err := NewFileWriter(fileConfig, NewTextFormatter())
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}

	multiWriter := NewMultiWriter(consoleWriter, fileWriter)
	defer multiWriter.Close()

	entry := NewLogEntry(INFO, "test", "multi writer test")
	err = multiWriter.Write(entry)
	if err != nil {
		t.Errorf("MultiWriter.Write() error = %v", err)
	}

	// Flush file writer
	fileWriter.Flush()

	// Verify file was written
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("Log file was not created by MultiWriter")
	}
}
