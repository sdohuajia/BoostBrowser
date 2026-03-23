from PyQt6.QtWidgets import (QDialog, QVBoxLayout, QHBoxLayout, QLineEdit, 
                               QPushButton, QTableWidget, QTableWidgetItem, 
                               QHeaderView, QLabel, QMessageBox, QFrame, QWidget)
from PyQt6.QtCore import Qt, pyqtSignal
import database

class ProxySelectorDialog(QDialog):
    proxy_selected = pyqtSignal(str)

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("选择代理 (Select Proxy)")
        self.setMinimumWidth(800)
        self.setMinimumHeight(500)
        self.selected_proxy = None
        self.setup_ui()
        self.load_data()

    def setup_ui(self):
        self.setStyleSheet("""
            QDialog { background: #141824; color: #dce4f6; }
            QLabel { color: #bfcae2; }
            QLineEdit {
                background: #1b2130; color: #e6eeff; border: 1px solid #34405b;
                border-radius: 8px; padding: 6px 10px;
            }
            QLineEdit:focus { border-color: #5f87d1; }
            QPushButton { 
                background: #1d2230; color: #d6dff3; border: 1px solid #36405a;
                border-radius: 8px; padding: 7px 14px; 
            }
            QPushButton:hover { background: #2a3246; }
            QPushButton#PrimaryBtn { background-color: #4f74b8; color: #f4f8ff; border: 1px solid #5f87d1; font-weight: 600; }
            QPushButton#PrimaryBtn:hover { background: #628bd8; }
            
            QPushButton#ActionBtn { 
                background-color: #24324c; color: #dbe8ff; border: 1px solid #3f5e94; 
                font-weight: 600; padding: 4px 8px; font-size: 12px;
            }
            QPushButton#ActionBtn:hover { background: #2f4163; }
            
            QTableWidget {
                background-color: #1b2130;
                color: #e6eeff;
                gridline-color: #2a3246;
                border: 1px solid #34405b;
                border-radius: 8px;
            }
            QTableWidget::item { padding: 5px; }
            QTableWidget::item:selected { background-color: #34405b; color: #ffffff; }
            QHeaderView::section {
                background-color: #141824;
                color: #bfcae2;
                padding: 10px;
                border: none;
                border-bottom: 1px solid #34405b;
                font-weight: bold;
            }
            
            QScrollBar:vertical {
                border: none;
                background: #141824;
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
        """)

        layout = QVBoxLayout(self)

        # Search box
        search_layout = QHBoxLayout()
        self.search_input = QLineEdit()
        self.search_input.setPlaceholderText("搜索代理地址、备注、环境名...")
        self.search_input.textChanged.connect(self.filter_data)
        search_layout.addWidget(QLabel("搜索:"))
        search_layout.addWidget(self.search_input)
        layout.addLayout(search_layout)

        # Table
        self.table = QTableWidget()
        self.table.setColumnCount(5)
        self.table.setHorizontalHeaderLabels(["ID", "代理地址", "备注/标签", "使用状态", "操作"])
        
        # Adjust header resizing
        header = self.table.horizontalHeader()
        header.setSectionResizeMode(QHeaderView.ResizeMode.Interactive)
        # ID column: fit to content
        header.setSectionResizeMode(0, QHeaderView.ResizeMode.ResizeToContents)
        # Proxy address: stretchable but starts at contents
        header.setSectionResizeMode(1, QHeaderView.ResizeMode.Stretch)
        # Usage status: fit to content
        header.setSectionResizeMode(3, QHeaderView.ResizeMode.ResizeToContents)
        # Action column: fixed width to ensure button fits
        header.setSectionResizeMode(4, QHeaderView.ResizeMode.Fixed)
        self.table.setColumnWidth(4, 90) # Sufficient width for "选择" button
        
        self.table.setSelectionBehavior(QTableWidget.SelectionBehavior.SelectRows)
        self.table.setSelectionMode(QTableWidget.SelectionMode.SingleSelection)
        self.table.setEditTriggers(QTableWidget.EditTrigger.NoEditTriggers)
        self.table.verticalHeader().setVisible(False)
        self.table.verticalHeader().setDefaultSectionSize(45) # Increase row height
        self.table.doubleClicked.connect(self.confirm_selection)
        layout.addWidget(self.table)

        # Info line
        self.info_label = QLabel("提示: 双击行或点击选择按钮以确认。")
        self.info_label.setStyleSheet("font-size: 11px; color: #8894b0;")
        layout.addWidget(self.info_label)

        # Buttons
        btn_layout = QHBoxLayout()
        self.btn_direct = QPushButton("直连 (不使用代理)")
        self.btn_direct.clicked.connect(self.select_direct)
        
        self.btn_confirm = QPushButton("确认选择")
        self.btn_confirm.setObjectName("PrimaryBtn")
        self.btn_confirm.clicked.connect(self.confirm_selection)
        
        self.btn_cancel = QPushButton("取消")
        self.btn_cancel.clicked.connect(self.reject)
        
        btn_layout.addWidget(self.btn_direct)
        btn_layout.addStretch()
        btn_layout.addWidget(self.btn_cancel)
        btn_layout.addWidget(self.btn_confirm)
        layout.addLayout(btn_layout)

    def load_data(self):
        self.proxies = database.get_all_proxies()
        self.usage_stats = database.get_proxy_usage_stats()
        self.display_data(self.proxies)

    def display_data(self, proxies):
        self.table.setRowCount(0)
        for p in proxies:
            row = self.table.rowCount()
            self.table.insertRow(row)
            
            proxy_str = p['proxy_str']
            if "://" not in proxy_str and p['type'] and p['type'].lower() != 'http':
                scheme = p['type'].lower()
                proxy_str_full = f"{scheme}://{proxy_str}"
            else:
                proxy_str_full = proxy_str

            # Usage status
            used_by = self.usage_stats.get(proxy_str_full, []) or self.usage_stats.get(proxy_str, [])
            if used_by:
                usage_text = f"已使用 ({len(used_by)}): " + ", ".join(used_by)
                color = "#f64e60" # Reddish for used
            else:
                usage_text = "未使用"
                color = "#1bc5bd" # Greenish for unused

            self.table.setItem(row, 0, QTableWidgetItem(str(p['id'])))
            self.table.setItem(row, 1, QTableWidgetItem(proxy_str_full))
            self.table.setItem(row, 2, QTableWidgetItem(p['notes'] or ""))
            
            usage_item = QTableWidgetItem(usage_text)
            usage_item.setForeground(Qt.GlobalColor.white)
            usage_item.setBackground(Qt.GlobalColor.transparent)
            # We can use tooltips for long list of names
            if used_by:
                usage_item.setToolTip("\n".join(used_by))
            self.table.setItem(row, 3, usage_item)
            
            # Action button cell
            btn = QPushButton("选择")
            btn.setObjectName("ActionBtn") # Use the better-looking action button style
            btn.setMinimumWidth(60)
            btn.setCursor(Qt.CursorShape.PointingHandCursor)
            btn.clicked.connect(lambda checked, ps=proxy_str_full: self.select_proxy(ps))
            
            # Put button in a widget to center it
            cell_widget = QWidget()
            cell_layout = QHBoxLayout(cell_widget)
            cell_layout.addWidget(btn)
            cell_layout.setContentsMargins(10, 2, 10, 2) # More horizontal padding
            cell_layout.setAlignment(Qt.AlignmentFlag.AlignCenter)
            self.table.setCellWidget(row, 4, cell_widget)

        # After data is loaded, we can fine-tune if needed
        # but the header modes set in setup_ui handle most of it.
        pass

    def filter_data(self):
        text = self.search_input.text().lower()
        if not text:
            self.display_data(self.proxies)
            return
            
        filtered = []
        for p in self.proxies:
            proxy_str = p['proxy_str'].lower()
            notes = (p['notes'] or "").lower()
            
            # Check if used by profile matching search
            used_by = self.usage_stats.get(p['proxy_str'], [])
            used_by_match = any(text in name.lower() for name in used_by)
            
            if text in proxy_str or text in notes or used_by_match:
                filtered.append(p)
        self.display_data(filtered)

    def select_proxy(self, proxy_str):
        self.selected_proxy = proxy_str
        self.accept()

    def select_direct(self):
        self.selected_proxy = ""
        self.accept()

    def confirm_selection(self):
        row = self.table.currentRow()
        if row >= 0:
            proxy_str = self.table.item(row, 1).text()
            self.select_proxy(proxy_str)
        else:
            QMessageBox.warning(self, "提示", "请先选择一个代理！")

    def get_selected_proxy(self):
        return self.selected_proxy
