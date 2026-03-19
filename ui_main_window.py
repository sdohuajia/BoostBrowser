import os
import subprocess
import threading
import json
import sqlite3
import re
import database
import win32gui
import win32process
import pygetwindow as gw
from PyQt6.QtWidgets import (
    QMainWindow, QWidget, QVBoxLayout, QHBoxLayout, 
    QPushButton, QLabel, QTableWidget, QTableWidgetItem, 
    QHeaderView, QStackedWidget, QFrame, QMessageBox,
    QCheckBox, QFormLayout, QLineEdit, QRadioButton, QApplication,
    QSystemTrayIcon, QMenu, QGroupBox, QButtonGroup
)
from PyQt6.QtCore import Qt, QSize, QTimer, QMetaObject, Q_ARG, pyqtSlot
from PyQt6.QtGui import QIcon, QFont, QAction, QCloseEvent
import ctypes
import sys
try:
    # Set AppID for Windows taskbar icon grouping support
    myappid = 'antigravity.browser.ads.1.0'
    ctypes.windll.shell32.SetCurrentProcessExplicitAppUserModelID(myappid)
except: pass

def resource_path(relative_path):
    """ Get absolute path to resource, works for dev and for PyInstaller """
    try:
        # PyInstaller creates a temp folder and stores path in _MEIPASS
        base_path = sys._MEIPASS
    except Exception:
        base_path = os.path.abspath(".")
    return os.path.join(base_path, relative_path)
from PyQt6.QtGui import QIcon, QFont
from ui_profile_dialog import ProfileDialog
from ui_proxy_dialog import ProxyDialog, TestThread
from browser_launcher import browser_controller
from input_syncer import InputSyncer
from ui_sync_status import SyncStatusWindow

class MainWindow(QMainWindow):
    def __init__(self):
        super().__init__()
        self.setWindowTitle("AntigravityAds - 专业多开代理防关联浏览器")
        self.resize(1100, 700)
        self.syncer = InputSyncer()
        self.status_window = None
        
        self.config_file = os.path.join(os.path.dirname(database.get_db_path()), 'config.json')
        self.settings = self.load_settings()
        self.is_force_quit = False
        
        self.setup_ui()
        self.setup_tray()
        self.load_data()

        # Timer to update browser statuses
        self.status_timer = QTimer(self)
        self.status_timer.timeout.connect(self.update_statuses)
        self.status_timer.start(2000)

    def load_settings(self):
        default_settings = {"close_behavior": "minimize"}
        if os.path.exists(self.config_file):
            try:
                with open(self.config_file, 'r', encoding='utf-8') as f:
                    return {**default_settings, **json.load(f)}
            except Exception: pass
        return default_settings
        
    def save_settings(self):
        try:
            with open(self.config_file, 'w', encoding='utf-8') as f:
                json.dump(self.settings, f, indent=4)
        except Exception: pass

    def setup_tray(self):
        self.tray_icon = QSystemTrayIcon(self)
        self.tray_icon.setIcon(QIcon(resource_path("app.png")))
        tray_menu = QMenu()
        show_action = QAction("显示主界面", self)
        show_action.triggered.connect(self.showNormal)
        quit_action = QAction("完全退出", self)
        quit_action.triggered.connect(self.force_quit)
        tray_menu.addAction(show_action)
        tray_menu.addAction(quit_action)
        self.tray_icon.setContextMenu(tray_menu)
        self.tray_icon.activated.connect(self.tray_activated)
        self.tray_icon.show()
        
    def tray_activated(self, reason):
        if reason == QSystemTrayIcon.ActivationReason.DoubleClick:
            self.showNormal()
            self.activateWindow()

    def force_quit(self):
        self.is_force_quit = True
        self.close()

    def closeEvent(self, event: QCloseEvent):
        behavior = self.settings.get("close_behavior", "minimize")
        
        if self.is_force_quit or behavior != "minimize":
            if behavior == "quit_and_close_browsers" or (self.is_force_quit and behavior == "quit_and_close_browsers"):
                self.action_close_all_running()
            event.accept()
        else:
            event.ignore()
            self.hide()
            self.tray_icon.showMessage(
                "AntigravityAds",
                "程序已最小化到托盘，双击图标恢复显示。",
                QSystemTrayIcon.MessageIcon.Information,
                2000
            )

    def setup_ui(self):
        self.setWindowIcon(QIcon(resource_path("app.png")))
        main_widget = QWidget()
        self.setCentralWidget(main_widget)
        main_layout = QHBoxLayout(main_widget)
        main_layout.setContentsMargins(0, 0, 0, 0)
        main_layout.setSpacing(0)

        # --- Sidebar ---
        sidebar = QFrame()
        sidebar.setFixedWidth(200)
        sidebar.setObjectName("Sidebar")
        sidebar_layout = QVBoxLayout(sidebar)
        
        logo_label = QLabel("🌌 AntigravityAds")
        logo_label.setObjectName("LogoLabel")
        logo_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        sidebar_layout.addWidget(logo_label)
        sidebar_layout.addSpacing(20)

        # Navigation buttons
        self.btn_profiles = self.create_nav_btn("🌐 环境管理", 0)
        self.btn_profiles.setObjectName("SidebarBtnActive")
        
        self.btn_proxies = self.create_nav_btn("🛡️ 代理管理", 1)
        self.btn_sync = self.create_nav_btn("🔄 同步器", 2)
        self.btn_settings = self.create_nav_btn("⚙️ 系统设置", 3)
        
        self.nav_buttons = [self.btn_profiles, self.btn_proxies, self.btn_sync, self.btn_settings]
        
        for btn in self.nav_buttons:
            sidebar_layout.addWidget(btn)
            
        sidebar_layout.addStretch()

        # --- QStackedWidget ---
        self.stack = QStackedWidget()
        self.stack.addWidget(self.create_profile_page())
        self.stack.addWidget(self.create_proxy_page())
        self.stack.addWidget(self.create_sync_page())
        self.stack.addWidget(self.create_system_page())

        main_layout.addWidget(sidebar)
        main_layout.addWidget(self.stack)

        # Style
        self.setStyleSheet("""
            #Sidebar { background-color: #1e1e2e; border-right: 1px solid #313244; }
            #LogoLabel { color: #cba6f7; font-size: 18px; font-weight: bold; padding: 20px; }
            QPushButton#SidebarBtn, QPushButton#SidebarBtnActive {
                background-color: transparent; color: #a6adc8; border: none;
                padding: 12px 20px; text-align: left; font-size: 14px; border-radius: 8px; margin: 2px 10px;
            }
            QPushButton#SidebarBtn:hover { background-color: #313244; color: #f5e0dc; }
            QPushButton#SidebarBtnActive { background-color: #89b4fa; color: #11111b; font-weight: bold; }
            
            #PageHeader { font-size: 22px; font-weight: bold; color: #cdd6f4; margin-bottom: 10px; }
            #PrimaryBtn { background-color: #89b4fa; color: #11111b; border-radius: 5px; padding: 8px 15px; font-weight: bold; }
            #PrimaryBtn:hover { background-color: #b4befe; }
            
            #ActionBtn { background-color: #313244; color: #cdd6f4; border: 1px solid #45475a; border-radius: 4px; padding: 4px 10px; }
            #ActionBtn:hover { background-color: #45475a; }
            #ActionBtnLaunch { background-color: #a6e3a1; color: #11111b; border-radius: 4px; padding: 4px 10px; font-weight: bold; }
            #ActionBtnClose { background-color: #f38ba8; color: #11111b; border-radius: 4px; padding: 4px 10px; font-weight: bold; }
            #ActionBtnDanger { background-color: transparent; color: #f38ba8; border: 1px solid #f38ba8; border-radius: 4px; padding: 4px 10px; }
            #ActionBtnDanger:hover { background-color: #f38ba8; color: #11111b; }
            
            QTableWidget { background-color: #181825; color: #cdd6f4; gridline-color: #313244; border: none; }
            QHeaderView::section { background-color: #1e1e2e; color: #bac2de; padding: 8px; border: none; border-bottom: 2px solid #313244; }
        """)

    def create_nav_btn(self, text, index):
        btn = QPushButton(text)
        btn.setObjectName("SidebarBtn")
        btn.setCursor(Qt.CursorShape.PointingHandCursor)
        btn.clicked.connect(lambda: self.switch_page(index))
        return btn

    def switch_page(self, index):
        self.stack.setCurrentIndex(index)
        for i, btn in enumerate(self.nav_buttons):
            btn.setObjectName("SidebarBtnActive" if i == index else "SidebarBtn")
            btn.style().unpolish(btn)
            btn.style().polish(btn)

    def create_profile_page(self):
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(20, 20, 20, 20)
        
        header_layout = QHBoxLayout()
        header = QLabel("环境管理 (Profiles)")
        header.setObjectName("PageHeader")
        header_layout.addWidget(header)
        header_layout.addStretch()
        
        btn_add = QPushButton("+ 新建环境")
        btn_add.setObjectName("PrimaryBtn")
        btn_add.clicked.connect(self.action_new_profile)
        header_layout.addWidget(btn_add)
        layout.addLayout(header_layout)
        
        self.table = QTableWidget()
        self.table.setColumnCount(6)
        self.table.setHorizontalHeaderLabels(["ID", "名称", "代理", "备注", "状态", "操作"])
        self.table.horizontalHeader().setSectionResizeMode(QHeaderView.ResizeMode.Stretch)
        self.table.horizontalHeader().setSectionResizeMode(0, QHeaderView.ResizeMode.ResizeToContents)
        self.table.verticalHeader().setVisible(False)
        self.table.verticalHeader().setDefaultSectionSize(50)
        self.table.setSortingEnabled(True) # Enable sorting
        layout.addWidget(self.table)
        return page

    def create_proxy_page(self):
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(20, 20, 20, 20)
        
        header_layout = QHBoxLayout()
        header = QLabel("代理管理 (Proxies)")
        header.setObjectName("PageHeader")
        header_layout.addWidget(header)
        header_layout.addStretch()
        
        btn_bulk = QPushButton("批量导入")
        btn_bulk.setObjectName("ActionBtn")
        btn_bulk.clicked.connect(self.action_bulk_proxy)
        header_layout.addWidget(btn_bulk)
        
        btn_add = QPushButton("+ 添加代理")
        btn_add.setObjectName("PrimaryBtn")
        btn_add.clicked.connect(self.action_new_proxy)
        header_layout.addWidget(btn_add)
        layout.addLayout(header_layout)
        
        self.proxy_table = QTableWidget()
        self.proxy_table.setColumnCount(7)
        self.proxy_table.setHorizontalHeaderLabels(["ID", "类型", "地址", "国家/地区", "状态", "备注", "操作"])
        self.proxy_table.horizontalHeader().setSectionResizeMode(QHeaderView.ResizeMode.Stretch)
        self.proxy_table.verticalHeader().setVisible(False)
        self.proxy_table.setSortingEnabled(True)
        layout.addWidget(self.proxy_table)
        return page

    def create_sync_page(self):
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(20, 20, 20, 20)
        
        header = QLabel("同步器 (Synchronizer)")
        header.setObjectName("PageHeader")
        layout.addWidget(header)
        
        controls = QHBoxLayout()
        btn_tile = QPushButton("一键平铺 (Tile)")
        btn_tile.setObjectName("ActionBtn")
        self.btn_tile = QPushButton("一键平铺 (Tile)")
        self.btn_tile.setObjectName("ActionBtn")
        self.btn_tile.clicked.connect(self.action_tile_windows)
        
        self.btn_close_all = QPushButton("一键关闭选定")
        self.btn_close_all.setObjectName("ActionBtnDanger")
        self.btn_close_all.clicked.connect(self.action_close_all_running)
        
        self.btn_refresh = QPushButton("刷新列表")
        self.btn_refresh.setObjectName("ActionBtn")
        self.btn_refresh.clicked.connect(self.load_sync_data)
        
        self.btn_start_sync = QPushButton("开启同步控制")
        self.btn_start_sync.setObjectName("PrimaryBtn")
        self.btn_start_sync.clicked.connect(self.action_start_input_sync)
        
        controls.addWidget(self.btn_tile)
        controls.addWidget(self.btn_close_all)
        controls.addWidget(self.btn_refresh)
        controls.addWidget(self.btn_start_sync)
        controls.addStretch()
        layout.addLayout(controls)
        
        # --- Extension Install Area ---
        ext_frame = QFrame()
        ext_frame.setObjectName("ExtFrame")
        ext_layout = QHBoxLayout(ext_frame)
        ext_layout.setContentsMargins(10, 8, 10, 8)
        ext_layout.setSpacing(8)
        
        ext_icon = QLabel("🧩")
        ext_icon.setFixedWidth(24)
        ext_layout.addWidget(ext_icon)
        
        ext_label = QLabel("插件地址:")
        ext_label.setStyleSheet("color: #cdd6f4; font-size: 13px;")
        ext_label.setFixedWidth(60)
        ext_layout.addWidget(ext_label)
        
        self.ext_url_input = QLineEdit()
        self.ext_url_input.setPlaceholderText("粘贴 Chrome 商店地址，例如: https://chromewebstore.google.com/detail/xxx/mcohilncbfahbmgdjkbpemcciiolgcge")
        self.ext_url_input.setStyleSheet(
            "QLineEdit { background-color: #313244; color: #cdd6f4; border: 1px solid #45475a; "
            "border-radius: 4px; padding: 6px 10px; font-size: 13px; }"
            "QLineEdit:focus { border-color: #89b4fa; }"
        )
        ext_layout.addWidget(self.ext_url_input)
        
        btn_install_ext = QPushButton("🚀 批量安装插件")
        btn_install_ext.setObjectName("PrimaryBtn")
        btn_install_ext.setFixedWidth(130)
        btn_install_ext.clicked.connect(self.action_install_extension)
        ext_layout.addWidget(btn_install_ext)
        
        ext_frame.setStyleSheet(
            "#ExtFrame { background-color: #1e1e2e; border: 1px solid #313244; border-radius: 6px; margin-top: 4px; }"
        )
        layout.addWidget(ext_frame)
        
        self.sync_table = QTableWidget()
        self.sync_table.setColumnCount(5)
        self.sync_table.setHorizontalHeaderLabels(["跟随", "ID", "名称", "状态", "主控"])
        self.sync_table.horizontalHeader().setSectionResizeMode(QHeaderView.ResizeMode.Stretch)
        self.sync_table.horizontalHeader().setSectionResizeMode(0, QHeaderView.ResizeMode.Fixed)
        self.sync_table.horizontalHeader().setSectionResizeMode(4, QHeaderView.ResizeMode.Fixed)
        self.sync_table.setColumnWidth(0, 50)
        self.sync_table.setColumnWidth(4, 50)
        self.sync_table.verticalHeader().setVisible(False)
        layout.addWidget(self.sync_table)
        
        self.sync_timer = QTimer(self)
        self.sync_timer.timeout.connect(self.load_sync_data)
        self.sync_timer.start(5000)
        
        return page

    def create_system_page(self):
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(20, 20, 20, 20)
        header = QLabel("系统设置")
        header.setObjectName("PageHeader")
        layout.addWidget(header)
        
        # --- Close Behavior Group ---
        group_box = QGroupBox("关闭主窗口(X)时的行为:")
        group_box.setStyleSheet(
            "QGroupBox { color: #cdd6f4; font-size: 14px; font-weight: bold; border: 1px solid #313244; border-radius: 6px; margin-top: 10px; } "
            "QGroupBox::title { subcontrol-origin: margin; left: 10px; padding: 0 5px 0 5px; } "
            "QRadioButton { color: #bac2de; font-weight: normal; margin-top: 5px; margin-bottom: 5px; }"
        )
        group_layout = QVBoxLayout(group_box)
        group_layout.setSpacing(10)
        group_layout.setContentsMargins(15, 25, 15, 15)
        
        self.behavior_group = QButtonGroup(page)
        
        rb_minimize = QRadioButton("最小化到系统托盘（默认，保持后台运行）")
        rb_quit = QRadioButton("直接退出程序（可能会有浏览器仍留在后台运行）")
        rb_quit_all = QRadioButton("退出程序并强制关闭所有关联浏览器（安全推荐）")
        
        self.behavior_group.addButton(rb_minimize, 1)
        self.behavior_group.addButton(rb_quit, 2)
        self.behavior_group.addButton(rb_quit_all, 3)
        
        group_layout.addWidget(rb_minimize)
        group_layout.addWidget(rb_quit)
        group_layout.addWidget(rb_quit_all)
        
        current_beh = self.settings.get("close_behavior", "minimize")
        if current_beh == "minimize": rb_minimize.setChecked(True)
        elif current_beh == "quit": rb_quit.setChecked(True)
        elif current_beh == "quit_and_close_browsers": rb_quit_all.setChecked(True)
        
        self.behavior_group.idToggled.connect(self._on_close_behavior_changed)
        
        layout.addWidget(group_box)
        layout.addStretch()
        return page

    def _on_close_behavior_changed(self, id, checked):
        if not checked: return
        behavior = "minimize"
        if id == 2: behavior = "quit"
        elif id == 3: behavior = "quit_and_close_browsers"
        self.settings["close_behavior"] = behavior
        self.save_settings()

    def load_data(self):
        # 1. Profiles
        self.table.setSortingEnabled(False) # Disable while loading
        self.profiles = database.get_all_profiles()
        self.table.setRowCount(len(self.profiles))
        for row, p in enumerate(self.profiles):
            id_item = QTableWidgetItem()
            id_item.setData(Qt.ItemDataRole.DisplayRole, p['id']) # Numeric sort
            id_item.setTextAlignment(Qt.AlignmentFlag.AlignCenter)
            self.table.setItem(row, 0, id_item)
            self.table.setItem(row, 1, QTableWidgetItem(p['name']))
            self.table.setItem(row, 2, QTableWidgetItem(p['proxy'] or "直连"))
            self.table.setItem(row, 3, QTableWidgetItem(p['notes'] or ""))
            
            is_running = browser_controller.is_running(p['id'])
            status_item = QTableWidgetItem("运行中 🟢" if is_running else "已关闭 🔴")
            status_item.setTextAlignment(Qt.AlignmentFlag.AlignCenter)
            self.table.setItem(row, 4, status_item)
            
            widget = QWidget()
            layout = QHBoxLayout(widget)
            layout.setContentsMargins(2, 2, 2, 2)
            
            btn_launch = QPushButton("关闭" if is_running else "启动")
            btn_launch.setObjectName("ActionBtnClose" if is_running else "ActionBtnLaunch")
            btn_launch.clicked.connect(lambda _, profile=p: self.action_toggle_launch(profile))
            
            btn_edit = QPushButton("编辑")
            btn_edit.setObjectName("ActionBtn")
            btn_edit.clicked.connect(lambda _, profile=p: self.action_edit_profile(profile))
            
            btn_del = QPushButton("删除")
            btn_del.setObjectName("ActionBtnDanger")
            btn_del.clicked.connect(lambda _, profile=p: self.action_delete_profile(profile))
            
            layout.addWidget(btn_launch)
            layout.addWidget(btn_edit)
            layout.addWidget(btn_del)
            self.table.setCellWidget(row, 5, widget)
        
        self.table.setSortingEnabled(True) # Re-enable

        # 2. Proxies
        self.proxy_table.setSortingEnabled(False)
        self.proxies = database.get_all_proxies()
        self.proxy_table.setRowCount(len(self.proxies))
        for row, p in enumerate(self.proxies):
            self.proxy_table.setItem(row, 0, QTableWidgetItem(str(p['id'])))
            self.proxy_table.setItem(row, 1, QTableWidgetItem(p['type']))
            self.proxy_table.setItem(row, 2, QTableWidgetItem(p['proxy_str']))
            self.proxy_table.setItem(row, 3, QTableWidgetItem(p['region'] or "-"))
            
            status = p['last_status'] or "未测试"
            status_item = QTableWidgetItem(status)
            if "成功" in status or "通过" in status: status_item.setForeground(Qt.GlobalColor.green)
            elif "失败" in status: status_item.setForeground(Qt.GlobalColor.red)
            self.proxy_table.setItem(row, 4, status_item)
            self.proxy_table.setItem(row, 5, QTableWidgetItem(p['notes'] or ""))
            
            widget = QWidget()
            layout = QHBoxLayout(widget)
            layout.setContentsMargins(2, 2, 2, 2)
            
            btn_test = QPushButton("测试")
            btn_test.setObjectName("ActionBtn")
            btn_test.clicked.connect(lambda _, proxy=p: self.action_test_existing_proxy(proxy))
            
            btn_edit = QPushButton("编辑")
            btn_edit.setObjectName("ActionBtn")
            btn_edit.clicked.connect(lambda _, proxy=p: self.action_edit_proxy(proxy))
            
            btn_del = QPushButton("删除")
            btn_del.setObjectName("ActionBtnDanger")
            btn_del.clicked.connect(lambda _, proxy=p: self.action_delete_proxy(proxy['id']))
            
            layout.addWidget(btn_test)
            layout.addWidget(btn_edit)
            layout.addWidget(btn_del)
            self.proxy_table.setCellWidget(row, 6, widget)

        self.proxy_table.setSortingEnabled(True)
        self.load_sync_data()

    def load_sync_data(self):
        if self.stack.currentIndex() != 2: return
        self.sync_table.setRowCount(0)
        active_ids = list(browser_controller.active_processes.keys())
        for pid_val in active_ids:
            conn = database.get_connection()
            p = conn.execute("SELECT * FROM profiles WHERE id = ?", (pid_val,)).fetchone()
            conn.close()
            if not p: continue
            
            row = self.sync_table.rowCount()
            self.sync_table.insertRow(row)
            
            # Follower Checkbox
            chk = QCheckBox()
            chk.setChecked(True)
            chk_widget = QWidget()
            chk_layout = QHBoxLayout(chk_widget)
            chk_layout.addWidget(chk)
            chk_layout.setAlignment(Qt.AlignmentFlag.AlignCenter)
            chk_layout.setContentsMargins(0,0,0,0)
            self.sync_table.setCellWidget(row, 0, chk_widget)
            
            self.sync_table.setItem(row, 1, QTableWidgetItem(str(p['id'])))
            self.sync_table.setItem(row, 2, QTableWidgetItem(p['name']))
            self.sync_table.setItem(row, 3, QTableWidgetItem("正在运行"))
            
            # Master Radio
            rad = QRadioButton()
            if row == 0: rad.setChecked(True) # Default first is master
            rad_widget = QWidget()
            rad_layout = QHBoxLayout(rad_widget)
            rad_layout.addWidget(rad)
            rad_layout.setAlignment(Qt.AlignmentFlag.AlignCenter)
            rad_layout.setContentsMargins(0,0,0,0)
            self.sync_table.setCellWidget(row, 4, rad_widget)

    def action_start_input_sync(self):
        # 1. Find Master HWND
        master_id = -1
        for row in range(self.sync_table.rowCount()):
            rad_widget = self.sync_table.cellWidget(row, 4)
            if rad_widget and rad_widget.findChild(QRadioButton).isChecked():
                master_id = int(self.sync_table.item(row, 1).text())
                break
        
        if master_id == -1:
            QMessageBox.warning(self, "提示", "请选择一个主控窗口")
            return
            
        master_proc = browser_controller.active_processes.get(master_id)
        if not master_proc: return
        master_hwnd = self.find_main_window_for_pid(master_proc.pid)
        
        # 2. Find Follower HWNDs
        follower_hwnds = []
        for row in range(self.sync_table.rowCount()):
            chk_widget = self.sync_table.cellWidget(row, 0)
            if chk_widget and chk_widget.findChild(QCheckBox).isChecked():
                fid = int(self.sync_table.item(row, 1).text())
                fproc = browser_controller.active_processes.get(fid)
                if fproc:
                    fhnd = self.find_main_window_for_pid(fproc.pid)
                    if fhnd: follower_hwnds.append(fhnd)
        
        if not master_hwnd or not follower_hwnds:
            QMessageBox.warning(self, "提示", "未能定位浏览器窗口，请确保已启动")
            return
            
        self.syncer.start(master_hwnd, follower_hwnds)
        
        # Show Floating Window
        if not self.status_window:
            self.status_window = SyncStatusWindow()
            self.status_window.stop_requested.connect(self.action_stop_input_sync)
            self.status_window.config_changed.connect(self.action_update_sync_config)
            
        # Position floating window at top right
        screen = QApplication.primaryScreen().geometry()
        self.status_window.move(screen.width() - 250, 100)
        self.status_window.show()

        self.btn_start_sync.setEnabled(False)

    def action_update_sync_config(self, mouse_enabled, key_enabled):
        self.syncer.mouse_enabled = mouse_enabled
        self.syncer.key_enabled = key_enabled

    def action_stop_input_sync(self):
        self.syncer.stop()
        if self.status_window:
            self.status_window.hide()
        self.btn_start_sync.setEnabled(True)

    def action_tile_windows(self):
        selected_ids = []
        for row in range(self.sync_table.rowCount()):
            chk_widget = self.sync_table.cellWidget(row, 0)
            if chk_widget and chk_widget.findChild(QCheckBox).isChecked():
                selected_ids.append(int(self.sync_table.item(row, 1).text()))
        
        hwnds = []
        for profile_id in selected_ids:
            proc = browser_controller.active_processes.get(profile_id)
            if proc:
                hwnd = self.find_main_window_for_pid(proc.pid)
                if hwnd: hwnds.append(hwnd)
        
        if not hwnds: return
        
        num = len(hwnds)
        cols = 2 if num > 1 else 1
        if num > 4: cols = 3
        rows = (num + cols - 1) // cols
        
        desktop = QApplication.primaryScreen().geometry()
        w = desktop.width() // cols
        h = desktop.height() // rows
        
        import win32con
        for i, hwnd in enumerate(hwnds):
            r, c = i // cols, i % cols
            win32gui.ShowWindow(hwnd, win32con.SW_RESTORE)
            win32gui.SetWindowPos(hwnd, win32con.HWND_TOP, c*w, r*h, w, h, win32con.SWP_SHOWWINDOW)

    def find_main_window_for_pid(self, pid):
        result = []
        def callback(hwnd, _):
            if win32gui.IsWindowVisible(hwnd):
                _, found_pid = win32process.GetWindowThreadProcessId(hwnd)
                if found_pid == pid:
                    title = win32gui.GetWindowText(hwnd)
                    if title: result.append(hwnd)
        win32gui.EnumWindows(callback, None)
        return result[0] if result else None

    def action_close_all_running(self):
        for row in range(self.sync_table.rowCount()):
            chk_widget = self.sync_table.cellWidget(row, 0)
            if chk_widget and chk_widget.findChild(QCheckBox).isChecked():
                pid_val = int(self.sync_table.item(row, 1).text())
                browser_controller.close_profile(pid_val)
        self.load_data()

    def action_toggle_launch(self, profile):
        pid = profile['id']
        if browser_controller.is_running(pid):
            browser_controller.close_profile(pid)
        else:
            if not browser_controller.launch_profile(profile):
                QMessageBox.warning(self, "错误", "启动失败")
        self.load_data()

    def action_install_extension(self):
        """通过 CDP 向所有运行中的浏览器安装 Chrome 插件"""
        url = self.ext_url_input.text().strip()
        if not url:
            QMessageBox.warning(self, "提示", "请先输入 Chrome 商店插件地址")
            return
        
        # Validate: must be a Chrome Web Store URL
        if not re.search(r'chromewebstore\.google\.com/detail/', url):
            QMessageBox.warning(self, "格式错误",
                "请输入有效的 Chrome 商店地址\n"
                "例如: https://chromewebstore.google.com/detail/okx-wallet/mcohilncbfahbmgdjkbpemcciiolgcge")
            return
        
        # Check running profiles
        running_ids = [pid for pid in browser_controller.active_processes
                       if browser_controller.is_running(pid)]
        if not running_ids:
            QMessageBox.information(self, "提示", "没有正在运行的浏览器，请先启动浏览器环境")
            return
        
        # Run in background thread to avoid UI freeze
        def do_install():
            results = browser_controller.install_extension_to_all(url)
            
            success_count = sum(1 for ok, _ in results.values() if ok)
            fail_count = len(results) - success_count
            
            details = []
            for pid, (ok, msg) in results.items():
                icon = "✅" if ok else "❌"
                details.append(f"{icon} 环境 {pid}: {msg}")
            
            msg_text = (
                f"操作完成！\n"
                f"✅ 成功: {success_count} 个  ❌ 失败: {fail_count} 个\n\n"
                + "\n".join(details) +
                "\n\n⚠️ 请在每个浏览器中点击 [添加到 Chrome] 完成安装"
            )
            
            QMetaObject.invokeMethod(
                self, "_show_install_result",
                Qt.ConnectionType.QueuedConnection,
                Q_ARG(str, msg_text)
            )
        
        threading.Thread(target=do_install, daemon=True).start()
    
    @pyqtSlot(str)
    def _show_install_result(self, msg_text):
        QMessageBox.information(self, "批量安装插件", msg_text)

    def update_statuses(self):
        if self.stack.currentIndex() == 0:
            for row in range(self.table.rowCount()):
                pid = int(self.table.item(row, 0).text())
                is_running = browser_controller.is_running(pid)
                text = "运行中 🟢" if is_running else "已关闭 🔴"
                if self.table.item(row, 4).text() != text:
                    self.load_data()
                    break

    def action_new_profile(self):
        dialog = ProfileDialog(self)
        if dialog.exec():
            database.add_profile(**dialog.get_data())
            self.load_data()

    def action_edit_profile(self, profile):
        dialog = ProfileDialog(self, profile)
        if dialog.exec():
            database.update_profile(profile['id'], **dialog.get_data())
            self.load_data()

    def action_delete_profile(self, profile):
        if browser_controller.is_running(profile['id']): return
        if QMessageBox.question(self, "确认", f"确定删除 {profile['name']}?") == QMessageBox.StandardButton.Yes:
            database.delete_profile(profile['id'])
            self.load_data()

    def action_new_proxy(self):
        dialog = ProxyDialog(self)
        if dialog.exec():
            d = dialog.get_data()
            database.add_proxy(d['proxy_str'], d['type'], d['notes'])
            self.load_data()

    def action_edit_proxy(self, p):
        dialog = ProxyDialog(self)
        dialog.set_data(p)
        if dialog.exec():
            d = dialog.get_data()
            database.update_proxy(p['id'], d['proxy_str'], d['type'], d['notes'])
            self.load_data()

    def action_test_existing_proxy(self, p):
        def on_fin(_, details):
            database.update_proxy_status(p['id'], details['status_msg'], details['region'])
            self.load_data()
        tester = TestThread(p['proxy_str'], p['type'])
        tester.finished.connect(on_fin)
        tester.start()
        if not hasattr(self, '_testers'): self._testers = []
        self._testers.append(tester)

    def action_bulk_proxy(self):
        from PyQt6.QtWidgets import QInputDialog
        text, ok = QInputDialog.getMultiLineText(self, "导入", "每行一个代理:")
        if ok and text:
            for line in text.strip().split('\n'):
                if line.strip(): database.add_proxy(line.strip())
            self.load_data()

    def action_delete_proxy(self, proxy_id):
        if QMessageBox.question(self, "确认", "删除此代理?") == QMessageBox.StandardButton.Yes:
            database.delete_proxy(proxy_id)
            self.load_data()

if __name__ == "__main__":
    import sys
    app = QApplication(sys.argv)
    window = MainWindow()
    window.show()
    sys.exit(app.exec())
