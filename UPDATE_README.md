# 自动升级（方案 A · GitHub Releases）

> 这是一个独立的副本，源代码已和原 `Ant-Browser-master` 解耦。
> 路径：`C:\Users\testuser\Desktop\Ant-Browser-update`

## 一次性配置

打开 `backend/app_updater.go`，在顶部把这两行改成你自己的 GitHub 仓库：

```go
const (
    githubOwner = "YOUR_GITHUB_USER"   // ← 改成你的 GitHub 用户名或组织名
    githubRepo  = "boost-browser"      // ← 改成你的仓库名
    ...
)
```

## 每次发版（3 步）

### 1. 改版本号

`wails.json` 里：

```json
"productVersion": "1.5.6"   // 从 1.5.5 升到 1.5.6
```

### 2. 一键打包

```powershell
cd C:\Users\testuser\Desktop\Ant-Browser-update
powershell -ExecutionPolicy Bypass -File scripts\build_release.ps1
```

完成后 `build\release\` 下会出现 **3 个文件**：

| 文件 | 说明 |
|---|---|
| `boost-browser.exe` | 主程序（约 38MB） |
| `boost-browser.exe.sha256` | 哈希文件，纯文本一行 |
| `updater.exe` | 替换主程序的小工具（约 2MB） |

### 3. 上传到 GitHub Releases

1. 打开 `https://github.com/<你的用户>/<仓库>/releases/new`
2. **Choose a tag** → 输入 `v1.5.6` → 选 **Create new tag: v1.5.6 on publish**
3. **Release title** 填 `v1.5.6`
4. **Description** 写更新内容（用户的升级弹窗会显示这段文本，支持中文）：
   ```
   - 修复网站连接扩展时登录请求弹窗过大的问题
   - 通用连接/登录请求弹窗自动收敛为标准扩展弹窗尺寸
   - 保持正常钱包官网页面不被误缩小
   ```
5. **Attach binaries** 区域：把 `build\release\` 下的 **3 个文件全部拖进去**
6. 点 **Publish release**

发完后，所有用户在下次启动 boost-browser（5 秒后）会收到弹窗提示升级。

## 客户端升级流程（自动）

```
用户启动 boost-browser
     ↓ 5 秒后
GET https://api.github.com/repos/{owner}/{repo}/releases/latest
     ↓ 比版本号
有新版 → 弹 Modal「发现新版本 1.5.6」+ 显示 release notes
     ↓ 用户点「立即更新」
流式下载 boost-browser.exe + 校验 SHA256（带进度条）
     ↓ 下载完成
弹「重启更新」按钮
     ↓ 用户点击
启动 updater.exe → 主程序退出
     ↓ updater.exe 干的事
1. 等主进程退出（最多 30s）
2. 备份旧 exe → boost-browser.exe.bak
3. 替换：新 exe → boost-browser.exe
4. 启动新版（带 --post-update 参数）
5. 等 30s 看新版是否写 .update_success 标记
   - 写了 → 删 .bak，升级成功
   - 没写 → 自动回滚 .bak，启动旧版
6. 自删 updater.exe
```

## 强制升级

在 GitHub Release Description 里加 `[force]` 三个字符（任何位置都行），客户端检测到后会去掉「稍后再说」按钮，用户必须升级。

```
[force]
紧急修复：高危 bug
- 修复登录态泄露
```

## 故障排查

### 升级失败排查日志

| 日志位置 | 内容 |
|---|---|
| `data\logs\app.log` | 主程序日志，搜 `Updater` 关键字 |
| `data\logs\updater.log` | updater 工具日志 |
| `boost-browser.exe.bak` | 升级失败时会保留，确认后可手动删 |

### 跳过 GitHub API 限流

GitHub 未授权 API 限流是 60次/小时/IP。如果你的用户量上千，会触发限流。届时可以：

- 改用 GitHub 镜像（jsDelivr 镜像 raw 文件）
- 或者改成腾讯云 COS / Cloudflare R2，参考方案 C

### 国内用户下载慢

GitHub Release 在国内速度通常 10-50 KB/s，38MB 文件大约要 15-30 分钟。建议：

- 在 release notes 里说明「升级耗时 15 分钟以上属于正常」
- 长期方案是迁到方案 C（GitHub Actions + 自建 CDN）

## 不会被触碰的文件

本副本对原 `Ant-Browser-master` 目录**零影响**，所有改动都在 `Ant-Browser-update` 里：

新增：
- `backend/app_updater.go`
- `backend/app_updater_windows.go`
- `backend/app_updater_other.go`
- `backend/cmd/updater/main.go`
- `frontend/src/modules/updater/UpdateChecker.tsx`
- `scripts/build_release.ps1`
- `UPDATE_README.md`（本文）

修改：
- `main.go` —— 加 `--post-update` 参数检测
- `frontend/src/App.tsx` —— 挂载 `<UpdateChecker />`
- `frontend/src/modules/settings/SettingsPage.tsx` —— 加「检查更新」按钮
