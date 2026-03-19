import win32gui
import win32con
import win32api
import pynput
import threading
import time

class InputSyncer:
    def __init__(self):
        self.master_hwnd = None
        self.follower_hwnds = []
        self.active = False
        self.mouse_enabled = True
        self.key_enabled = True
        
        self.mouse_listener = None
        self.key_listener = None
        self.last_sync_time = 0

    def start(self, master_hwnd, follower_hwnds):
        self.master_hwnd = master_hwnd
        self.follower_hwnds = [h for h in follower_hwnds if h != master_hwnd]
        self.active = True
        
        # Start Listeners in separate threads
        self.mouse_listener = pynput.mouse.Listener(on_click=self.on_click, on_scroll=self.on_scroll)
        self.key_listener = pynput.keyboard.Listener(on_press=self.on_press, on_release=self.on_release)
        
        self.mouse_listener.start()
        self.key_listener.start()

    def stop(self):
        self.active = False
        # Stop listeners in a separate thread to avoid UI hang
        def _stop_listeners():
            if self.mouse_listener:
                self.mouse_listener.stop()
            if self.key_listener:
                self.key_listener.stop()
            self.mouse_listener = None
            self.key_listener = None
            
        threading.Thread(target=_stop_listeners, daemon=True).start()

    def is_master_active(self):
        if not self.master_hwnd: return False
        try:
            return win32gui.GetForegroundWindow() == self.master_hwnd
        except: return False

    def on_click(self, x, y, button, pressed):
        if not self.active or not self.mouse_enabled or not self.is_master_active():
            return
            
        # Convert screen coords to master client coords
        try:
            client_x, client_y = win32gui.ScreenToClient(self.master_hwnd, (int(x), int(y)))
        except: return
        
        # Build lparam
        lparam = win32api.MAKELONG(client_x, client_y)
        
        msg = win32con.WM_LBUTTONDOWN if pressed else win32con.WM_LBUTTONUP
        if button == pynput.mouse.Button.right:
            msg = win32con.WM_RBUTTONDOWN if pressed else win32con.WM_RBUTTONUP
            
        for hwnd in self.follower_hwnds:
            if not win32gui.IsWindow(hwnd): continue
            
            # Optimization: Send MOUSEMOVE to "wake up" the target area hover state
            if pressed:
                win32gui.PostMessage(hwnd, win32con.WM_MOUSEMOVE, 0, lparam)
            
            win32gui.PostMessage(hwnd, msg, win32con.MK_LBUTTON if pressed else 0, lparam)

    def on_scroll(self, x, y, dx, dy):
        if not self.active or not self.mouse_enabled or not self.is_master_active():
            return
        
        delta = 120 if dy > 0 else -120
        wparam = win32api.MAKELONG(0, delta)
        
        for hwnd in self.follower_hwnds:
            if not win32gui.IsWindow(hwnd): continue
            # WM_MOUSEWHEEL takes screen coords, not client coords
            lparam = win32api.MAKELONG(int(x), int(y))
            win32gui.PostMessage(hwnd, win32con.WM_MOUSEWHEEL, wparam, lparam)

    def on_press(self, key):
        if not self.active or not self.key_enabled or not self.is_master_active():
            return
            
        try:
            # Handle special keys
            if hasattr(key, 'vk'):
                vk = key.vk
            elif hasattr(key, 'value') and hasattr(key.value, 'vk'):
                vk = key.value.vk
            else:
                char = getattr(key, 'char', None)
                if char:
                    vk = win32api.VkKeyScan(char) & 0xFF
                else: return
            
            for hwnd in self.follower_hwnds:
                if not win32gui.IsWindow(hwnd): continue
                
                # Send KeyDown
                win32gui.PostMessage(hwnd, win32con.WM_KEYDOWN, vk, 0)
                
                # For printable characters, also send WM_CHAR for better compatibility
                if hasattr(key, 'char') and key.char:
                    win32gui.PostMessage(hwnd, win32con.WM_CHAR, ord(key.char), 0)
                    
        except Exception:
            pass

    def on_release(self, key):
        if not self.active or not self.key_enabled or not self.is_master_active():
            return
        try:
            if hasattr(key, 'vk'):
                vk = key.vk
            elif hasattr(key, 'value') and hasattr(key.value, 'vk'):
                vk = key.value.vk
            else:
                char = getattr(key, 'char', None)
                if char:
                    vk = win32api.VkKeyScan(char) & 0xFF
                else: return

            for hwnd in self.follower_hwnds:
                if not win32gui.IsWindow(hwnd): continue
                win32gui.PostMessage(hwnd, win32con.WM_KEYUP, vk, 0)
        except Exception:
            pass
