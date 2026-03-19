import sys
import asyncio
from PyQt6.QtWidgets import QApplication
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
    
    app = QApplication(sys.argv)
    
    # Modern style for the app
    app.setStyle("Fusion")
    
    window = MainWindow()
    window.show()
    sys.exit(app.exec())

if __name__ == "__main__":
    main()
