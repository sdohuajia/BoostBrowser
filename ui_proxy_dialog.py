from PyQt6.QtWidgets import (QDialog, QVBoxLayout, QHBoxLayout, QFormLayout, 
                               QLineEdit, QTextEdit, QPushButton, QLabel, 
                               QComboBox, QMessageBox, QFrame)
from PyQt6.QtCore import Qt, QThread, pyqtSignal
from proxy_tester import test_proxy

class TestThread(QThread):
    finished = pyqtSignal(bool, dict)

    def __init__(self, proxy_str, proxy_type):
        super().__init__()
        self.proxy_str = proxy_str
        self.proxy_type = proxy_type

    def run(self):
        success, details = test_proxy(self.proxy_str, self.proxy_type)
        self.finished.emit(success, details)

class ProxyDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("添加代理")
        self.setMinimumWidth(500)
        self.setup_ui()

    def setup_ui(self):
        self.setStyleSheet("""
            QDialog { background: #141824; color: #dce4f6; }
            QLabel { color: #bfcae2; }
            QLineEdit, QTextEdit, QComboBox {
                background: #1b2130; color: #e6eeff; border: 1px solid #34405b;
                border-radius: 8px; padding: 6px 10px;
            }
            QLineEdit:focus, QTextEdit:focus, QComboBox:focus { border-color: #5f87d1; }
            QPushButton {
                background: #1d2230; color: #d6dff3; border: 1px solid #36405a;
                border-radius: 8px; padding: 7px 14px;
            }
            QPushButton:hover { background: #2a3246; }
            QPushButton#PrimaryBtn { background-color: #4f74b8; color: #f4f8ff; border: 1px solid #5f87d1; font-weight: 700; }
            QPushButton#PrimaryBtn:hover { background: #628bd8; }
            QPushButton#ActionBtn { background-color: #24324c; color: #dbe8ff; border: 1px solid #3f5e94; font-weight: 600; }
            QPushButton#ActionBtn:hover { background: #2f4163; }
        """)

        layout = QVBoxLayout(self)
        
        # Row 1: Proxy Type & Channel
        row1 = QHBoxLayout()
        
        type_layout = QVBoxLayout()
        type_layout.addWidget(QLabel("代理类型"))
        self.type_combo = QComboBox()
        self.type_combo.addItems(["HTTP", "HTTPS", "SOCKS5", "SOCKS4", "SSH"])
        type_layout.addWidget(self.type_combo)
        
        channel_layout = QVBoxLayout()
        channel_layout.addWidget(QLabel("IP查询渠道"))
        self.channel_combo = QComboBox()
        self.channel_combo.addItems(["ip-api.com", "ipinfo.io", "ipify.org"])
        channel_layout.addWidget(self.channel_combo)
        
        row1.addLayout(type_layout)
        row1.addLayout(channel_layout)
        layout.addLayout(row1)
        
        # Row 2: Tags
        layout.addWidget(QLabel("标签"))
        self.tag_combo = QComboBox()
        self.tag_combo.setEditable(True)
        self.tag_combo.setPlaceholderText("查找标签 / 创建标签")
        layout.addWidget(self.tag_combo)
        
        # Row 3: Proxy Info
        layout.addWidget(QLabel("代理信息"))
        self.proxy_info = QTextEdit()
        self.proxy_info.setPlaceholderText("可以在此填写您的代理信息\n格式: ip:port 或 user:pass@ip:port")
        self.proxy_info.setMinimumHeight(150)
        layout.addWidget(self.proxy_info)
        
        # Status Label
        self.status_label = QLabel("等待测试...")
        self.status_label.setStyleSheet("color: #a4a8ba; font-style: italic;")
        layout.addWidget(self.status_label)
        
        # Buttons
        btn_layout = QHBoxLayout()
        
        self.btn_test = QPushButton("测试代理")
        self.btn_test.setObjectName("ActionBtn")
        self.btn_test.clicked.connect(self.action_test)
        
        self.btn_save = QPushButton("确认添加")
        self.btn_save.setObjectName("PrimaryBtn")
        self.btn_save.clicked.connect(self.action_save)
        
        self.btn_cancel = QPushButton("取消")
        self.btn_cancel.clicked.connect(self.reject)
        
        btn_layout.addWidget(self.btn_test)
        btn_layout.addStretch()
        btn_layout.addWidget(self.btn_cancel)
        btn_layout.addWidget(self.btn_save)
        
        layout.addLayout(btn_layout)

    def action_test(self):
        proxy_str = self.proxy_info.toPlainText().strip()
        if not proxy_str:
            QMessageBox.warning(self, "错误", "请先输入代理信息")
            return
            
        selected_type = self.type_combo.currentText().lower()
        
        # Smart fix for common user error: 
        # dropdown is SOCKS5 but string is http://...
        if "://" in proxy_str:
            scheme = proxy_str.split("://")[0].lower()
            if scheme != selected_type and not (scheme == 'socks5h' and selected_type == 'socks5'):
                # Ask or fix? Let's just fix the internal call but keep UI as is, 
                # or better, update the UI to avoid confusion.
                pass

        self.status_label.setText("正在测试中，请稍候...")
        self.status_label.setStyleSheet("color: #a4a8ba; font-style: italic;")
        self.btn_test.setEnabled(False)
        
        self.test_thread = TestThread(proxy_str, self.type_combo.currentText())
        self.test_thread.finished.connect(self.on_test_finished)
        self.test_thread.start()

    def on_test_finished(self, success, details):
        self.btn_test.setEnabled(True)
        if success:
            msg = f"🟢 测试通过! IP: {details.get('ip')} ({details.get('region')})"
            self.status_label.setStyleSheet("color: #1bc5bd; font-weight: bold;")
        else:
            err = details.get('status_msg', '未知错误')
            if 'RemoteDisconnected' in err or 'Connection aborted' in err:
                msg = f"🔴 测试失败: 协议不匹配或代理已关闭\n提示: 检查输入的 http:// 协议头是否与下拉菜单 '{self.type_combo.currentText()}' 一致"
            else:
                msg = f"🔴 测试失败: {err}"
            self.status_label.setStyleSheet("color: #f64e60;")
            
        self.status_label.setText(msg)
        self.last_test_details = details

    def action_save(self):
        proxy_str = self.proxy_info.toPlainText().strip()
        if not proxy_str:
            QMessageBox.warning(self, "错误", "代理信息不能为空")
            return
            
        self.result_data = {
            "proxy_str": proxy_str,
            "type": self.type_combo.currentText(),
            "notes": self.tag_combo.currentText(),
            "test_details": getattr(self, "last_test_details", {})
        }
        self.accept()

    def set_data(self, proxy_obj):
        # Set type
        idx = self.type_combo.findText(proxy_obj['type'])
        if idx >= 0: self.type_combo.setCurrentIndex(idx)
        
        # Set notes
        self.tag_combo.setCurrentText(proxy_obj['notes'] or "")
        
        # Set proxy string
        self.proxy_info.setPlainText(proxy_obj['proxy_str'])
        
        # Set window title
        self.setWindowTitle("编辑代理")
        self.btn_save.setText("保存修改")

    def get_data(self):
        return self.result_data

class BulkProxyDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setStyleSheet("""
            QDialog { background: #141824; color: #dce4f6; }
            QLabel { color: #bfcae2; }
            QTextEdit, QComboBox {
                background: #1b2130; color: #e6eeff; border: 1px solid #34405b;
                border-radius: 8px; padding: 6px 10px;
            }
            QTextEdit:focus, QComboBox:focus { border-color: #5f87d1; }
            QPushButton {
                background: #1d2230; color: #d6dff3; border: 1px solid #36405a;
                border-radius: 8px; padding: 7px 14px;
            }
            QPushButton#PrimaryBtn { background-color: #4f74b8; color: #f4f8ff; border: 1px solid #5f87d1; font-weight: 700; }
        """)
        self.setWindowTitle("批量导入代理")
        self.setMinimumWidth(450)
        self.setMinimumHeight(400)
        
        layout = QVBoxLayout(self)
        
        # Proxy Type
        type_layout = QHBoxLayout()
        type_layout.addWidget(QLabel("批量设定代理类型:"))
        self.type_combo = QComboBox()
        self.type_combo.addItems(["HTTP", "HTTPS", "SOCKS5", "SOCKS4", "SSH"])
        type_layout.addWidget(self.type_combo)
        type_layout.addStretch()
        layout.addLayout(type_layout)
        
        # Text Area
        layout.addWidget(QLabel("请输入代理列表 (每行一个代理):"))
        self.text_edit = QTextEdit()
        self.text_edit.setPlaceholderText("可以在此粘贴您的代理列表\n格式: ip:port 或 username:password@ip:port")
        layout.addWidget(self.text_edit)
        
        # Buttons
        btn_layout = QHBoxLayout()
        self.btn_cancel = QPushButton("取消")
        self.btn_cancel.clicked.connect(self.reject)
        self.btn_save = QPushButton("确认导入")
        self.btn_save.setObjectName("PrimaryBtn")
        self.btn_save.clicked.connect(self.accept)
        
        btn_layout.addStretch()
        btn_layout.addWidget(self.btn_cancel)
        btn_layout.addWidget(self.btn_save)
        layout.addLayout(btn_layout)

    def get_data(self):
        proxy_type = self.type_combo.currentText()
        text = self.text_edit.toPlainText().strip()
        lines = [line.strip() for line in text.split('\n') if line.strip()]
        return {"type": proxy_type, "proxies": lines}
