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
import http.client

# Modern Chrome User-Agents for common operating systems
MODERN_USER_AGENTS = [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.7100.50 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.7000.40 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.6900.30 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.6998.35 Safari/537.36"
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

def create_stealth_extension(extension_dir, memory=8, cores=8, vendor='', renderer='', ua='', languages='zh-CN,en-US', timezone='Auto', dnt=True):
    os.makedirs(extension_dir, exist_ok=True)
    manifest_json = {
        "manifest_version": 2,
        "name": "Antigravity Browser Stealth",
        "version": "1.4.1",
        "description": "Premium Anti-detection, Timezone & Language Protection",
        "permissions": ["<all_urls>", "webNavigation"],
        "content_scripts": [
            {
                "matches": ["<all_urls>"],
                "js": ["content_script.js"],
                "run_at": "document_start",
                "all_frames": True
            }
        ]
    }
    
    # Defaults
    memory = memory or 8
    cores = cores or 8
    vendor = vendor or "Google Inc. (NVIDIA)"
    renderer = renderer or "ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0, D3D11)"
    
    # Parse UA for Client Hints
    import re
    chrome_version_match = re.search(r'Chrome/(\d+)', ua)
    major_version = chrome_version_match.group(1) if chrome_version_match else "134"

    # Languages list
    lang_list = [l.strip() for l in languages.split(',')] if languages else ['zh-CN', 'zh', 'en-US', 'en']
    
    # Timezone injection logic
    tz_script = ""
    if timezone and timezone != 'Auto':
        tz_script = f"""
            try {{
                Intl.DateTimeFormat.prototype.resolvedOptions = (original => {{
                    return function() {{
                        const options = original.apply(this, arguments);
                        options.timeZone = "{timezone}";
                        return options;
                    }};
                }})(Intl.DateTimeFormat.prototype.resolvedOptions);
            }} catch(e) {{}}
        """

    content_script_js = f"""
    (function() {{
        const stealth = function() {{
            const setProperty = (obj, prop, value) => {{
                try {{
                    Object.defineProperty(obj, prop, {{
                        get: () => value,
                        set: () => {{}},
                        enumerable: true,
                        configurable: true
                    }});
                }} catch (e) {{}}
            }};

            const hideFromToString = (fn) => {{
                try {{
                    const originalToString = Function.prototype.toString;
                    setProperty(fn, 'toString', function() {{
                        if (this === fn) return `function ${{fn.name}}() {{ [native code] }}`;
                        return originalToString.call(this);
                    }});
                }} catch (e) {{}}
            }};

            // 1. Better WebDriver Hiding
            try {{
                const newProto = Object.getPrototypeOf(navigator);
                Object.defineProperty(newProto, 'webdriver', {{
                    get: () => false,
                    enumerable: true,
                    configurable: true
                }});
            }} catch (e) {{}}

            // 2. Client Hints (userAgentData)
            if (navigator.userAgentData) {{
                const uaData = {{
                    brands: [
                        {{ brand: 'Not(A:Brand', version: '99' }},
                        {{ brand: 'Google Chrome', version: '{major_version}' }},
                        {{ brand: 'Chromium', version: '{major_version}' }}
                    ],
                    mobile: false,
                    platform: 'Windows'
                }};
                setProperty(navigator, 'userAgentData', uaData);
            }}

            // 3. Hardware & Identity
            setProperty(navigator, 'deviceMemory', {memory});
            setProperty(navigator, 'hardwareConcurrency', {cores});
            setProperty(navigator, 'languages', {json.dumps(lang_list)});
            setProperty(navigator, 'maxTouchPoints', 0);
            setProperty(navigator, 'pdfViewerEnabled', true);
            setProperty(navigator, 'doNotTrack', "{'1' if dnt else '0'}");

            // 4. Timezone Mocking
            {tz_script}

            // 5. Deeper WebGL Masking
            try {{
                const getParameter = WebGLRenderingContext.prototype.getParameter;
                const getParameterProxy = function(parameter) {{
                    if (parameter === 37445) return "{vendor}";
                    if (parameter === 37446) return "{renderer}";
                    return getParameter.apply(this, arguments);
                }};
                hideFromToString(getParameterProxy);
                WebGLRenderingContext.prototype.getParameter = getParameterProxy;
            }} catch (e) {{}}

            // 6. Rich Plugins
            try {{
                const plugins = [
                    {{ name: 'PDF Viewer', filename: 'internal-pdf-viewer', description: 'Portable Document Format' }},
                    {{ name: 'Chrome PDF Viewer', filename: 'internal-pdf-viewer', description: 'Portable Document Format' }}
                ];
                setProperty(navigator, 'plugins', plugins);
            }} catch (e) {{}}

            // 7. Automation Property Cleanup
            const clean = () => {{
                for (const prop in window) {{
                    if (prop.startsWith('cdc_') || prop.startsWith('__$')) {{
                        delete window[prop];
                    }}
                }}
            }};
            clean();
            setInterval(clean, 1000);
            
            // 8. Chrome Runtime Mock
            window.chrome = window.chrome || {{}};
            if (!window.chrome.runtime) {{
                window.chrome.runtime = {{ 
                    sendMessage: () => {{}}, 
                    connect: () => ({{ onMessage: {{ addListener: () => {{}} }}, postMessage: () => {{}} }}) 
                }};
            }}
        }};

        try {{
            const script = document.createElement('script');
            script.textContent = `(${{stealth.toString()}})();`;
            (document.head || document.documentElement).appendChild(script);
            script.remove();
        }} catch (e) {{}}
    }})();
    """
    with open(os.path.join(extension_dir, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(manifest_json, f)
    with open(os.path.join(extension_dir, "content_script.js"), "w", encoding="utf-8") as f:
        f.write(content_script_js)

class BrowserController:
    def __init__(self):
        self.active_processes = {}
        self.relay_processes = {}
        self.profile_debug_ports = {}  # profile_id -> debug_port
        
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
        
        # Determine User-Agent - Essential for consistency
        ua = profile.get('user_agent')
        if not ua:
            ver = profile.get('chrome_version', '146')
            build = random.randint(6000, 7100)
            patch = random.randint(1, 150)
            ua = f"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/{ver}.0.{build}.{patch} Safari/537.36"
        
        # Base arguments - Optimized for Stealth and Passing CF
        args = [
            chrome_exe,
            f"--user-data-dir={profile_dir}",
            # Top-tier Anti-detection
            "--disable-blink-features=AutomationControlled",
            f"--user-agent={ua}",
            "--do-not-track",
            # Standard browser flags
            "--no-first-run",
            "--no-default-browser-check",
            "--disable-infobars",
            "--password-store=basic",
            "--use-mock-keychain",
            "--lang=zh-CN",
            # Performance & Stability (Disabling intrusive features)
            "--disable-features=IsolateOrigins,site-per-process,Translate,OptimizationHints,OptimizationTargetPrediction,OptimizationGuideModelDownloading,InsecureDownloadWarnings",
            "--disable-component-update",
            "--disable-background-timer-throttling",
            "--disable-backgrounding-occluded-windows",
            "--disable-renderer-backgrounding",
            "--disable-client-side-phishing-detection",
            "--disable-default-apps",
            "--disable-extensions-http-throttling",
            "--disable-popup-blocking",
            "--metrics-recording-only",
            # Suppress automation-related logs and features
            "--test-type",
            "--flag-switches-begin",
            "--disable-blink-features=AutomationControlled",
            "--flag-switches-end"
        ]
        
        loaded_extensions = []
        
        # Always use stealth extension with profile-specific parameters
        stealth_ext_dir = os.path.join(profile_dir, "stealth_ext")
        create_stealth_extension(
            stealth_ext_dir,
            memory=profile.get('device_memory'),
            cores=profile.get('hardware_concurrency'),
            vendor=profile.get('webgl_vendor'),
            renderer=profile.get('webgl_renderer'),
            ua=ua,
            languages=profile.get('languages'),
            timezone=profile.get('timezone'),
            dnt=True
        )
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
        
        # Assign a unique debug port for CDP access
        debug_port = get_free_port()
        args.append(f"--remote-debugging-port={debug_port}")
        self.profile_debug_ports[profile_id] = debug_port
        log_debug(f"CDP debug port for profile {profile_id}: {debug_port}")
            
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
        self.profile_debug_ports.pop(profile_id, None)

    def close_relay(self, profile_id):
        if profile_id in self.relay_processes:
            try: self.relay_processes[profile_id].terminate()
            except: pass
            del self.relay_processes[profile_id]

    def _cdp_open_url(self, debug_port, url):
        """Use CDP to open a URL in a new tab for the browser at the given debug port."""
        try:
            conn = http.client.HTTPConnection("127.0.0.1", debug_port, timeout=5)
            # Get list of targets
            conn.request("GET", "/json")
            resp = conn.getresponse()
            targets = json.loads(resp.read().decode())
            conn.close()
            
            # Find an existing page target to use
            page_target = next((t for t in targets if t.get('type') == 'page'), None)
            if not page_target:
                log_debug(f"No page target found on port {debug_port}")
                return False
            
            ws_id = page_target['id']
            
            # Use /json/new to open a new tab with the URL
            conn2 = http.client.HTTPConnection("127.0.0.1", debug_port, timeout=5)
            encoded_url = urllib.parse.quote(url, safe=':/?=&%')
            conn2.request("GET", f"/json/new?{encoded_url}")
            resp2 = conn2.getresponse()
            result = resp2.read()
            conn2.close()
            log_debug(f"Opened new tab on port {debug_port}: {result}")
            return True
        except Exception as e:
            log_debug(f"CDP error on port {debug_port}: {e}")
            return False

    def install_extension_to_profile(self, profile_id, store_url):
        """Install a Chrome extension to a specific running profile via CDP."""
        if not self.is_running(profile_id):
            return False, "未运行"
        debug_port = self.profile_debug_ports.get(profile_id)
        if not debug_port:
            return False, "无调试端口"
        success = self._cdp_open_url(debug_port, store_url)
        return success, "已打开安装页面" if success else "打开失败"

    def install_extension_to_all(self, store_url):
        """Install a Chrome extension to all running profiles via CDP."""
        results = {}
        running_ids = list(self.active_processes.keys())
        for profile_id in running_ids:
            if self.is_running(profile_id):
                ok, msg = self.install_extension_to_profile(profile_id, store_url)
                results[profile_id] = (ok, msg)
        return results

browser_controller = BrowserController()
