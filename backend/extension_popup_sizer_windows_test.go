//go:build windows

package backend

import "testing"

func TestNeedsExtensionPopupResizeStrongPromptLargeWindow(t *testing.T) {
	if !needsExtensionPopupResize("petra - prompt", 980, 900) {
		t.Fatalf("Petra prompt oversized popup should be clamped")
	}
}

func TestNeedsExtensionPopupResizeOKXProductPopupIsClamped(t *testing.T) {
	if !needsExtensionPopupResize("okx wallet - boost browser", 1920, 1032) {
		t.Fatalf("oversized OKX extension popup with Boost Browser suffix should be clamped")
	}
}

func TestNeedsExtensionPopupResizePhantomProductPopupIsClamped(t *testing.T) {
	for _, title := range []string{"phantom - boost browser", "phantom wallet - boost browser"} {
		if !needsExtensionPopupResize(title, 1280, 900) {
			t.Fatalf("oversized Phantom extension popup %q with Boost Browser suffix should be clamped", title)
		}
	}
}

func TestNeedsExtensionPopupResizeOKXWebsiteTitleIsNotClamped(t *testing.T) {
	if needsExtensionPopupResize("web3 入口，一个就够 - okx wallet", 980, 900) {
		t.Fatalf("OKX website/browser tab title must not be clamped as an extension popup")
	}
}

func TestNeedsExtensionPopupResizeGenericLoginRequestIsClamped(t *testing.T) {
	if !needsExtensionPopupResize("登录请求", 980, 900) {
		t.Fatalf("oversized generic extension login request should be clamped")
	}
}

func TestNeedsExtensionPopupResizeGenericLoginRequestWithProductSuffixIsClamped(t *testing.T) {
	if !needsExtensionPopupResize("登录请求 - Boost Browser", 1280, 900) {
		t.Fatalf("oversized generic extension login request with product suffix should be clamped")
	}
}

func TestShouldHandleHiddenExtensionPopupWindowRejectsLoginRequest(t *testing.T) {
	rect := winRect{Left: -32000, Top: -32000, Right: -30720, Bottom: -31100}
	if shouldHandleHiddenExtensionPopupWindow("登录请求", "Chrome_WidgetWin_1", rect) {
		t.Fatalf("hidden generic login request popup must stay hidden")
	}
}

func TestExtensionPopupWindowPlacementClampsGenericLoginRequest(t *testing.T) {
	x, y, w, h, shouldMove := extensionPopupWindowPlacement("登录请求", winRect{Left: 100, Top: 100, Right: 1380, Bottom: 1000})
	if !shouldMove {
		t.Fatalf("visible oversized generic login request should be resized")
	}
	if x != 100 || y != 100 || w != 390 || h != 620 {
		t.Fatalf("unexpected popup bounds: x=%d y=%d w=%d h=%d", x, y, w, h)
	}
}

func TestNeedsExtensionPopupResizeKnownWalletWebsiteTitlesAreNotClamped(t *testing.T) {
	cases := []string{
		"the crypto wallet for defi, web3 dapps and nfts | metamask",
		"rabby wallet - your ethereum wallet",
		"phantom - crypto wallet",
		"bitget wallet | your web3 trading wallet",
		"keplr wallet - interchain wallet",
		"petra wallet | aptos wallet",
		"web3 钱包官网",
	}
	for _, title := range cases {
		for _, size := range [][2]int{{980, 900}, {800, 700}} {
			if needsExtensionPopupResize(title, size[0], size[1]) {
				t.Fatalf("wallet website/browser tab title %q at %dx%d must not be clamped as an extension popup", title, size[0], size[1])
			}
		}
	}
}

func TestNeedsExtensionPopupResizeKnownWalletStandalonePopups(t *testing.T) {
	cases := []string{"metamask", "rabby", "phantom", "bitget wallet", "keplr", "petra"}
	for _, title := range cases {
		if !needsExtensionPopupResize(title, 980, 900) {
			t.Fatalf("oversized standalone wallet popup %q should be clamped", title)
		}
	}
}

func TestNeedsExtensionPopupResizeOKXStandalonePopup(t *testing.T) {
	if !needsExtensionPopupResize("okx wallet", 980, 900) {
		t.Fatalf("oversized standalone OKX popup should still be clamped")
	}
}

func TestNeedsExtensionPopupResizeKeepsNormalWalletPopup(t *testing.T) {
	if needsExtensionPopupResize("petra - prompt", 390, 620) {
		t.Fatalf("normal extension popup size should not be resized")
	}
}

func TestNeedsExtensionPopupResizeGenericWalletMainPageIsConservative(t *testing.T) {
	if needsExtensionPopupResize("best wallet docs - boost browser", 1280, 900) {
		t.Fatalf("generic wallet browser pages should not be clamped")
	}
}

func TestLooksLikeWalletExtensionPopupIncludesPetra(t *testing.T) {
	if !looksLikeWalletExtensionPopup("petra - prompt") {
		t.Fatalf("Petra prompt title should be recognized as an extension popup")
	}
}

func TestMayBeGenericExtensionPopupWindowAllowsNonWalletChromePopup(t *testing.T) {
	if !mayBeGenericExtensionPopupWindow("Moss Account Manager", "Chrome_WidgetWin_1") {
		t.Fatalf("generic extension settings popup should be considered a candidate")
	}
}

func TestShouldRestoreGenericExtensionPopupWindowAllowsNonWalletChromePopup(t *testing.T) {
	rect := winRect{Left: -32000, Top: -32000, Right: -31580, Bottom: -31420}
	if !shouldRestoreGenericExtensionPopupWindow("Moss Account Manager", "Chrome_WidgetWin_1", rect) {
		t.Fatalf("generic extension settings popup should be restored onscreen")
	}
}

func TestShouldRestoreGenericExtensionPopupWindowAllowsUntitledOffscreenChromePopup(t *testing.T) {
	rect := winRect{Left: -32000, Top: -32000, Right: -31620, Bottom: -31400}
	if !shouldRestoreGenericExtensionPopupWindow("", "Chrome_WidgetWin_1", rect) {
		t.Fatalf("untitled offscreen popup-sized chrome window should be restored onscreen")
	}
}

func TestShouldHandleHiddenExtensionPopupWindowRejectsUntitledOffscreenChromePopup(t *testing.T) {
	rect := winRect{Left: -32000, Top: -32000, Right: -31620, Bottom: -31400}
	if shouldHandleHiddenExtensionPopupWindow("", "Chrome_WidgetWin_1", rect) {
		t.Fatalf("hidden untitled offscreen popup-sized chrome window must stay hidden")
	}
}

func TestShouldRestoreGenericExtensionPopupWindowRejectsUntitledLargeWindow(t *testing.T) {
	rect := winRect{Left: -32000, Top: -32000, Right: -30720, Bottom: -31100}
	if shouldRestoreGenericExtensionPopupWindow("", "Chrome_WidgetWin_1", rect) {
		t.Fatalf("untitled large offscreen chrome window must not be treated as a generic extension popup")
	}
}

func TestShouldRestoreGenericExtensionPopupWindowRejectsMainBrowserWindow(t *testing.T) {
	rect := winRect{Left: -32000, Top: -32000, Right: -30720, Bottom: -31100}
	for _, title := range []string{"Moss - Boost Browser", "新标签页 - Chromium", "Phantom - Google Chrome"} {
		if shouldRestoreGenericExtensionPopupWindow(title, "Chrome_WidgetWin_1", rect) {
			t.Fatalf("main browser window %q must not be treated as a generic extension popup", title)
		}
	}
}

func TestShouldRestoreGenericExtensionPopupWindowRejectsDevTools(t *testing.T) {
	rect := winRect{Left: -32000, Top: -32000, Right: -30720, Bottom: -31100}
	if shouldRestoreGenericExtensionPopupWindow("Developer Tools", "Chrome_WidgetWin_1", rect) {
		t.Fatalf("devtools window must not be treated as a generic extension popup")
	}
}

func TestLooksLikeServiceWorkerDevToolsTitleIncludesGenericDevTools(t *testing.T) {
	cases := []string{"devtools", "service worker", "developer tools", "\u5f00\u53d1\u8005\u5de5\u5177"}
	for _, title := range cases {
		if !looksLikeServiceWorkerDevToolsTitle(title) {
			t.Fatalf("%q should be recognized as service worker/devtools window", title)
		}
	}
}

func TestAuxiliaryIMEWindowTitleOrClassIsExcluded(t *testing.T) {
	cases := []struct {
		title string
		class string
	}{
		{title: "Default IME", class: ""},
		{title: "", class: "IME"},
		{title: "Microsoft IME", class: "Chrome_WidgetWin_0"},
	}
	for _, tc := range cases {
		if !isAuxiliaryIMEWindowTitleOrClass(tc.title, tc.class) {
			t.Fatalf("title=%q class=%q should be treated as auxiliary IME window", tc.title, tc.class)
		}
	}
	if isAuxiliaryIMEWindowTitleOrClass("Moss - Boost Browser", "Chrome_WidgetWin_1") {
		t.Fatalf("normal browser window should not be treated as auxiliary IME window")
	}
}

func TestStartupBrowserWindowPlacementRestoresOffscreenDevToolsPosition(t *testing.T) {
	x, y, w, h, shouldMove := startupBrowserWindowPlacement(winRect{Left: -32000, Top: -32000, Right: -30720, Bottom: -31100})
	if !shouldMove {
		t.Fatalf("offscreen DevTools window should be moved back onscreen")
	}
	if x != 80 || y != 80 || w != 1280 || h != 900 {
		t.Fatalf("unexpected restored bounds: x=%d y=%d w=%d h=%d", x, y, w, h)
	}
}

func TestExtensionPopupWindowPlacementMovesOffscreenWalletPopupOnscreen(t *testing.T) {
	x, y, w, h, shouldMove := extensionPopupWindowPlacement("metamask", winRect{Left: -32000, Top: -32000, Right: -31610, Bottom: -31380})
	if !shouldMove {
		t.Fatalf("offscreen wallet popup should be moved onscreen for manual user-opened popup")
	}
	if x != 120 || y != 120 || w != 390 || h != 620 {
		t.Fatalf("unexpected popup bounds: x=%d y=%d w=%d h=%d", x, y, w, h)
	}
}

func TestExtensionPopupWindowPlacementLeavesVisibleNormalWalletPopup(t *testing.T) {
	_, _, _, _, shouldMove := extensionPopupWindowPlacement("metamask", winRect{Left: 200, Top: 150, Right: 590, Bottom: 770})
	if shouldMove {
		t.Fatalf("visible normal-sized wallet popup should be left alone")
	}
}

func TestGenericExtensionPopupWindowPlacementClampsVisibleOversizedNonWalletPopup(t *testing.T) {
	x, y, w, h, shouldMove := genericExtensionPopupWindowPlacement("Moss Account Manager", "Chrome_WidgetWin_1", winRect{Left: 100, Top: 100, Right: 1380, Bottom: 1000})
	if !shouldMove {
		t.Fatalf("visible oversized generic extension popup should be normalized")
	}
	if x != 100 || y != 100 || w != 390 || h != 620 {
		t.Fatalf("unexpected popup bounds: x=%d y=%d w=%d h=%d", x, y, w, h)
	}
}

func TestGenericExtensionPopupWindowPlacementKeepsNormalSizedNonWalletPopup(t *testing.T) {
	x, y, w, h, shouldMove := genericExtensionPopupWindowPlacement("Moss Account Manager", "Chrome_WidgetWin_1", winRect{Left: 120, Top: 120, Right: 540, Bottom: 760})
	if shouldMove {
		t.Fatalf("normal sized generic extension popup should be left untouched, got x=%d y=%d w=%d h=%d", x, y, w, h)
	}
}

func TestNeedsGenericExtensionPopupResizeRejectsMainBrowserWindowTitle(t *testing.T) {
	if needsGenericExtensionPopupResize("Moss - Boost Browser", "Chrome_WidgetWin_1", 1280, 900) {
		t.Fatalf("main browser window title must not be clamped as generic extension popup")
	}
}
