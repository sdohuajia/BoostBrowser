const chromeVersion = /Chrome\/([0-9.]+)/.exec(navigator.userAgent)[1];
const store_extensions = new Map();
const googleUpdateUrl = "https://clients2.google.com/service/update2/crx";
const WEBSTORE = { chrome: 0, edge: 1, opera: 2, chromenew: 3 };
const DEFAULT_MANAGEMENT_OPTIONS = {
    auto_update: true,
    check_store_apps: true,
    check_external_apps: true,
    update_period_in_minutes: 60,
    removed_extensions: {},
    // Boost Browser 改造：保持 manually_install=false。
    // 这条路径会通过 newTabUrl 在新标签页打开 .crx 下载 URL，
    // 配合启动时通过 Local State 注入的 chrome://flags 实验
    //   extension-mime-request-handling@2 ("Always prompt for install")
    // → Chrome 直接弹原生"添加扩展程序？"对话框，一步到位安装。
    // 之前 v1.2.1 临时开过 manually_install=true（saveAs:true 路径），
    // 那条路径会强制弹"另存为"对话框 + 让用户手动拖到 chrome://extensions，
    // 不是用户期望的体验，已回滚。
    manually_install: false,
    webstore_integration: true,
};
var fromXML;
const UPDATE_API_LIMIT = 100; // this appears to be 179 from testing; using 100 to be safe.
// prettier-ignore
!function(r){var t={"&amp;":"&","&lt;":"<","&gt;":">","&apos;":"'","&quot;":'"'};function n(r){return r&&r.replace(/^\s+|\s+$/g,"")}function s(r){return r.replace(/(&(?:lt|gt|amp|apos|quot|#(?:\d{1,6}|x[0-9a-fA-F]{1,5}));)/g,(function(r){if("#"===r[1]){var n="x"===r[2]?parseInt(r.substr(3),16):parseInt(r.substr(2),10);if(n>-1)return String.fromCharCode(n)}return t[r]||r}))}function e(r,t){if("string"==typeof r)return r;var u=r.r;if(u)return u;var a,o=function(r,t){if(r.t){for(var e,u,a=r.t.split(/([^\s='"]+(?:\s*=\s*(?:'[\S\s]*?'|"[\S\s]*?"|[^\s'"]*))?)/),o=a.length,i=0;i<o;i++){var l=n(a[i]);if(l){e||(e={});var c=l.indexOf("=");if(c<0)l="@"+l,u=null;else{u=l.substr(c+1).replace(/^\s+/,""),l="@"+l.substr(0,c).replace(/\s+$/,"");var p=u[0];p!==u[u.length-1]||"'"!==p&&'"'!==p||(u=u.substr(1,u.length-2)),u=s(u)}t&&(u=t(l,u)),f(e,l,u)}}return e}}(r,t),i=r.f,l=i.length;if(o||l>1)a=o||{},i.forEach((function(r){"string"==typeof r?f(a,"#",r):f(a,r.n,e(r,t))}));else if(l){var c=i[0];if(a=e(c,t),c.n){var p={};p[c.n]=a,a=p}}else a=r.c?null:"";return t&&(a=t(r.n||"",a)),a}function f(r,t,n){if(void 0!==n){var s=r[t];s instanceof Array?s.push(n):r[t]=t in r?[s,n]:n}}r.fromXML=fromXML=function(r,t){return e(function(r){for(var t=String.prototype.split.call(r,/<([^!<>?](?:'[\S\s]*?'|"[\S\s]*?"|[^'"<>])*|!(?:--[\S\s]*?--|\[[^\[\]'"<>]+\[[\S\s]*?]]|DOCTYPE[^\[<>]*?\[[\S\s]*?]|(?:ENTITY[^"<>]*?"[\S\s]*?")?[\S\s]*?)|\?[\S\s]*?\?)>/),e=t.length,f={f:[]},u=f,a=[],o=0;o<e;){var i=t[o++];i&&v(i);var l=t[o++];l&&c(l)}return f;function c(r){var t=r.length,n=r[0];if("/"===n)for(var s=r.replace(/^\/|[\s\/].*$/g,"").toLowerCase();a.length;){var e=u.n&&u.n.toLowerCase();if(u=a.pop(),e===s)break}else if("?"===n)p({n:"?",r:r.substr(1,t-2)});else if("!"===n)"[CDATA["===r.substr(1,7)&&"]]"===r.substr(-2)?v(r.substr(8,t-10)):p({n:"!",r:r.substr(1)});else{var f=function(r){var t={f:[]},n=(r=r.replace(/\s*\/?$/,"")).search(/[\s='"\/]/);n<0?t.n=r:(t.n=r.substr(0,n),t.t=r.substr(n));return t}(r);p(f),"/"===r[t-1]?f.c=1:(a.push(u),u=f)}}function p(r){u.f.push(r)}function v(r){(r=n(r))&&p(s(r))}}(r),t)}}("object"==typeof exports&&exports||{});
store_extensions.set(/clients2\.google\.com\/service\/update2\/crx/, {
    baseUrl:
        "https://clients2.google.com/service/update2/crx?response=updatecheck&acceptformat=crx2,crx3&prodversion=",
    name: "CWS Extensions",
});
// edge requires &v=<version> for each extension or else it returns empty results
store_extensions.set(/edge\.microsoft\.com\/extensionwebstorebase\/v1\/crx/, {
    baseUrl:
        "https://edge.microsoft.com/extensionwebstorebase/v1/crx?os=win&arch=x64&os_arch=x86_64&nacl_arch=x86-64&prod=edgecrx&prodchannel=&lang=en-US&acceptformat=crx3&prodversion=",
    name: "Edge Extensions",
    ignore: true,
});
// opera requires an opera UA or else this request will return 404
store_extensions.set(/extension-updates\.opera\.com\/api\/omaha\/update/, {
    baseUrl:
        "https://extension-updates.opera.com/api/omaha/update/?os=win&arch=x64&os_arch=x86_64&nacl_arch=x86-64&prod=chromiumcrx&prodchannel=Stable&lang=en-US&acceptformat=crx3&prodversion=",
    name: "Opera Extensions",
    userAgent: "foobar",
    ignore: true,
});

const is_cws = /chrome.google.com\/webstore/i;
const is_ncws = /chromewebstore.google.com\//i;
const is_ows = /addons.opera.com\/.*extensions/i;
const is_ews = /microsoftedge\.microsoft\.com\/addons\//i;
const cws_re = /.*detail\/[^\/]*\/([a-z]{32})/i;
const ncws_re = /.*detail(?:\/[^\/]+)?\/([a-z]{32})/i;
const ows_re = /.*details\/([^\/?#]+)/i;
const ews_re = /.*addons\/.+?\/([a-z]{32})/i;

const WEBSTORE_MAP = new Map();
WEBSTORE_MAP.set(is_cws, WEBSTORE.chrome);
WEBSTORE_MAP.set(is_ews, WEBSTORE.edge);
WEBSTORE_MAP.set(is_ows, WEBSTORE.opera);
WEBSTORE_MAP.set(is_ncws, WEBSTORE.chromenew);

function version_is_newer(current, available) {
    let current_subvs = current.split(".");
    let available_subvs = available.split(".");
    for (let i = 0; i < 4; i++) {
        let ver_diff =
            (parseInt(available_subvs[i]) || 0) -
            (parseInt(current_subvs[i]) || 0);
        if (ver_diff > 0) return true;
        else if (ver_diff < 0) return false;
    }
    return false;
}

function getExtensionId(url) {
    return (cws_re.exec(url) ||
        ncws_re.exec(url) ||
        ows_re.exec(url) ||
        ews_re.exec(url) || [undefined, undefined])[1];
}

function buildExtensionUrl(href, extensionId = undefined) {
    extensionId = extensionId || getExtensionId(href);
    if (extensionId == undefined) return;
    if (is_cws.test(href) || is_ncws.test(href)) {
        var chromeVersion = /Chrome\/([0-9.]+)/.exec(navigator.userAgent)[1];
        return (
            "https://clients2.google.com/service/update2/crx?response=redirect&acceptformat=crx2,crx3&prodversion=" +
            chromeVersion +
            "&x=id%3D" +
            extensionId +
            "%26installsource%3Dondemand%26uc"
        );
    }
    if (is_ows.test(href)) {
        return (
            "https://addons.opera.com/extensions/download/" + extensionId + "/"
        );
    }
    if (is_ews.test(href)) {
        return (
            "https://edge.microsoft.com/extensionwebstorebase/v1/crx?response=redirect&x=id%3D" +
            extensionId +
            "%26installsource%3Dondemand%26uc"
        );
    }
}

function promptInstall(
    crx_url,
    is_webstore,
    browser = WEBSTORE.chrome,
    custom_msg_handler = undefined,
) {
    chrome.storage.sync.get(DEFAULT_MANAGEMENT_OPTIONS, function (settings) {
        // Boost Browser 强制覆盖：旧版 helper 默认 manually_install=true，
        // 并且把 true 写进了 chrome.storage.sync，后续改默认值不会覆盖已存的。
        // 直接在调用点强制 false，确保 webstore 链路始终走 newTabUrl，
        // 由 Chrome 自带的 extension-mime-request-handling@2 flag 弹原生安装框。
        if (is_webstore) {
            settings.manually_install = false;
        }
        var msgHandler = custom_msg_handler || chrome.runtime.sendMessage;
        if (is_webstore && !settings.manually_install) {
            // Boost Browser 方案 B：优先把 crx 交给本地 LaunchServer 处理。
            // 由 Go 端 fetch crx → 解包 → 写到 <appRoot>/extensions/imported/<extId>/
            // → 追加到当前 active profile 的 --load-extension。
            // 失败时回退到原 newTabUrl 路径（保持兼容老 chromium）。
            tryBoostInstall(crx_url, function (ok) {
                if (ok) return;
                switch (browser) {
                    case WEBSTORE.edge:
                        msgHandler({ newTabUrl: crx_url });
                        break;
                    case WEBSTORE.opera:
                        msgHandler({ manualInstallDownloadUrl: crx_url });
                        break;
                    default:
                        msgHandler({ newTabUrl: crx_url });
                        break;
                }
            });
            return;
        }
        if (settings.manually_install) {
            msgHandler({
                manualInstallDownloadUrl: crx_url,
            });
            return;
        } else {
            msgHandler({
                nonWebstoreDownloadUrl: crx_url,
            });
            return;
        }
    });
}

// Boost Browser 本地 LaunchServer 安装链路。
// 关键：util.js 是 content script，跑在 chromewebstore.google.com 页面上下文里，
// 直接 fetch http://127.0.0.1:* 会被浏览器 CORS 拦掉（content script 的 fetch
// 受页面 CORS 约束，host_permissions 不绕过）。
// 所以 content script 这里只发消息给 service worker，由 background.js 在
// 扩展上下文里 fetch（不受页面 CORS 限制，只受 host_permissions 控制）。
function tryBoostInstall(crx_url, done) {
    try {
        chrome.runtime.sendMessage({ boostInstallCrxUrl: crx_url }, (resp) => {
            if (chrome.runtime.lastError) {
                console.warn("[BoostInstall] sendMessage failed:", chrome.runtime.lastError.message);
                done(false);
                return;
            }
            if (resp && resp.ok) {
                done(true);
            } else {
                if (resp && resp.error) {
                    console.warn("[BoostInstall] server rejected:", resp.status, resp.error);
                }
                done(false);
            }
        });
    } catch (e) {
        console.warn("[BoostInstall] dispatch error:", e);
        done(false);
    }
}

// boostInstallFromStore：仅在 service worker 上下文调用（background.js 通过
// importScripts 拉进 util.js，所以这里定义的函数在两边都能看到，但 content
// script 不会主动调用它，避免 CORS）。
// 返回 Promise<{ok, status?, error?, extensionId?, extensionName?}>。
async function boostInstallFromStore(crx_url) {
    let cfg;
    try {
        const epResp = await fetch(chrome.runtime.getURL("boost_endpoint.json"), { cache: "no-store" });
        if (!epResp.ok) {
            return { ok: false, status: 0, error: "boost_endpoint.json missing" };
        }
        cfg = await epResp.json();
        if (!cfg || typeof cfg.port !== "number" || cfg.port <= 0) {
            return { ok: false, status: 0, error: "boost_endpoint.json invalid" };
        }
    } catch (e) {
        return { ok: false, status: 0, error: "read boost_endpoint failed: " + String(e) };
    }
    const url = `http://127.0.0.1:${cfg.port}/api/extension/install-from-store`;
    const headers = { "Content-Type": "application/json" };
    if (cfg.apiKey && cfg.apiHeader) {
        headers[cfg.apiHeader] = cfg.apiKey;
    }
    let r;
    try {
        r = await fetch(url, {
            method: "POST",
            headers: headers,
            body: JSON.stringify({ crxUrl: crx_url }),
        });
    } catch (e) {
        return { ok: false, status: 0, error: "fetch failed: " + String(e) };
    }
    let body = null;
    try { body = await r.json(); } catch (e) {}
    if (r.status >= 200 && r.status < 300 && body && body.ok) {
        _showBoostInstallToast(body);
        return { ok: true, extensionId: body.extensionId, extensionName: body.extensionName };
    }
    return { ok: false, status: r.status, error: (body && body.error) || `HTTP ${r.status}` };
}

function _showBoostInstallToast(body) {
    const name = (body && body.extensionName) || (body && body.extensionId) || "扩展";
    try {
        chrome.notifications &&
            chrome.notifications.create({
                type: "basic",
                iconUrl: "assets/icon/icon_128.png",
                title: "扩展已下载",
                message: `${name} 已加入启动参数，重启该实例后生效。`,
            });
    } catch (e) {}
}

function checkForUpdates(
    update_callback = null,
    failure_callback = null,
    completed_callback = null,
    custom_ext_list = [],
) {
    chrome.management.getAll(function (e) {
        e.push(...custom_ext_list);
        let default_options = { ...DEFAULT_MANAGEMENT_OPTIONS };
        e.forEach(function (ex) {
            default_options[ex.id] = false;
        });
        chrome.storage.sync.get(default_options, function (stored_values) {
            stored_values["ignored_extensions"] = [];
            chrome.storage.managed.get(stored_values, function (settings) {
                settings.ignored_extensions.forEach((ignored_appid) => {
                    if (ignored_appid in settings)
                        settings[ignored_appid] = true;
                });
                delete settings.ignored_extensions;
                let updateUrl =
                    "https://clients2.google.com/service/update2/crx?response=updatecheck&acceptformat=crx2,crx3&prodversion=" +
                    chromeVersion;
                let installed_versions = {};
                let updateUrls = [];
                Array.from(store_extensions.values()).forEach(
                    (x) => delete x.updateUrl,
                );
                function _add_url(updateUrls, updaterOptions) {
                    if (!settings.check_store_apps) return false;
                    if (updaterOptions.ignore) return false;
                    if (!("updateUrl" in updaterOptions)) return false;
                    updateUrls.push({
                        url: updaterOptions.updateUrl,
                        name: updaterOptions.name,
                    });
                    return true;
                }
                e.forEach(function (ex) {
                    if (ex.updateUrl && !settings[ex.id]) {
                        let is_from_store = false;
                        for (const [re, updaterOptions] of store_extensions) {
                            if (re.test(ex.updateUrl)) {
                                is_from_store = true;
                                updaterOptions.updateUrl =
                                    updaterOptions.updateUrl ||
                                    updaterOptions.baseUrl + chromeVersion;
                                updaterOptions.updateUrl +=
                                    "&x=id%3D" + ex.id + "%26uc";
                            }
                            updaterOptions.extCount =
                                (updaterOptions.extCount ?? 0) + 1;
                            if (
                                updaterOptions.extCount >= UPDATE_API_LIMIT &&
                                _add_url(updateUrls, updaterOptions)
                            ) {
                                delete updaterOptions.updateUrl;
                                updaterOptions.extCount = 0;
                            }
                        }
                        if (!is_from_store && settings.check_external_apps) {
                            updateUrls.push({
                                url: ex.updateUrl,
                                name: ex.name,
                                id: ex.id,
                            });
                        }
                        installed_versions[ex.id] = ex;
                    }
                });
                for (const [re, updaterOptions] of store_extensions) {
                    _add_url(updateUrls, updaterOptions);
                }
                function update_extension(ext_url, ext_id, ext_name) {
                    let is_webstore = Array.from(store_extensions.keys()).some(
                        (x) => x.test(ext_url),
                    );
                    return new Promise((resolve, reject) => {
                        fetch(ext_url)
                            .then((r) => {
                                if (r.status != 200) {
                                    return Promise.reject();
                                } else return r.text();
                            })
                            .then((txt) => {
                                let xml = fromXML(txt);
                                if (xml.gupdate.app["@appid"]) {
                                    // its a single ext, put into array of size 1
                                    xml.gupdate.app = [xml.gupdate.app];
                                }
                                return xml;
                            })
                            .then((data) => {
                                let updateCount = 0;
                                for (extinfo of data?.gupdate?.app ?? []) {
                                    if (!extinfo.updatecheck) continue;
                                    let updatever =
                                        extinfo.updatecheck["@version"];
                                    let appid = extinfo["@appid"];
                                    let updatestatus =
                                        extinfo.updatecheck["@status"];
                                    if (
                                        (updatestatus == "ok" ||
                                            !is_webstore) &&
                                        updatever &&
                                        installed_versions[appid] !==
                                            undefined &&
                                        version_is_newer(
                                            installed_versions[appid].version,
                                            updatever,
                                        )
                                    ) {
                                        updateCount++;
                                        if (update_callback)
                                            update_callback(
                                                extinfo.updatecheck,
                                                installed_versions,
                                                appid,
                                                updatever,
                                                is_webstore,
                                            );
                                        if (
                                            appid in
                                            stored_values["removed_extensions"]
                                        ) {
                                            delete stored_values[
                                                "removed_extensions"
                                            ][appid];
                                            chrome.storage.sync.set({
                                                removed_extensions:
                                                    stored_values[
                                                        "removed_extensions"
                                                    ],
                                            });
                                        }
                                    }
                                    if (
                                        failure_callback &&
                                        updatestatus == "noupdate" &&
                                        !(
                                            appid in
                                            stored_values["removed_extensions"]
                                        )
                                    )
                                        failure_callback(
                                            true,
                                            installed_versions[appid],
                                        );
                                    // }
                                }
                                chrome.action.getBadgeText(
                                    {},
                                    function (currentText) {
                                        let disp =
                                            (updateCount || "") +
                                            (parseInt(currentText) || "") +
                                            "";
                                        chrome.action.setBadgeText(
                                            {
                                                text: disp,
                                            },
                                            () => {
                                                chrome.storage.local.set(
                                                    {
                                                        badge_display: disp,
                                                    },
                                                    () => {
                                                        resolve();
                                                    },
                                                );
                                            },
                                        );
                                    },
                                );
                            })
                            .catch((e) => {
                                console.error(
                                    `Error updating extension [${
                                        ext_id || ext_name
                                    }]:`,
                                    e,
                                );
                                if (failure_callback) {
                                    if (ext_id)
                                        failure_callback(
                                            false,
                                            installed_versions[ext_id],
                                        );
                                    else
                                        failure_callback(false, {
                                            name: ext_name,
                                        });
                                }
                                reject();
                            });
                    });
                }
                chrome.action.setBadgeText(
                    {
                        text: "",
                    },
                    () => {
                        let promises = updateUrls
                            .filter((x) => x.url)
                            .map((uurl) =>
                                update_extension(uurl.url, uurl.id, uurl.name),
                            );
                        Promise.allSettled(promises).then((plist) => {
                            if (plist.some((x) => x.status == "rejected")) {
                                chrome.action.getBadgeText(
                                    {},
                                    function (currentText) {
                                        if (!(parseInt(currentText) > 0))
                                            chrome.action.setBadgeText({
                                                text: "?",
                                            });
                                    },
                                );
                            }
                            if (completed_callback) completed_callback();
                        });
                    },
                );
            });
        });
    });
}
