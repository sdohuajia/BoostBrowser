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
    QSystemTrayIcon, QMenu, QGroupBox, QButtonGroup, QAbstractItemView,
    QProgressBar
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
from ui_proxy_dialog import ProxyDialog, TestThread, BulkProxyDialog
from browser_launcher import browser_controller
from input_syncer import InputSyncer
from ui_sync_status import SyncStatusWindow

class MainWindow(QMainWindow):
    def __init__(self):
        super().__init__()
        # Ensure native title bar and buttons are present
        self.setWindowFlags(Qt.WindowType.Window | 
                           Qt.WindowType.WindowMinMaxButtonsHint | 
                           Qt.WindowType.WindowCloseButtonHint | 
                           Qt.WindowType.WindowSystemMenuHint)
        
        self.setWindowTitle("AntigravityAds - 专业多开代理防关联浏览器")
        self.resize(1240, 780)
        self.setMinimumSize(1120, 700)
        self.syncer = InputSyncer()
        self.status_window = None
        
        self.config_file = os.path.join(os.path.dirname(database.get_db_path()), 'config.json')
        self.settings = self.load_settings()
        self.is_force_quit = False
        
        self.setup_ui()
        self.setup_tray()
        self.load_data()
        self._restore_ui_state()

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
        self._save_ui_state()
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
        main_widget.setObjectName("CentralWidget")
        self.setCentralWidget(main_widget)
        main_layout = QHBoxLayout(main_widget)
        main_layout.setContentsMargins(0, 0, 0, 0)
        main_layout.setSpacing(0)

        # --- Sidebar ---
        sidebar = QFrame()
        sidebar.setFixedWidth(228)
        sidebar.setObjectName("Sidebar")
        sidebar_layout = QVBoxLayout(sidebar)
        
        logo_label = QLabel("✦ AntigravityAds Pro")
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
            QWidget#CentralWidget {
                background-color: #11131a;
                color: #d9e0ee;
                font-family: "Microsoft YaHei", "PingFang SC", "Segoe UI";
                font-size: 13px;
            }

            #Sidebar {
                background-color: #171a23;
                border-right: 1px solid #2a3040;
            }
            #LogoLabel {
                color: #b5c9ff;
                font-size: 18px;
                font-weight: 700;
                letter-spacing: 0.5px;
                padding: 22px 16px;
            }

            QPushButton#SidebarBtn, QPushButton#SidebarBtnActive {
                background-color: transparent;
                color: #9eabc6;
                border: 1px solid transparent;
                padding: 12px 16px;
                text-align: left;
                font-size: 14px;
                border-radius: 10px;
                margin: 3px 12px;
            }
            QPushButton#SidebarBtn:hover {
                background-color: #202635;
                color: #e9efff;
                border: 1px solid #2f3850;
            }
            QPushButton#SidebarBtnActive {
                background-color: #334869;
                color: #e9f1ff;
                border: 1px solid #4a6ca1;
                font-weight: 700;
            }

            #PageHeader {
                font-size: 23px;
                font-weight: 700;
                color: #edf2ff;
                margin-bottom: 12px;
            }

            #PrimaryBtn {
                background-color: #4f74b8;
                color: #f4f8ff;
                border: 1px solid #5f87d1;
                border-radius: 8px;
                padding: 8px 16px;
                font-weight: 700;
            }
            #PrimaryBtn:hover { background-color: #628bd8; }
            #PrimaryBtn:pressed { background-color: #4567a5; }

            #ActionBtn {
                background-color: #1d2230;
                color: #d6dff3;
                border: 1px solid #36405a;
                border-radius: 8px;
                padding: 4px 10px;
            }
            #ActionBtn:hover { background-color: #2a3143; }

            #ActionBtnLaunch {
                background-color: #2f7f56;
                color: #effff5;
                border: 1px solid #40a873;
                border-radius: 8px;
                padding: 4px 10px;
                font-weight: 700;
            }
            #ActionBtnClose {
                background-color: #8f3f56;
                color: #fff4f6;
                border: 1px solid #ba5977;
                border-radius: 8px;
                padding: 4px 10px;
                font-weight: 700;
            }
            #ActionBtnDanger {
                background-color: transparent;
                color: #ff8fa9;
                border: 1px solid #b65a72;
                border-radius: 8px;
                padding: 4px 10px;
            }
            #ActionBtnDanger:hover {
                background-color: #a84e67;
                color: #fff3f6;
            }

            QTableWidget {
                background-color: #141824;
                alternate-background-color: #181d2b;
                color: #dce4f6;
                gridline-color: #2b3245;
                border: 1px solid #2d3549;
                border-radius: 10px;
                selection-background-color: #334869;
                selection-color: #ffffff;
            }
            QTableWidget::item { padding: 6px; }
            QTableCornerButton::section {
                background-color: #1b2030;
                border: none;
                border-bottom: 1px solid #323b53;
            }
            QHeaderView::section {
                background-color: #1b2030;
                color: #afbdd8;
                padding: 9px;
                border: none;
                border-bottom: 1px solid #323b53;
                font-weight: 700;
            }

            QLineEdit, QTextEdit, QComboBox {
                background-color: #1b2130;
                color: #deE8ff;
                border: 1px solid #333d56;
                border-radius: 8px;
                padding: 6px 10px;
            }
            QLineEdit:focus, QTextEdit:focus, QComboBox:focus {
                border: 1px solid #5f87d1;
            }

            QComboBox QAbstractItemView {
                background-color: #1b2130;
                color: #e6eeff;
                selection-background-color: #34405b;
                outline: none;
            }

            QGroupBox {
                color: #dce5fa;
                border: 1px solid #2c3346;
                border-radius: 10px;
                margin-top: 12px;
                padding-top: 8px;
            }
            QGroupBox::title {
                subcontrol-origin: margin;
                left: 12px;
                padding: 0 6px;
                color: #b8c8ea;
            }

            QScrollBar:vertical {
                border: none;
                background: #11131a;
                width: 10px;
                margin: 0px 0 0px 0;
            }
            QScrollBar::handle:vertical {
                background: #34405b;
                min-height: 20px;
                border-radius: 5px;
            }
            QScrollBar::handle:vertical:hover {
                background: #4f74b8;
            }
            QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {
                height: 0px;
            }
            QScrollBar::add-page:vertical, QScrollBar::sub-page:vertical {
                background: none;
            }
            QScrollBar:horizontal {
                border: none;
                background: #11131a;
                height: 10px;
                margin: 0 0px 0 0px;
            }
            QScrollBar::handle:horizontal {
                background: #34405b;
                min-width: 20px;
                border-radius: 5px;
            }
            QScrollBar::handle:horizontal:hover {
                background: #4f74b8;
            }
            QScrollBar::add-line:horizontal, QScrollBar::sub-line:horizontal {
                width: 0px;
            }
            QScrollBar::add-page:horizontal, QScrollBar::sub-page:horizontal {
                background: none;
            }
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

    def _setup_table_behavior(self, table: QTableWidget):
        table.setAlternatingRowColors(True)
        table.setSelectionBehavior(QAbstractItemView.SelectionBehavior.SelectRows)
        table.setSelectionMode(QAbstractItemView.SelectionMode.SingleSelection)
        table.setEditTriggers(QAbstractItemView.EditTrigger.NoEditTriggers)
        table.setFocusPolicy(Qt.FocusPolicy.NoFocus)

    def _make_status_chip(self, text: str, kind: str = "neutral"):
        palette = {
            "success": ("#143228", "#79e2b1", "#2d7f5d"),
            "danger": ("#3a1d27", "#ffb0c0", "#9c4d63"),
            "warn": ("#3c3216", "#ffd98f", "#9f7d31"),
            "neutral": ("#252b3a", "#c8d4ef", "#4a5777")
        }
        bg, fg, border = palette.get(kind, palette["neutral"])
        label = QLabel(text)
        label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        # Use a more robust style, reduce padding to save space, set a minimum width
        label.setStyleSheet(
            f"background:{bg}; color:{fg}; border:1px solid {border}; border-radius:4px; "
            f"padding:2px 6px; font-weight:600;"
        )
        label.setMinimumWidth(60) # Ensure "已关闭" (3 chars) has enough space

        wrapper = QWidget()
        layout = QHBoxLayout(wrapper)
        layout.setContentsMargins(0, 0, 0, 0) # Remove container margins
        layout.addWidget(label)
        layout.setAlignment(Qt.AlignmentFlag.AlignCenter)
        return wrapper

    def _make_stat_card(self, title: str, initial: str = "0"):
        card = QFrame()
        card.setStyleSheet(
            "QFrame { background:#171d2a; border:1px solid #2e3850; border-radius:10px; }"
        )
        layout = QVBoxLayout(card)
        layout.setContentsMargins(12, 8, 12, 8)
        layout.setSpacing(2)

        title_lbl = QLabel(title)
        title_lbl.setStyleSheet("color:#8fa3c8; font-size:11px; font-weight:600;")
        value_lbl = QLabel(initial)
        value_lbl.setStyleSheet("color:#edf2ff; font-size:18px; font-weight:800;")

        layout.addWidget(title_lbl)
        layout.addWidget(value_lbl)
        card.value_label = value_lbl
        return card

    def _make_action_btn(self, text: str, obj_name: str = "ActionBtn"):
        btn = QPushButton(text)
        btn.setObjectName(obj_name)
        btn.setMinimumHeight(30)
        btn.setCursor(Qt.CursorShape.PointingHandCursor)
        return btn

    def _confirm(self, title: str, text: str):
        box = QMessageBox(self)
        box.setWindowTitle(title)
        box.setText(text)
        box.setIcon(QMessageBox.Icon.Warning)
        box.setStandardButtons(QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.Cancel)
        box.setDefaultButton(QMessageBox.StandardButton.Cancel)
        box.setStyleSheet(
            "QMessageBox { background:#151a27; color:#dce4f6; }"
            "QPushButton { min-width:88px; padding:6px 10px; border-radius:8px;"
            " background:#1d2230; color:#d6dff3; border:1px solid #36405a; }"
            "QPushButton:hover { background:#2a3143; }"
        )
        return box.exec() == QMessageBox.StandardButton.Yes

    def _sync_empty_hint(self, table: QTableWidget, hint_label: QLabel, text: str):
        if table.rowCount() == 0:
            hint_label.setText(text)
            hint_label.show()
        else:
            hint_label.hide()

    def _save_ui_state(self):
        self.settings["window_geometry"] = [self.x(), self.y(), self.width(), self.height()]
        self.settings["active_page"] = self.stack.currentIndex()
        self.settings["table_columns"] = {
            "profiles": [self.table.columnWidth(i) for i in range(self.table.columnCount())],
            "proxies": [self.proxy_table.columnWidth(i) for i in range(self.proxy_table.columnCount())],
            "sync": [self.sync_table.columnWidth(i) for i in range(self.sync_table.columnCount())],
        }
        self.save_settings()

    def _restore_ui_state(self):
        geo = self.settings.get("window_geometry")
        if isinstance(geo, list) and len(geo) == 4:
            x, y, w, h = geo
            
            # Additional Safety: Ensure window size is within current screen bounds
            # This handles cases where the window was saved on a high-DPI or larger screen
            screen = QApplication.primaryScreen().availableGeometry()
            w = min(w, screen.width())
            h = min(h, screen.height())
            
            # Ensure window is within screen limits
            x = max(screen.left(), min(x, screen.right() - 100))
            y = max(screen.top(), min(y, screen.bottom() - 100))
            
            self.setGeometry(x, y, w, h)

        def apply_widths(table, key):
            cols = self.settings.get("table_columns", {}).get(key, [])
            if isinstance(cols, list):
                for i, w in enumerate(cols[:table.columnCount()]):
                    # Do NOT restore widths for fixed-width columns that we want to control
                    # Profiles: 4 (Status), 5 (Action)
                    # Proxies: 4 (Status), 6 (Action)
                    if key == "profiles" and i in [4, 5]: continue
                    if key == "proxies" and i in [4, 6]: continue
                    
                    if isinstance(w, int) and w > 30:
                        table.setColumnWidth(i, w)

        apply_widths(self.table, "profiles")
        apply_widths(self.proxy_table, "proxies")
        apply_widths(self.sync_table, "sync")

        page_index = self.settings.get("active_page", 0)
        if isinstance(page_index, int) and 0 <= page_index < self.stack.count():
            self.switch_page(page_index)

    def create_profile_page(self):
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(20, 20, 20, 20)
        
        header_layout = QHBoxLayout()
        header = QLabel("环境管理 (Profiles)")
        header.setObjectName("PageHeader")
        header_layout.addWidget(header)
        header_layout.addStretch()
        
        btn_add = self._make_action_btn("＋ 新建环境", "PrimaryBtn")
        btn_add.clicked.connect(self.action_new_profile)
        header_layout.addWidget(btn_add)
        layout.addLayout(header_layout)

        stats_layout = QHBoxLayout()
        self.profile_stat_total = self._make_stat_card("环境总数")
        self.profile_stat_running = self._make_stat_card("运行中")
        self.profile_stat_offline = self._make_stat_card("已关闭")
        stats_layout.addWidget(self.profile_stat_total)
        stats_layout.addWidget(self.profile_stat_running)
        stats_layout.addWidget(self.profile_stat_offline)
        stats_layout.addStretch()
        layout.addLayout(stats_layout)

        self.table = QTableWidget()
        self.table.setColumnCount(6)
        self.table.setHorizontalHeaderLabels(["ID", "名称", "代理", "备注", "状态", "操作"])
        # Setup header resizing
        header = self.table.horizontalHeader()
        header.setSectionResizeMode(QHeaderView.ResizeMode.Interactive)
        # ID: fit to content
        header.setSectionResizeMode(0, QHeaderView.ResizeMode.ResizeToContents) # ID
        # Name: stretch
        header.setSectionResizeMode(1, QHeaderView.ResizeMode.Stretch) # Name
        # Proxy/Notes: stretch
        header.setSectionResizeMode(2, QHeaderView.ResizeMode.Stretch) # Proxy
        header.setSectionResizeMode(3, QHeaderView.ResizeMode.Stretch) # Notes
        # Status: Fixed width to ensure chip is visible
        header.setSectionResizeMode(4, QHeaderView.ResizeMode.Fixed)
        self.table.setColumnWidth(4, 100) # Increased to 100
        # Action: Fixed width to ensure all 3 buttons are visible
        header.setSectionResizeMode(5, QHeaderView.ResizeMode.Fixed)
        self.table.setColumnWidth(5, 280) # Increased to 280 to handle icons and text comfortably
        
        self.table.verticalHeader().setVisible(False)
        self.table.verticalHeader().setDefaultSectionSize(55)
        self._setup_table_behavior(self.table)
        self.table.setSortingEnabled(True) # Enable sorting
        layout.addWidget(self.table)

        self.profile_empty_label = QLabel("暂无环境，点击右上角“＋ 新建环境”开始。")
        self.profile_empty_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.profile_empty_label.setStyleSheet("color:#8ea0c5; padding:10px;")
        layout.addWidget(self.profile_empty_label)
        self.profile_empty_label.hide()
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
        
        btn_bulk_test = self._make_action_btn("◉ 批量测试", "ActionBtn")
        btn_bulk_test.clicked.connect(self.action_bulk_test_proxies)
        header_layout.addWidget(btn_bulk_test)

        btn_bulk = self._make_action_btn("⇪ 批量导入", "ActionBtn")
        btn_bulk.clicked.connect(self.action_bulk_proxy)
        header_layout.addWidget(btn_bulk)

        btn_add = self._make_action_btn("＋ 添加代理", "PrimaryBtn")
        btn_add.clicked.connect(self.action_new_proxy)
        header_layout.addWidget(btn_add)
        layout.addLayout(header_layout)

        stats_layout = QHBoxLayout()
        self.proxy_stat_total = self._make_stat_card("代理总数")
        self.proxy_stat_ok = self._make_stat_card("可用")
        self.proxy_stat_fail = self._make_stat_card("不可用")
        self.proxy_stat_unknown = self._make_stat_card("未测试")
        stats_layout.addWidget(self.proxy_stat_total)
        stats_layout.addWidget(self.proxy_stat_ok)
        stats_layout.addWidget(self.proxy_stat_fail)
        stats_layout.addWidget(self.proxy_stat_unknown)
        stats_layout.addStretch()
        layout.addLayout(stats_layout)

        self.proxy_table = QTableWidget()
        self.proxy_table.setColumnCount(7)
        # Setup header resizing for proxy table
        p_header = self.proxy_table.horizontalHeader()
        p_header.setSectionResizeMode(QHeaderView.ResizeMode.Interactive)
        p_header.setSectionResizeMode(0, QHeaderView.ResizeMode.ResizeToContents) # ID
        p_header.setSectionResizeMode(1, QHeaderView.ResizeMode.ResizeToContents) # Type
        p_header.setSectionResizeMode(4, QHeaderView.ResizeMode.Fixed) # Status
        self.proxy_table.setColumnWidth(4, 110) # Increased
        p_header.setSectionResizeMode(6, QHeaderView.ResizeMode.Fixed) # Action
        self.proxy_table.setColumnWidth(6, 220) # Increased
        
        p_header.setSectionResizeMode(2, QHeaderView.ResizeMode.Stretch) # Address
        p_header.setSectionResizeMode(3, QHeaderView.ResizeMode.Stretch) # Region
        p_header.setSectionResizeMode(5, QHeaderView.ResizeMode.Stretch) # Notes
        
        self.proxy_table.verticalHeader().setVisible(False)
        self.proxy_table.verticalHeader().setDefaultSectionSize(55)
        self._setup_table_behavior(self.proxy_table)
        self.proxy_table.setSortingEnabled(True)
        layout.addWidget(self.proxy_table)

        self.proxy_progress = QProgressBar()
        self.proxy_progress.setTextVisible(True)
        self.proxy_progress.setFormat("批量测试进度 %v/%m")
        self.proxy_progress.setVisible(False)
        layout.addWidget(self.proxy_progress)

        self.proxy_empty_label = QLabel("暂无代理，点击“＋ 添加代理”或“⇪ 批量导入”。")
        self.proxy_empty_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.proxy_empty_label.setStyleSheet("color:#8ea0c5; padding:10px;")
        layout.addWidget(self.proxy_empty_label)
        self.proxy_empty_label.hide()
        return page

    def create_sync_page(self):
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(20, 20, 20, 20)
        
        header = QLabel("同步器 (Synchronizer)")
        header.setObjectName("PageHeader")
        layout.addWidget(header)
        
        controls = QHBoxLayout()
        self.btn_tile = self._make_action_btn("▦ 一键平铺", "ActionBtn")
        self.btn_tile.clicked.connect(self.action_tile_windows)

        self.btn_close_all = self._make_action_btn("✖ 一键关闭选定", "ActionBtnDanger")
        self.btn_close_all.clicked.connect(self.action_close_all_running)

        self.btn_refresh = self._make_action_btn("↻ 刷新列表", "ActionBtn")
        self.btn_refresh.clicked.connect(self.load_sync_data)

        self.btn_start_sync = self._make_action_btn("▶ 开启同步控制", "PrimaryBtn")
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
        self._setup_table_behavior(self.sync_table)
        layout.addWidget(self.sync_table)

        self.sync_empty_label = QLabel("当前没有运行中的浏览器，先到“环境管理”启动几个实例。")
        self.sync_empty_label.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.sync_empty_label.setStyleSheet("color:#8ea0c5; padding:10px;")
        layout.addWidget(self.sync_empty_label)
        self.sync_empty_label.hide()
        
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
            status_text = "运行中" if is_running else "已关闭"
            status_kind = "success" if is_running else "danger"
            self.table.setCellWidget(row, 4, self._make_status_chip(status_text, status_kind))
            
            widget = QWidget()
            layout = QHBoxLayout(widget)
            layout.setContentsMargins(2, 2, 2, 2)
            
            btn_launch = self._make_action_btn("■ 关闭" if is_running else "▶ 启动", "ActionBtnClose" if is_running else "ActionBtnLaunch")
            btn_launch.clicked.connect(lambda _, profile=p: self.action_toggle_launch(profile))

            btn_edit = self._make_action_btn("✎ 编辑", "ActionBtn")
            btn_edit.clicked.connect(lambda _, profile=p: self.action_edit_profile(profile))

            btn_del = self._make_action_btn("🗑 删除", "ActionBtnDanger")
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
            if "成功" in status or "通过" in status:
                self.proxy_table.setCellWidget(row, 4, self._make_status_chip(status, "success"))
            elif "失败" in status:
                self.proxy_table.setCellWidget(row, 4, self._make_status_chip(status, "danger"))
            else:
                self.proxy_table.setCellWidget(row, 4, self._make_status_chip(status, "warn"))
            self.proxy_table.setItem(row, 5, QTableWidgetItem(p['notes'] or ""))
            
            widget = QWidget()
            layout = QHBoxLayout(widget)
            layout.setContentsMargins(2, 2, 2, 2)
            
            btn_test = self._make_action_btn("◉ 测试", "ActionBtn")
            btn_test.clicked.connect(lambda _, proxy=p: self.action_test_existing_proxy(proxy))

            btn_edit = self._make_action_btn("✎ 编辑", "ActionBtn")
            btn_edit.clicked.connect(lambda _, proxy=p: self.action_edit_proxy(proxy))

            btn_del = self._make_action_btn("🗑 删除", "ActionBtnDanger")
            btn_del.clicked.connect(lambda _, proxy=p: self.action_delete_proxy(proxy['id']))
            
            layout.addWidget(btn_test)
            layout.addWidget(btn_edit)
            layout.addWidget(btn_del)
            self.proxy_table.setCellWidget(row, 6, widget)

        self.proxy_table.setSortingEnabled(True)
        self.refresh_dashboard_stats()
        self._sync_empty_hint(self.table, self.profile_empty_label, "暂无环境，点击右上角“＋ 新建环境”开始。")
        self._sync_empty_hint(self.proxy_table, self.proxy_empty_label, "暂无代理，点击“＋ 添加代理”或“⇪ 批量导入”。")
        self.load_sync_data()

    def refresh_dashboard_stats(self):
        profiles = getattr(self, 'profiles', [])
        proxies = getattr(self, 'proxies', [])

        running_count = 0
        for p in profiles:
            if browser_controller.is_running(p['id']):
                running_count += 1

        if hasattr(self, 'profile_stat_total'):
            self.profile_stat_total.value_label.setText(str(len(profiles)))
            self.profile_stat_running.value_label.setText(str(running_count))
            self.profile_stat_offline.value_label.setText(str(max(0, len(profiles) - running_count)))

        ok_count = fail_count = unknown_count = 0
        for p in proxies:
            status = (p.get('last_status') or '').strip()
            if ("成功" in status) or ("通过" in status):
                ok_count += 1
            elif ("失败" in status) or ("异常" in status) or ("超时" in status):
                fail_count += 1
            else:
                unknown_count += 1

        if hasattr(self, 'proxy_stat_total'):
            self.proxy_stat_total.value_label.setText(str(len(proxies)))
            self.proxy_stat_ok.value_label.setText(str(ok_count))
            self.proxy_stat_fail.value_label.setText(str(fail_count))
            self.proxy_stat_unknown.value_label.setText(str(unknown_count))

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

        self._sync_empty_hint(self.sync_table, self.sync_empty_label, "当前没有运行中的浏览器，先到“环境管理”启动几个实例。")

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
            self.status_window.tile_requested.connect(self.action_tile_windows)
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
        
        desktop = QApplication.primaryScreen().availableGeometry()
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
                status_text = "运行中" if is_running else "已关闭"
                cell = self.table.cellWidget(row, 4)
                label = cell.findChild(QLabel) if cell else None
                current_text = label.text() if label else ""
                if current_text != status_text:
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
        if browser_controller.is_running(profile['id']):
            return
        if self._confirm("删除环境", f"确定删除环境「{profile['name']}」吗？\n此操作不可撤销。"):
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

    def update_proxy_row_status(self, proxy_id, status_msg, region):
        database.update_proxy_status(proxy_id, status_msg, region)
        for row in range(self.proxy_table.rowCount()):
            if int(self.proxy_table.item(row, 0).text()) == proxy_id:
                if "成功" in status_msg or "通过" in status_msg:
                    kind = "success"
                elif "失败" in status_msg or "异常" in status_msg or "超时" in status_msg:
                    kind = "danger"
                else:
                    kind = "warn"
                self.proxy_table.setCellWidget(row, 4, self._make_status_chip(status_msg, kind))
                self.proxy_table.setItem(row, 3, QTableWidgetItem(region or "-"))
                break
        self.proxies = database.get_all_proxies()
        self.refresh_dashboard_stats()

    def action_test_existing_proxy(self, p):
        def on_fin(success, details):
            self.update_proxy_row_status(p['id'], details.get('status_msg', '未知错误'), details.get('region'))
        tester = TestThread(p['proxy_str'], p['type'])
        tester.finished.connect(on_fin)
        tester.start()
        if not hasattr(self, '_testers'): self._testers = []
        self._testers.append(tester)
        
        # update ui immediately for single test
        for row in range(self.proxy_table.rowCount()):
            if int(self.proxy_table.item(row, 0).text()) == p['id']:
                self.proxy_table.setCellWidget(row, 4, self._make_status_chip("测试中...", "neutral"))
                break

    def action_bulk_test_proxies(self):
        proxies = database.get_all_proxies()
        if not proxies:
            QMessageBox.information(self, "提示", "列表中没有可测试的代理。")
            return

        if not hasattr(self, '_testers'):
            self._testers = []

        self.bulk_test_total = len(proxies)
        self.bulk_test_done = 0
        self.proxy_progress.setVisible(True)
        self.proxy_progress.setRange(0, self.bulk_test_total)
        self.proxy_progress.setValue(0)

        # update ui state
        for row in range(self.proxy_table.rowCount()):
            self.proxy_table.setCellWidget(row, 4, self._make_status_chip("测试中...", "neutral"))

        for p in proxies:
            tester = TestThread(p['proxy_str'], p['type'])

            # create closure to keep proxy_id alive
            def make_callback(proxy_id):
                def on_fin(success, details):
                    self.update_proxy_row_status(proxy_id, details.get('status_msg', '未知错误'), details.get('region'))
                    self.bulk_test_done += 1
                    self.proxy_progress.setValue(self.bulk_test_done)
                    if self.bulk_test_done >= self.bulk_test_total:
                        self.proxy_progress.setVisible(False)
                return on_fin

            tester.finished.connect(make_callback(p['id']))
            tester.start()
            self._testers.append(tester)

    def action_bulk_proxy(self):
        dialog = BulkProxyDialog(self)
        if dialog.exec():
            data = dialog.get_data()
            ptype = data["type"]
            for proxy_str in data["proxies"]:
                database.add_proxy(proxy_str, ptype)
            self.load_data()

    def action_delete_proxy(self, proxy_id):
        if self._confirm("删除代理", "确定删除这个代理吗？\n删除后将无法恢复。"):
            database.delete_proxy(proxy_id)
            self.load_data()

if __name__ == "__main__":
    import sys
    app = QApplication(sys.argv)
    window = MainWindow()
    window.show()
    sys.exit(app.exec())
