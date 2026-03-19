# google Browser (googleAds)

一款专为跨境电商和社交媒体营销设计的指纹浏览器管理工具。支持多账号独立指纹、现代 Windows 内核模拟、高级隐匿补丁以及多窗口同步操作。

## 🚀 核心功能

- **高级隐匿补丁**：内置 Stealth 扩展，完美绕过 `navigator.webdriver` 检测，掩盖多种自动化指纹。
- **内核版本选择**：支持自定义选择 Chrome 内核版本（90 - 146），自动生成匹配的现代 Windows User-Agent。
- **数值智能排序**：环境列表支持 ID 数值排序及名称、状态等多维度排序。
- **多账号管理**：支持批量导入、编辑、备注、代理配置。
- **内置同步器**：支持主窗口与多子窗口间的鼠标、键盘操作同步。
- **代理支持**：支持 HTTP/HTTPS/SOCKS5 代理，并内置代理请求中转系统。

## 📦 安装与运行

### 1. 终端用户 (推荐)
1. 下载最新的 `googleAds.zip`。
2. 解压到本地文件夹。
3. 双击运行 `googleAds.exe` 即可启动，无需安装任何环境。

### 2. 开发者 (源码运行)
1. 克隆仓库：`git clone <repository_url>`
2. 安装依赖：
   ```bash
   pip install PyQt6 pygetwindow pynput pypiwin32 Faker PyInstaller PySocks requests
   ```
3. 启动程序：
   ```bash
   python main.py
   ```

## 🛠 开发与打包

如果你想自己修改代码并重新打包：
1. 修改相应的 `.py` 文件。
2. 运行打包脚本：
   ```bash
   python build_exe.py
   ```
3. 打包完成后，EXE 文件将出现在 `dist/` 目录下。

## 📝 注意事项
- 请确保电脑已安装 Chrome 浏览器。
- 软件产生的环境数据存放在 `browser_data` 文件夹中，数据库存放在 `profiles.db` 中。

---
*Powered by google Team*
