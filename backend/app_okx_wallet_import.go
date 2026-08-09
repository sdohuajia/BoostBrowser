package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// The imported unpacked source directory keeps the Chrome Web Store ID, while
	// Chromium assigns this runtime ID to the unpacked extension.
	okxWalletExtensionSourceID = "mcohilncbfahbmgdjkbpemcciiolgcge"
	okxWalletExtensionID       = "ajmpgkcoippjhgcipkhbbajcinebaohc"
)

type OKXWalletImportItem struct {
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

type OKXWalletImportResult struct {
	Total     int                   `json:"total"`
	Succeeded int                   `json:"succeeded"`
	Failed    int                   `json:"failed"`
	Items     []OKXWalletImportItem `json:"items"`
}

var profileOrdinal = regexp.MustCompile(`(\d+)(?:\D*)$`)

// OKXWalletBatchImport imports one mnemonic per profile in the natural order of
// profile names. Mnemonics and password are handled in memory only and are not
// persisted or included in the result/logs.
func (a *App) OKXWalletBatchImport(mnemonicsText, password string, profileIDs []string) (*OKXWalletImportResult, error) {
	mnemonics := nonEmptyLines(mnemonicsText)
	password = strings.TrimSpace(password)
	if len(mnemonics) == 0 {
		return nil, fmt.Errorf("助记词文本为空")
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("钱包密码至少需要 8 位")
	}
	profiles := a.BrowserProfileList()
	selected := map[string]bool{}
	for _, id := range profileIDs {
		if strings.TrimSpace(id) != "" {
			selected[strings.TrimSpace(id)] = true
		}
	}
	if len(selected) > 0 {
		filtered := profiles[:0]
		for _, p := range profiles {
			if selected[p.ProfileId] {
				filtered = append(filtered, p)
			}
		}
		profiles = filtered
	}
	sort.SliceStable(profiles, func(i, j int) bool { return naturalProfileLess(profiles[i].ProfileName, profiles[j].ProfileName) })
	if len(profiles) == 0 {
		return nil, fmt.Errorf("没有可处理的实例")
	}
	if len(mnemonics) < len(profiles) {
		return nil, fmt.Errorf("助记词数量(%d)少于实例数量(%d)", len(mnemonics), len(profiles))
	}
	result := &OKXWalletImportResult{Total: len(profiles), Items: make([]OKXWalletImportItem, 0, len(profiles))}
	for i, profile := range profiles {
		item := OKXWalletImportItem{ProfileID: profile.ProfileId, ProfileName: profile.ProfileName}
		launchArgs := strings.ToLower(strings.Join(profile.LaunchArgs, "\n"))
		if !strings.Contains(launchArgs, okxWalletExtensionSourceID) && !strings.Contains(launchArgs, okxWalletExtensionID) {
			item.Status = "not_installed"
			item.Error = "未安装 OKX Wallet，请先在扩展管理中分配"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		active := &profile
		if !profile.Running || !profile.DebugReady {
			started, err := a.BrowserInstanceStart(profile.ProfileId)
			if err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			active = started
		}
		if active.DebugPort <= 0 {
			item.Status = "failed"
			item.Error = "实例未提供 CDP 调试端口"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if err := importOKXWalletViaCDP(active.DebugPort, mnemonics[i], password); err != nil {
			// OKX's SES UI is sometimes exposed to Windows UI Automation but hidden
			// from CDP's DOM and execution contexts. Continue the same flow through
			// the native accessibility tree before reporting failure.
			if uiaErr := importOKXWalletViaUIA(mnemonics[i], password); uiaErr != nil {
				err = fmt.Errorf("CDP: %v; UI Automation: %v", err, uiaErr)
				item.Status = "failed"
				item.Error = err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
		}
		completeOKXWalletImport(result, &item, func(profileID string) error {
			_, err := a.BrowserInstanceStop(profileID)
			return err
		})
		result.Items = append(result.Items, item)
	}
	return result, nil
}

// completeOKXWalletImport reports success only after the verified wallet import
// has also stopped its browser instance. A failed stop remains visible so the
// caller can safely retry the close without re-importing the wallet.
func completeOKXWalletImport(result *OKXWalletImportResult, item *OKXWalletImportItem, stopProfile func(string) error) {
	if err := stopProfile(item.ProfileID); err != nil {
		item.Status = "close_failed"
		item.Error = fmt.Sprintf("wallet imported but automatic instance close failed: %v", err)
		result.Failed++
		return
	}
	item.Status = "success"
	result.Succeeded++
}

func nonEmptyLines(text string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if v := strings.TrimSpace(line); v != "" && !strings.HasPrefix(v, "#") {
			out = append(out, v)
		}
	}
	return out
}
func naturalProfileLess(a, b string) bool {
	ma, mb := profileOrdinal.FindStringSubmatch(a), profileOrdinal.FindStringSubmatch(b)
	if len(ma) > 1 && len(mb) > 1 {
		if len(ma[1]) != len(mb[1]) {
			return len(ma[1]) < len(mb[1])
		}
		if ma[1] != mb[1] {
			return ma[1] < mb[1]
		}
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

func importOKXWalletViaCDP(port int, mnemonic, password string) error {
	wsURL, err := getBrowserWebSocketURL(port)
	if err != nil {
		return err
	}
	headers := http.Header{"Origin": []string{"http://127.0.0.1"}}
	browserConn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		return err
	}
	defer browserConn.Close()

	// Open the notification shell, then attach to the SES child target that
	// owns the actual React form.
	importURL := "chrome-extension://" + okxWalletExtensionID + "/notification.html#/import-with-seed-phrase-and-private-key?openFromThisPage=1"
	if err := browserConn.WriteJSON(cdpMessage{Id: 1, Method: "Target.createTarget", Params: map[string]any{"url": importURL}}); err != nil {
		return err
	}
	var created cdpResponse
	if err := browserConn.ReadJSON(&created); err != nil {
		return err
	}
	if created.Error != nil {
		return fmt.Errorf("open OKX import page failed: %s", created.Error.Message)
	}
	targetID, _ := created.Result["targetId"].(string)
	if targetID == "" {
		return fmt.Errorf("OKX import page did not return a target")
	}
	parentTargetID := targetID
	if sesTargetID, sesErr := okxFindSESTarget(browserConn, parentTargetID, 20*time.Second); sesErr == nil {
		targetID = sesTargetID
	}
	pageURL := fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/%s", port, targetID)
	pageConn, _, err := websocket.DefaultDialer.Dial(pageURL, headers)
	if err != nil {
		return err
	}
	defer pageConn.Close()

	if err := okxWaitFor(pageConn, 2, `document.querySelectorAll('input').length >= 12`, 12*time.Second); err != nil {
		return fmt.Errorf("OKX mnemonic form did not become ready: %w", err)
	}
	mnemonicJSON, _ := json.Marshal(strings.Fields(mnemonic))
	fillMnemonic := fmt.Sprintf(`(()=>{const words=%s,inputs=[...document.querySelectorAll('input')].filter(e=>e.offsetParent);if(words.length!==12||inputs.length<12)throw Error('expected 12 mnemonic inputs');const set=(e,v)=>{const d=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');d.set.call(e,v);e.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:v}));e.dispatchEvent(new Event('change',{bubbles:true}))};words.forEach((w,i)=>set(inputs[i],w));return true})()`, mnemonicJSON)
	if err := okxEvaluate(pageConn, 3, fillMnemonic); err != nil {
		return fmt.Errorf("OKX mnemonic input failed: %w", err)
	}
	if err := okxWaitFor(pageConn, 4, `document.querySelector('button:not([disabled])')`, 8*time.Second); err != nil {
		return fmt.Errorf("OKX mnemonic confirmation did not become enabled: %w", err)
	}
	if err := okxClick(pageConn, 5, `Import|导入|Confirm|Bestätigen|下一步|Next`); err != nil {
		return fmt.Errorf("OKX mnemonic confirmation failed: %w", err)
	}

	// Password is the default authentication method in every locale. The
	// route is stable even though the visible labels are localized.
	if err := okxWaitFor(pageConn, 6, `location.hash.includes('password-type') && document.querySelectorAll('input').length === 0`, 12*time.Second); err != nil {
		return fmt.Errorf("OKX password verification page did not become ready: %w", err)
	}
	if err := okxClick(pageConn, 7, `Password verification|Passwort验证|密码验证|验证|Continue|继续|下一步|Next`); err != nil {
		return fmt.Errorf("OKX password verification next step failed: %w", err)
	}
	if err := okxWaitFor(pageConn, 8, `document.querySelectorAll('input[type="password"],input').length >= 2`, 12*time.Second); err != nil {
		return fmt.Errorf("OKX password form did not become ready: %w", err)
	}
	passwordJSON, _ := json.Marshal(password)
	fillPassword := fmt.Sprintf(`(()=>{const p=%s,inputs=[...document.querySelectorAll('input')].filter(e=>e.offsetParent);if(inputs.length<2)throw Error('password inputs not found');const set=(e,v)=>{const d=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');d.set.call(e,v);e.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:v}));e.dispatchEvent(new Event('change',{bubbles:true}))};set(inputs[0],p);set(inputs[1],p);return true})()`, passwordJSON)
	if err := okxEvaluate(pageConn, 9, fillPassword); err != nil {
		return fmt.Errorf("OKX password input failed: %w", err)
	}
	if err := okxWaitFor(pageConn, 10, `document.querySelector('button:not([disabled])')`, 8*time.Second); err != nil {
		return fmt.Errorf("OKX password confirmation did not become enabled: %w", err)
	}
	if err := okxClick(pageConn, 11, `Confirm|Bestätigen|确认|完成|Done|Create`); err != nil {
		return fmt.Errorf("OKX password confirmation failed: %w", err)
	}
	if err := okxVerifyWalletHome(browserConn, port, headers); err != nil {
		return err
	}
	return nil
}

func okxVerifyWalletHome(browserConn *websocket.Conn, port int, headers http.Header) error {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		response, err := okxCDPCall(browserConn, 100+attempt, "Target.createTarget", map[string]any{
			"url": "chrome-extension://" + okxWalletExtensionID + "/popup-init.html",
		}, 8*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		if response.Error != nil {
			lastErr = fmt.Errorf("open OKX wallet home failed: %s", response.Error.Message)
			continue
		}
		targetID, _ := response.Result["targetId"].(string)
		if targetID == "" {
			lastErr = fmt.Errorf("OKX wallet home did not return a target")
			continue
		}
		popupURL := fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/%s", port, targetID)
		popupConn, _, dialErr := websocket.DefaultDialer.Dial(popupURL, headers)
		if dialErr != nil {
			lastErr = dialErr
			continue
		}
		waitErr := okxWaitFor(popupConn, 200+attempt, `location.hash === '#/' && document.body && document.querySelectorAll('input').length === 0 && document.body.innerText.trim().length > 200 && /XRP|USDT|ETH|SOL|BTC/.test(document.body.innerText)`, 5*time.Second)
		popupConn.Close()
		if waitErr == nil {
			return nil
		}
		lastErr = waitErr
		_, _ = okxCDPCall(browserConn, 300+attempt, "Target.closeTarget", map[string]any{"targetId": targetID}, 3*time.Second)
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("OKX wallet home did not become ready: %w", lastErr)
}

func okxFindSESTarget(conn *websocket.Conn, parentTargetID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := okxCDPCall(conn, 50, "Target.getTargets", map[string]any{}, 3*time.Second)
		if err == nil && response.Error == nil {
			if targets, ok := response.Result["targetInfos"].([]any); ok {
				for _, raw := range targets {
					target, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					typ, _ := target["type"].(string)
					url, _ := target["url"].(string)
					id, _ := target["targetId"].(string)
					parent, _ := target["parentFrameId"].(string)
					if id != "" && parent == parentTargetID && typ == "iframe" && strings.Contains(url, "/ses.html#/") {
						return id, nil
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return "", fmt.Errorf("OKX SES form target did not become ready")
}

// importOKXWalletViaUIA handles the OKX SES renderer when the page is visible
// to Windows UI Automation but its controls are not reachable through CDP.
// Input is sent over stdin so mnemonic/password values never appear in a
// process command line or in browser logs.
func importOKXWalletViaUIA(mnemonic, password string) error {
	payload, err := json.Marshal(map[string]string{"mnemonic": mnemonic, "password": password})
	if err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", okxUIAPowerShellScript)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("native OKX automation failed: %s", message)
	}
	return nil
}

const okxUIAPowerShellScript = `
$ErrorActionPreference = 'Stop'
$stage = 'initialization'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -AssemblyName System.Windows.Forms

$request = [Console]::In.ReadToEnd() | ConvertFrom-Json
$words = @([string]$request.mnemonic -split '\s+' | Where-Object { $_ })
$password = [string]$request.password
if ($words.Count -ne 12) { throw 'expected exactly 12 mnemonic words' }

$root = [System.Windows.Automation.AutomationElement]::RootElement
$windowCondition = New-Object System.Windows.Automation.PropertyCondition(
    [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
    [System.Windows.Automation.ControlType]::Window)
$editCondition = New-Object System.Windows.Automation.PropertyCondition(
    [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
    [System.Windows.Automation.ControlType]::Edit)
$buttonCondition = New-Object System.Windows.Automation.PropertyCondition(
    [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
    [System.Windows.Automation.ControlType]::Button)

function Get-Text([System.Windows.Automation.AutomationElement]$element) {
    try {
        return (([string]$element.Current.Name) + ' ' + ([string]$element.Current.HelpText)).Trim()
    } catch { return '' }
}

function Get-VisibleDescendants($window, $condition) {
    $all = $window.FindAll([System.Windows.Automation.TreeScope]::Descendants, $condition)
    $result = @()
    for ($i = 0; $i -lt $all.Count; $i++) {
        $item = $all.Item($i)
        try {
            if (-not $item.Current.IsOffscreen -and $item.Current.IsEnabled) {
                $result += $item
            }
        } catch { }
    }
    return $result
}

function Find-OKXWindow {
    $windows = $root.FindAll([System.Windows.Automation.TreeScope]::Children, $windowCondition)
    $fallback = $null
    for ($i = 0; $i -lt $windows.Count; $i++) {
        $window = $windows.Item($i)
        $title = Get-Text $window
        if ($title -notmatch '(?i)OKX|Wallet') { continue }
        $edits = @(Get-VisibleDescendants $window $editCondition | Where-Object {
            $_.Current.AutomationId -ne 'view_1012' -and
            (Get-Text $_) -notmatch '(?i)address and search bar|地址和搜索栏'
        })
        if ($edits.Count -ge 12) { return $window }
        if ($null -eq $fallback) { $fallback = $window }
    }
    if ($null -ne $fallback) { return $fallback }
    return $null
}

function Get-WindowText($window) {
    $parts = @((Get-Text $window))
    try {
        $all = $window.FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
        for ($i = 0; $i -lt $all.Count; $i++) {
            $name = Get-Text $all.Item($i)
            if ($name -ne '') { $parts += $name }
        }
    } catch { }
    return ($parts -join ' ')
}

function Invoke-Element($element) {
    try {
        $pattern = $element.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
        $pattern.Invoke()
        return
    } catch { }
    try {
        $pattern = $element.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern)
        $pattern.Select()
        return
    } catch { }
    $element.SetFocus()
    [System.Windows.Forms.SendKeys]::SendWait('{ENTER}')
}

function Set-UIAInput($element, [string]$value) {
    $element.SetFocus()
    [System.Windows.Forms.SendKeys]::SendWait('^a')
    [System.Windows.Forms.SendKeys]::SendWait('{BACKSPACE}')
    $escaped = ''
    foreach ($character in $value.ToCharArray()) {
        if ('+^%~(){}[]!'.IndexOf([string]$character) -ge 0) {
            $escaped += '{' + [string]$character + '}'
        } else {
            $escaped += [string]$character
        }
    }
    [System.Windows.Forms.SendKeys]::SendWait($escaped)
}

function Find-Action($window, $pattern) {
    $buttons = @(Get-VisibleDescendants $window $buttonCondition)
    for ($i = $buttons.Count - 1; $i -ge 0; $i--) {
        if ((Get-Text $buttons[$i]) -match $pattern) { return $buttons[$i] }
    }
    return $null
}

function Wait-Until([scriptblock]$condition, [int]$seconds = 30) {
    $until = [DateTime]::UtcNow.AddSeconds($seconds)
    do {
        $value = & $condition
        if ($value) { return $value }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $until)
    throw "UI Automation timed out at $stage"
}

$stage = 'OKX wallet window'
$window = Wait-Until { Find-OKXWindow }
$stage = 'mnemonic inputs'
$mnemonicEdits = @(Wait-Until {
    $items = @(Get-VisibleDescendants $window $editCondition | Where-Object {
        $_.Current.AutomationId -ne 'view_1012' -and
        (Get-Text $_) -notmatch '(?i)address and search bar|地址和搜索栏'
    })
    if ($items.Count -ge 12) { $items | Select-Object -First 12 }
})
for ($i = 0; $i -lt 12; $i++) {
    Set-UIAInput $mnemonicEdits[$i] $words[$i]
}

$stage = 'mnemonic confirmation'
$confirm = Wait-Until { Find-Action $window '(?i)确认|Confirm|Import|导入|下一步|Next' }
Invoke-Element $confirm

$stage = 'password verification page'
$verification = Wait-Until {
    $allText = Get-WindowText $window
    if ($allText -match '(?i)密码验证|Password verification') { $window } else { $null }
}
$stage = 'password verification option'
$option = Wait-Until {
    $items = @(Get-VisibleDescendants $window ([System.Windows.Automation.Condition]::TrueCondition))
    $items | Where-Object { (Get-Text $_) -match '(?i)密码验证|Password verification' } | Select-Object -First 1
}
Invoke-Element $option
$stage = 'password verification next button'
$next = Wait-Until { Find-Action $window '(?i)下一步|Next|Continue' }
Invoke-Element $next

$stage = 'wallet password inputs'
$passwordEdits = @(Wait-Until {
    $items = @(Get-VisibleDescendants $window $editCondition | Where-Object {
        $_.Current.AutomationId -ne 'view_1012' -and
        (Get-Text $_) -notmatch '(?i)address and search bar|地址和搜索栏'
    })
    if ($items.Count -ge 2) { $items | Select-Object -First 2 }
})
for ($i = 0; $i -lt 2; $i++) {
    Set-UIAInput $passwordEdits[$i] $password
}
$stage = 'wallet password confirmation'
$confirmPassword = Wait-Until { Find-Action $window '(?i)确认|Confirm|完成|Done|Create' }
Invoke-Element $confirmPassword

$stage = 'wallet onboarding or home'
$home = Wait-Until {
    $text = Get-WindowText $window
    if ($text -match '(?i)开启你的 Web3 之旅|Start your Web3 journey|Send|Receive|发送|接收') { $window } else { $null }
}
$start = Find-Action $home '(?i)开启你的 Web3 之旅|Start your Web3 journey|开始|Start'
if ($null -ne $start) { Invoke-Element $start }
$stage = 'wallet home'
Wait-Until {
    $text = Get-WindowText $window
    $text -match '(?i)Send|Receive|发送|接收'
} | Out-Null
'OKX_UIA_SUCCESS'
`

func okxEvaluate(conn *websocket.Conn, id int, expression string) error {
	contexts, err := okxExecutionContexts(conn, id+1000)
	if err != nil {
		return err
	}
	var lastErr error
	for _, contextID := range contexts {
		if err := okxEvaluateInContext(conn, id, expression, contextID); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func okxWaitFor(conn *websocket.Conn, id int, predicate string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	expression := fmt.Sprintf(`(()=>{try{return Boolean(%s)}catch(_){return false}})()`, predicate)
	for time.Now().Before(deadline) {
		contexts, err := okxExecutionContexts(conn, id+1000)
		if err == nil {
			for _, contextID := range contexts {
				value, evalErr := okxEvaluateBoolean(conn, id, expression, contextID)
				if evalErr == nil && value {
					return nil
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out")
}

func okxClick(conn *websocket.Conn, id int, labelRegex string) error {
	raw, _ := json.Marshal(labelRegex)
	expression := fmt.Sprintf(`(()=>{const r=new RegExp(%s,'i');const text=e=>(e.innerText||e.getAttribute('aria-label')||'').trim();const shown=e=>e.offsetParent;const buttons=[...document.querySelectorAll('button')].filter(shown);const roles=[...document.querySelectorAll('[role="button"]')].filter(e=>shown(e)&&e.tagName!=='BUTTON');const primary=[...buttons,...roles];const secondary=[...document.querySelectorAll('[role="group"],a')].filter(shown);const enabled=e=>!e.disabled&&!e.getAttribute('aria-disabled');const e=primary.find(x=>enabled(x)&&r.test(text(x)))||(buttons.length===1&&enabled(buttons[0])?buttons[0]:null)||secondary.find(x=>enabled(x)&&r.test(text(x)));if(!e)throw Error('click target not found: '+r);e.click();return true})()`, raw)
	return okxEvaluate(conn, id, expression)
}

func okxEvaluateInContext(conn *websocket.Conn, id int, expression string, contextID int) error {
	expression = okxScopedExpression(expression)
	params := map[string]any{"expression": expression, "returnByValue": true}
	if contextID > 0 {
		params["contextId"] = contextID
	}
	response, err := okxCDPCall(conn, id, "Runtime.evaluate", params, 8*time.Second)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf(response.Error.Message)
	}
	if exception, ok := response.Result["exceptionDetails"].(map[string]any); ok {
		message, _ := exception["text"].(string)
		if message == "" {
			message = "page script exception"
		}
		return fmt.Errorf(message)
	}
	return nil
}

func okxEvaluateBoolean(conn *websocket.Conn, id int, expression string, contextID int) (bool, error) {
	expression = okxScopedExpression(expression)
	params := map[string]any{"expression": expression, "returnByValue": true}
	if contextID > 0 {
		params["contextId"] = contextID
	}
	response, err := okxCDPCall(conn, id, "Runtime.evaluate", params, 2*time.Second)
	if err != nil || response.Error != nil {
		if err == nil {
			err = fmt.Errorf(response.Error.Message)
		}
		return false, err
	}
	if result, ok := response.Result["result"].(map[string]any); ok {
		value, _ := result["value"].(bool)
		return value, nil
	}
	return false, nil
}

func okxScopedExpression(expression string) string {
	// OKX renders the form through an SES/shadow-root boundary in some builds.
	// Expose a query helper inside each execution context and use it for all
	// selectors emitted by the importer.
	expression = strings.ReplaceAll(expression, "document.querySelectorAll(", "__boostQuery(")
	const prefix = `(()=>{const __boostRoots=[];const __boostWalk=r=>{__boostRoots.push(r);for(const n of r.querySelectorAll('*'))if(n.shadowRoot)__boostWalk(n.shadowRoot)};__boostWalk(document);const __boostQuery=s=>__boostRoots.flatMap(r=>[...r.querySelectorAll(s)]);return `
	return prefix + expression + `})()`
}

func okxExecutionContexts(conn *websocket.Conn, id int) ([]int, error) {
	response, err := okxCDPCall(conn, id, "Page.getFrameTree", map[string]any{}, 2*time.Second)
	if err != nil || response.Error != nil {
		if err == nil {
			err = fmt.Errorf(response.Error.Message)
		}
		return nil, err
	}
	root, _ := response.Result["frameTree"].(map[string]any)
	frameIDs := make([]string, 0)
	collectOKXFrameIDs(root, &frameIDs)
	contexts := []int{0}
	for index, frameID := range frameIDs {
		// The root context is already covered by context ID 0.
		if index == 0 {
			continue
		}
		world, err := okxCDPCall(conn, id+index+1, "Page.createIsolatedWorld", map[string]any{"frameId": frameID, "worldName": "boost-okx-import", "grantUniveralAccess": true}, 2*time.Second)
		if err != nil || world.Error != nil {
			continue
		}
		if value, ok := world.Result["executionContextId"].(float64); ok && value > 0 {
			contexts = append(contexts, int(value))
		}
	}
	return contexts, nil
}

func collectOKXFrameIDs(tree map[string]any, out *[]string) {
	if tree == nil {
		return
	}
	if frame, ok := tree["frame"].(map[string]any); ok {
		if id, ok := frame["id"].(string); ok && id != "" {
			*out = append(*out, id)
		}
	}
	if children, ok := tree["childFrames"].([]any); ok {
		for _, raw := range children {
			if child, ok := raw.(map[string]any); ok {
				collectOKXFrameIDs(child, out)
			}
		}
	}
}

func okxCDPCall(conn *websocket.Conn, id int, method string, params map[string]any, timeout time.Duration) (cdpResponse, error) {
	if err := conn.WriteJSON(cdpMessage{Id: id, Method: method, Params: params}); err != nil {
		return cdpResponse{}, err
	}
	conn.SetReadDeadline(time.Now().Add(timeout))
	defer conn.SetReadDeadline(time.Time{})
	for {
		var response cdpResponse
		if err := conn.ReadJSON(&response); err != nil {
			return cdpResponse{}, err
		}
		if response.Id == id {
			return response, nil
		}
	}
}
