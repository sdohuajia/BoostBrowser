package logger

import (
	"os"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// mockFileInfo 模拟文件信息用于测试
type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return m.modTime }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() interface{}   { return nil }

// TestProperty6_SizeBasedRotationTrigger 属性测试：大小分片触发
// **Property 6: Size-Based Rotation Trigger**
// **Validates: Requirements 4.2**
// *For any* configured max file size S, when the current log file size exceeds S,
// a new log file SHALL be created before writing the next entry.
func TestProperty6_SizeBasedRotationTrigger(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// 生成随机的最大文件大小 (1KB - 100MB)
	maxSizeGen := gen.Int64Range(1024, 100*1024*1024)
	// 生成随机的当前文件大小 (0 - 200MB)
	currentSizeGen := gen.Int64Range(0, 200*1024*1024)

	properties.Property("size rotation triggers when file size >= maxSize", prop.ForAll(
		func(maxSize, currentSize int64) bool {
			policy := NewSizeRotationPolicy(maxSize)

			fileInfo := &mockFileInfo{
				name:    "test.log",
				size:    currentSize,
				modTime: time.Now(),
			}

			entry := NewLogEntry(INFO, "test", "test message")
			shouldRotate := policy.ShouldRotate(fileInfo, entry)

			// 当文件大小 >= 最大大小时，应该触发分片
			expected := currentSize >= maxSize
			return shouldRotate == expected
		},
		maxSizeGen,
		currentSizeGen,
	))

	properties.TestingRun(t)
}

// TestProperty8_HistoryFileLimit 属性测试：历史文件数量限制
// **Property 8: History File Limit**
// **Validates: Requirements 4.5**
// *For any* configured max backup count N, the number of rotated log files
// SHALL never exceed N, with oldest files deleted first.
func TestProperty8_HistoryFileLimit(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// 生成随机的最大备份数 (1-20)
	maxBackupsGen := gen.IntRange(1, 20)
	// 生成随机的初始文件数 (0-30)
	initialFilesGen := gen.IntRange(0, 30)

	properties.Property("history files never exceed maxBackups after cleanup", prop.ForAll(
		func(maxBackups, initialFiles int) bool {
			// 创建临时目录
			tempDir, err := os.MkdirTemp("", "rotation_test_*")
			if err != nil {
				t.Logf("Failed to create temp dir: %v", err)
				return false
			}
			defer os.RemoveAll(tempDir)

			basePath := tempDir + "/app.log"

			// 创建初始的分片文件
			baseTime := time.Now().AddDate(0, 0, -initialFiles)
			for i := 0; i < initialFiles; i++ {
				fileTime := baseTime.AddDate(0, 0, i)
				fileName := tempDir + "/app." + fileTime.Format("2006-01-02") + ".log"
				f, err := os.Create(fileName)
				if err != nil {
					t.Logf("Failed to create file: %v", err)
					return false
				}
				f.Close()
				// 设置文件修改时间以便排序
				os.Chtimes(fileName, fileTime, fileTime)
			}

			// 创建 RotationManager 并执行清理
			manager := NewRotationManager(RotationManagerConfig{
				BasePath:   basePath,
				MaxBackups: maxBackups,
			})

			// 执行清理
			err = manager.cleanupOldFiles()
			if err != nil {
				t.Logf("Cleanup failed: %v", err)
				return false
			}

			// 检查剩余文件数
			count, err := manager.GetRotatedFileCount()
			if err != nil {
				t.Logf("Failed to get file count: %v", err)
				return false
			}

			// 文件数应该不超过 maxBackups
			return count <= maxBackups
		},
		maxBackupsGen,
		initialFilesGen,
	))

	properties.TestingRun(t)
}

// TestProperty9_RotatedFileNamingFormat 属性测试：分片文件命名格式
// **Property 9: Rotated File Naming Format**
// **Validates: Requirements 4.6**
// *For any* rotated log file, the filename SHALL match the pattern
// `{basename}.{timestamp}[.{sequence}].log` where timestamp is in ISO date format.
func TestProperty9_RotatedFileNamingFormat(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// 生成随机的基础文件名（使用字母数字字符）
	baseNameGen := gen.AlphaString().Map(func(s string) string {
		if s == "" || len(s) == 0 {
			return "app"
		}
		if len(s) > 20 {
			return s[:20]
		}
		return s
	})

	// 生成随机时间戳 (过去一年内)
	timestampGen := gen.Int64Range(0, 365*24).Map(func(hours int64) time.Time {
		return time.Now().Add(-time.Duration(hours) * time.Hour)
	})

	// 生成随机的时间间隔类型
	intervalGen := gen.OneConstOf(Daily, Hourly)

	properties.Property("time rotation generates valid file names", prop.ForAll(
		func(baseName string, timestamp time.Time, interval TimeInterval) bool {
			policy := NewTimeRotationPolicy(interval)
			fileName := policy.GetRotatedFileName(baseName+".log", timestamp)

			// 验证文件名格式
			return ValidateRotatedFileName(fileName)
		},
		baseNameGen,
		timestampGen,
		intervalGen,
	))

	// 测试大小分片的文件命名
	properties.Property("size rotation generates valid file names", prop.ForAll(
		func(baseName string, timestamp time.Time, maxSize int64) bool {
			policy := NewSizeRotationPolicy(maxSize)
			fileName := policy.GetRotatedFileName(baseName+".log", timestamp)

			// 验证文件名格式
			return ValidateRotatedFileName(fileName)
		},
		baseNameGen,
		timestampGen,
		gen.Int64Range(1024, 100*1024*1024),
	))

	properties.TestingRun(t)
}
