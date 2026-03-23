import sys
import os
import asyncio
from PyQt6.QtWidgets import QApplication
from PyQt6.QtCore import Qt
from PyQt6.QtNetwork import QLocalServer, QLocalSocket
from ui_main_window import MainWindow
import proxy_relay

def main():
    # If launched with --relay, run the proxy relay logic instead of the GUI
    if "--relay" in sys.argv:
        # IMPORTANT: Remove "--relay" so proxy_relay's argparse doesn't crash on unknown argument
        sys.argv.remove("--relay")
        
        try:
            asyncio.run(proxy_relay.main())
        except (KeyboardInterrupt, SystemExit):
            pass
        except Exception as e:
            # Log error since this is a background process
            with open("proxy_relay.log", "a") as f:
                import datetime
                f.write(f"[{datetime.datetime.now()}] Relay crash: {e}\n")
        return

    # Initialize database before starting the UI
    import database
    database.init_db()
    
    # DPI Awareness for Windows EXE
    import ctypes
    try:
        # PROCESS_SYSTEM_DPI_AWARE (1) for Windows 8.1+
        ctypes.windll.shcore.SetProcessDpiAwareness(1)
    except Exception:
        try:
            # Fallback for Windows 7/8
            ctypes.windll.user32.SetProcessDPIAware()
        except Exception:
            pass

    os.environ.setdefault("QT_ENABLE_HIGHDPI_SCALING", "1")
    os.environ.setdefault("QT_AUTO_SCREEN_SCALE_FACTOR", "1")

    app = QApplication(sys.argv)
    app.setHighDpiScaleFactorRoundingPolicy(Qt.HighDpiScaleFactorRoundingPolicy.PassThrough)

    # Single Instance Check
    server_name = "AntigravityAds_SingleInstance_9988"
    socket = QLocalSocket()
    socket.connectToServer(server_name)
    if socket.waitForConnected(500):
        # Already running, just exit
        return

    # Clean up old server if it exists
    QLocalServer.removeServer(server_name)
    server = QLocalServer()
    server.listen(server_name)

    # Modern style for the app
    app.setStyle("Fusion")
    
    window = MainWindow()
    
    # When a new instance tries to start, show the current one
    server.newConnection.connect(lambda: (window.showNormal(), window.activateWindow(), window.raise_()))

    window.show()
    sys.exit(app.exec())

if __name__ == "__main__":
    main()
