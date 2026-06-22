import { Suspense, lazy, useEffect, useState } from 'react'
import type { ComponentType } from 'react'
import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom'
import { ThemeProvider } from './shared/theme'
import { Layout } from './shared/layout'
import { ToastContainer, Modal, Button, Loading } from './shared/components'
import { AlertCircle } from 'lucide-react'
import { useNotificationStore } from './store/notificationStore'
import { useBackupStore } from './store/backupStore'
import { ForceQuit as ForceQuitApp, IsWindowSyncPanelMode, RecordLifecycleEvent, SaveNativeMainWindowBounds } from './wailsjs/go/main/App'
import { Environment, Quit, WindowGetPosition, WindowGetSize, WindowHide, WindowIsMaximised, WindowIsMinimised, WindowMinimise, WindowSetPosition, WindowSetSize } from './wailsjs/runtime/runtime'

function lazyNamed<TModule extends Record<string, ComponentType<any>>>(
  loader: () => Promise<TModule>,
  exportName: keyof TModule,
) {
  return lazy(async () => {
    const module = await loader()
    return {
      default: module[exportName] as ComponentType<any>,
    }
  })
}

const DashboardPage = lazyNamed(() => import('./modules/dashboard/DashboardPage'), 'DashboardPage')
const SettingsPage = lazyNamed(() => import('./modules/settings/SettingsPage'), 'SettingsPage')
const ProfilePage = lazyNamed(() => import('./modules/profile/ProfilePage'), 'ProfilePage')

const ChartsPage = lazyNamed(() => import('./modules/charts/ChartsPage'), 'ChartsPage')
const BrowserListPage = lazyNamed(() => import('./modules/browser/pages/BrowserListPage'), 'BrowserListPage')
const BrowserDetailPage = lazyNamed(() => import('./modules/browser/pages/BrowserDetailPage'), 'BrowserDetailPage')
const BrowserEditPage = lazyNamed(() => import('./modules/browser/pages/BrowserEditPage'), 'BrowserEditPage')
const BrowserCopyPage = lazyNamed(() => import('./modules/browser/pages/BrowserCopyPage'), 'BrowserCopyPage')
const BatchCreatePage = lazyNamed(() => import('./modules/browser/pages/BatchCreatePage'), 'BatchCreatePage')
const BrowserLogsPage = lazyNamed(() => import('./modules/browser/pages/BrowserLogsPage'), 'BrowserLogsPage')
const ProxyPoolPage = lazyNamed(() => import('./modules/browser/pages/ProxyPoolPage'), 'ProxyPoolPage')
const CoreManagementPage = lazyNamed(() => import('./modules/browser/pages/CoreManagementPage'), 'CoreManagementPage')
const BookmarkSettingsPage = lazyNamed(() => import('./modules/browser/pages/BookmarkSettingsPage'), 'BookmarkSettingsPage')
const LaunchApiDocsPage = lazyNamed(() => import('./modules/browser/pages/LaunchApiDocsPage'), 'LaunchApiDocsPage')
const TagManagementPage = lazyNamed(() => import('./modules/browser/pages/TagManagementPage'), 'TagManagementPage')

import { UpdateChecker } from './modules/updater/UpdateChecker'
const WindowSyncPage = lazyNamed(() => import('./modules/browser/pages/WindowSyncPage'), 'WindowSyncPage')
const ExtensionManagementPage = lazyNamed(() => import('./modules/browser/pages/ExtensionManagementPage'), 'ExtensionManagementPage')
const AutomationPage = lazyNamed(() => import('./modules/browser/pages/AutomationPage'), 'AutomationPage')
const UsageTutorialPage = lazyNamed(() => import('./modules/browser/pages/UsageTutorialPage'), 'UsageTutorialPage')
const QuickLaunchModal = lazyNamed(() => import('./modules/browser/components/QuickLaunchModal'), 'QuickLaunchModal')

const MAIN_WINDOW_BOUNDS_STORAGE_KEY = 'boost:main-window-bounds:v1'

type SavedMainWindowBounds = {
  x: number
  y: number
  width: number
  height: number
}

function readSavedMainWindowBounds(): SavedMainWindowBounds | null {
  try {
    const raw = localStorage.getItem(MAIN_WINDOW_BOUNDS_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<SavedMainWindowBounds>
    if (
      typeof parsed?.x !== 'number' ||
      typeof parsed?.y !== 'number' ||
      typeof parsed?.width !== 'number' ||
      typeof parsed?.height !== 'number'
    ) {
      return null
    }
    if (parsed.width < 1200 || parsed.height < 700) {
      return null
    }
    return {
      x: Math.round(parsed.x),
      y: Math.round(parsed.y),
      width: Math.round(parsed.width),
      height: Math.round(parsed.height),
    }
  } catch {
    return null
  }
}

function writeSavedMainWindowBounds(bounds: SavedMainWindowBounds) {
  try {
    localStorage.setItem(MAIN_WINDOW_BOUNDS_STORAGE_KEY, JSON.stringify(bounds))
  } catch {
    // ignore write failures
  }
}

async function saveMainWindowBoundsSnapshot() {
  const [isMaximised, isMinimised] = await Promise.all([
    WindowIsMaximised(),
    WindowIsMinimised(),
  ])
  if (isMaximised || isMinimised) return false

  const [position, size] = await Promise.all([
    WindowGetPosition(),
    WindowGetSize(),
  ])
  if (!position || !size) return false
  if (size.w < 1200 || size.h < 700) return false

  const bounds = {
    x: Math.round(position.x),
    y: Math.round(position.y),
    width: Math.round(size.w),
    height: Math.round(size.h),
  }
  writeSavedMainWindowBounds(bounds)
  try {
    await SaveNativeMainWindowBounds(bounds)
  } catch {
    // ignore native snapshot failures
  }
  return true
}

function useWailsNotifications() {
  const addNotification = useNotificationStore((s) => s.addNotification)

  useEffect(() => {
    const runtime = (window as any).runtime
    if (!runtime?.EventsOn) return

    const offCrashed = runtime.EventsOn(
      'browser:instance:crashed',
      (data: { profileId: string; profileName: string; error: string }) => {
        addNotification({
          type: 'error',
          title: '实例异常退出',
          message: `「${data.profileName || data.profileId}」意外崩溃：${data.error}`,
        })
      }
    )

    const offBridgeFailed = runtime.EventsOn(
      'proxy:bridge:failed',
      (data: { profileId: string; profileName: string; error: string }) => {
        addNotification({
          type: 'error',
          title: '代理连接失败',
          message: `「${data.profileName || data.profileId}」代理桥接启动失败：${data.error}`,
        })
      }
    )

    const offBridgeDied = runtime.EventsOn(
      'proxy:bridge:died',
      (data: { key: string; error: string }) => {
        addNotification({
          type: 'warning',
          title: '连接池节点失效',
          message: `代理节点 ${data.key} 连接中断，相关实例可能无法访问网络`,
        })
      }
    )

    return () => {
      offCrashed?.()
      offBridgeFailed?.()
      offBridgeDied?.()
    }
  }, [addNotification])
}

function CloseConfirmModal() {
  const [open, setOpen] = useState(false)
  const [platform, setPlatform] = useState('windows')
  const [quittingAction, setQuittingAction] = useState<'app-only' | 'app-and-browser' | null>(null)
  const importInProgress = useBackupStore((s) => s.importInProgress)
  const importProgress = useBackupStore((s) => s.importProgress)
  const importMessage = useBackupStore((s) => s.importMessage)
  const supportsTray = platform === 'windows'
  const quitting = quittingAction !== null

  useEffect(() => {
    const runtime = (window as any).runtime
    if (!runtime?.EventsOn) return

    const off = runtime.EventsOn('app:request-close', () => {
      setQuittingAction(null)
      setOpen(true)
    })
    return () => {
      if (typeof off === 'function') off()
    }
  }, [])

  useEffect(() => {
    let cancelled = false

    Environment()
      .then((info) => {
        if (!cancelled && info?.platform) {
          setPlatform(info.platform)
        }
      })
      .catch(() => {})

    return () => {
      cancelled = true
    }
  }, [])

  const closeModal = () => {
    if (quitting) return
    setOpen(false)
  }

  const handleMinimize = async () => {
    if (quitting) return
    RecordLifecycleEvent('frontend-click', ['action=minimize-to-tray']).catch(() => {})
    try {
      await saveMainWindowBoundsSnapshot()
    } catch {}
    setOpen(false)
    if (supportsTray) {
      WindowHide()
      return
    }
    WindowMinimise()
  }

  const handleQuitAppOnly = async () => {
    setQuittingAction('app-only')
    try {
      await RecordLifecycleEvent('frontend-click', ['action=hide-to-tray'])
      try {
        await saveMainWindowBoundsSnapshot()
      } catch {}
      setOpen(false)
      if (supportsTray) {
        WindowHide()
        return
      }
      WindowMinimise()
    } catch (error) {
      console.error('Hide to tray failed', error)
      setQuittingAction(null)
    }
  }

  const handleQuitAppAndBrowsers = async () => {
    setQuittingAction('app-and-browser')
    try {
      await RecordLifecycleEvent('frontend-click', ['action=quit-app-and-browser'])
      try {
        await saveMainWindowBoundsSnapshot()
      } catch {}
      await Promise.race([
        ForceQuitApp(),
        new Promise((resolve) => setTimeout(resolve, 1200)),
      ])
    } catch (error) {
      console.error('ForceQuit failed, falling back to runtime.Quit()', error)
    }
    Quit()
  }

  return (
    <Modal
      open={open}
      onClose={closeModal}
      title={importInProgress ? '关闭应用确认' : undefined}
      width={importInProgress ? '360px' : '420px'}
      closable={!quitting}
    >
      <div className="flex flex-col items-center pt-2 pb-6 px-4">
        <div className={`w-12 h-12 rounded-full flex items-center justify-center mb-4 ${
          importInProgress ? 'bg-amber-50 text-amber-500' : 'bg-red-50 text-red-500'
        }`}>
          <AlertCircle className="w-6 h-6" />
        </div>
        {importInProgress && (
          <h3 className="text-lg font-medium text-[var(--color-text-primary)] mb-2">
            正在加载中，是否关闭？
          </h3>
        )}
        {importInProgress ? (
          <p className="text-sm text-[var(--color-text-secondary)] text-center mb-6">
            当前正在加载配置
            {importProgress > 0 ? `（${importProgress}%）` : ''}。
            <br />
            {importMessage || '强制关闭会中断本次加载，是否仍要关闭应用？'}
          </p>
        ) : (
          <p className="mb-6 text-sm text-center text-[var(--color-text-secondary)]">
            可隐藏主窗口到托盘，或连同浏览器一起关闭。
          </p>
        )}

        <div className={`w-full ${importInProgress ? 'flex gap-3' : 'flex flex-col gap-2'}`}>
          {importInProgress ? (
            <>
              <Button variant="secondary" className="flex-1" onClick={closeModal} disabled={quitting}>
                继续加载
              </Button>
              <Button
                variant="danger"
                className="flex-1"
                onClick={handleQuitAppAndBrowsers}
                loading={quittingAction === 'app-and-browser'}
              >
                仍要关闭
              </Button>
            </>
          ) : (
            <>
              <Button
                variant="secondary"
                className="w-full !bg-[#f3f4f6] !border-[#e5e7eb] !text-[var(--color-text-primary)] hover:!bg-[#e5e7eb]"
                onClick={supportsTray ? handleMinimize : closeModal}
                disabled={quitting}
              >
                {supportsTray ? '最小化到托盘' : '取消'}
              </Button>
              <Button
                className="w-full"
                onClick={handleQuitAppOnly}
                loading={quittingAction === 'app-only'}
                disabled={quitting}
              >
                隐藏到托盘（主程序保持运行）
              </Button>
              <Button
                variant="danger"
                className="w-full"
                onClick={handleQuitAppAndBrowsers}
                loading={quittingAction === 'app-and-browser'}
                disabled={quitting}
              >
                退出应用与浏览器
              </Button>
            </>
          )}
        </div>
      </div>
    </Modal>
  )
}

function App() {
  useWailsNotifications()
  const [quickLaunchOpen, setQuickLaunchOpen] = useState(false)
  const [syncPanelMode, setSyncPanelMode] = useState(false)
  const [panelModeLoaded, setPanelModeLoaded] = useState(false)
  const routeFallback = (
    <div className="flex min-h-[240px] items-center justify-center py-10">
      <Loading text="页面加载中..." />
    </div>
  )

  useEffect(() => {
    let cancelled = false
    IsWindowSyncPanelMode()
      .then((enabled) => {
        if (!cancelled) {
          setSyncPanelMode(enabled === true)
          setPanelModeLoaded(true)
        }
      })
      .catch(() => {
        if (!cancelled) {
          setPanelModeLoaded(true)
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.isComposing) return
      if (!(event.ctrlKey || event.metaKey)) return
      if (event.key.toLowerCase() !== 'k') return
      event.preventDefault()
      setQuickLaunchOpen((prev) => !prev)
    }

    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [])

  useEffect(() => {
    document.body.classList.toggle('sync-panel-mode', syncPanelMode)
    return () => {
      document.body.classList.remove('sync-panel-mode')
    }
  }, [syncPanelMode])

  useEffect(() => {
    if (!panelModeLoaded || syncPanelMode) return

    let cancelled = false

    const restoreBounds = async () => {
      const saved = readSavedMainWindowBounds()
      if (!saved) return
      try {
        await WindowSetSize(saved.width, saved.height)
        await WindowSetPosition(saved.x, saved.y)
        await new Promise((resolve) => window.setTimeout(resolve, 150))
      } catch {
        // ignore restore failures
      }
    }

    const persistBounds = async () => {
      try {
        if (cancelled) return
        await saveMainWindowBoundsSnapshot()
      } catch {
        // ignore snapshot failures
      }
    }

    void restoreBounds().then(() => {
      void persistBounds()
    })

    const interval = window.setInterval(() => {
      void persistBounds()
    }, 1500)

    const handleBeforeUnload = () => {
      void persistBounds()
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'hidden') {
        void persistBounds()
      }
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      cancelled = true
      window.clearInterval(interval)
      window.removeEventListener('beforeunload', handleBeforeUnload)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [panelModeLoaded, syncPanelMode])

  if (!panelModeLoaded) {
    return (
      <ThemeProvider>
        <div className="flex min-h-screen items-center justify-center bg-[var(--color-bg-base)]">
          <Loading text="同步器窗口加载中..." />
        </div>
      </ThemeProvider>
    )
  }

  return (
    <ThemeProvider>
      <Router>
        <Layout syncPanelMode={syncPanelMode}>
          <Suspense fallback={routeFallback}>
            <Routes>
              {syncPanelMode ? (
                <>
                  <Route path="/" element={<Navigate to="/browser/sync" replace />} />
                  <Route path="/browser/sync" element={<WindowSyncPage />} />
                  <Route path="*" element={<Navigate to="/browser/sync" replace />} />
                </>
              ) : (
                <>
                  <Route path="/" element={<DashboardPage />} />
                  <Route path="/charts" element={<ChartsPage />} />
                  <Route path="/settings" element={<SettingsPage />} />
                  <Route path="/profile" element={<ProfilePage />} />
                  
                  <Route path="/browser/list" element={<BrowserListPage />} />
                  <Route path="/browser/detail/:id" element={<BrowserDetailPage />} />
                  <Route path="/browser/edit/:id" element={<BrowserEditPage />} />
                  <Route path="/browser/copy/:id" element={<BrowserCopyPage />} />
                  <Route path="/browser/batch-create" element={<BatchCreatePage />} />
                  <Route path="/browser/monitor" element={<Navigate to="/browser/list" replace />} />
                  <Route path="/browser/logs" element={<BrowserLogsPage />} />
                  <Route path="/browser/proxy-pool" element={<ProxyPoolPage />} />
                  <Route path="/browser/cores" element={<CoreManagementPage />} />
                  <Route path="/browser/bookmarks" element={<BookmarkSettingsPage />} />
                  <Route path="/browser/automation" element={<AutomationPage />} />
                  <Route path="/browser/launch-api" element={<LaunchApiDocsPage />} />
                  <Route path="/browser/tags" element={<TagManagementPage />} />
                  <Route path="/browser/sync" element={<Navigate to="/browser/list" replace />} />
                  <Route path="/browser/extensions" element={<ExtensionManagementPage />} />
                  <Route path="/system/tutorial" element={<UsageTutorialPage />} />
                </>
              )}
            </Routes>
          </Suspense>
        </Layout>
        <ToastContainer />
        {!syncPanelMode && <CloseConfirmModal />}
        {!syncPanelMode && <UpdateChecker />}
        {!syncPanelMode && (
          <Suspense fallback={null}>
            {quickLaunchOpen ? (
              <QuickLaunchModal open={quickLaunchOpen} onClose={() => setQuickLaunchOpen(false)} />
            ) : null}
          </Suspense>
        )}
      </Router>
    </ThemeProvider>
  )
}

export default App
