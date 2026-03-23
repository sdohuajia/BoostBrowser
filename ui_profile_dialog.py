from PyQt6.QtWidgets import (QDialog, QVBoxLayout, QFormLayout, 
                               QLineEdit, QTextEdit, QPushButton, QHBoxLayout, QMessageBox, QLabel, QComboBox, QGroupBox)
from PyQt6.QtCore import Qt
from ui_proxy_selector import ProxySelectorDialog

class ProfileDialog(QDialog):
    def __init__(self, parent=None, profile=None):
        super().__init__(parent)
        self.profile = profile
        self.setWindowTitle("创建环境" if not profile else "编辑环境")
        self.setMinimumWidth(400)
        self.setup_ui()
        if profile:
            self.load_data()
        else:
            # For new profile: default version 146 and auto-randomize
            self.version_input.setCurrentText("146")
            self.generate_random_ua()
            self.generate_random_hw()

    def setup_ui(self):
        self.setStyleSheet("""
            QDialog { background: #141824; color: #dce4f6; }
            QLabel { color: #bfcae2; }
            QLineEdit, QTextEdit, QComboBox {
                background: #1b2130; color: #e6eeff; border: 1px solid #34405b;
                border-radius: 8px; padding: 6px 10px;
            }
            QLineEdit:focus, QTextEdit:focus, QComboBox:focus { border-color: #5f87d1; }
            QComboBox QAbstractItemView {
                background-color: #1b2130;
                color: #e6eeff;
                selection-background-color: #34405b;
                outline: none;
            }
            QPushButton { 
                background: #1d2230; color: #d6dff3; border: 1px solid #36405a;
                border-radius: 8px; padding: 7px 14px; 
            }
            QPushButton:hover { background: #2a3246; }
            QPushButton#ActionBtn { background-color: #24324c; color: #dbe8ff; border: 1px solid #3f5e94; font-weight: 600; }
            QPushButton#ActionBtn:hover { background: #2f4163; }

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
            QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {
                height: 0px;
            }
            QScrollBar::add-page:vertical, QScrollBar::sub-page:vertical {
                background: none;
            }
            QScrollBar:horizontal {
                border: none;
                background: #141824;
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

        layout = QVBoxLayout(self)
        form_layout = QFormLayout()

        self.name_input = QLineEdit()
        self.name_input.setPlaceholderText("例如: Facebook 账号 1")
        form_layout.addRow("环境名称 <font color='red'>*</font>:", self.name_input)

        self.notes_input = QTextEdit()
        self.notes_input.setPlaceholderText("账号密码或其它备注信息...")
        self.notes_input.setMaximumHeight(80)
        form_layout.addRow("备注信息:", self.notes_input)

        self.proxy_input = QLineEdit()
        self.proxy_input.setReadOnly(True)
        self.proxy_input.setPlaceholderText("点击选择代理...")
        
        self.btn_select_proxy = QPushButton("选择代理")
        self.btn_select_proxy.setObjectName("ActionBtn")
        self.btn_select_proxy.clicked.connect(self.open_proxy_selector)
        
        proxy_row = QHBoxLayout()
        proxy_row.addWidget(self.proxy_input)
        proxy_row.addWidget(self.btn_select_proxy)
        
        form_layout.addRow("代理 (Proxy):", proxy_row)

        self.version_input = QComboBox()
        self.version_input.addItems(["146", "145", "140", "136", "135", "134", "133", "132", "131", "130", "125", "120", "110", "100", "90"])
        form_layout.addRow("内核版本 (Chrome):", self.version_input)

        self.timezone_input = QComboBox()
        self.timezone_input.setEditable(True)
        self.timezone_input.addItems(["Auto", "UTC", "Asia/Shanghai", "America/New_York", "Europe/London", "Asia/Tokyo"])
        form_layout.addRow("时区 (Timezone):", self.timezone_input)

        self.languages_input = QLineEdit()
        self.languages_input.setPlaceholderText("例如: zh-CN,zh;q=0.9,en;q=0.8")
        form_layout.addRow("语言 (Languages):", self.languages_input)

        self.ua_input = QTextEdit()
        self.ua_input.setPlaceholderText("可留空，默认为所选内核版本的现代 UA")
        self.ua_input.setMaximumHeight(60)
        
        # Adding a button to generate random UA
        ua_layout = QHBoxLayout()
        ua_layout.addWidget(self.ua_input)
        
        self.btn_random_ua = QPushButton("随机 UA")
        self.btn_random_ua.setObjectName("ActionBtn")
        self.btn_random_ua.clicked.connect(self.generate_random_ua)
        
        # form_layout.addRow("User-Agent:", ua_layout) # QFormLayout doesn't easily accept layouts as fields without a wrapper
        
        ua_widget = QVBoxLayout()
        ua_widget.addLayout(ua_layout)
        ua_widget.addWidget(self.btn_random_ua)
        
        form_layout.addRow("User-Agent:", ua_widget)

        layout.addLayout(form_layout)

        # --- Hardware Fingerprinting ---
        hw_group = QGroupBox("硬件指纹 (Hardware Fingerprinting)")
        hw_form = QFormLayout()
        
        self.memory_input = QComboBox()
        self.memory_input.addItems(["2", "4", "8", "16", "32"])
        self.memory_input.setCurrentText("8")
        hw_form.addRow("设备内存 (GB):", self.memory_input)
        
        self.cpu_input = QComboBox()
        self.cpu_input.addItems(["2", "4", "8", "12", "16", "24", "32"])
        self.cpu_input.setCurrentText("8")
        hw_form.addRow("CPU 核心数:", self.cpu_input)
        
        self.webgl_vendor_input = QLineEdit()
        self.webgl_vendor_input.setPlaceholderText("例如: Google Inc. (NVIDIA)")
        hw_form.addRow("WebGL 厂商:", self.webgl_vendor_input)
        
        self.webgl_renderer_input = QLineEdit()
        self.webgl_renderer_input.setPlaceholderText("例如: ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 ...)")
        hw_form.addRow("WebGL 渲染器:", self.webgl_renderer_input)
        
        self.btn_random_hw = QPushButton("随机硬件指纹")
        self.btn_random_hw.setObjectName("ActionBtn")
        self.btn_random_hw.clicked.connect(self.generate_random_hw)
        hw_form.addRow("", self.btn_random_hw)
        
        hw_group.setLayout(hw_form)
        layout.addWidget(hw_group)

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

    def open_proxy_selector(self):
        dialog = ProxySelectorDialog(self)
        if dialog.exec():
            selected = dialog.get_selected_proxy()
            self.proxy_input.setText(selected)

    def generate_random_hw(self):
        import random
        # Real-world common hardware profiles
        memories = ["4", "8", "8", "16", "16", "32"]
        cpus = ["4", "4", "8", "8", "12", "16"]
        
        gpu_profiles = [
            ("Google Inc. (NVIDIA)", "ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0, D3D11)"),
            ("Google Inc. (NVIDIA)", "ANGLE (NVIDIA, NVIDIA GeForce GTX 1650 Direct3D11 vs_5_0 ps_5_0, D3D11)"),
            ("Google Inc. (NVIDIA)", "ANGLE (NVIDIA, NVIDIA GeForce RTX 4070 Laptop GPU Direct3D11 vs_5_0 ps_5_0, D3D11)"),
            ("Google Inc. (Intel)", "ANGLE (Intel, Intel(R) UHD Graphics 630 Direct3D11 vs_5_0 ps_5_0, D3D11)"),
            ("Google Inc. (Intel)", "ANGLE (Intel, Intel(R) Iris(R) Xe Graphics Direct3D11 vs_5_0 ps_5_0, D3D11)"),
            ("Google Inc. (ATI Technologies Inc.)", "ANGLE (ATI Technologies Inc., AMD Radeon(TM) Graphics Direct3D11 vs_5_0 ps_5_0, D3D11)")
        ]
        
        self.memory_input.setCurrentText(random.choice(memories))
        self.cpu_input.setCurrentText(random.choice(cpus))
        
        vendor, renderer = random.choice(gpu_profiles)
        self.webgl_vendor_input.setText(vendor)
        self.webgl_renderer_input.setText(renderer)

    def load_data(self):
        self.name_input.setText(self.profile.get('name', ''))
        self.notes_input.setPlainText(self.profile.get('notes', ''))
        self.proxy_input.setText(self.profile.get('proxy', ''))
        self.ua_input.setPlainText(self.profile.get('user_agent', ''))
        
        version = self.profile.get('chrome_version', '146')
        index = self.version_input.findText(version)
        if index >= 0:
            self.version_input.setCurrentIndex(index)
        else:
            self.version_input.setCurrentText(version)
            
        # Hardware
        self.memory_input.setCurrentText(str(self.profile.get('device_memory', '8')))
        self.cpu_input.setCurrentText(str(self.profile.get('hardware_concurrency', '8')))
        self.webgl_vendor_input.setText(self.profile.get('webgl_vendor', 'Google Inc. (NVIDIA)'))
        self.webgl_renderer_input.setText(self.profile.get('webgl_renderer', 'ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0, D3D11)'))
            
        # Load new fields
        self.timezone_input.setCurrentText(self.profile.get('timezone', 'Auto'))
        self.languages_input.setText(self.profile.get('languages', 'zh-CN,en-US'))

    def save_data(self):
        name = self.name_input.text().strip()
        if not name:
            QMessageBox.warning(self, "提示", "环境名称不能为空！")
            return

        # Handle proxy value from proxy input
        proxy_val = self.proxy_input.text().strip()

        self.profile_data = {
            'name': name,
            'notes': self.notes_input.toPlainText().strip(),
            'proxy': proxy_val,
            'user_agent': self.ua_input.toPlainText().strip(),
            'chrome_version': self.version_input.currentText().strip() or '133',
            'device_memory': int(self.memory_input.currentText() or 8),
            'hardware_concurrency': int(self.cpu_input.currentText() or 8),
            'webgl_vendor': self.webgl_vendor_input.text().strip(),
            'webgl_renderer': self.webgl_renderer_input.text().strip(),
            'timezone': self.timezone_input.currentText().strip(),
            'languages': self.languages_input.text().strip()
        }
        self.accept()

    def get_data(self):
        return self.profile_data
