package logger

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InterceptorConfig 拦截器配置
type InterceptorConfig struct {
	Enabled         bool
	LogParameters   bool
	LogResults      bool
	SensitiveFields []string
}

// MethodInterceptor 方法拦截器
// 用于自动记录方法调用的 AOP 组件
type MethodInterceptor struct {
	logger          *Logger
	config          InterceptorConfig
	sensitiveFields map[string]bool
	mu              sync.RWMutex
}

// CallContext 调用上下文
type CallContext struct {
	RequestID  string
	MethodName string
	StartTime  time.Time
	Parameters []interface{}
}

// NewMethodInterceptor 创建新的方法拦截器
func NewMethodInterceptor(logger *Logger, config InterceptorConfig) *MethodInterceptor {
	sensitiveFields := make(map[string]bool)
	for _, field := range config.SensitiveFields {
		sensitiveFields[strings.ToLower(field)] = true
	}

	return &MethodInterceptor{
		logger:          logger,
		config:          config,
		sensitiveFields: sensitiveFields,
	}
}

// GenerateRequestID 生成唯一的请求 ID
func GenerateRequestID() string {
	return uuid.New().String()
}

// WrapFunc 包装无参数无返回值的函数
func (m *MethodInterceptor) WrapFunc(name string, fn func()) func() {
	if !m.config.Enabled {
		return fn
	}

	return func() {
		ctx := m.beforeCall(name, nil)
		defer m.afterCallRecover(ctx, nil, nil)

		fn()
	}
}

// WrapFuncWithError 包装返回 error 的函数
func (m *MethodInterceptor) WrapFuncWithError(name string, fn func() error) func() error {
	if !m.config.Enabled {
		return fn
	}

	return func() error {
		ctx := m.beforeCall(name, nil)
		var err error

		defer func() {
			m.afterCallRecover(ctx, nil, err)
		}()

		err = fn()
		return err
	}
}

// WrapFuncResult 包装有返回值的函数（使用 interface{}）
func (m *MethodInterceptor) WrapFuncResult(name string, fn func() interface{}) func() interface{} {
	if !m.config.Enabled {
		return fn
	}

	return func() interface{} {
		ctx := m.beforeCall(name, nil)
		var result interface{}

		defer func() {
			m.afterCallRecover(ctx, result, nil)
		}()

		result = fn()
		return result
	}
}

// WrapFuncResultError 包装有返回值和 error 的函数
func (m *MethodInterceptor) WrapFuncResultError(name string, fn func() (interface{}, error)) func() (interface{}, error) {
	if !m.config.Enabled {
		return fn
	}

	return func() (interface{}, error) {
		ctx := m.beforeCall(name, nil)
		var result interface{}
		var err error

		defer func() {
			m.afterCallRecover(ctx, result, err)
		}()

		result, err = fn()
		return result, err
	}
}

// WrapMethod1Arg 包装单参数方法
func (m *MethodInterceptor) WrapMethod1Arg(name string, fn func(interface{}) interface{}) func(interface{}) interface{} {
	if !m.config.Enabled {
		return fn
	}

	return func(p interface{}) interface{} {
		ctx := m.beforeCall(name, []interface{}{p})
		var result interface{}

		defer func() {
			m.afterCallRecover(ctx, result, nil)
		}()

		result = fn(p)
		return result
	}
}

// WrapMethod1ArgError 包装单参数返回 error 的方法
func (m *MethodInterceptor) WrapMethod1ArgError(name string, fn func(interface{}) (interface{}, error)) func(interface{}) (interface{}, error) {
	if !m.config.Enabled {
		return fn
	}

	return func(p interface{}) (interface{}, error) {
		ctx := m.beforeCall(name, []interface{}{p})
		var result interface{}
		var err error

		defer func() {
			m.afterCallRecover(ctx, result, err)
		}()

		result, err = fn(p)
		return result, err
	}
}

// beforeCall 方法调用前的处理
func (m *MethodInterceptor) beforeCall(methodName string, params []interface{}) *CallContext {
	ctx := &CallContext{
		RequestID:  GenerateRequestID(),
		MethodName: methodName,
		StartTime:  time.Now(),
		Parameters: params,
	}

	// 记录方法入口日志
	entry := NewLogEntry(INFO, "interceptor", fmt.Sprintf("Method call started: %s", methodName))
	entry.WithRequestID(ctx.RequestID)
	entry.WithMethod(methodName)

	// 添加参数信息
	if m.config.LogParameters && len(params) > 0 {
		maskedParams := m.maskSensitiveParams(params)
		entry.WithFields(map[string]interface{}{
			"parameters": maskedParams,
		})
	}

	// 添加调用位置
	if file, line := m.getCaller(); file != "" {
		entry.WithCaller(file, line)
	}

	m.safeLog(entry)

	return ctx
}

// afterCallRecover 方法调用后的处理（带 panic 恢复）
func (m *MethodInterceptor) afterCallRecover(ctx *CallContext, result interface{}, err error) {
	// 捕获 panic，确保日志错误不影响业务
	if r := recover(); r != nil {
		m.handlePanic(ctx, r)
		// 重新抛出 panic，让业务代码处理
		panic(r)
	}

	m.afterCall(ctx, result, err)
}

// afterCall 方法调用后的处理
func (m *MethodInterceptor) afterCall(ctx *CallContext, result interface{}, err error) {
	duration := time.Since(ctx.StartTime).Milliseconds()

	var entry *LogEntry
	if err != nil {
		// 错误情况
		entry = NewLogEntry(ERROR, "interceptor", fmt.Sprintf("Method call failed: %s", ctx.MethodName))
		entry.WithError(err.Error())

		// 获取堆栈信息
		stack := m.getStackTrace()
		if stack != "" {
			if entry.Fields == nil {
				entry.Fields = make(map[string]interface{})
			}
			entry.Fields["stack_trace"] = stack
		}
	} else {
		// 成功情况
		entry = NewLogEntry(INFO, "interceptor", fmt.Sprintf("Method call completed: %s", ctx.MethodName))

		// 记录返回结果
		if m.config.LogResults && result != nil {
			maskedResult := m.maskSensitiveValue("result", result)
			if entry.Fields == nil {
				entry.Fields = make(map[string]interface{})
			}
			entry.Fields["result"] = maskedResult
		}
	}

	entry.WithRequestID(ctx.RequestID)
	entry.WithMethod(ctx.MethodName)
	entry.WithDuration(duration)

	m.safeLog(entry)
}

// handlePanic 处理 panic
func (m *MethodInterceptor) handlePanic(ctx *CallContext, panicValue interface{}) {
	duration := time.Since(ctx.StartTime).Milliseconds()

	entry := NewLogEntry(ERROR, "interceptor", fmt.Sprintf("Method call panicked: %s", ctx.MethodName))
	entry.WithRequestID(ctx.RequestID)
	entry.WithMethod(ctx.MethodName)
	entry.WithDuration(duration)
	entry.WithError(fmt.Sprintf("panic: %v", panicValue))

	// 获取堆栈信息
	stack := m.getStackTrace()
	if stack != "" {
		if entry.Fields == nil {
			entry.Fields = make(map[string]interface{})
		}
		entry.Fields["stack_trace"] = stack
	}

	m.safeLog(entry)
}

// safeLog 安全地记录日志（捕获所有错误）
func (m *MethodInterceptor) safeLog(entry *LogEntry) {
	defer func() {
		if r := recover(); r != nil {
			// 日志系统出错，静默处理，不影响业务
			fmt.Printf("[INTERCEPTOR ERROR] Failed to log: %v\n", r)
		}
	}()

	if m.logger != nil {
		m.logger.LogEntry(entry)
	}
}

// maskSensitiveParams 对敏感参数进行脱敏
func (m *MethodInterceptor) maskSensitiveParams(params []interface{}) []interface{} {
	if len(m.sensitiveFields) == 0 {
		return params
	}

	masked := make([]interface{}, len(params))
	for i, param := range params {
		masked[i] = m.maskValue(param)
	}
	return masked
}

// maskValue 对值进行脱敏处理
func (m *MethodInterceptor) maskValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	v := reflect.ValueOf(value)

	switch v.Kind() {
	case reflect.Map:
		return m.maskMap(v)
	case reflect.Struct:
		return m.maskStruct(v)
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return m.maskValue(v.Elem().Interface())
	default:
		return value
	}
}

// maskMap 对 map 进行脱敏
func (m *MethodInterceptor) maskMap(v reflect.Value) interface{} {
	result := make(map[string]interface{})

	iter := v.MapRange()
	for iter.Next() {
		key := fmt.Sprintf("%v", iter.Key().Interface())
		val := iter.Value().Interface()

		if m.isSensitiveField(key) {
			result[key] = "***"
		} else {
			result[key] = m.maskValue(val)
		}
	}

	return result
}

// maskStruct 对结构体进行脱敏
func (m *MethodInterceptor) maskStruct(v reflect.Value) interface{} {
	result := make(map[string]interface{})
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		fieldName := field.Name
		fieldValue := v.Field(i).Interface()

		if m.isSensitiveField(fieldName) {
			result[fieldName] = "***"
		} else {
			result[fieldName] = m.maskValue(fieldValue)
		}
	}

	return result
}

// maskSensitiveValue 对单个值进行脱敏（用于返回值）
func (m *MethodInterceptor) maskSensitiveValue(fieldName string, value interface{}) interface{} {
	if m.isSensitiveField(fieldName) {
		return "***"
	}
	return m.maskValue(value)
}

// isSensitiveField 检查字段是否为敏感字段
func (m *MethodInterceptor) isSensitiveField(fieldName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sensitiveFields[strings.ToLower(fieldName)]
}

// AddSensitiveField 添加敏感字段
func (m *MethodInterceptor) AddSensitiveField(fieldName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sensitiveFields[strings.ToLower(fieldName)] = true
}

// RemoveSensitiveField 移除敏感字段
func (m *MethodInterceptor) RemoveSensitiveField(fieldName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sensitiveFields, strings.ToLower(fieldName))
}

// getCaller 获取调用位置
func (m *MethodInterceptor) getCaller() (string, int) {
	// 跳过拦截器内部的调用栈
	for i := 3; i < 10; i++ {
		_, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		// 跳过拦截器自身的文件
		if !strings.Contains(file, "interceptor.go") {
			// 只保留文件名
			parts := strings.Split(file, "/")
			if len(parts) > 0 {
				return parts[len(parts)-1], line
			}
			return file, line
		}
	}
	return "", 0
}

// getStackTrace 获取堆栈信息
func (m *MethodInterceptor) getStackTrace() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// SetEnabled 设置拦截器启用状态
func (m *MethodInterceptor) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Enabled = enabled
}

// IsEnabled 检查拦截器是否启用
func (m *MethodInterceptor) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Enabled
}

// GetConfig 获取拦截器配置
func (m *MethodInterceptor) GetConfig() InterceptorConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Intercept 通用拦截方法，用于手动记录方法调用
// 返回 CallContext 用于后续调用 Complete 或 Fail
func (m *MethodInterceptor) Intercept(methodName string, params ...interface{}) *CallContext {
	if !m.config.Enabled {
		return &CallContext{
			RequestID:  GenerateRequestID(),
			MethodName: methodName,
			StartTime:  time.Now(),
			Parameters: params,
		}
	}
	return m.beforeCall(methodName, params)
}

// Complete 标记方法调用成功完成
func (m *MethodInterceptor) Complete(ctx *CallContext, result interface{}) {
	if !m.config.Enabled {
		return
	}
	m.afterCall(ctx, result, nil)
}

// Fail 标记方法调用失败
func (m *MethodInterceptor) Fail(ctx *CallContext, err error) {
	if !m.config.Enabled {
		return
	}
	m.afterCall(ctx, nil, err)
}

// GetRequestID 获取调用上下文的请求 ID
func (ctx *CallContext) GetRequestID() string {
	return ctx.RequestID
}

// GetMethodName 获取调用上下文的方法名
func (ctx *CallContext) GetMethodName() string {
	return ctx.MethodName
}

// GetDuration 获取调用耗时（毫秒）
func (ctx *CallContext) GetDuration() int64 {
	return time.Since(ctx.StartTime).Milliseconds()
}
