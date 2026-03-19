import os
import subprocess
import threading
import time
import socket
import win32gui
import win32process
import collections
import shutil
import ctypes
import sys
import urllib.parse
import json
import random

# Modern Chrome User-Agents for common operating systems
MODERN_USER_AGENTS = [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"
]

# Logging setup
def log_debug(msg):
    try:
        with open("browser_launcher.log", "a", encoding="utf-8") as f:
            f.write(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {msg}\n")
    except: pass

CHROME_PATHS = [
    r"C:\Program Files\Google\Chrome\Application\chrome.exe",
    r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
    os.path.join(os.environ.get("LOCALAPPDATA", ""), r"Google\Chrome\Application\chrome.exe")
]

def get_chrome_path():
    for path in CHROME_PATHS:
        if path and os.path.exists(path):
            return path
    return "chrome.exe"

def get_free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(('', 0))
        return s.getsockname()[1]

def parse_proxy(proxy_str):
    if "://" not in proxy_str:
        proxy_str = "http://" + proxy_str
        
    parsed = urllib.parse.urlparse(proxy_str)
    scheme = parsed.scheme if parsed.scheme else "http"
    host = parsed.hostname
    port = parsed.port if parsed.port else 80
    user = parsed.username or ""
    password = parsed.password or ""
    return scheme, host, port, user, password

def create_proxy_extension(user, password, extension_dir):
    os.makedirs(extension_dir, exist_ok=True)
    manifest_json = {
        "version": "1.0.0",
        "manifest_version": 2,
        "name": "AntigravityAds Auth",
        "permissions": ["webRequest", "webRequestBlocking", "<all_urls>"],
        "background": {"scripts": ["background.js"]}
    }
    background_js = f"""
    chrome.webRequest.onAuthRequired.addListener(
        function(details) {{
            return {{ authCredentials: {{ username: "{user}", password: "{password}" }} }};
        }},
        {{urls: ["<all_urls>"]}},
        ['blocking']
    );
    """
    with open(os.path.join(extension_dir, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(manifest_json, f)
    with open(os.path.join(extension_dir, "background.js"), "w", encoding="utf-8") as f:
        f.write(background_js)

def create_stealth_extension(extension_dir):
    os.makedirs(extension_dir, exist_ok=True)
    manifest_json = {
        "manifest_version": 2,
        "name": "Antigravity Browser Stealth",
        "version": "1.2.0",
        "content_scripts": [
            {
                "matches": ["<all_urls>"],
                "js": ["content_script.js"],
                "run_at": "document_start",
                "all_frames": True
            }
        ]
    }
    
    # This script runs in the 'isolated world' but injects a script tag into the 'main world'
    content_script_js = """
    (function() {
        try {
            const script = document.createElement('script');
            script.textContent = `
                (function() {
                    // 1. Hide navigator.webdriver
                    try {
                        const newProto = Object.getPrototypeOf(navigator);
                        delete newProto.webdriver;
                        Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
                    } catch (e) {
                         Object.defineProperty(navigator, 'webdriver', { get: () => false });
                    }

                    // 2. Hide navigator.platform
                    Object.defineProperty(navigator, 'platform', { get: () => 'Win32' });

                    // 3. Mask Languages
                    Object.defineProperty(navigator, 'languages', { get: () => ['zh-CN', 'zh', 'en-US', 'en'] });

                    // 4. Mock Chrome properties
                    window.chrome = window.chrome || {
                        app: { isInstalled: false, installState: () => {}, getDetails: () => {}, getIsInstalled: () => {} },
                        runtime: {
                            OnInstalledReason: { INSTALL: 'install', UPDATE: 'update', CHROME_UPDATE: 'chrome_update', SHARED_MODULE_UPDATE: 'shared_module_update' },
                            OnRestartRequiredReason: { APP_UPDATE: 'app_update', OS_UPDATE: 'os_update', PERIODIC: 'periodic' },
                            PlatformArch: { ARM: 'arm', ARM64: 'arm64', X86_32: 'x86-32', X86_64: 'x86-64' },
                            PlatformNaclArch: { ARM: 'arm', X86_32: 'x86-32', X86_64: 'x86-64' },
                            PlatformOs: { ANDROID: 'android', CROS: 'cros', LINUX: 'linux', MAC: 'mac', OPENBSD: 'openbsd', WIN: 'win' },
                            RequestUpdateCheckStatus: { THROTTLED: 'throttled', NO_UPDATE: 'no_update', UPDATE_AVAILABLE: 'update_available' }
                        },
                        csi: () => {},
                        loadTimes: () => {}
                    };

                    // 5. Mask Plugins
                    const pluginPlaceholder = {
                        description: "Portable Document Format",
                        filename: "internal-pdf-viewer",
                        name: "Chrome PDF Viewer",
                        0: {type: "application/pdf", suffixes: "pdf", description: "Portable Document Format"}
                    };
                    Object.defineProperty(navigator, 'plugins', { get: () => [pluginPlaceholder] });
                    
                    console.log('Antigravity Stealth: Success');
                })();
            `;
            document.documentElement.prepend(script);
            script.remove();
        } catch (e) {
            console.error('Stealth injection failed:', e);
        }
    })();
    """
    with open(os.path.join(extension_dir, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(manifest_json, f)
    with open(os.path.join(extension_dir, "content_script.js"), "w", encoding="utf-8") as f:
        f.write(content_script_js)

class BrowserController:
    def __init__(self):
        self.active_processes = {}
        self.relay_processes = {}
        
        if getattr(sys, 'frozen', False):
            self.base_dir = os.path.dirname(sys.executable)
        else:
            self.base_dir = os.path.dirname(os.path.abspath(__file__))
        log_debug(f"Initialized BrowserController. BaseDir: {self.base_dir}")

    def launch_profile(self, profile):
        profile_id = profile['id']
        if self.is_running(profile_id):
            log_debug(f"Profile {profile_id} already running.")
            return False
        
        chrome_exe = get_chrome_path()
        browser_data_root = os.path.join(self.base_dir, "browser_data")
        profile_dir = os.path.join(browser_data_root, f"profile_{profile_id}")
        
        # Clean up possible lock files if browser is not running
        self._cleanup_locks(profile_dir)
        os.makedirs(profile_dir, exist_ok=True)
        
        log_debug(f"Launching profile {profile_id}. Chrome: {chrome_exe}")
        
        # Base arguments
        args = [
            chrome_exe,
            f"--user-data-dir={profile_dir}",
            "--no-first-run",
            "--no-default-browser-check",
            "--disable-sync",
            "--disable-background-networking",
            # Anti-detection and Stealth flags
            "--disable-blink-features=AutomationControlled",
            "--disable-infobars",
            "--password-store=basic",
            "--use-mock-keychain",
            "--disable-features=IsolateOrigins,site-per-process",
            "--lang=zh-CN",
            "--flag-switches-begin",
            "--disable-blink-features=AutomationControlled",
            "--flag-switches-end"
        ]
        
        loaded_extensions = []
        
        # Always use stealth extension
        stealth_ext_dir = os.path.join(profile_dir, "stealth_ext")
        create_stealth_extension(stealth_ext_dir)
        loaded_extensions.append(stealth_ext_dir)
        
        # Proxy treatment
        if profile.get('proxy'):
            proxy = profile['proxy']
            scheme, host, port, user, password = parse_proxy(proxy)
            log_debug(f"Proxy detected: {scheme}://{host}:{port}")
            
            if scheme.startswith('socks') and user and password:
                local_port = get_free_port()
                if getattr(sys, 'frozen', False):
                    relay_cmd = [sys.executable, "--relay"]
                else:
                    relay_cmd = [sys.executable, sys.argv[0], "--relay"]
                
                relay_cmd += [
                    "--local-port", str(local_port),
                    "--remote-host", host,
                    "--remote-port", str(port),
                    "--user", user,
                    "--pwd", password,
                    "--type", scheme
                ]
                
                try:
                    relay_proc = subprocess.Popen(relay_cmd, creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
                    self.relay_processes[profile_id] = relay_proc
                    args.append(f"--proxy-server=http://127.0.0.1:{local_port}")
                    log_debug(f"Relay started on port {local_port}")
                except Exception as e:
                    log_debug(f"Relay failed: {e}")
                    args.append(f"--proxy-server={scheme}://{host}:{port}")
            else:
                args.append(f"--proxy-server={scheme}://{host}:{port}")
                ext_dir = os.path.join(profile_dir, "proxy_ext")
                create_proxy_extension(user, password, ext_dir)
                loaded_extensions.append(ext_dir)
        
        if loaded_extensions:
            args.append(f"--load-extension={','.join(loaded_extensions)}")
        
        if profile.get('user_agent'):
            ua = profile['user_agent']
            # If manually specified UA is old or mismatched (e.g. Opera, MSIE, Ancient Windows, Linux on Windows), replace it
            suspicious_markers = [
                "Android 1.5", "Firefox/5.0", "Opera", "X11", "Linux", "Macintosh", "Intel Mac",
                "MSIE", "Trident", "Windows 98", "Win 9x", "Windows NT 5", "Windows NT 6", "Windows NT 6.1"
            ]
            if any(marker in ua for marker in suspicious_markers):
                version = profile.get('chrome_version', '146')
                ua = f"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{version}.0.0.0 Safari/537.36"
                log_debug(f"Suspicious or mismatched UA detected ({profile.get('user_agent')}), replaced with: {ua}")
            args.append(f"--user-agent={ua}")
        else:
            # Generate UA based on selected version
            version = profile.get('chrome_version', '146')
            ua = f"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{version}.0.0.0 Safari/537.36"
            args.append(f"--user-agent={ua}")
            
        # Target URL
        args.append("https://www.whoer.net")
        
        log_debug(f"Full Args: {' '.join(args)}")
        
        try:
            process = subprocess.Popen(args)
            self.active_processes[profile_id] = process
            log_debug(f"Process started. PID: {process.pid}")
            
            # Name polling
            threading.Thread(target=self._rename_window_task, args=(profile_id, profile['name']), daemon=True).start()
            
            # Wait thread
            def wait_thread():
                process.wait()
                log_debug(f"Process {profile_id} (PID {process.pid}) exited.")
                if profile_id in self.active_processes:
                    del self.active_processes[profile_id]
                self.close_relay(profile_id)
            threading.Thread(target=wait_thread, daemon=True).start()
            
            return True
        except Exception as e:
            log_debug(f"Launch error: {e}")
            return False

    def _cleanup_locks(self, profile_dir):
        """Try to clean up Chrome lock files if they exist."""
        lock_file = os.path.join(profile_dir, "SingletonLock")
        if os.path.exists(lock_file):
            try: os.remove(lock_file)
            except: pass

    def _rename_window_task(self, profile_id, name):
        """Rename window title after a short delay."""
        for _ in range(40): # Increased polling
            time.sleep(0.5)
            if profile_id not in self.active_processes: break
            pid = self.active_processes[profile_id].pid
            hwnd = self._find_hwnd_by_pid(pid)
            if hwnd:
                try:
                    # Change to a unique title
                    win32gui.SetWindowText(hwnd, f"[{profile_id}] {name}")
                    log_debug(f"Window renamed for profile {profile_id}")
                except: pass
                break

    def _find_hwnd_by_pid(self, pid):
        def callback(hwnd, hwnds):
            if win32gui.IsWindowVisible(hwnd):
                try:
                    _, found_pid = win32process.GetWindowThreadProcessId(hwnd)
                    if found_pid == pid:
                        hwnds.append(hwnd)
                except: pass
        hwnds = []
        win32gui.EnumWindows(callback, hwnds)
        return hwnds[0] if hwnds else None

    def is_running(self, profile_id):
        if profile_id in self.active_processes:
            if self.active_processes[profile_id].poll() is None:
                return True
            del self.active_processes[profile_id]
        return False

    def close_profile(self, profile_id):
        if profile_id in self.active_processes:
            try: self.active_processes[profile_id].terminate()
            except: pass
            del self.active_processes[profile_id]
        self.close_relay(profile_id)

    def close_relay(self, profile_id):
        if profile_id in self.relay_processes:
            try: self.relay_processes[profile_id].terminate()
            except: pass
            del self.relay_processes[profile_id]

browser_controller = BrowserController()
