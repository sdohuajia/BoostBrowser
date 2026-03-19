from PyQt6.QtWidgets import QWidget, QVBoxLayout, QHBoxLayout, QPushButton, QLabel, QCheckBox
from PyQt6.QtCore import Qt, QPoint, pyqtSignal

class SyncStatusWindow(QWidget):
    stop_requested = pyqtSignal()
    config_changed = pyqtSignal(bool, bool) # mouse_enabled, key_enabled

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowFlags(Qt.WindowType.WindowStaysOnTopHint | Qt.WindowType.FramelessWindowHint | Qt.WindowType.Tool)
        self.setAttribute(Qt.WidgetAttribute.WA_TranslucentBackground)
        
        self.old_pos = None
        self.setup_ui()
        
    def setup_ui(self):
        layout = QVBoxLayout(self)
        
        # Main container with style
        container = QWidget()
        container.setObjectName("Container")
        container.setStyleSheet("""
            #Container {
                background-color: rgba(30, 30, 46, 230);
                border: 2px solid #89b4fa;
                border-radius: 12px;
            }
            QLabel { color: #cdd6f4; font-size: 14px; font-weight: bold; }
            QCheckBox { color: #bac2de; font-size: 12px; }
            QPushButton#StopBtn {
                background-color: #f38ba8;
                color: #11111b;
                border: none;
                border-radius: 6px;
                padding: 8px;
                font-weight: bold;
                font-size: 13px;
            }
            QPushButton#StopBtn:hover { background-color: #eba0ac; }
        """)
        
        c_layout = QVBoxLayout(container)
        
        # Title/Status
        self.status_label = QLabel("🔄 同步中...")
        c_layout.addWidget(self.status_label, alignment=Qt.AlignmentFlag.AlignCenter)
        
        # Configs
        self.chk_mouse = QCheckBox("同步鼠标动作")
        self.chk_mouse.setChecked(True)
        self.chk_mouse.stateChanged.connect(self.emit_config)
        
        self.chk_key = QCheckBox("同步键盘输入")
        self.chk_key.setChecked(True)
        self.chk_key.stateChanged.connect(self.emit_config)
        
        c_layout.addWidget(self.chk_mouse)
        c_layout.addWidget(self.chk_key)
        
        # Stop Button
        btn_stop = QPushButton("停止同步")
        btn_stop.setObjectName("StopBtn")
        btn_stop.clicked.connect(self.stop_requested.emit)
        c_layout.addWidget(btn_stop)
        
        layout.addWidget(container)
        self.setFixedSize(180, 150)

    def emit_config(self):
        self.config_changed.emit(self.chk_mouse.isChecked(), self.chk_key.isChecked())

    def mousePressEvent(self, event):
        if event.button() == Qt.MouseButton.LeftButton:
            self.old_pos = event.globalPosition().toPoint()

    def mouseMoveEvent(self, event):
        if self.old_pos:
            delta = event.globalPosition().toPoint() - self.old_pos
            self.move(self.x() + delta.x(), self.y() + delta.y())
            self.old_pos = event.globalPosition().toPoint()

    def mouseReleaseEvent(self, event):
        self.old_pos = None
