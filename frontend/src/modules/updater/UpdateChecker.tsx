import { useEffect, useRef, useState } from 'react'
import { Download, AlertCircle, CheckCircle2, ArrowUpCircle, X } from 'lucide-react'
import { Modal, Button, Progress, toast } from '../../shared/components'
import { CheckUpdate, DownloadUpdate, ApplyUpdate } from '../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'

// localStorage key：记录用户点过"稍后再说"的版本号
// 下次启动若 latest 仍等于此值，则不再自动弹 Modal，只在右下角显示小提示
const POSTPONE_KEY = 'boost:postponed-update-version'

/**
 * 触发一次手动检查更新（在 SettingsPage 等地方调用）
 *   import { triggerUpdateCheck } from '.../UpdateChecker'
 *   triggerUpdateCheck()
 */
export function triggerUpdateCheck() {
  window.dispatchEvent(new CustomEvent('boost:check-update'))
}

export interface UpdateInfo {
  hasUpdate: boolean
  current: string
  latest: string
  force: boolean
  releaseNotes: string
  url: string
  sha256: string
  size: number
}

type Phase = 'idle' | 'banner' | 'prompt' | 'downloading' | 'ready' | 'applying'

/**
 * 自动升级组件 —— 在 App.tsx 顶层挂一次即可。
 * - 启动后 5s 静默检查
 * - 监听 'boost:check-update' window 事件实现手动触发
 * - 有新版 → 弹 Modal → 下载 + 进度条 → 重启
 */
export function UpdateChecker() {
  const [phase, setPhase] = useState<Phase>('idle')
  const [info, setInfo] = useState<UpdateInfo | null>(null)
  const [progress, setProgress] = useState(0)
  const [downloadedPath, setDownloadedPath] = useState('')
  const [errorMsg, setErrorMsg] = useState('')
  const checkedOnceRef = useRef(false)
  const phaseRef = useRef<Phase>('idle')
  phaseRef.current = phase

  // 进度事件
  useEffect(() => {
    const handler = (p: any) => {
      if (p && typeof p.percent === 'number') setProgress(p.percent)
    }
    EventsOn('update:progress', handler)
    return () => EventsOff('update:progress')
  }, [])

  // 手动触发（点设置里的"检查更新"）→ 清掉稍后标记，强制走完整流程
  useEffect(() => {
    const onManual = () => {
      if (phaseRef.current !== 'idle' && phaseRef.current !== 'banner') return
      try { localStorage.removeItem(POSTPONE_KEY) } catch {}
      runCheck(false)
    }
    window.addEventListener('boost:check-update', onManual)
    return () => window.removeEventListener('boost:check-update', onManual)
  }, [])

  // 启动 5s 后静默检查一次
  useEffect(() => {
    if (checkedOnceRef.current) return
    checkedOnceRef.current = true
    const timer = setTimeout(() => runCheck(true), 5000)
    return () => clearTimeout(timer)
  }, [])

  async function runCheck(silent: boolean) {
    try {
      const r = (await CheckUpdate()) as UpdateInfo
      if (!r) return
      if (!r.hasUpdate) {
        if (!silent) toast.success(`已是最新版本 ${r.current}`)
        // 已经是最新版了，清掉历史 postpone 标记（避免脏数据）
        try { localStorage.removeItem(POSTPONE_KEY) } catch {}
        return
      }
      setInfo(r)
      // 强制更新 → 永远弹 Modal；非强制 → 检查 localStorage
      if (silent && !r.force) {
        let postponed = ''
        try { postponed = localStorage.getItem(POSTPONE_KEY) || '' } catch {}
        if (postponed && postponed === r.latest) {
          // 用户点过稍后 + 还是同一个版本 → 只显示小角标
          setPhase('banner')
          return
        }
      }
      setPhase('prompt')
    } catch (e: any) {
      if (!silent) toast.error('检查更新失败: ' + (e?.message || String(e)))
    }
  }

  async function startDownload() {
    if (!info) return
    setPhase('downloading')
    setProgress(0)
    setErrorMsg('')
    try {
      const path = (await DownloadUpdate(info.url, info.sha256)) as string
      setDownloadedPath(path)
      setPhase('ready')
    } catch (e: any) {
      const msg = e?.message || String(e)
      setErrorMsg(msg)
      setPhase('prompt')
      toast.error('下载失败: ' + msg)
    }
  }

  async function applyAndRestart() {
    if (!downloadedPath) return
    setPhase('applying')
    try {
      await ApplyUpdate(downloadedPath)
      // 主程序会在 ApplyUpdate 内 Quit；窗口随后被 updater 替换的新版本接管
    } catch (e: any) {
      const msg = e?.message || String(e)
      setErrorMsg(msg)
      setPhase('ready')
      toast.error('应用更新失败: ' + msg)
    }
  }

  function postpone() {
    if (info?.force) return
    // 记住用户对这个版本点过"稍后再说"
    if (info?.latest) {
      try { localStorage.setItem(POSTPONE_KEY, info.latest) } catch {}
    }
    // 切到右下角小角标，下次启动直接走 banner 路径
    setPhase('banner')
    setErrorMsg('')
  }

  function dismissBanner() {
    setPhase('idle')
    setInfo(null)
  }

  function reopenFromBanner() {
    if (!info) return
    setErrorMsg('')
    setPhase('prompt')
  }

  // banner 状态：右下角小提示
  if (phase === 'banner' && info) {
    return (
      <div
        className="fixed bottom-4 right-4 z-50 flex items-center gap-2 px-3 py-2 rounded-lg shadow-lg border border-blue-200 bg-white hover:bg-blue-50 transition cursor-pointer"
        onClick={reopenFromBanner}
        title={`点击查看 v${info.latest} 更新详情`}
      >
        <ArrowUpCircle className="w-4 h-4 text-blue-500 flex-shrink-0" />
        <span className="text-xs text-[var(--color-text-primary)]">
          有可用更新 <span className="font-mono text-blue-600">v{info.latest}</span>
        </span>
        <button
          className="ml-1 p-0.5 rounded hover:bg-gray-100 text-gray-400 hover:text-gray-600"
          onClick={(e) => { e.stopPropagation(); dismissBanner() }}
          title="本次启动不再提示"
        >
          <X className="w-3 h-3" />
        </button>
      </div>
    )
  }

  if (phase === 'idle' || !info) return null

  return (
    <Modal
      open={true}
      onClose={() => phase === 'prompt' && !info.force && postpone()}
      width="480px"
      closable={phase === 'prompt' && !info.force}
    >
      <div className="px-5 py-4">
        <div className="flex items-start gap-3 mb-4">
          <div className="w-10 h-10 rounded-full bg-blue-50 text-blue-500 flex items-center justify-center flex-shrink-0">
            {phase === 'ready' ? <CheckCircle2 className="w-5 h-5" /> : <Download className="w-5 h-5" />}
          </div>
          <div className="flex-1">
            <h3 className="text-base font-semibold text-[var(--color-text-primary)]">
              {phase === 'prompt' && '发现新版本'}
              {phase === 'downloading' && '正在下载更新'}
              {phase === 'ready' && '更新已就绪'}
              {phase === 'applying' && '即将重启应用'}
            </h3>
            <p className="text-xs text-[var(--color-text-secondary)] mt-0.5">
              {phase === 'prompt' && (
                <>
                  当前版本 <span className="font-mono">{info.current}</span> → 最新版本{' '}
                  <span className="font-mono text-[var(--color-text-primary)]">{info.latest}</span>
                  {info.size > 0 && (
                    <span className="ml-2">· {(info.size / 1024 / 1024).toFixed(1)} MB</span>
                  )}
                </>
              )}
              {phase === 'downloading' && '请勿关闭应用'}
              {phase === 'ready' && '点击重启完成更新'}
              {phase === 'applying' && '应用正在退出，请稍候...'}
            </p>
          </div>
        </div>

        {phase === 'prompt' && info.releaseNotes && (
          <div className="mb-4 p-3 rounded-lg bg-[var(--color-bg-secondary)] max-h-40 overflow-y-auto">
            <pre className="text-xs whitespace-pre-wrap font-sans text-[var(--color-text-secondary)] leading-relaxed">
              {info.releaseNotes}
            </pre>
          </div>
        )}

        {(phase === 'downloading' || phase === 'applying') && (
          <div className="my-4">
            <Progress percent={progress} />
            <div className="text-xs text-[var(--color-text-secondary)] text-right mt-1">{progress}%</div>
          </div>
        )}

        {errorMsg && phase === 'prompt' && (
          <div className="mb-4 p-2 rounded bg-red-50 text-red-600 text-xs flex items-start gap-2">
            <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
            <span>{errorMsg}</span>
          </div>
        )}

        {phase === 'prompt' && info.force && (
          <div className="mb-4 p-2 rounded bg-amber-50 text-amber-700 text-xs flex items-start gap-2">
            <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
            <span>本次为强制更新，必须升级后才能继续使用</span>
          </div>
        )}

        <div className="flex gap-2 justify-end mt-2">
          {phase === 'prompt' && (
            <>
              {!info.force && (
                <Button variant="secondary" onClick={postpone}>
                  稍后再说
                </Button>
              )}
              <Button variant="primary" onClick={startDownload}>
                立即更新
              </Button>
            </>
          )}
          {phase === 'ready' && (
            <Button variant="primary" onClick={applyAndRestart}>
              重启更新
            </Button>
          )}
        </div>
      </div>
    </Modal>
  )
}
