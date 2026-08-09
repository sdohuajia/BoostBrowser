# v2.0.3

- Improve multi-instance window synchronization reliability for web navigation.
- Select the visible CDP tab instead of relying on unstable target-list ordering.
- Retry failed follower navigation and only mark a URL as synchronized after every follower accepts it.
- Navigation recovery is enabled by default for HTTP/HTTPS pages; set `BOOST_BROWSER_ENABLE_SYNC_URL_SYNC=0` to disable it.
- This is a normal update, not a forced update.

# v2.0.2

- Hide the PowerShell console used by the OKX UI Automation fallback.
- Match the OKX window to the active browser instance process.
- Unlock the OKX `#/unlock` page before wallet-home verification.
- Automatically close the instance only after wallet-home verification succeeds.
- This is a normal update, not a forced update.

# v2.0.1

- OKX Wallet 批量导入在钱包主页校验成功后，自动关闭对应浏览器实例。
- 若实例关闭失败，任务结果会显示 `close_failed`，避免将实例仍运行的情况误报为完整成功。
- 这是普通更新，不是强制更新。

# v2.0.0

- 新增 OKX Wallet 批量导入：按实例顺序导入助记词，自动处理助记词确认、钱包密码设置与钱包主页状态校验。
- 支持 OKX SES 子页面和 Windows UI Automation 回退，兼容多语言确认按钮与钱包弹窗。
- 新增 OKX 钱包批量导入页面，并完善扩展分配与实例运行状态处理。
- 这是普通更新，不是强制更新。

# v1.9.0

- 升级窗口同步浮窗：启动时位于屏幕顶部中央，支持拖动；展开/收起不会再把窗口跳回固定位置。
- 同步面板会恢复并显示当前正在运行的 Boost Browser 环境；HubSDK 测试环境继续使用 19877 本地服务的已验证实例发现链路。
- 同步中的顶部控制条会显示正在同步的环境数量，保留停止同步与窗口排列快捷操作。
- 修复同步浮窗被非 Boost Browser 启动器（例如 Python 启动器）重复拉起时出现空白控制台的问题。
- 修复普通网页窗口被误判为扩展弹窗、被缩放到 390×620 的问题；扩展授权/请求弹窗的尺寸处理仍保留。
- 这是普通更新。

# v1.8.0

- 修复正常网页窗口（例如 X 页面）被误判为扩展弹窗，导致自动缩小到 390×620 并反复回到固定位置的问题。
- 保留钱包/扩展请求弹窗尺寸修正，但仅对明确的扩展/请求/授权类窗口生效，避免误伤普通网页。
- 这是普通更新，不是强制更新。

# v1.7.8

- 修复旧实例在 Chrome 应用商店仍显示“切换到 Chrome”导致无法添加扩展的问题。
- 兼容新版 Chrome Web Store 的“添加至 Chrome”按钮结构，确保 Boost helper 可接管安装按钮。
- 清理实例-1 残留的 Linux 指纹参数，改为 Windows Chrome 指纹并保留原有 Profile/扩展数据。
- 这是普通更新。

# v1.7.6

- 修复窗口同步面板在多开实例后显示“运行中 0 · 共 0”的问题：同步面板现在按实例 user-data-dir 与调试端口恢复运行态，不再误依赖内核进程必须位于主程序 chrome 目录。
- 降低同步面板轮询 Windows 进程时的并发扫描压力，避免多开实例后 PowerShell/CIM 扫描堆积导致面板卡顿或崩溃感。
- 这是普通更新。
