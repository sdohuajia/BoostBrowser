package browser

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"boost-browser/backend/internal/logger"
	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// DownloadProgress 进度信息载体
type DownloadProgress struct {
	Phase    string `json:"phase"`    // "downloading" 或 "extracting" 或 "done" 或 "error"
	Progress int    `json:"progress"` // 进度百分比 0-100
	Message  string `json:"message"`  // 附加详情
}

type coreDownloadWriter struct {
	writeFunc func(p []byte) (n int, err error)
	ctx       context.Context
}

func (cw *coreDownloadWriter) Write(p []byte) (int, error) {
	select {
	case <-cw.ctx.Done():
		return 0, cw.ctx.Err()
	default:
	}
	return cw.writeFunc(p)
}

// DownloadAndExtractCore 执行异步下载解压并在过程中发送事件
func (m *Manager) DownloadAndExtractCore(ctx context.Context, coreName string, targetUrl string, proxyConfig string) {
	log := logger.New("Browser")
	t := time.Now()

	sendEvent := func(phase string, progress int, msg string) {
		runtime.EventsEmit(ctx, "download:progress", DownloadProgress{
			Phase:    phase,
			Progress: progress,
			Message:  msg,
		})
	}

	sendEvent("downloading", 0, "开始解析地址并创建下载请求: "+targetUrl)

	// 1. 检查名称重复
	coreName = strings.TrimSpace(coreName)
	for _, c := range m.ListCores() {
		if strings.EqualFold(c.CoreName, coreName) || filepath.Base(c.CorePath) == coreName {
			sendEvent("error", 0, "名称已存在，请换一个名称")
			return
		}
	}

	// 确保外层 chrome/ 目录存在
	chromeDir := m.ResolveRelativePath("chrome")
	if err := os.MkdirAll(chromeDir, 0755); err != nil {
		sendEvent("error", 0, "创建 chrome 目录失败")
		return
	}

	targetDir := filepath.Join(chromeDir, coreName)
	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		sendEvent("error", 0, "同名文件夹已存在: "+coreName)
		return
	}
	// 2. 准备 HttpClient（优先从 Windows 注册表读取真实系统代理，而非仅靠环境变量）
	transport := &http.Transport{}
	if proxyConfig == "__system__" {
		// http.ProxyFromEnvironment 只读环境变量，而 Clash 的全局代理写在 Windows 注册表里
		// 必须直接读取注册表才能拿到正确的代理地址
		if sysProxy, rErr := readSystemProxy(); rErr == nil && sysProxy != "" {
			if proxyURL, pErr := url.Parse(sysProxy); pErr == nil {
				transport.Proxy = http.ProxyURL(proxyURL)
				sendEvent("downloading", 0, "已从系统注册表读取代理: "+sysProxy)
			} else {
				// 解析失败则回退到环境变量
				transport.Proxy = http.ProxyFromEnvironment
			}
		} else {
			// 没有系统代理配置或读取失败，尝试环境变量兜底
			transport.Proxy = http.ProxyFromEnvironment
			sendEvent("downloading", 0, "系统注册表无代理配置，使用环境变量兜底")
		}
	} else if proxyConfig != "" && proxyConfig != "direct://" && proxyConfig != "__direct__" {
		if proxyURL, pErr := url.Parse(proxyConfig); pErr == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		} else {
			sendEvent("error", 0, "代理地址解析失败: "+pErr.Error())
			return
		}
	}

	client := &http.Client{
		Timeout:   0, // 取消全局超时，依靠 context 和分片连接维持
		Transport: transport,
	}

	tempFile, err := os.CreateTemp(chromeDir, "download_*.zip")
	if err != nil {
		sendEvent("error", 0, "创建临时文件失败: "+err.Error())
		return
	}
	tempFilePath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempFilePath) // 清理临时文件
	}()

	sendEvent("downloading", 0, "开始分析下载链接(检测多线程支持)...")

	err = doConcurrentDownload(ctx, client, targetUrl, tempFile, sendEvent)
	if err != nil {
		sendEvent("error", 0, "下载失败: "+err.Error())
		return
	}

	tempFile.Close() // 解压前先关闭写句柄
	sendEvent("extracting", 0, "下载完成，正在准备解压文件...")
	log.Info("内核下载完成", logger.F("url", targetUrl), logger.F("temp", tempFilePath), logger.F("cost", time.Since(t).String()))

	// 3. 执行解压，并剥离顶层文件夹
	if err := extractZipAndStripRoot(tempFilePath, targetDir, func(p int, msg string) {
		sendEvent("extracting", p, msg)
	}); err != nil {
		os.RemoveAll(targetDir) // 删除不完整的解压文件
		sendEvent("error", 0, "解压失败: "+err.Error())
		return
	}

	// 4. 将新内核配置入库
	corePath := filepath.Join("chrome", coreName)
	if m.ValidateCorePath(corePath).Valid {
		newCore := CoreInput{
			CoreId:    uuid.NewString(), // 使用固定的 UUID 或生成新的
			CoreName:  coreName,
			CorePath:  corePath,
			IsDefault: len(m.ListCores()) == 0, // 如果没有其他内核，这设为默认
		}
		if err := m.SaveCore(newCore); err != nil {
			sendEvent("error", 0, "保存配置入库失败: "+err.Error())
			return
		}
		sendEvent("done", 100, "内核下载与配置成功！")
		log.Info("内核下载配置入库成功", logger.F("core_name", coreName))
	} else {
		os.RemoveAll(targetDir) // 删除不正确的解压内容
		sendEvent("error", 0, fmt.Sprintf("解压后未找到浏览器可执行文件（候选：%s），请检查压缩包内容！", strings.Join(CoreExecutableCandidates(), ", ")))
	}
}

// extractZipAndStripRoot 解压 ZIP 包，如果其所有文件全被同一个根目录包裹，则剥离这层根目录解压至 dest
// progressCb 为进度回调 (0-100%, statusType_msg)
func extractZipAndStripRoot(zipPath, dest string, progressCb func(int, string)) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if len(r.File) == 0 {
		return fmt.Errorf("空的压缩包")
	}

	// 探测是否存在单一顶层目录
	var rootPrefix string
	hasCommonRoot := true

	for _, f := range r.File {
		cleanName := filepath.ToSlash(f.Name)
		parts := strings.SplitN(cleanName, "/", 2)

		// 检查空名称文件，理论上不该有
		if len(parts) == 0 || parts[0] == "" {
			continue
		}

		if rootPrefix == "" {
			rootPrefix = parts[0] + "/"
		} else if !strings.HasPrefix(cleanName, rootPrefix) && cleanName != strings.TrimSuffix(rootPrefix, "/") {
			hasCommonRoot = false
			break
		}
	}

	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}

	totalFiles := len(r.File)
	for i, f := range r.File {
		// 报告进度 (逢 5% 更新一下)
		percent := int((float64(i) / float64(totalFiles)) * 100)
		if i%50 == 0 {
			progressCb(percent, fmt.Sprintf("正在解压文件 %d / %d...", i+1, totalFiles))
		}

		cleanName := filepath.ToSlash(f.Name)
		if hasCommonRoot {
			if cleanName == rootPrefix || cleanName == strings.TrimSuffix(rootPrefix, "/") {
				// 忽略外包装本层目录条目
				continue
			}
			cleanName = strings.TrimPrefix(cleanName, rootPrefix)
		}

		if cleanName == "" || cleanName == "/" {
			continue
		}

		fpath := filepath.Join(dest, filepath.FromSlash(cleanName))
		// 防止 Zip Slip 漏洞
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("非法文件路径: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("打开解压文件写入失败 %s: %v", fpath, err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("读取压缩包文件失败 %s: %v", f.Name, err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("写入文件流失败 %s: %v", fpath, err)
		}
	}

	progressCb(100, "解压完成！")
	return nil
}

func doConcurrentDownload(ctx context.Context, client *http.Client, targetUrl string, tempFile *os.File, sendEvent func(string, int, string)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetUrl, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return fmt.Errorf("HTTP状态码异常: %d", resp.StatusCode)
	}

	var totalSize int64 = resp.ContentLength
	supportRange := resp.StatusCode == http.StatusPartialContent

	if supportRange {
		cr := resp.Header.Get("Content-Range")
		if cr != "" {
			parts := strings.Split(cr, "/")
			if len(parts) == 2 {
				fmt.Sscanf(parts[1], "%d", &totalSize)
			}
		}
	}
	resp.Body.Close()

	if totalSize <= 0 || !supportRange {
		sendEvent("downloading", 0, "服务器不支持多线程，回退至单流下载...")
		return doSingleThreadDownload(ctx, client, targetUrl, tempFile, totalSize, sendEvent)
	}

	sendEvent("downloading", 0, fmt.Sprintf("支持多线程分片下载，总大小 %.2f MB", float64(totalSize)/1024/1024))

	if err := tempFile.Truncate(totalSize); err != nil {
		return err
	}

	numWorkers := 8
	chunkSize := totalSize / int64(numWorkers)

	var wg sync.WaitGroup
	var downloaded int64
	var mu sync.Mutex
	var lastTick time.Time
	var downloadErr error

	for i := 0; i < numWorkers; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == numWorkers-1 {
			end = totalSize - 1
		}

		wg.Add(1)
		go func(part int, start, end int64) {
			defer wg.Done()

			for retry := 0; retry < 3; retry++ {
				if ctx.Err() != nil {
					return
				}

				req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetUrl, nil)
				if err != nil {
					mu.Lock()
					if downloadErr == nil {
						downloadErr = err
					}
					mu.Unlock()
					return
				}
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
				pResp, err := client.Do(req)
				if err != nil {
					time.Sleep(2 * time.Second)
					continue
				}

				buf := make([]byte, 256*1024)
				var written int64

				for {
					if ctx.Err() != nil {
						pResp.Body.Close()
						return
					}
					n, rErr := pResp.Body.Read(buf)
					if n > 0 {
						tempFile.WriteAt(buf[:n], start+written)
						written += int64(n)

						mu.Lock()
						downloaded += int64(n)
						if time.Since(lastTick) > time.Second {
							percent := int((float64(downloaded) / float64(totalSize)) * 100)
							sendEvent("downloading", percent, fmt.Sprintf("并行下载中... %.2f MB / %.2f MB", float64(downloaded)/1024/1024, float64(totalSize)/1024/1024))
							lastTick = time.Now()
						}
						mu.Unlock()
					}
					if rErr == io.EOF {
						break
					}
					if rErr != nil {
						mu.Lock()
						if downloadErr == nil {
							downloadErr = rErr
						}
						mu.Unlock()
						pResp.Body.Close()
						return
					}
				}
				pResp.Body.Close()
				return
			}
		}(i, start, end)
	}

	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return downloadErr
}

func doSingleThreadDownload(ctx context.Context, client *http.Client, targetUrl string, tempFile *os.File, totalSize int64, sendEvent func(string, int, string)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetUrl, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP状态码异常: %d", resp.StatusCode)
	}

	var downloaded int64
	var lastTick time.Time

	pw := &coreDownloadWriter{
		writeFunc: func(p []byte) (n int, err error) {
			n, err = tempFile.Write(p)
			if n > 0 {
				downloaded += int64(n)
				if totalSize > 0 && time.Since(lastTick) > time.Second {
					percent := int((float64(downloaded) / float64(totalSize)) * 100)
					sendEvent("downloading", percent, fmt.Sprintf("单流下载中... %.2f MB / %.2f MB", float64(downloaded)/1024/1024, float64(totalSize)/1024/1024))
					lastTick = time.Now()
				}
			}
			return n, err
		},
		ctx: ctx,
	}

	buf := make([]byte, 1024*1024)
	_, err = io.CopyBuffer(pw, resp.Body, buf)
	return err
}
