package logger

import (
	"errors"
	"sync"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty3_RequestIDUniqueness 属性测试：请求 ID 唯一性
// **Property 3: Request ID Uniqueness**
// **Validates: Requirements 2.4**
// *For any* sequence of N method calls through the interceptor, all N generated
// request IDs SHALL be unique (no duplicates).
func TestProperty3_RequestIDUniqueness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("All generated request IDs are unique", prop.ForAll(
		func(n int) bool {
			if n <= 0 {
				return true
			}

			ids := make(map[string]bool)
			for i := 0; i < n; i++ {
				id := GenerateRequestID()
				if ids[id] {
					// Duplicate found
					return false
				}
				ids[id] = true
			}
			return true
		},
		gen.IntRange(1, 1000),
	))

	properties.TestingRun(t)
}

// TestProperty3_RequestIDUniqueness_Concurrent 并发场景下的请求 ID 唯一性
func TestProperty3_RequestIDUniqueness_Concurrent(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Concurrent request ID generation produces unique IDs", prop.ForAll(
		func(goroutines int, idsPerGoroutine int) bool {
			if goroutines <= 0 || idsPerGoroutine <= 0 {
				return true
			}

			var mu sync.Mutex
			ids := make(map[string]bool)
			var wg sync.WaitGroup

			for g := 0; g < goroutines; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < idsPerGoroutine; i++ {
						id := GenerateRequestID()
						mu.Lock()
						if ids[id] {
							mu.Unlock()
							return
						}
						ids[id] = true
						mu.Unlock()
					}
				}()
			}

			wg.Wait()

			// Verify total count matches expected
			expectedCount := goroutines * idsPerGoroutine
			return len(ids) == expectedCount
		},
		gen.IntRange(1, 10),
		gen.IntRange(1, 100),
	))

	properties.TestingRun(t)
}

// TestProperty4_SensitiveFieldMasking 属性测试：敏感字段脱敏
// **Property 4: Sensitive Field Masking**
// **Validates: Requirements 2.5**
// *For any* log entry containing fields configured as sensitive, the logged value
// SHALL be masked (e.g., "***") and SHALL NOT contain the original value.
func TestProperty4_SensitiveFieldMasking(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Test map masking
	properties.Property("Sensitive fields in maps are masked", prop.ForAll(
		func(sensitiveField string, sensitiveValue string, normalField string, normalValue string) bool {
			// Skip empty field names
			if sensitiveField == "" || normalField == "" {
				return true
			}
			// Ensure fields are different
			if sensitiveField == normalField {
				return true
			}

			interceptor := NewMethodInterceptor(nil, InterceptorConfig{
				Enabled:         true,
				LogParameters:   true,
				SensitiveFields: []string{sensitiveField},
			})

			input := map[string]interface{}{
				sensitiveField: sensitiveValue,
				normalField:    normalValue,
			}

			masked := interceptor.maskValue(input)
			maskedMap, ok := masked.(map[string]interface{})
			if !ok {
				return false
			}

			// Sensitive field should be masked
			if maskedMap[sensitiveField] != "***" {
				return false
			}

			// Normal field should not be masked
			if maskedMap[normalField] != normalValue {
				return false
			}

			return true
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString(),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// TestProperty4_SensitiveFieldMasking_CaseInsensitive 测试大小写不敏感
func TestProperty4_SensitiveFieldMasking_CaseInsensitive(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Sensitive field matching is case-insensitive", prop.ForAll(
		func(fieldName string, value string) bool {
			if fieldName == "" {
				return true
			}

			// Configure with lowercase
			interceptor := NewMethodInterceptor(nil, InterceptorConfig{
				Enabled:         true,
				LogParameters:   true,
				SensitiveFields: []string{fieldName},
			})

			// Test with various case variations
			variations := []string{
				fieldName,
				toUpperCase(fieldName),
				toLowerCase(fieldName),
				mixedCase(fieldName),
			}

			for _, variant := range variations {
				input := map[string]interface{}{
					variant: value,
				}

				masked := interceptor.maskValue(input)
				maskedMap, ok := masked.(map[string]interface{})
				if !ok {
					return false
				}

				// All variations should be masked
				if maskedMap[variant] != "***" {
					return false
				}
			}

			return true
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// Helper functions for case conversion
func toUpperCase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			result[i] = c - 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func toLowerCase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func mixedCase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i%2 == 0 {
			if c >= 'a' && c <= 'z' {
				result[i] = c - 32
			} else {
				result[i] = c
			}
		} else {
			if c >= 'A' && c <= 'Z' {
				result[i] = c + 32
			} else {
				result[i] = c
			}
		}
	}
	return string(result)
}

// TestProperty4_SensitiveFieldMasking_NestedStructures 测试嵌套结构脱敏
func TestProperty4_SensitiveFieldMasking_NestedStructures(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Sensitive fields in nested maps are masked", prop.ForAll(
		func(sensitiveValue string) bool {
			interceptor := NewMethodInterceptor(nil, InterceptorConfig{
				Enabled:         true,
				LogParameters:   true,
				SensitiveFields: []string{"password", "token", "secret"},
			})

			// Create nested structure
			input := map[string]interface{}{
				"user": map[string]interface{}{
					"name":     "testuser",
					"password": sensitiveValue,
				},
				"auth": map[string]interface{}{
					"token": sensitiveValue,
				},
			}

			masked := interceptor.maskValue(input)
			maskedMap, ok := masked.(map[string]interface{})
			if !ok {
				return false
			}

			// Check nested password is masked
			userMap, ok := maskedMap["user"].(map[string]interface{})
			if !ok {
				return false
			}
			if userMap["password"] != "***" {
				return false
			}
			if userMap["name"] != "testuser" {
				return false
			}

			// Check nested token is masked
			authMap, ok := maskedMap["auth"].(map[string]interface{})
			if !ok {
				return false
			}
			if authMap["token"] != "***" {
				return false
			}

			return true
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// TestProperty11_LoggerFaultIsolation 属性测试：日志系统错误隔离
// **Property 11: Logger Fault Isolation**
// **Validates: Requirements 5.3**
// *For any* error occurring in the logging system (file write failure, formatter
// error, etc.), the wrapped business method SHALL still execute and return normally.
func TestProperty11_LoggerFaultIsolation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Business method executes normally even when logger is nil", prop.ForAll(
		func(input int) bool {
			// Create interceptor with nil logger (simulates logger failure)
			interceptor := NewMethodInterceptor(nil, InterceptorConfig{
				Enabled:       true,
				LogParameters: true,
				LogResults:    true,
			})

			// Wrap a simple function
			expectedResult := input * 2
			wrappedFn := interceptor.WrapFuncResult("TestMethod", func() interface{} {
				return input * 2
			})

			// Execute wrapped function
			result := wrappedFn()

			// Verify business logic executed correctly
			return result == expectedResult
		},
		gen.Int(),
	))

	properties.TestingRun(t)
}

// TestProperty11_LoggerFaultIsolation_WithError 测试返回错误的方法
func TestProperty11_LoggerFaultIsolation_WithError(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Error-returning method works with nil logger", prop.ForAll(
		func(shouldError bool) bool {
			interceptor := NewMethodInterceptor(nil, InterceptorConfig{
				Enabled:       true,
				LogParameters: true,
				LogResults:    true,
			})

			var expectedErr error
			if shouldError {
				expectedErr = errors.New("test error")
			}

			wrappedFn := interceptor.WrapFuncWithError("TestMethod", func() error {
				return expectedErr
			})

			// Execute wrapped function
			resultErr := wrappedFn()

			// Verify error is returned correctly
			if shouldError {
				return resultErr != nil && resultErr.Error() == "test error"
			}
			return resultErr == nil
		},
		gen.Bool(),
	))

	properties.TestingRun(t)
}

// faultyWriter 模拟故障的写入器
type faultyWriter struct {
	shouldPanic bool
}

func (w *faultyWriter) Write(entry *LogEntry) error {
	if w.shouldPanic {
		panic("simulated writer panic")
	}
	return errors.New("simulated write error")
}

func (w *faultyWriter) Close() error {
	return nil
}

// TestProperty11_LoggerFaultIsolation_FaultyWriter 测试故障写入器
func TestProperty11_LoggerFaultIsolation_FaultyWriter(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Business method executes normally with faulty writer", prop.ForAll(
		func(input string) bool {
			// Create a logger with faulty writer
			logger := &Logger{
				level:   INFO,
				writers: []Writer{&faultyWriter{shouldPanic: false}},
			}

			interceptor := NewMethodInterceptor(logger, InterceptorConfig{
				Enabled:       true,
				LogParameters: true,
				LogResults:    true,
			})

			// Wrap a simple function
			expectedResult := "processed: " + input
			wrappedFn := interceptor.WrapFuncResult("TestMethod", func() interface{} {
				return "processed: " + input
			})

			// Execute wrapped function
			result := wrappedFn()

			// Verify business logic executed correctly
			return result == expectedResult
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}

// TestProperty11_LoggerFaultIsolation_DisabledInterceptor 测试禁用的拦截器
func TestProperty11_LoggerFaultIsolation_DisabledInterceptor(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Disabled interceptor passes through without modification", prop.ForAll(
		func(input int) bool {
			interceptor := NewMethodInterceptor(nil, InterceptorConfig{
				Enabled: false,
			})

			expectedResult := input * 3
			wrappedFn := interceptor.WrapFuncResult("TestMethod", func() interface{} {
				return input * 3
			})

			result := wrappedFn()
			return result == expectedResult
		},
		gen.Int(),
	))

	properties.TestingRun(t)
}
