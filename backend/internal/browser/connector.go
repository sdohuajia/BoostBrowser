package browser

var defaultVerificationURLs = []string{
	"https://ippure.com/",
}

// BuildLaunchArgs 构建启动参数
func BuildLaunchArgs(args []string, profile *Profile) []string {
	args = append(args, defaultVerificationURLs...)
	return args
}

// GetDefaultVerificationURLs 返回默认验证 URL 列表（用于 CDP 导航）
func GetDefaultVerificationURLs() []string {
	result := make([]string, len(defaultVerificationURLs))
	copy(result, defaultVerificationURLs)
	return result
}
