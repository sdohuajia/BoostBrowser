package logger

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ConsoleWriter 控制台写入器
// 将日志输出到标准输出
type ConsoleWriter struct {
	formatter Formatter
	mu        sync.Mutex
}

// NewConsoleWriter 创建新的控制台写入器
func NewConsoleWriter(formatter Formatter) *ConsoleWriter {
	if formatter == nil {
		formatter = NewTextFormatter()
	}
	return &ConsoleWriter{
		formatter: formatter,
	}
}

// Write 写入日志条目到控制台
func (w *ConsoleWriter) Write(entry *LogEntry) error {
	if entry == nil {
		return nil
	}

	data, err := w.formatter.Format(entry)
	if err != nil {
		return fmt.Errorf("failed to format log entry: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	_, err = os.Stdout.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to console: %w", err)
	}

	return nil
}

// Close 关闭控制台写入器（无操作）
func (w *ConsoleWriter) Close() error {
	return nil
}

// FileWriterConfig 文件写入器配置
type FileWriterConfig struct {
	FilePath       string        // 日志文件路径
	BufferSize     int           // 缓冲区大小（字节），默认 4KB
	FlushInterval  time.Duration // 刷新间隔，默认 1s
	AsyncQueueSize int           // 异步队列大小，默认 1000
}

// DefaultFileWriterConfig 返回默认的文件写入器配置
func DefaultFileWriterConfig(filePath string) FileWriterConfig {
	return FileWriterConfig{
		FilePath:       filePath,
		BufferSize:     4 * 1024, // 4KB
		FlushInterval:  time.Second,
		AsyncQueueSize: 1000,
	}
}

// FileWriter 文件写入器
// 支持缓冲写入和异步写入
type FileWriter struct {
	config    FileWriterConfig
	formatter Formatter
	file      *os.File
	buffer    *bufio.Writer
	mu        sync.Mutex

	// 异步写入相关
	asyncChan   chan *LogEntry
	done        chan struct{}
	wg          sync.WaitGroup
	asyncMode   bool
	flushTicker *time.Ticker
}

// NewFileWriter 创建新的文件写入器（同步模式）
func NewFileWriter(config FileWriterConfig, formatter Formatter) (*FileWriter, error) {
	if formatter == nil {
		formatter = NewTextFormatter()
	}

	// 确保目录存在
	dir := filepath.Dir(config.FilePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	// 打开文件（追加模式）
	file, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// 设置默认缓冲区大小
	bufferSize := config.BufferSize
	if bufferSize <= 0 {
		bufferSize = 4 * 1024 // 4KB
	}

	w := &FileWriter{
		config:    config,
		formatter: formatter,
		file:      file,
		buffer:    bufio.NewWriterSize(file, bufferSize),
		asyncMode: false,
	}

	return w, nil
}

// Write 写入日志条目到文件（同步模式）
func (w *FileWriter) Write(entry *LogEntry) error {
	if entry == nil {
		return nil
	}

	// 如果是异步模式，发送到队列
	if w.asyncMode {
		return w.writeAsync(entry)
	}

	return w.writeSync(entry)
}

// writeSync 同步写入
func (w *FileWriter) writeSync(entry *LogEntry) error {
	data, err := w.formatter.Format(entry)
	if err != nil {
		return fmt.Errorf("failed to format log entry: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	_, err = w.buffer.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to buffer: %w", err)
	}

	return nil
}

// Flush 刷新缓冲区到文件
func (w *FileWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buffer != nil {
		return w.buffer.Flush()
	}
	return nil
}

// Close 关闭文件写入器
func (w *FileWriter) Close() error {
	// 如果是异步模式，先停止异步写入
	if w.asyncMode {
		w.stopAsync()
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var errs []error

	// 刷新缓冲区
	if w.buffer != nil {
		if err := w.buffer.Flush(); err != nil {
			errs = append(errs, fmt.Errorf("failed to flush buffer: %w", err))
		}
	}

	// 关闭文件
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close file: %w", err))
		}
		w.file = nil
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetFilePath 获取当前日志文件路径
func (w *FileWriter) GetFilePath() string {
	return w.config.FilePath
}

// NewAsyncFileWriter 创建新的异步文件写入器
func NewAsyncFileWriter(config FileWriterConfig, formatter Formatter) (*FileWriter, error) {
	w, err := NewFileWriter(config, formatter)
	if err != nil {
		return nil, err
	}

	// 启用异步模式
	w.enableAsync()

	return w, nil
}

// enableAsync 启用异步写入模式
func (w *FileWriter) enableAsync() {
	if w.asyncMode {
		return
	}

	queueSize := w.config.AsyncQueueSize
	if queueSize <= 0 {
		queueSize = 1000
	}

	flushInterval := w.config.FlushInterval
	if flushInterval <= 0 {
		flushInterval = time.Second
	}

	w.asyncChan = make(chan *LogEntry, queueSize)
	w.done = make(chan struct{})
	w.flushTicker = time.NewTicker(flushInterval)
	w.asyncMode = true

	// 启动后台写入 goroutine
	w.wg.Add(1)
	go w.asyncWriteLoop()
}

// asyncWriteLoop 异步写入循环
func (w *FileWriter) asyncWriteLoop() {
	defer w.wg.Done()

	for {
		select {
		case entry, ok := <-w.asyncChan:
			if !ok {
				// 通道已关闭，处理剩余日志
				return
			}
			// 写入日志（忽略错误，避免阻塞）
			_ = w.writeSync(entry)

		case <-w.flushTicker.C:
			// 定期刷新缓冲区
			_ = w.Flush()

		case <-w.done:
			// 收到停止信号，处理剩余日志
			w.drainQueue()
			return
		}
	}
}

// drainQueue 清空队列中的剩余日志
func (w *FileWriter) drainQueue() {
	for {
		select {
		case entry, ok := <-w.asyncChan:
			if !ok {
				return
			}
			_ = w.writeSync(entry)
		default:
			// 队列已空
			return
		}
	}
}

// writeAsync 异步写入（非阻塞）
func (w *FileWriter) writeAsync(entry *LogEntry) error {
	select {
	case w.asyncChan <- entry:
		return nil
	default:
		// 队列满，丢弃日志（非阻塞）
		return fmt.Errorf("async queue full, log entry dropped")
	}
}

// stopAsync 停止异步写入
func (w *FileWriter) stopAsync() {
	if !w.asyncMode {
		return
	}

	// 停止定时器
	if w.flushTicker != nil {
		w.flushTicker.Stop()
	}

	// 发送停止信号
	close(w.done)

	// 等待后台 goroutine 完成
	w.wg.Wait()

	// 关闭通道
	close(w.asyncChan)

	w.asyncMode = false
}

// IsAsync 返回是否为异步模式
func (w *FileWriter) IsAsync() bool {
	return w.asyncMode
}

// QueueLength 返回当前异步队列长度（用于监控）
func (w *FileWriter) QueueLength() int {
	if !w.asyncMode {
		return 0
	}
	return len(w.asyncChan)
}

// MultiWriter 多写入器
// 同时写入多个目标
type MultiWriter struct {
	writers []Writer
}

// NewMultiWriter 创建多写入器
func NewMultiWriter(writers ...Writer) *MultiWriter {
	return &MultiWriter{
		writers: writers,
	}
}

// Write 写入日志到所有写入器
func (w *MultiWriter) Write(entry *LogEntry) error {
	var lastErr error
	for _, writer := range w.writers {
		if err := writer.Write(entry); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Close 关闭所有写入器
func (w *MultiWriter) Close() error {
	var lastErr error
	for _, writer := range w.writers {
		if err := writer.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// AddWriter 添加写入器
func (w *MultiWriter) AddWriter(writer Writer) {
	w.writers = append(w.writers, writer)
}
