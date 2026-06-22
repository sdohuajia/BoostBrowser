package backend

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"boost-browser/backend/internal/logger"

	"golang.org/x/sys/windows"
)

// Windows 常量（badge 图标专用）
const (
	WM_SETICON      = 0x0080
	WM_GETICON      = 0x007F
	ICON_BIG        = 1
	ICON_SMALL      = 0
	IMAGE_ICON      = 1
	LR_LOADFROMFILE = 0x0010
	GCL_HICON       = uintptr(0xFFFFFFF2) // -14
	diNormal        = 3
	DIB_RGB_COLORS  = 0
	BI_RGB          = 0
)

// badgeIconFileCache 缓存已生成的 badge 图标文件路径（key 为显示序号，value 为 .ico 文件路径）
var badgeIconFileCache struct {
	sync.RWMutex
	data map[int]string // 序号 -> .ico 文件路径
}

func init() {
	badgeIconFileCache.data = make(map[int]string)
}

// ============================================================================
// GDI 图标提取
// ============================================================================

// getWindowIcon 获取窗口的 HICON，依次尝试 ICON_BIG、ICON_SMALL、GetClassLongPtr(GCL_HICON)
func getWindowIcon(hwnd windows.HWND) (uintptr, error) {
	// 尝试 WM_GETICON ICON_BIG
	ret, _, _ := procSendMessageW.Call(uintptr(hwnd), uintptr(WM_GETICON), uintptr(ICON_BIG), 0)
	if ret != 0 {
		return ret, nil
	}

	// 尝试 WM_GETICON ICON_SMALL
	ret, _, _ = procSendMessageW.Call(uintptr(hwnd), uintptr(WM_GETICON), uintptr(ICON_SMALL), 0)
	if ret != 0 {
		return ret, nil
	}

	// 尝试 GetClassLongPtr(GCL_HICON) 作为兜底
	ret, _, _ = procGetClassLongPtrW.Call(uintptr(hwnd), GCL_HICON)
	if ret != 0 {
		return ret, nil
	}

	return 0, fmt.Errorf("窗口无图标句柄")
}

// hiconToImage 将 HICON 转换为 Go image.NRGBA（64x64）
// 使用 GDI: CreateDIBSection + DrawIconEx + 读取像素数据，处理 BGRA 预乘alpha
func hiconToImage(hIcon uintptr) (*image.NRGBA, error) {
	const size = 64

	// 获取屏幕 DC
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer procReleaseDC.Call(0, screenDC)

	// 创建兼容内存 DC
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer procDeleteDC.Call(memDC)

	// 构造 BITMAPINFOHEADER（自顶向下，32位 BGRA）
	type bitmapInfoHeader struct {
		BiSize          uint32
		BiWidth         int32
		BiHeight        int32
		BiPlanes        uint16
		BiBitCount      uint16
		BiCompression   uint32
		BiSizeImage     uint32
		BiXPelsPerMeter int32
		BiYPelsPerMeter int32
		BiClrUsed       uint32
		BiClrImportant  uint32
	}

	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       size,
		BiHeight:      -size, // 负数表示自顶向下
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: BI_RGB,
	}

	// 创建 DIB Section
	var ppBits unsafe.Pointer
	hBitmap, _, _ := procCreateDIBSection.Call(
		memDC,
		uintptr(unsafe.Pointer(&bmi)),
		uintptr(DIB_RGB_COLORS),
		uintptr(unsafe.Pointer(&ppBits)),
		0, 0,
	)
	if hBitmap == 0 || ppBits == nil {
		return nil, fmt.Errorf("CreateDIBSection 失败")
	}
	defer procDeleteObject.Call(hBitmap)

	// 选入内存 DC
	oldBmp, _, _ := procSelectObject.Call(memDC, hBitmap)
	defer procSelectObject.Call(memDC, oldBmp)

	// 用 DrawIconEx 绘制图标到 DIB
	ret, _, _ := procDrawIconEx.Call(
		memDC,             // hdc
		0,                 // xLeft
		0,                 // yTop
		hIcon,             // hIcon
		uintptr(size),     // cxWidth
		uintptr(size),     // cyHeight
		0,                 // istepIfAni
		0,                 // hbrFlickerFreeDraw (NULL)
		uintptr(diNormal), // diFlags
	)
	if ret == 0 {
		return nil, fmt.Errorf("DrawIconEx 失败")
	}

	// 从 ppBits 读取像素数据（BGRA 预乘 alpha → NRGBA straight alpha）
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	pixelCount := size * size
	bgraData := unsafe.Slice((*byte)(ppBits), pixelCount*4)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			off := (y*size + x) * 4
			b := bgraData[off+0]
			g := bgraData[off+1]
			r := bgraData[off+2]
			a := bgraData[off+3]

			if a == 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
				continue
			}

			// 反预乘 alpha: R = min(R_premul * 255 / A, 255)
			rOut := uint8(min(uint16(r)*255/uint16(a), 255))
			gOut := uint8(min(uint16(g)*255/uint16(a), 255))
			bOut := uint8(min(uint16(b)*255/uint16(a), 255))

			img.SetNRGBA(x, y, color.NRGBA{R: rOut, G: gOut, B: bOut, A: a})
		}
	}

	return img, nil
}

// ============================================================================
// 图标绘制辅助
// ============================================================================

// drawCircle 在 img 上绘制填充圆（带抗锯齿）
func drawCircle(img *image.NRGBA, cx, cy, r int, col color.NRGBA) {
	for y := cy - r - 1; y <= cy+r+1; y++ {
		for x := cx - r - 1; x <= cx+r+1; x++ {
			if x < 0 || x >= img.Bounds().Dx() || y < 0 || y >= img.Bounds().Dy() {
				continue
			}
			dx := float64(x) - float64(cx)
			dy := float64(y) - float64(cy)
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist <= float64(r) {
				img.Set(x, y, col)
			} else if dist <= float64(r)+0.8 {
				// 抗锯齿
				alpha := 1.0 - (dist-float64(r))/0.8
				bg := img.NRGBAAt(x, y)
				ratio := alpha
				outR := uint8(float64(col.R)*ratio + float64(bg.R)*(1-ratio))
				outG := uint8(float64(col.G)*ratio + float64(bg.G)*(1-ratio))
				outB := uint8(float64(col.B)*ratio + float64(bg.B)*(1-ratio))
				outA := uint8(float64(col.A)*ratio + float64(bg.A)*(1-ratio))
				img.Set(x, y, color.NRGBA{R: outR, G: outG, B: outB, A: outA})
			}
		}
	}
}

// generateFallbackIcon 生成固定的旧版 Boost Browser 任务栏底图：蓝色 Chrome-like 圆环。
// 不读取 chrome.exe 自带图标，避免切到 Google Chrome 后变成官方四色 Chrome 图标。
func generateFallbackIcon() *image.NRGBA {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	cx, cy := 32.0, 32.0
	outerR := 27.0
	innerR := 13.0
	centerR := 8.5

	blend := func(dst, src color.NRGBA, alpha float64) color.NRGBA {
		if alpha < 0 {
			alpha = 0
		}
		if alpha > 1 {
			alpha = 1
		}
		return color.NRGBA{
			R: uint8(float64(src.R)*alpha + float64(dst.R)*(1-alpha)),
			G: uint8(float64(src.G)*alpha + float64(dst.G)*(1-alpha)),
			B: uint8(float64(src.B)*alpha + float64(dst.B)*(1-alpha)),
			A: uint8(float64(src.A)*alpha + float64(dst.A)*(1-alpha)),
		}
	}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > outerR+1 {
				continue
			}
			alpha := 1.0
			if dist > outerR {
				alpha = 1 - (dist - outerR)
			}

			angle := math.Atan2(dy, dx)
			if angle < 0 {
				angle += 2 * math.Pi
			}
			// 三段蓝色圆环，模拟旧版“蓝色浏览器”视觉，而不是单一云状圆点。
			base := color.NRGBA{R: 37, G: 99, B: 235, A: 255}
			switch {
			case angle < 2*math.Pi/3:
				base = color.NRGBA{R: 30, G: 105, B: 230, A: 255}
			case angle < 4*math.Pi/3:
				base = color.NRGBA{R: 23, G: 78, B: 180, A: 255}
			default:
				base = color.NRGBA{R: 74, G: 144, B: 245, A: 255}
			}
			// 从左上到右下的轻微高光，让任务栏小尺寸更接近原来的立体图标。
			highlight := (float64(size-x) + float64(size-y)) / float64(size*2)
			base.R = uint8(math.Min(255, float64(base.R)+28*highlight))
			base.G = uint8(math.Min(255, float64(base.G)+32*highlight))
			base.B = uint8(math.Min(255, float64(base.B)+36*highlight))

			if dist < innerR {
				// 内圈白色分隔环
				base = color.NRGBA{R: 245, G: 249, B: 255, A: 255}
			}
			if dist < centerR {
				// 中心蓝点
				t := dist / centerR
				base = color.NRGBA{R: uint8(82 - 22*t), G: uint8(164 - 34*t), B: 255, A: 255}
			}
			img.SetNRGBA(x, y, blend(img.NRGBAAt(x, y), base, alpha))
		}
	}
	return img
}

// overlayBadgeNumber 在图标上叠加编号。
// 旧实现固定右上角圆形 badge + 3x3 字体，多位数会被圆形裁掉，任务栏缩放后经常只剩红点看不到数字。
// 新实现按位数自适应为右上角红色胶囊，1~4 位都尽量完整显示。
func overlayBadgeNumber(img *image.NRGBA, number int) *image.NRGBA {
	const size = 64

	numStr := fmt.Sprintf("%d", number)
	if len(numStr) > 4 {
		numStr = numStr[len(numStr)-4:]
	}
	digits := map[byte][]string{
		'0': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
		'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
		'2': {"01110", "10001", "00001", "00110", "01000", "10000", "11111"},
		'3': {"01110", "10001", "00001", "00110", "00001", "10001", "01110"},
		'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
		'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
		'6': {"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
		'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
		'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
		'9': {"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
	}

	pixel := 3
	if len(numStr) >= 3 {
		pixel = 2
	}
	fontW := 5
	fontH := 7
	gap := 1
	totalFontWidth := len(numStr)*fontW*pixel + (len(numStr)-1)*gap*pixel
	totalFontHeight := fontH * pixel

	padX := 5
	padY := 4
	pillW := totalFontWidth + padX*2
	pillH := totalFontHeight + padY*2
	if pillW < pillH {
		pillW = pillH
	}
	pillX := size - pillW - 2
	if pillX < 1 {
		pillX = 1
	}
	pillY := 2
	radius := pillH / 2

	// 白色描边 + 红色底，做成胶囊而不是固定圆，避免 2/3/4 位数字被裁剪。
	drawRoundedPill(img, pillX-2, pillY-2, pillW+4, pillH+4, radius+2, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	drawRoundedPill(img, pillX, pillY, pillW, pillH, radius, color.NRGBA{R: 220, G: 38, B: 38, A: 255})

	drawStartX := pillX + (pillW-totalFontWidth)/2
	drawStartY := pillY + (pillH-totalFontHeight)/2
	curX := drawStartX
	for _, ch := range []byte(numStr) {
		font, ok := digits[ch]
		if !ok {
			continue
		}
		for row, line := range font {
			for col, c := range line {
				if c != '1' {
					continue
				}
				for py := 0; py < pixel; py++ {
					for px := 0; px < pixel; px++ {
						ix := curX + col*pixel + px
						iy := drawStartY + row*pixel + py
						if ix >= 0 && ix < size && iy >= 0 && iy < size {
							img.Set(ix, iy, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
						}
					}
				}
			}
		}
		curX += fontW*pixel + gap*pixel
	}

	return img
}

func drawRoundedPill(img *image.NRGBA, x, y, w, h, r int, col color.NRGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	if r > h/2 {
		r = h / 2
	}
	for yy := y; yy < y+h; yy++ {
		for xx := x + r; xx < x+w-r; xx++ {
			if xx >= 0 && xx < img.Bounds().Dx() && yy >= 0 && yy < img.Bounds().Dy() {
				img.Set(xx, yy, col)
			}
		}
	}
	drawCircle(img, x+r, y+r, r, col)
	drawCircle(img, x+w-r-1, y+r, r, col)
}

// generateBadgeIconImage 生成固定的 Boost Browser 蓝色浏览器图标并叠加红色编号角标。
// 不再使用 Chrome 窗口自身图标作为底图：切到系统最新版 Chrome 后，Chrome 会暴露
// 多色官方图标；用户要求任务栏仍保持之前的蓝色 Chrome 类图标 + 红色编号 badge 样式。
func generateBadgeIconImage(pid int, number int) *image.NRGBA {
	baseImg := generateFallbackIcon()
	overlayBadgeNumber(baseImg, number)
	return baseImg
}

// ============================================================================
// ICO 文件生成
// ============================================================================

// scaleImage 将图像缩放到指定尺寸（最近邻插值）
func scaleImage(src *image.NRGBA, dstSize int) *image.NRGBA {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstSize, dstSize))

	for dy := 0; dy < dstSize; dy++ {
		for dx := 0; dx < dstSize; dx++ {
			sx := dx * srcW / dstSize
			sy := dy * srcH / dstSize
			dst.Set(dx, dy, src.NRGBAAt(sx, sy))
		}
	}
	return dst
}

// generateBadgeICO 生成带编号的 ICO 文件字节数据（包含 16x16 和 32x32 两种尺寸）。
func generateBadgeICO(pid int, number int) ([]byte, error) {
	// 生成 64x64 原图（提取进程图标 + 叠加角标）
	srcImg := generateBadgeIconImage(pid, number)

	// 缩放到 16x16 和 32x32
	img16 := scaleImage(srcImg, 16)
	img32 := scaleImage(srcImg, 32)

	// 编码为 PNG
	var buf16, buf32 bytes.Buffer
	if err := png.Encode(&buf16, img16); err != nil {
		return nil, err
	}
	if err := png.Encode(&buf32, img32); err != nil {
		return nil, err
	}

	png16Data := buf16.Bytes()
	png32Data := buf32.Bytes()

	// ICO 文件格式
	// 参考：https://en.wikipedia.org/wiki/ICO_(file_format)
	type icoDirEntry struct {
		Width       uint8
		Height      uint8
		ColorCount  uint8
		Reserved    uint8
		Planes      uint16
		BitCount    uint16
		BytesInRes  uint32
		ImageOffset uint32
	}

	type icoDirHeader struct {
		Reserved  uint16
		ImageType uint16
		NumImages uint16
	}

	header := icoDirHeader{
		Reserved:  0,
		ImageType: 1, // ICON
		NumImages: 2,
	}

	entry16 := icoDirEntry{
		Width:       16,
		Height:      16,
		ColorCount:  0,
		Reserved:    0,
		Planes:      1,
		BitCount:    32,
		BytesInRes:  uint32(len(png16Data)),
		ImageOffset: uint32(binary.Size(header) + binary.Size(icoDirEntry{})*2),
	}
	entry32 := icoDirEntry{
		Width:       32,
		Height:      32,
		ColorCount:  0,
		Reserved:    0,
		Planes:      1,
		BitCount:    32,
		BytesInRes:  uint32(len(png32Data)),
		ImageOffset: entry16.ImageOffset + uint32(len(png16Data)),
	}

	var result bytes.Buffer
	_ = binary.Write(&result, binary.LittleEndian, header)
	_ = binary.Write(&result, binary.LittleEndian, entry16)
	_ = binary.Write(&result, binary.LittleEndian, entry32)
	result.Write(png16Data)
	result.Write(png32Data)

	return result.Bytes(), nil
}

// getBadgeICOFilePath 获取带编号的 ICO 文件路径（带缓存，key 包含 pid）。
// 文件保存在临时目录下的 badge_icons 子目录中。
func getBadgeICOFilePath(pid int, number int) (string, error) {
	cacheKey := number // 缓存 key 仍按编号（同一编号图标相同即可复用）

	badgeIconFileCache.RLock()
	if path, ok := badgeIconFileCache.data[cacheKey]; ok {
		badgeIconFileCache.RUnlock()
		// 检查文件是否仍然存在
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		// 文件不存在，需要重新生成
	} else {
		badgeIconFileCache.RUnlock()
	}

	icoData, err := generateBadgeICO(pid, number)
	if err != nil {
		return "", err
	}

	// 保存到临时目录
	tmpDir := filepath.Join(os.TempDir(), "boost_browser_badge_icons")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", err
	}
	icoPath := filepath.Join(tmpDir, fmt.Sprintf("badge_%d.ico", number))
	if err := os.WriteFile(icoPath, icoData, 0644); err != nil {
		return "", err
	}

	badgeIconFileCache.Lock()
	badgeIconFileCache.data[cacheKey] = icoPath
	badgeIconFileCache.Unlock()

	return icoPath, nil
}

// ============================================================================
// 图标设置（Windows API）
// ============================================================================

// loadIconFromFile 从文件加载图标句柄
func loadIconFromFile(icoPath string, size int) (windows.HWND, error) {
	pathPtr, _ := windows.UTF16PtrFromString(icoPath)

	handle, _, err := procLoadImageW.Call(
		0,                                // hInstance (NULL)
		uintptr(unsafe.Pointer(pathPtr)), // lpszName
		uintptr(IMAGE_ICON),              // uType
		uintptr(size),                    // cxDesired
		uintptr(size),                    // cyDesired
		uintptr(LR_LOADFROMFILE),         // fuLoad
	)
	if handle == 0 {
		return 0, fmt.Errorf("LoadImage 失败: %v (path=%s)", err, icoPath)
	}
	return windows.HWND(handle), nil
}

// setWindowIcon 为指定进程的窗口设置自定义图标。
func setWindowIcon(pid int, icoPath string) error {
	hwnd, err := findProcessWindow(pid)
	if err != nil {
		return fmt.Errorf("查找窗口失败: %v", err)
	}

	// 任务栏在部分 DPI/缩放设置下会优先使用 ICON_SMALL。
	// 旧逻辑给 ICON_SMALL 加载 16x16，右上角数字缩放后容易只剩红色底、不见白字。
	// 这里小/大图标都加载 32x32，让 Explorer 需要小图标时自己缩放，数字保真度更高。
	smallIcon, err := loadIconFromFile(icoPath, 32)
	if err != nil {
		return fmt.Errorf("加载小图标失败: %v", err)
	}

	// 加载 32x32 大图标（任务栏大图标 / Alt+Tab）
	bigIcon, err := loadIconFromFile(icoPath, 32)
	if err != nil {
		return fmt.Errorf("加载大图标失败: %v", err)
	}

	// 设置图标
	procSendMessageW.Call(uintptr(hwnd), uintptr(WM_SETICON), uintptr(ICON_SMALL), uintptr(smallIcon))
	procSendMessageW.Call(uintptr(hwnd), uintptr(WM_SETICON), uintptr(ICON_BIG), uintptr(bigIcon))

	return nil
}

// setBadgeForInstance 为指定进程设置带编号的任务栏图标。
// pid 为 Chromium/Chrome 主进程的进程 ID，displayNumber 为显示序号。
// 通过 Windows GDI 从窗口提取原始浏览器图标，叠加红色编号角标后设置到任务栏。
//
// 注意：这里故意只做“启动阶段有限重试”，不再保留长期 badge watchdog。
// 线上 crashprobe 已证明主程序闪退的首个稳定止血点是关闭常驻托盘；而旧的
// badge watchdog 还会为每个实例常驻一个 Win32/GDI 刷新循环，风险会随着实例数
// 量叠加。用户当前明确要求把实例数字加回来，因此这里先恢复一次性 badge 设置：
// 在窗口刚出现时重试若干次，成功后立即退出，避免后台无限循环再次放大宿主风
// 险。后续若确认需要“数字长期自愈”，再单独做隔离实现。
func setBadgeForInstance(pid int, displayNumber int) error {
	log := logger.New("BadgeIcon")
	icoPath, err := getBadgeICOFilePath(pid, displayNumber)
	if err != nil {
		log.Warn("badge ICO 文件生成失败", logger.F("error", err.Error()))
		return err
	}

	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		if !isProcessAlive(pid) {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("浏览器进程已退出: pid=%d", pid)
		}

		err = setWindowIcon(pid, icoPath)
		if err == nil {
			log.Info("任务栏 badge 图标设置成功",
				logger.F("pid", pid),
				logger.F("display_number", displayNumber),
				logger.F("attempt", attempt+1),
			)
			return nil
		}

		lastErr = err
		if attempt < 29 {
			time.Sleep(1 * time.Second)
		}
	}

	log.Warn("任务栏 badge 图标启动阶段设置失败（已放弃重试）",
		logger.F("pid", pid),
		logger.F("display_number", displayNumber),
		logger.F("error", lastErr.Error()),
	)
	return lastErr
}
