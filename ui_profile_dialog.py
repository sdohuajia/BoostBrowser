from PyQt6.QtWidgets import (QDialog, QVBoxLayout, QFormLayout, 
                               QLineEdit, QTextEdit, QPushButton, QHBoxLayout, QMessageBox, QLabel, QComboBox)
from PyQt6.QtCore import Qt

class ProfileDialog(QDialog):
    def __init__(self, parent=None, profile=None):
        super().__init__(parent)
        self.profile = profile
        self.setWindowTitle("创建环境" if not profile else "编辑环境")
        self.setMinimumWidth(400)
        self.setup_ui()
        if profile:
            self.load_data()

    def setup_ui(self):
        self.setStyleSheet("""
            QDialog { background: #141824; color: #dce4f6; }
            QLabel { color: #bfcae2; }
            QLineEdit, QTextEdit, QComboBox {
                background: #1b2130; color: #e6eeff; border: 1px solid #34405b;
                border-radius: 8px; padding: 6px 10px;
            }
            QLineEdit:focus, QTextEdit:focus, QComboBox:focus { border-color: #5f87d1; }
            QPushButton { border-radius: 8px; padding: 7px 14px; border: 1px solid #3a4662; }
            QPushButton:hover { background: #2a3246; }
        """)

        layout = QVBoxLayout(self)
        form_layout = QFormLayout()

        self.name_input = QLineEdit()
        self.name_input.setPlaceholderText("例如: Facebook 账号 1")
        form_layout.addRow("环境名称 <font color='red'>*</font>:", self.name_input)

        self.notes_input = QTextEdit()
        self.notes_input.setPlaceholderText("账号密码或其它备注信息...")
        self.notes_input.setMaximumHeight(80)
        form_layout.addRow("备注信息:", self.notes_input)

        self.proxy_input = QComboBox()
        self.proxy_input.setEditable(True)
        self.proxy_input.setPlaceholderText("支持: http://user:pass@ip:port 或从下拉列表选择")
        self.load_proxies_into_combo()
        form_layout.addRow("代理 (Proxy):", self.proxy_input)

        self.version_input = QComboBox()
        self.version_input.addItems(["135", "134", "133", "132", "131", "130", "125", "120", "110", "100", "90"])
        form_layout.addRow("内核版本 (Chrome):", self.version_input)

        self.ua_input = QTextEdit()
        self.ua_input.setPlaceholderText("可留空，默认为所选内核版本的现代 UA")
        self.ua_input.setMaximumHeight(60)
        
        # Adding a button to generate random UA
        ua_layout = QHBoxLayout()
        ua_layout.addWidget(self.ua_input)
        
        self.btn_random_ua = QPushButton("随机 UA")
        self.btn_random_ua.clicked.connect(self.generate_random_ua)
        
        # form_layout.addRow("User-Agent:", ua_layout) # QFormLayout doesn't easily accept layouts as fields without a wrapper
        
        ua_widget = QVBoxLayout()
        ua_widget.addLayout(ua_layout)
        ua_widget.addWidget(self.btn_random_ua)
        
        form_layout.addRow("User-Agent:", ua_widget)

        layout.addLayout(form_layout)

        # Buttons
        btn_layout = QHBoxLayout()
        self.btn_save = QPushButton("保存 (Save)")
        self.btn_save.setDefault(True)
        self.btn_cancel = QPushButton("取消 (Cancel)")
        
        # Styles for buttons
        self.btn_save.setStyleSheet("background-color: #4f74b8; color: #f4f8ff; border: 1px solid #5f87d1;")
        self.btn_cancel.setStyleSheet("background-color: #1d2230; color: #d6dff3; border: 1px solid #36405a;")

        self.btn_save.clicked.connect(self.save_data)
        self.btn_cancel.clicked.connect(self.reject)

        btn_layout.addStretch()
        btn_layout.addWidget(self.btn_cancel)
        btn_layout.addWidget(self.btn_save)

        layout.addLayout(btn_layout)

    def generate_random_ua(self):
        import random
        version = self.version_input.currentText().strip()
        # Randomize build and patch numbers to look like a real Chrome version release
        # Examples: 133.0.6943.54, 134.0.6998.35
        build = random.randint(6000, 7100)
        patch = random.randint(1, 150)
        ua = f"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{version}.0.{build}.{patch} Safari/537.36"
        self.ua_input.setPlainText(ua)

    def load_proxies_into_combo(self):
        import database
        self.proxy_input.addItem("") # Empty option for direct connection
        proxies = database.get_all_proxies()
        for p in proxies:
            proxy_str = p['proxy_str']
            # If it doesn't already have a scheme, prepend it from the type
            if "://" not in proxy_str and p['type'] and p['type'].lower() != 'http':
                scheme = p['type'].lower()
                # For requests/chrome, socks5h is often better but let's use socks5
                proxy_str = f"{scheme}://{proxy_str}"
            self.proxy_input.addItem(proxy_str)

    def load_data(self):
        self.name_input.setText(self.profile.get('name', ''))
        self.notes_input.setPlainText(self.profile.get('notes', ''))
        self.proxy_input.setCurrentText(self.profile.get('proxy', ''))
        self.ua_input.setPlainText(self.profile.get('user_agent', ''))
        
        version = self.profile.get('chrome_version', '133')
        index = self.version_input.findText(version)
        if index >= 0:
            self.version_input.setCurrentIndex(index)
        else:
            self.version_input.setCurrentText(version)

    def save_data(self):
        name = self.name_input.text().strip()
        if not name:
            QMessageBox.warning(self, "提示", "环境名称不能为空！")
            return

        self.profile_data = {
            'name': name,
            'notes': self.notes_input.toPlainText().strip(),
            'proxy': self.proxy_input.currentText().strip(),
            'user_agent': self.ua_input.toPlainText().strip(),
            'chrome_version': self.version_input.currentText().strip() or '133'
        }
        self.accept()

    def get_data(self):
        return self.profile_data
