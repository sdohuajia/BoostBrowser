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
        self.version_input.addItems(["146", "140", "130", "120", "110", "100", "90"])
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
        self.btn_save.setStyleSheet("background-color: #2b5fe8; color: white; padding: 6px; border-radius: 4px;")
        self.btn_cancel.setStyleSheet("background-color: #4a4a4a; color: white; padding: 6px; border-radius: 4px;")

        self.btn_save.clicked.connect(self.save_data)
        self.btn_cancel.clicked.connect(self.reject)

        btn_layout.addStretch()
        btn_layout.addWidget(self.btn_cancel)
        btn_layout.addWidget(self.btn_save)

        layout.addLayout(btn_layout)

    def generate_random_ua(self):
        try:
            from faker import Faker
            fake = Faker()
            self.ua_input.setPlainText(fake.user_agent())
        except ImportError:
            QMessageBox.warning(self, "错误", "缺少 Faker 库，请使用 pip install Faker 安装。")

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
        
        version = self.profile.get('chrome_version', '146')
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
            'chrome_version': self.version_input.currentText().strip()
        }
        self.accept()

    def get_data(self):
        return self.profile_data
