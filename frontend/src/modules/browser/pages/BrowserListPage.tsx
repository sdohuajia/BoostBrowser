import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Activity, CheckCircle, ChevronDown, ChevronRight, ChevronUp, Copy, Download, Edit2, FileText, FolderInput, Key, Layers, Pencil, Play, Plus, RefreshCw, RotateCcw, Settings, Sliders, Square, Star, Trash2, Upload, Wand2, XCircle, LayoutGrid, List } from 'lucide-react'
import { Badge, Button, Card, FormItem, Input, Modal, StatCard, Table, Textarea, toast } from '../../../shared/components'
import { reloadConfig } from '../../dashboard/api'
import type { TableColumn } from '../../../shared/components/Table'
import type { BrowserCore, BrowserCoreInput, BrowserProfile, BrowserProxy, BrowserSettings, BrowserGroupWithCount } from '../types'
import { InstanceFilterBar, EMPTY_FILTERS, isFiltersEmpty } from '../components/InstanceFilterBar'
import type { InstanceFilters } from '../components/InstanceFilterBar'
import { KeywordsModal } from '../components/KeywordsModal'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import { resolveActionErrorMessage, resolveActionFeedback } from '../utils/actionErrors'
import {
  copyBrowserProfile,
  deleteBrowserCore,
  deleteBrowserProfile,
  fetchBrowserCores,
  fetchBrowserProfiles,
  fetchBrowserProxies,
  fetchBrowserSettings,
  fetchGroups,
  createGroup,
  updateGroup,
  deleteGroup,
  moveInstancesToGroup,
  importExtensionToBrowserProfiles,
  importMoreLoginProfiles,
  importRunningMoreLoginProfiles,
  regenerateBrowserProfileCode,
  randomizeProfileFingerprint,
  restartBrowserInstance,
  saveBrowserCore,
  saveBrowserSettings,
  setBrowserProfileCode,
  setDefaultBrowserCore,
  startBrowserInstance,
  stopBrowserInstance,
  validateBrowserCorePath,
  validateProxyConfig,
} from '../api'

// 批量操作工具栏
function BatchToolbar({
  selectedCount,
  totalCount,
  onSelectAll,
  onDeselectAll,
  onBatchStart,
  onBatchStop,
  onBatchDelete,
  onBatchMoveToGroup,
  onBatchRandomizeFingerprint,
  groups,
  batchLoading,
}: {
  selectedCount: number
  totalCount: number
  onSelectAll: () => void
  onDeselectAll: () => void
  onBatchStart: () => void
  onBatchStop: () => void
  onBatchDelete: () => void
  onBatchMoveToGroup: (groupId: string) => void
  onBatchRandomizeFingerprint: () => void
  groups: BrowserGroupWithCount[]
  batchLoading: boolean
}) {
  const [moveOpen, setMoveOpen] = useState(false)
  if (selectedCount === 0) return null
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 bg-[var(--color-accent)]/10 border border-[var(--color-accent)]/20 rounded-lg">
      <span className="text-sm font-medium text-[var(--color-accent)]">已选 {selectedCount} / {totalCount}</span>
      <div className="flex gap-1.5 ml-auto items-center">
        <Button size="sm" variant="ghost" onClick={onSelectAll}>全选</Button>
        <Button size="sm" variant="ghost" onClick={onDeselectAll}>取消</Button>
        <Button size="sm" onClick={onBatchStart} loading={batchLoading} title="批量启动">
          <Play className="w-3.5 h-3.5" />启动
        </Button>
        <Button size="sm" variant="secondary" onClick={onBatchStop} loading={batchLoading} title="批量停止">
          <Square className="w-3.5 h-3.5" />停止
        </Button>
        <div className="relative">
          <Button size="sm" variant="secondary" onClick={() => setMoveOpen(v => !v)} title="移动到分组">
            <FolderInput className="w-3.5 h-3.5" />移到分组
          </Button>
          {moveOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setMoveOpen(false)} />
              <div className="absolute right-0 top-full mt-1 z-50 min-w-[180px] max-h-[320px] overflow-y-auto bg-[var(--color-bg-surface)] border border-[var(--color-border-default)] rounded-lg shadow-lg py-1">
                <button
                  className="w-full text-left px-3 py-1.5 text-sm hover:bg-[var(--color-bg-muted)] text-[var(--color-text-muted)]"
                  onClick={() => { setMoveOpen(false); onBatchMoveToGroup('') }}
                >
                  移出分组（设为未分组）
                </button>
                <div className="my-1 border-t border-[var(--color-border-muted)]" />
                {groups.length === 0 ? (
                  <div className="px-3 py-2 text-xs text-[var(--color-text-muted)]">暂无分组，请先在筛选区新建</div>
                ) : (
                  groups.map(g => (
                    <button
                      key={g.groupId}
                      className="w-full text-left px-3 py-1.5 text-sm hover:bg-[var(--color-bg-muted)] flex items-center justify-between gap-2"
                      onClick={() => { setMoveOpen(false); onBatchMoveToGroup(g.groupId) }}
                    >
                      <span className="truncate">{g.groupName}</span>
                      <span className="text-xs text-[var(--color-text-muted)] shrink-0">{g.instanceCount}</span>
                    </button>
                  ))
                )}
              </div>
            </>
          )}
        </div>
        <Button size="sm" variant="secondary" onClick={onBatchRandomizeFingerprint} loading={batchLoading} title="批量随机指纹种子（运行中的实例会跳过）">
          <Wand2 className="w-3.5 h-3.5" />随机指纹
        </Button>
        <Button size="sm" variant="ghost" onClick={onBatchDelete} title="批量删除" className="text-red-500 hover:text-red-600">
          <Trash2 className="w-3.5 h-3.5" />删除
        </Button>
      </div>
    </div>
  )
}

const resolveProfileStatus = (running: boolean, debugReady: boolean, starting: boolean, stopping: boolean) => {
  if (starting) {
    return { variant: 'info' as const, label: '启动中' }
  }
  if (stopping) {
    return { variant: 'default' as const, label: '停止中' }
  }
  if (running && !debugReady) {
    return { variant: 'info' as const, label: '运行中（待就绪）' }
  }
  if (running) {
    return { variant: 'success' as const, label: '运行中' }
  }
  return { variant: 'warning' as const, label: '已停止' }
}
const formatTime = (value?: string) => {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN')
}

function LaunchCodeCell({ profileId, code, onRefresh }: { profileId: string; code: string; onRefresh: () => void }) {
  const [loading, setLoading] = useState(false)

  const handleCopy = () => {
    if (!code) return
    navigator.clipboard.writeText(code).then(() => toast.success('已复制快捷码'))
  }

  const handleRegenerate = async () => {
    setLoading(true)
    try {
      await regenerateBrowserProfileCode(profileId)
      onRefresh()
      toast.success('快捷码已重新生成')
    } catch {
      toast.error('重新生成失败')
    } finally {
      setLoading(false)
    }
  }

  const handleCustomCode = async () => {
    const next = prompt('请输入自定义 Code（4-32位，仅支持字母/数字/_/-）', code || '')
    if (next == null) return
    const value = next.trim()
    if (!value) {
      toast.error('Code 不能为空')
      return
    }
    setLoading(true)
    try {
      const applied = await setBrowserProfileCode(profileId, value)
      onRefresh()
      toast.success(`Code 已更新为 ${applied}`)
    } catch (error: any) {
      toast.error(error?.message || '设置自定义 Code 失败')
    } finally {
      setLoading(false)
    }
  }

  if (!code) return <span className="text-[var(--color-text-muted)] text-xs">-</span>

  return (
    <div className="flex items-center gap-1">
      <code className="text-xs font-mono bg-[var(--color-bg-secondary)] px-1.5 py-0.5 rounded text-[var(--color-accent)]">{code}</code>
      <button onClick={handleCopy} className="p-0.5 hover:text-[var(--color-accent)] text-[var(--color-text-muted)] transition-colors" title="复制">
        <Copy className="w-3 h-3" />
      </button>
      <button onClick={handleRegenerate} disabled={loading} className="p-0.5 hover:text-[var(--color-accent)] text-[var(--color-text-muted)] transition-colors disabled:opacity-50" title="重新生成">
        <RefreshCw className="w-3 h-3" />
      </button>
      <button onClick={handleCustomCode} disabled={loading} className="p-0.5 hover:text-[var(--color-accent)] text-[var(--color-text-muted)] transition-colors disabled:opacity-50" title="自定义">
        <Pencil className="w-3 h-3" />
      </button>
    </div>
  )
}

function KeywordInlineRow({ keywords }: { keywords: string[] }) {
  const [expanded, setExpanded] = useState(false)
  const cRef = (useMemo(() => ({ current: null as HTMLDivElement | null }), []) as unknown) as React.MutableRefObject<HTMLDivElement | null>
  const [isOverflowing, setIsOverflowing] = useState(false)

  useEffect(() => {
    if (cRef.current) {
      setIsOverflowing(cRef.current.scrollHeight > 36)
    }
  }, [keywords])

  if (!keywords?.length) {
    return <span className="text-xs text-[var(--color-text-muted)] italic">暂无关键字</span>
  }

  return (
    <div className="flex items-start gap-4 w-full">
      <div
        ref={cRef}
        className={`flex flex-wrap gap-2 flex-1 transition-all duration-300 ${expanded ? '' : 'overflow-hidden max-h-[32px]'}`}
      >
        {keywords.map((kw, i) => (
          <span
            key={i}
            className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs
              bg-[var(--color-bg-surface)] border border-[var(--color-border-default)]
              text-[var(--color-text-secondary)] max-w-[200px]"
            title={kw}
          >
            <span className="text-[var(--color-text-muted)] font-mono shrink-0">{i + 1}.</span>
            <span className="truncate">{kw}</span>
          </span>
        ))}
      </div>
      {isOverflowing && (
        <button
          onClick={() => setExpanded(!expanded)}
          className="shrink-0 flex items-center gap-1 text-xs font-medium text-[var(--color-accent)] hover:text-indigo-400 mt-1 focus:outline-none"
        >
          {expanded ? (
            <>收回 <ChevronUp className="w-3.5 h-3.5" /></>
          ) : (
            <>展开详情 <ChevronDown className="w-3.5 h-3.5" /></>
          )}
        </button>
      )}
    </div>
  )
}

export function BrowserListPage() {
  const [profiles, setProfiles] = useState<BrowserProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [proxies, setProxies] = useState<BrowserProxy[]>([])
  const [groups, setGroups] = useState<BrowserGroupWithCount[]>([])

  // 视图模式
  const [viewMode, setViewMode] = useState<'card' | 'table'>(() => {
    return (localStorage.getItem('browser:viewMode') as 'card' | 'table') || 'table'
  })

  // 勾选状态
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [batchLoading, setBatchLoading] = useState(false)
  const [deleteModal, setDeleteModal] = useState<{ open: boolean; ids: string[]; names: string[] }>({ open: false, ids: [], names: [] })
  const [deleteCache, setDeleteCache] = useState(false)
  const [deletingProfiles, setDeletingProfiles] = useState(false)

  // 筛选状态（从 localStorage 恢复）
  const [filters, setFilters] = useState<InstanceFilters>(() => {
    try {
      const saved = localStorage.getItem('browser:filters')
      if (saved) {
        const parsed = JSON.parse(saved)
        return { ...EMPTY_FILTERS, ...parsed, tags: new Set(parsed.tags || []) }
      }
    } catch { /* ignore */ }
    return EMPTY_FILTERS
  })
  const [headerCollapsed, setHeaderCollapsed] = useState(() => {
    return localStorage.getItem('browser:headerCollapsed') === 'true'
  })

  // 持久化筛选状态
  useEffect(() => {
    const serializable = { ...filters, tags: Array.from(filters.tags) }
    localStorage.setItem('browser:filters', JSON.stringify(serializable))
  }, [filters])

  useEffect(() => {
    localStorage.setItem('browser:viewMode', viewMode)
  }, [viewMode])

  useEffect(() => {
    localStorage.setItem('browser:headerCollapsed', String(headerCollapsed))
  }, [headerCollapsed])

  // 代理不支持弹窗
  const [proxyErrorModal, setProxyErrorModal] = useState(false)
  const [proxyErrorMsg, setProxyErrorMsg] = useState('')
  const [opError, setOpError] = useState('')
  const [pendingStartId, setPendingStartId] = useState<string | null>(null)
  const [startingIds, setStartingIds] = useState<Set<string>>(new Set())
  const [stoppingIds, setStoppingIds] = useState<Set<string>>(new Set())
  const profilesRef = useRef<BrowserProfile[]>([])
  const silentRefreshInFlightRef = useRef(false)

  // 关键字弹窗
  const [kwModal, setKwModal] = useState<{ open: boolean; profile: BrowserProfile | null }>({ open: false, profile: null })

  const openKwModal = (profile: BrowserProfile) => setKwModal({ open: true, profile })
  const closeKwModal = () => setKwModal({ open: false, profile: null })

  // 复制弹窗
  const [copyModal, setCopyModal] = useState<{ open: boolean; profile: BrowserProfile | null }>({ open: false, profile: null })
  const [copyName, setCopyName] = useState('')
  const [copying, setCopying] = useState(false)

  // 扩展导入弹窗
  const [extensionModalOpen, setExtensionModalOpen] = useState(false)
  const [extensionUrl, setExtensionUrl] = useState('')
  const [importingExtension, setImportingExtension] = useState(false)

  const openCopyModal = (profile: BrowserProfile) => {
    setCopyName(profile.profileName + ' (副本)')
    setCopyModal({ open: true, profile })
  }
  const closeCopyModal = () => {
    setCopyModal({ open: false, profile: null })
    setCopyName('')
  }

  // 基础配置弹窗
  const [settingsModalOpen, setSettingsModalOpen] = useState(false)
  const [settings, setSettings] = useState<BrowserSettings>({
    userDataRoot: 'data',
    defaultFingerprintArgs: [],
    defaultLaunchArgs: [],
    defaultProxy: '',
    startReadyTimeoutMs: 3000,
    startStableWindowMs: 1200,
  })
  const [fingerprintText, setFingerprintText] = useState('')
  const [launchText, setLaunchText] = useState('')
  const [savingSettings, setSavingSettings] = useState(false)

  // 内核管理
  const [cores, setCores] = useState<BrowserCore[]>([])
  const [coreModalOpen, setCoreModalOpen] = useState(false)
  const [coreForm, setCoreForm] = useState<BrowserCoreInput>({ coreId: '', coreName: '', corePath: '', isDefault: false })
  const [coreValidation, setCoreValidation] = useState<{ valid: boolean; message: string } | null>(null)
  const [savingCore, setSavingCore] = useState(false)

  // （扩容限制已移除）

  const updatePendingIds = (
    setter: React.Dispatch<React.SetStateAction<Set<string>>>,
    profileId: string,
    active: boolean
  ) => {
    setter(prev => {
      const next = new Set(prev)
      if (active) {
        next.add(profileId)
      } else {
        next.delete(profileId)
      }
      return next
    })
  }

  const replaceProfilesState = (items: BrowserProfile[]) => {
    profilesRef.current = items
    setProfiles(items)
  }

  const updateProfilesState = (updater: (items: BrowserProfile[]) => BrowserProfile[]) => {
    const next = updater(profilesRef.current)
    profilesRef.current = next
    setProfiles(next)
  }

  const mergeProfileState = (profile: BrowserProfile | null | undefined) => {
    if (!profile) return
    updateProfilesState(prev => prev.map(item => (
      item.profileId === profile.profileId ? { ...item, ...profile } : item
    )))
  }

  const syncProfiles = (items: BrowserProfile[], syncRuntimeState: boolean) => {
    if (syncRuntimeState) {
      const previousById = new Map(profilesRef.current.map(item => [item.profileId, item]))
      const newlyRunning = items.find(item => item.running && !previousById.get(item.profileId)?.running)
      if (newlyRunning) {
        updatePendingIds(setStartingIds, newlyRunning.profileId, false)
        updatePendingIds(setStoppingIds, newlyRunning.profileId, false)
      }
      items.forEach(item => {
        if (!item.running && previousById.get(item.profileId)?.running) {
          updatePendingIds(setStartingIds, item.profileId, false)
          updatePendingIds(setStoppingIds, item.profileId, false)
        }
      })
    }
    replaceProfilesState(items)
  }

  const loadProfiles = async ({ silent = false, syncRuntimeState = false }: { silent?: boolean; syncRuntimeState?: boolean } = {}) => {
    if (silent && silentRefreshInFlightRef.current) {
      return profilesRef.current
    }
    if (!silent) {
      setLoading(true)
    } else {
      silentRefreshInFlightRef.current = true
    }
    try {
      const items = await fetchBrowserProfiles()
      syncProfiles(items, syncRuntimeState)
      return items
    } finally {
      if (silent) {
        silentRefreshInFlightRef.current = false
      } else {
        setLoading(false)
      }
    }
  }

  const loadGroups = async () => {
    setGroups(await fetchGroups())
  }

  const loadSettings = async () => {
    const data = await fetchBrowserSettings()
    setSettings(data)
    setFingerprintText((data.defaultFingerprintArgs || []).join('\n'))
    setLaunchText((data.defaultLaunchArgs || []).join('\n'))
  }

  const loadCores = async () => {
    setCores(await fetchBrowserCores())
  }

  useEffect(() => {
    void loadProfiles()
    loadGroups()
    void reloadConfig()
    fetchBrowserProxies().then(setProxies)
    fetchBrowserCores().then(setCores)

    // 监听浏览器实例生命周期事件，自动更新状态
    const offStarted = EventsOn('browser:instance:started', (payload: any) => {
      const profileId = typeof payload === 'string' ? payload : payload?.profileId
      if (profileId) {
        updatePendingIds(setStartingIds, profileId, false)
        updatePendingIds(setStoppingIds, profileId, false)
      }
      void loadProfiles({ silent: true, syncRuntimeState: true })
    })
    const offUpdated = EventsOn('browser:instance:updated', () => {
      void loadProfiles({ silent: true, syncRuntimeState: true })
    })
    const offStopped = EventsOn('browser:instance:stopped', (payload: any) => {
      const profileId = typeof payload === 'string' ? payload : payload?.profileId
      if (profileId) {
        updatePendingIds(setStartingIds, profileId, false)
        updatePendingIds(setStoppingIds, profileId, false)
      }
      void loadProfiles({ silent: true, syncRuntimeState: true })
    })
    const offCrashed = EventsOn('browser:instance:crashed', (payload: any) => {
      const profileId = typeof payload === 'string' ? payload : payload?.profileId
      if (profileId) {
        updatePendingIds(setStartingIds, profileId, false)
        updatePendingIds(setStoppingIds, profileId, false)
      }
      void loadProfiles({ silent: true, syncRuntimeState: true })
    })

    const timer = window.setInterval(() => {
      if (document.visibilityState !== 'visible') return
      void loadProfiles({ silent: true, syncRuntimeState: true })
    }, 2000)

    return () => {
      window.clearInterval(timer)
      offStarted?.()
      offUpdated?.()
      offStopped?.()
      offCrashed?.()
    }
  }, [])

  const runningCount = useMemo(() => profiles.filter(p => p.running).length, [profiles])
  const allTags = useMemo(() => {
    const set = new Set<string>()
    profiles.forEach(p => p.tags?.forEach(t => set.add(t)))
    return Array.from(set).sort()
  }, [profiles])

  const defaultCore = useMemo(() => {
    return cores.find(core => core.isDefault) || cores[0] || null
  }, [cores])

  const resolveProfileCore = (profile: BrowserProfile) => {
    const coreId = (profile.coreId || '').trim()
    if (coreId && !/^default$/i.test(coreId)) {
      return cores.find(core => core.coreId === coreId) || null
    }
    return defaultCore
  }

  const getProfileCoreLabel = (profile: BrowserProfile) => {
    const resolvedCore = resolveProfileCore(profile)
    if (resolvedCore) {
      return resolvedCore.coreName
    }

    const coreId = (profile.coreId || '').trim()
    if (!coreId || /^default$/i.test(coreId)) {
      return '使用默认内核'
    }
    return coreId
  }

  const isProfileStarting = (profileId: string) => startingIds.has(profileId)
  const isProfileStopping = (profileId: string) => stoppingIds.has(profileId)
  const isProfileBusy = (profileId: string) => isProfileStarting(profileId) || isProfileStopping(profileId)

  const getProfileStatus = (profile: BrowserProfile) => (
    resolveProfileStatus(profile.running, profile.debugReady, isProfileStarting(profile.profileId), isProfileStopping(profile.profileId))
  )

  const groupNameById = useMemo(() => {
    const mapping = new Map<string, string>()
    groups.forEach(group => {
      mapping.set(group.groupId, group.groupName)
    })
    return mapping
  }, [groups])

  const hasActiveFilters = useMemo(() => !isFiltersEmpty(filters), [filters])

  const filteredProfiles = useMemo(() => {
    const naturalCompare = (a: string, b: string): number => {
      const re = /(\d+)|(\D+)/g
      const partsA = a.match(re) || []
      const partsB = b.match(re) || []
      for (let i = 0; i < Math.max(partsA.length, partsB.length); i++) {
        if (i >= partsA.length) return -1
        if (i >= partsB.length) return 1
        const pa = partsA[i], pb = partsB[i]
        const na = Number(pa), nb = Number(pb)
        if (!isNaN(na) && !isNaN(nb)) {
          if (na !== nb) return na - nb
        } else {
          const cmp = pa.localeCompare(pb, 'zh-CN')
          if (cmp !== 0) return cmp
        }
      }
      return 0
    }
    return profiles.filter(p => {
      // 分组筛选
      if (filters.groupId === '__ungrouped__' && p.groupId) return false
      if (filters.groupId && filters.groupId !== '__ungrouped__' && p.groupId !== filters.groupId) return false

      if (filters.keyword) {
        const q = filters.keyword.toLowerCase()
        const searchTargets = [
          p.profileName,
          p.launchCode,
          p.proxyConfig,
          p.proxyId,
          groupNameById.get(p.groupId || '') || '',
          ...(p.tags || []),
          ...(p.keywords || []),
        ].filter(Boolean).map(value => String(value).toLowerCase())
        const hit = searchTargets.some(value => value.includes(q))
        if (!hit) return false
      }
      if (filters.status === 'running' && !p.running) return false
      if (filters.status === 'stopped' && p.running) return false
      if (filters.proxyId === '__none__' && (p.proxyId || p.proxyConfig)) return false
      if (filters.proxyId && filters.proxyId !== '__none__' && p.proxyId !== filters.proxyId) return false
      if (filters.coreId) {
        const effectiveCore = resolveProfileCore(p)
        if (!effectiveCore || effectiveCore.coreId !== filters.coreId) return false
      }
      if (filters.tags.size > 0 && !p.tags?.some(t => filters.tags.has(t))) return false
      if (filters.kwSearch) {
        const q = filters.kwSearch.toLowerCase()
        const hit = p.keywords?.some(v => v.toLowerCase().includes(q))
        if (!hit) return false
      }
      return true
    }).sort((a, b) => naturalCompare(a.profileName, b.profileName))
  }, [profiles, filters, defaultCore, cores, groupNameById])

  const visibleProfileIds = useMemo(() => new Set(filteredProfiles.map(p => p.profileId)), [filteredProfiles])
  const visibleSelectedIds = useMemo(() => (
    new Set(Array.from(selectedIds).filter(id => visibleProfileIds.has(id)))
  ), [selectedIds, visibleProfileIds])

  useEffect(() => {
    if (visibleSelectedIds.size === selectedIds.size) return
    setSelectedIds(visibleSelectedIds)
  }, [selectedIds, visibleSelectedIds])

  const handleStart = async (profileId: string) => {
    const profile = profiles.find(p => p.profileId === profileId)
    updatePendingIds(setStartingIds, profileId, true)
    try {
      if (profile) {
        const result = await validateProxyConfig(profile.proxyConfig || '', profile.proxyId || '')
        if (!result.supported) {
          setProxyErrorMsg(result.errorMsg)
          setPendingStartId(profileId)
          setProxyErrorModal(true)
          return
        }
      }

      const startedProfile = await startBrowserInstance(profileId)
      mergeProfileState(startedProfile)
      if (startedProfile?.running && !startedProfile.debugReady && startedProfile.runtimeWarning) {
        toast.warning(startedProfile.runtimeWarning)
      } else {
        toast.success(`实例已启动${startedProfile?.profileName ? `：${startedProfile.profileName}` : ''}`)
      }
      await loadProfiles({ silent: true, syncRuntimeState: true })
    } catch (error: any) {
      const feedback = resolveActionFeedback(error, '实例启动失败')
      if (feedback.tone === 'warning') {
        toast.warning(feedback.message)
      } else {
        toast.error(feedback.message)
      }
      await loadProfiles({ silent: true, syncRuntimeState: true })
    } finally {
      updatePendingIds(setStartingIds, profileId, false)
    }
  }

  const handleStop = async (profileId: string) => {
    updatePendingIds(setStoppingIds, profileId, true)
    try {
      const stoppedProfile = await stopBrowserInstance(profileId)
      mergeProfileState(stoppedProfile)
      toast.success('实例已停止')
      await loadProfiles({ silent: true, syncRuntimeState: true })
    } catch (error: any) {
      toast.error(resolveActionErrorMessage(error, '实例停止失败'))
      await loadProfiles({ silent: true, syncRuntimeState: true })
    } finally {
      updatePendingIds(setStoppingIds, profileId, false)
    }
  }

  const handleRestart = async (profileId: string) => {
    updatePendingIds(setStoppingIds, profileId, true)
    try {
      const restartedProfile = await restartBrowserInstance(profileId)
      mergeProfileState(restartedProfile)
      toast.success(`实例已重启${restartedProfile?.profileName ? `：${restartedProfile.profileName}` : ''}`)
      await loadProfiles({ silent: true, syncRuntimeState: true })
    } catch (error: any) {
      const feedback = resolveActionFeedback(error, '实例重启失败')
      if (feedback.tone === 'warning') {
        toast.warning(feedback.message)
      } else {
        setOpError(feedback.message)
      }
      await loadProfiles({ silent: true, syncRuntimeState: true })
    } finally {
      updatePendingIds(setStoppingIds, profileId, false)
    }
  }

  const openDeleteModal = (ids: string[]) => {
    const uniqueIds = Array.from(new Set(ids)).filter(Boolean)
    if (uniqueIds.length === 0) return
    const names = uniqueIds.map(id => profiles.find(p => p.profileId === id)?.profileName || id)
    setDeleteCache(false)
    setDeleteModal({ open: true, ids: uniqueIds, names })
  }

  const closeDeleteModal = () => {
    if (deletingProfiles) return
    setDeleteModal({ open: false, ids: [], names: [] })
    setDeleteCache(false)
  }

  const confirmDeleteProfiles = async () => {
    const ids = deleteModal.ids
    if (ids.length === 0) return
    setDeletingProfiles(true)
    setBatchLoading(ids.length > 1)
    try {
      for (const id of ids) {
        await deleteBrowserProfile(id, deleteCache)
      }
      if (ids.length > 1) {
        setSelectedIds(new Set())
      }
      toast.success(`${ids.length > 1 ? `已删除 ${ids.length} 个实例` : '配置已删除'}${deleteCache ? '，缓存文件已删除' : ''}`)
      setDeleteModal({ open: false, ids: [], names: [] })
      setDeleteCache(false)
      await loadProfiles()
    } catch (error: any) {
      toast.error(error?.message || '删除失败')
    } finally {
      setDeletingProfiles(false)
      setBatchLoading(false)
    }
  }

  const handleDelete = async (profileId: string) => {
    openDeleteModal([profileId])
  }

  // 批量操作
  const toggleSelect = (profileId: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      next.has(profileId) ? next.delete(profileId) : next.add(profileId)
      return next
    })
  }



  const handleSelectAll = () => {
    setSelectedIds(new Set(filteredProfiles.map(p => p.profileId)))
  }

  const handleDeselectAll = () => {
    setSelectedIds(new Set())
  }

  const handleBatchStart = async () => {
    const ids = Array.from(visibleSelectedIds)
    if (ids.length === 0) return
    setBatchLoading(true)
    let success = 0, pending = 0, failed = 0
    const pendingMessages: string[] = []
    const failureMessages: string[] = []
    for (const id of ids) {
      const profile = profiles.find(p => p.profileId === id)
      if (!profile || profile.running) continue
      updatePendingIds(setStartingIds, id, true)
      try {
        const startedProfile = await startBrowserInstance(id)
        mergeProfileState(startedProfile)
        success++
      } catch (error: any) {
        const feedback = resolveActionFeedback(error, '实例启动失败')
        if (feedback.pendingAttach) {
          pending++
          pendingMessages.push(`${profile.profileName}：${feedback.message}`)
        } else {
          failed++
          failureMessages.push(`${profile.profileName}：${feedback.message}`)
        }
      } finally {
        updatePendingIds(setStartingIds, id, false)
      }
    }
    setBatchLoading(false)
    const summary = [`成功 ${success}`]
    if (pending > 0) summary.push(`待接管 ${pending}`)
    if (failed > 0) summary.push(`失败 ${failed}`)
    toast.success(`批量启动完成：${summary.join('，')}`)
    if (pendingMessages.length > 0) {
      const preview = pendingMessages.slice(0, 3)
      const more = pendingMessages.length > preview.length ? `\n另有 ${pendingMessages.length - preview.length} 个实例已打开窗口，仍在后台接管。` : ''
      toast.warning(`以下实例已打开窗口，仍在后台接管：\n${preview.join('\n')}${more}`)
    }
    if (failureMessages.length > 0) {
      const preview = failureMessages.slice(0, 3)
      const more = failureMessages.length > preview.length ? `\n另有 ${failureMessages.length - preview.length} 个实例启动失败，请逐个检查。` : ''
      toast.error(`以下实例启动失败：\n${preview.join('\n')}${more}`)
    }
    loadProfiles()
  }

  const handleBatchStop = async () => {
    const ids = Array.from(visibleSelectedIds)
    if (ids.length === 0) return
    setBatchLoading(true)
    let success = 0, failed = 0
    for (const id of ids) {
      const profile = profiles.find(p => p.profileId === id)
      if (!profile || !profile.running) continue
      updatePendingIds(setStoppingIds, id, true)
      try {
        const stoppedProfile = await stopBrowserInstance(id)
        mergeProfileState(stoppedProfile)
        success++
      } catch {
        failed++
      } finally {
        updatePendingIds(setStoppingIds, id, false)
      }
    }
    setBatchLoading(false)
    toast.success(`批量停止完成：成功 ${success}${failed > 0 ? `，失败 ${failed}` : ''}`)
    loadProfiles()
  }

  const handleBatchDelete = async () => {
    const ids = Array.from(visibleSelectedIds)
    if (ids.length === 0) return
    openDeleteModal(ids)
  }

  const handleBatchMoveToGroup = async (groupId: string) => {
    const ids = Array.from(visibleSelectedIds)
    if (ids.length === 0) return
    setBatchLoading(true)
    try {
      await moveInstancesToGroup(ids, groupId)
      const targetName = groupId === '' ? '未分组' : (groups.find(g => g.groupId === groupId)?.groupName || '分组')
      toast.success(`已将 ${ids.length} 个实例移动到「${targetName}」`)
      setSelectedIds(new Set())
      await Promise.all([loadProfiles(), loadGroups()])
    } catch (error: any) {
      toast.error(error?.message || '批量移动失败')
    } finally {
      setBatchLoading(false)
    }
  }

  const handleRandomizeFingerprint = async (profileId: string) => {
    const profile = profiles.find(p => p.profileId === profileId)
    if (!profile) return
    if (profile.running) {
      toast.warning('实例运行中，停止后再重置指纹')
      return
    }
    try {
      const updated = await randomizeProfileFingerprint(profileId)
      if (updated) mergeProfileState(updated)
      toast.success(`已为「${profile.profileName}」重新生成指纹`)
    } catch (error: any) {
      toast.error(error?.message || '重置指纹失败')
    }
  }

  const handleBatchRandomizeFingerprint = async () => {
    const ids = Array.from(visibleSelectedIds)
    if (ids.length === 0) return
    setBatchLoading(true)
    let success = 0, failed = 0, skipped = 0
    for (const id of ids) {
      const profile = profiles.find(p => p.profileId === id)
      if (!profile) continue
      if (profile.running) { skipped++; continue }
      try {
        const updated = await randomizeProfileFingerprint(id)
        if (updated) mergeProfileState(updated)
        success++
      } catch {
        failed++
      }
    }
    setBatchLoading(false)
    const summary = [`成功 ${success}`]
    if (skipped > 0) summary.push(`跳过运行中 ${skipped}`)
    if (failed > 0) summary.push(`失败 ${failed}`)
    toast.success(`批量随机指纹完成：${summary.join('，')}`)
    loadProfiles()
  }

  // 分组管理
  const [groupModal, setGroupModal] = useState<{ open: boolean; mode: 'create' | 'rename'; groupId: string; name: string; parentId: string }>({
    open: false, mode: 'create', groupId: '', name: '', parentId: '',
  })
  const [groupSubmitting, setGroupSubmitting] = useState(false)

  const openCreateGroupModal = () => {
    setGroupModal({ open: true, mode: 'create', groupId: '', name: '', parentId: '' })
  }
  const openRenameGroupModal = (groupId: string) => {
    const g = groups.find(x => x.groupId === groupId)
    if (!g) return
    setGroupModal({ open: true, mode: 'rename', groupId, name: g.groupName, parentId: g.parentId || '' })
  }
  const closeGroupModal = () => {
    if (groupSubmitting) return
    setGroupModal({ open: false, mode: 'create', groupId: '', name: '', parentId: '' })
  }
  const submitGroupModal = async () => {
    const name = groupModal.name.trim()
    if (!name) {
      toast.warning('请输入分组名称')
      return
    }
    setGroupSubmitting(true)
    try {
      if (groupModal.mode === 'create') {
        await createGroup({ groupName: name, parentId: groupModal.parentId, sortOrder: 0 })
        toast.success(`分组「${name}」已创建`)
      } else {
        const g = groups.find(x => x.groupId === groupModal.groupId)
        await updateGroup(groupModal.groupId, {
          groupName: name,
          parentId: groupModal.parentId || g?.parentId || '',
          sortOrder: g?.sortOrder ?? 0,
        })
        toast.success('分组已重命名')
      }
      await loadGroups()
      closeGroupModal()
    } catch (error: any) {
      toast.error(error?.message || '操作失败')
    } finally {
      setGroupSubmitting(false)
    }
  }

  const handleDeleteGroup = async (groupId: string) => {
    const g = groups.find(x => x.groupId === groupId)
    if (!g) return
    const inGroup = profiles.filter(p => p.groupId === groupId).length
    const msg = inGroup > 0
      ? `分组「${g.groupName}」内有 ${inGroup} 个实例。删除分组后这些实例将变为未分组（实例本身不会被删除）。确定继续？`
      : `确定删除分组「${g.groupName}」？`
    if (!confirm(msg)) return
    try {
      await deleteGroup(groupId)
      toast.success(`分组「${g.groupName}」已删除`)
      // 当前筛选选中的就是它，重置回全部
      if (filters.groupId === groupId) {
        setFilters({ ...filters, groupId: '' })
      }
      await Promise.all([loadProfiles(), loadGroups()])
    } catch (error: any) {
      toast.error(error?.message || '删除分组失败')
    }
  }

  const handleOpenExtensionModal = () => {
    if (visibleSelectedIds.size === 0) {
      toast.warning('请先勾选要导入扩展的实例')
      return
    }
    setExtensionModalOpen(true)
  }

  const handleImportExtension = async () => {
    const ids = Array.from(visibleSelectedIds)
    const url = extensionUrl.trim()
    if (ids.length === 0) {
      toast.warning('请先勾选要导入扩展的实例')
      return
    }
    if (!url) {
      toast.warning('请输入扩展程序下载地址')
      return
    }
    setImportingExtension(true)
    try {
      const result = await importExtensionToBrowserProfiles(ids, url)
      toast.success(result?.message || `扩展已导入到 ${ids.length} 个实例`)
      setExtensionModalOpen(false)
      setExtensionUrl('')
      await loadProfiles()
    } catch (error: any) {
      toast.error(error?.message || '扩展导入失败')
    } finally {
      setImportingExtension(false)
    }
  }

  const handleImportMoreLogin = async () => {
    try {
      const result = await importMoreLoginProfiles()
      if (result?.cancelled) {
        return
      }
      await Promise.all([loadProfiles(), loadGroups()])
      const imported = Number(result?.imported || 0)
      const createdGroups = Array.isArray(result?.createdGroups) ? result.createdGroups.length : 0
      const keyworded = Array.isArray(result?.profiles) ? result.profiles.length : imported
      const restoredBrowserData = Number(result?.restoredBrowserData || 0)
      const cacheRoot = typeof result?.cacheRoot === 'string' ? result.cacheRoot.trim() : ''
      const baseMessage = result?.message || (imported > 0 ? `成功导入 ${imported} 个环境` : '导入完成')
      const suffix = `${createdGroups > 0 ? `，新建 ${createdGroups} 个分组` : ''}${keyworded > 0 ? `，已自动写入可搜索关键字` : ''}${restoredBrowserData > 0 ? `，已恢复 ${restoredBrowserData} 个完整环境` : ''}${cacheRoot ? `，缓存目录：${cacheRoot}` : ''}`
      toast.success(`${baseMessage}${suffix}`)
    } catch (error: any) {
      toast.error(error?.message || '导入 MoreLogin 环境失败')
    }
  }

  const handleImportRunningMoreLogin = async () => {
    try {
      const result = await importRunningMoreLoginProfiles()
      if (result?.cancelled) {
        return
      }
      await Promise.all([loadProfiles(), loadGroups()])
      const imported = Number(result?.imported || 0)
      const copiedFiles = Number(result?.copiedFiles || 0)
      const createdGroups = Array.isArray(result?.createdGroups) ? result.createdGroups.length : 0
      const baseMessage = result?.message || (imported > 0 ? `成功导入 ${imported} 个运行中环境` : '导入完成')
      const suffix = `${createdGroups > 0 ? `，新建 ${createdGroups} 个分组` : ''}${copiedFiles > 0 ? `，已复制 ${copiedFiles} 个文件` : ''}`
      toast.success(`${baseMessage}${suffix}`)
    } catch (error: any) {
      toast.error(error?.message || '导入运行中的 MoreLogin 环境失败')
    }
  }

  const handleCopy = async (profileId: string) => {
    if (!copyModal.profile) return
    setCopying(true)
    try {
      await copyBrowserProfile(profileId, copyName)
      toast.success('实例已复制')
      closeCopyModal()
      loadProfiles()
    } catch (error: any) {
      closeCopyModal()
      setOpError(typeof error === 'string' ? error : error?.message || '复制失败')
    } finally {
      setCopying(false)
    }
  }

  const handleOpenSettings = async () => {
    await Promise.all([loadSettings(), loadCores()])
    setSettingsModalOpen(true)
  }

  const handleSaveSettings = async () => {
    setSavingSettings(true)
    try {
      await saveBrowserSettings({
        ...settings,
        defaultFingerprintArgs: fingerprintText.split('\n').map(s => s.trim()).filter(Boolean),
        defaultLaunchArgs: launchText.split('\n').map(s => s.trim()).filter(Boolean),
      })
      toast.success('配置已保存')
      setSettingsModalOpen(false)
    } catch (error: any) {
      toast.error(error?.message || '保存失败')
    } finally {
      setSavingSettings(false)
    }
  }

  // 内核管理
  const handleOpenCoreModal = (core?: BrowserCore) => {
    setCoreForm(core ? { ...core } : { coreId: '', coreName: '', corePath: '', isDefault: false })
    setCoreValidation(null)
    setCoreModalOpen(true)
  }

  const handleValidateCorePath = async () => {
    if (!coreForm.corePath.trim()) {
      setCoreValidation({ valid: false, message: '请输入路径' })
      return
    }
    const result = await validateBrowserCorePath(coreForm.corePath)
    setCoreValidation(result)
  }

  const handleSaveCore = async () => {
    if (!coreForm.coreName.trim()) {
      toast.error('请输入内核名称')
      return
    }
    if (!coreForm.corePath.trim()) {
      toast.error('请输入内核路径')
      return
    }
    setSavingCore(true)
    try {
      await saveBrowserCore(coreForm)
      toast.success('内核已保存')
      setCoreModalOpen(false)
      loadCores()
    } catch (error: any) {
      toast.error(error?.message || '保存失败')
    } finally {
      setSavingCore(false)
    }
  }

  const handleDeleteCore = async (coreId: string) => {
    if (cores.length <= 1) {
      toast.error('至少保留一个内核')
      return
    }
    await deleteBrowserCore(coreId)
    toast.success('内核已删除')
    loadCores()
  }

  const handleSetDefaultCore = async (coreId: string) => {
    await setDefaultBrowserCore(coreId)
    toast.success('已设为默认')
    loadCores()
  }

  const columns: TableColumn<BrowserProfile>[] = [
    {
      key: 'selection',
      title: (
        <input
          type="checkbox"
          className="w-4 h-4 rounded cursor-pointer accent-[var(--color-accent)]"
          checked={selectedIds.size > 0 && selectedIds.size === filteredProfiles.length}
          ref={(input) => { if (input) input.indeterminate = selectedIds.size > 0 && selectedIds.size < filteredProfiles.length }}
          onChange={(e) => {
            if (e.target.checked) handleSelectAll()
            else handleDeselectAll()
          }}
        />
      ),
      width: 40,
      render: (_, record) => (
        <input
          type="checkbox"
          className="w-4 h-4 rounded cursor-pointer accent-[var(--color-accent)]"
          checked={selectedIds.has(record.profileId)}
          onChange={() => toggleSelect(record.profileId)}
        />
      ),
    },
    {
      key: 'profileName',
      title: '实例名称',
      render: (value, record) => (
        <div className="flex flex-col gap-1">
          <Link className="text-[var(--color-accent)] text-sm font-medium hover:underline" to={`/browser/detail/${record.profileId}`}>
            {value}
          </Link>
          {record.tags && record.tags.length > 0 && (
            <div className="flex gap-1 flex-wrap">
              {record.tags.map(tag => <Badge variant="default" key={tag}>{tag}</Badge>)}
            </div>
          )}
        </div>
      ),
    },
    {
      key: 'running',
      title: '状态',
      width: 100,
      render: (_, record) => {
        const status = getProfileStatus(record)
        return <Badge variant={status.variant} dot>{status.label}</Badge>
      },
    },
    {
      key: 'coreId',
      title: '核心',
      render: (_, record) => {
        return <span className="text-xs">{getProfileCoreLabel(record)}</span>
      },
    },
    {
      key: 'proxyId',
      title: '代理',
      render: (value) => {
        const proxy = proxies.find(p => p.proxyId === value)
        return <span className="text-xs">{proxy ? proxy.proxyName : value || '-'}</span>
      },
    },
    {
      key: 'launchCode',
      title: '快捷打开码',
      render: (value, record) => <LaunchCodeCell profileId={record.profileId} code={value || ''} onRefresh={loadProfiles} />,
    },
    {
      key: 'keywords',
      title: '关键字',
      width: 200,
      render: (value) => <KeywordInlineRow keywords={value || []} />,
    },
    {
      key: 'updatedAt',
      title: '上次更新',
      render: formatTime,
    },
    {
      key: 'actions',
      title: '操作',
      align: 'right',
      render: (_, record) => {
        const isStarting = isProfileStarting(record.profileId)
        const isStopping = isProfileStopping(record.profileId)
        const isBusy = isProfileBusy(record.profileId)

        return (
          <div className="flex justify-end gap-1">
            {record.running ? (
              <Button size="sm" variant="secondary" onClick={() => handleStop(record.profileId)} title="停止" loading={isStopping}>
                {!isStopping && <Square className="w-3.5 h-3.5" />}
              </Button>
            ) : (
              <Button size="sm" onClick={() => handleStart(record.profileId)} title="启动" loading={isStarting}>
                {!isStarting && <Play className="w-3.5 h-3.5 fill-current" />}
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => handleRestart(record.profileId)} title="重启" disabled={isBusy}><RotateCcw className="w-3.5 h-3.5" /></Button>
            <Button size="sm" variant="ghost" onClick={() => openKwModal(record)} title="关键字" disabled={isBusy}><Key className="w-3.5 h-3.5" /></Button>
            <Link to={`/browser/edit/${record.profileId}`}><Button size="sm" variant="ghost" title="配置" disabled={isBusy}><Settings className="w-3.5 h-3.5" /></Button></Link>
            <Button size="sm" variant="ghost" onClick={() => handleRandomizeFingerprint(record.profileId)} title="随机指纹种子（每个实例独立隔离）" disabled={isBusy || record.running}><Wand2 className="w-3.5 h-3.5" /></Button>
            <Button size="sm" variant="ghost" onClick={() => openCopyModal(record)} title="克隆" disabled={isBusy}><Copy className="w-3.5 h-3.5" /></Button>
            <Button size="sm" variant="ghost" onClick={() => handleDelete(record.profileId)} title="删除" disabled={isBusy}><Trash2 className="w-3.5 h-3.5 text-red-500" /></Button>
          </div>
        )
      },
    },
  ]


  const coreColumns: TableColumn<BrowserCore>[] = [
    { key: 'coreName', title: '名称' },
    { key: 'corePath', title: '路径' },
    {
      key: 'isDefault',
      title: '默认',
      render: (value) => value ? <Star className="w-4 h-4 text-yellow-500 fill-yellow-500" /> : null,
    },
    {
      key: 'actions',
      title: '操作',
      align: 'right',
      render: (_, record) => (
        <div className="flex justify-end gap-1">
          {!record.isDefault && (
            <Button size="sm" variant="ghost" onClick={() => handleSetDefaultCore(record.coreId)} title="设为默认"><Star className="w-4 h-4" /></Button>
          )}
          <Button size="sm" variant="ghost" onClick={() => handleOpenCoreModal(record)} title="编辑"><Edit2 className="w-4 h-4" /></Button>
          <Button size="sm" variant="ghost" onClick={() => handleDeleteCore(record.coreId)} title="删除"><Trash2 className="w-4 h-4" /></Button>
        </div>
      ),
    },
  ]

  return (
    <div className="overflow-auto p-5 space-y-5 animate-fade-in h-full">
      {/* 页头 */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">实例列表</h1>
          <p className="text-sm text-[var(--color-text-muted)] mt-1">
            当前配置总数 {profiles.length}
            {filteredProfiles.length !== profiles.length && <span className="ml-1 text-[var(--color-accent)]">（已筛选 {filteredProfiles.length}）</span>}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" onClick={() => setHeaderCollapsed(prev => !prev)}>{headerCollapsed ? <ChevronRight className="w-4 h-4" /> : <ChevronUp className="w-4 h-4" />}{headerCollapsed ? '展开面板' : '收起面板'}</Button>
          <Button variant="secondary" size="sm" onClick={() => { void loadProfiles() }}><RefreshCw className="w-4 h-4" />刷新</Button>
          <Button variant="secondary" size="sm" onClick={handleImportMoreLogin} title="导入 MoreLogin 导出的 xlsx / txt 环境文件"><Upload className="w-4 h-4" />导入环境</Button>
          <Button variant="secondary" size="sm" onClick={handleImportRunningMoreLogin} title="导入当前正在运行的 MoreLogin 环境（当前先支持一次导入 1 个）"><FolderInput className="w-4 h-4" />导入运行中环境</Button>
          <Button variant="secondary" size="sm" onClick={handleOpenExtensionModal} disabled={selectedIds.size === 0} title="先勾选实例，再输入扩展下载地址"><Download className="w-4 h-4" />导入扩展</Button>
          <Button variant="secondary" size="sm" onClick={handleOpenSettings}><Sliders className="w-4 h-4" />基础配置</Button>
          <div className="flex items-center bg-[var(--color-bg-secondary)] rounded-md border border-[var(--color-border-default)] p-0.5 ml-2">
            <button
              className={`p-1.5 rounded text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)] transition-colors ${viewMode === 'card' ? 'bg-[var(--color-bg-surface)] shadow-sm text-[var(--color-accent)]' : ''}`}
              onClick={() => setViewMode('card')}
              title="卡片视图"
            >
              <LayoutGrid className="w-4 h-4" />
            </button>
            <button
              className={`p-1.5 rounded text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)] transition-colors ${viewMode === 'table' ? 'bg-[var(--color-bg-surface)] shadow-sm text-[var(--color-accent)]' : ''}`}
              onClick={() => setViewMode('table')}
              title="表格视图"
            >
              <List className="w-4 h-4" />
            </button>
          </div>
          <span className="w-px h-4 bg-[var(--color-border-muted)] mx-1 self-center"></span>
          <Link to="/browser/edit/new"><Button size="sm"><Plus className="w-4 h-4" />新建配置</Button></Link>
          <Link to="/browser/batch-create"><Button variant="secondary" size="sm"><Layers className="w-4 h-4" />批量创建</Button></Link>
        </div>
      </div>

      {/* 可折叠的统计+筛选区 */}
      {!headerCollapsed && (
        <>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <StatCard title="配置总数" value={`${profiles.length}`} icon={<FileText className="w-5 h-5" />} />
            <StatCard title="运行中实例" value={`${runningCount}`} icon={<Activity className="w-5 h-5" />} />
            <StatCard title="停止实例" value={`${profiles.length - runningCount}`} icon={<Square className="w-5 h-5 text-gray-400" />} />
          </div>

          <InstanceFilterBar
            filters={filters}
            onChange={setFilters}
            proxies={proxies}
            cores={cores}
            allTags={allTags}
            groups={groups}
            onCreateGroup={openCreateGroupModal}
            onRenameGroup={openRenameGroupModal}
            onDeleteGroup={handleDeleteGroup}
          />
        </>
      )}

      {/* 批量操作工具栏 */}
      <BatchToolbar
        selectedCount={visibleSelectedIds.size}
        totalCount={filteredProfiles.length}
        onSelectAll={handleSelectAll}
        onDeselectAll={handleDeselectAll}
        onBatchStart={handleBatchStart}
        onBatchStop={handleBatchStop}
        onBatchDelete={handleBatchDelete}
        onBatchMoveToGroup={handleBatchMoveToGroup}
        onBatchRandomizeFingerprint={handleBatchRandomizeFingerprint}
        groups={groups}
        batchLoading={batchLoading}
      />

      <Card padding="none">
        <div className="overflow-auto" style={{ maxHeight: 'calc(100vh - 320px)' }}>
          {/* Replace table with Flex column of Cards */}
          {loading ? (
            <div className="py-16 flex items-center justify-center text-sm text-[var(--color-text-muted)]">加载中...</div>
          ) : filteredProfiles.length === 0 ? (
            <div className="py-16 flex flex-col items-center justify-center gap-3 text-sm text-[var(--color-text-muted)]">
              <div>{hasActiveFilters && profiles.length > 0 ? `当前筛选条件下暂无数据（总实例 ${profiles.length}）` : '暂无数据'}</div>
              {hasActiveFilters && profiles.length > 0 && (
                <Button size="sm" variant="secondary" onClick={() => setFilters({ ...EMPTY_FILTERS, tags: new Set() })}>
                  清除筛选
                </Button>
              )}
            </div>
          ) : viewMode === 'table' ? (
            <Table
              columns={columns}
              data={filteredProfiles}
              rowKey="profileId"
            />
          ) : (
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 min-h-[500px] p-4 items-start content-start">
              {filteredProfiles.map((record) => {
                const isSelected = selectedIds.has(record.profileId)
                const core = resolveProfileCore(record)
                const proxy = proxies.find(p => p.proxyId === record.proxyId)
                const status = getProfileStatus(record)
                const isStarting = isProfileStarting(record.profileId)
                const isStopping = isProfileStopping(record.profileId)
                const isBusy = isProfileBusy(record.profileId)

                return (
                  <div
                    key={record.profileId}
                    className={`flex flex-col border rounded-xl bg-[var(--color-bg-surface)] p-3 shadow-[0_1px_4px_rgba(0,0,0,0.08)] transition-all duration-200 h-[320px] overflow-hidden
                        ${isSelected ? 'border-[var(--color-accent)] ring-1 ring-[var(--color-accent)]/20' : 'border-[var(--color-border-default)] hover:border-[var(--color-accent)]'}
                      `}
                  >
                    {/* Header Row: Title, Status, Checkbox, Actions */}
                    <div className="flex flex-col gap-3 pb-3 border-b border-[var(--color-border-muted)]/50 shrink-0">

                      <div className="flex justify-between items-start gap-2">
                        <div className="flex items-center gap-2 flex-wrap">
                          <input
                            type="checkbox"
                            className="w-4 h-4 rounded cursor-pointer accent-[var(--color-accent)] mt-0.5 shrink-0"
                            checked={isSelected}
                            onChange={() => toggleSelect(record.profileId)}
                          />
                          <Link className="text-[var(--color-accent)] font-medium text-sm hover:text-[var(--color-accent)] transition-colors truncate max-w-[200px]" to={`/browser/detail/${record.profileId}`}>
                            {record.profileName}
                          </Link>
                          {record.tags && record.tags.length > 0 && (
                            <div className="flex gap-1 ml-1">
                              {record.tags.map(tag => <Badge variant="default" key={tag}>{tag}</Badge>)}
                            </div>
                          )}
                        </div>

                        <Badge variant={status.variant} dot dotClassName="w-2 h-2 shrink-0">
                          {status.label}
                        </Badge>
                      </div>

                      <div className="flex items-center gap-1 flex-wrap">
                        {record.running ? (
                          <Button size="sm" variant="secondary" onClick={() => handleStop(record.profileId)} title={isStopping ? '停止中' : '停止'} loading={isStopping}>
                            {!isStopping && <Square className="w-4 h-4 mr-1.5" />}
                            {isStopping ? '停止中' : '停止'}
                          </Button>
                        ) : (
                          <Button size="sm" onClick={() => handleStart(record.profileId)} title={isStarting ? '启动中' : '启动'} loading={isStarting}>
                            {!isStarting && <Play className="w-4 h-4 fill-current mr-1.5" />}
                            {isStarting ? '启动中' : '启动'}
                          </Button>
                        )}
                        <span className="w-px h-4 bg-[var(--color-border-muted)] mx-1"></span>
                        <Button size="sm" variant="ghost" onClick={() => handleRestart(record.profileId)} title="重启" className="px-3" disabled={isBusy}><RotateCcw className="w-4 h-4 mr-1.5" />重启</Button>
                        <Button size="sm" variant="ghost" onClick={() => openKwModal(record)} title="关键字管理" className="px-3" disabled={isBusy}><Key className="w-4 h-4 mr-1.5" />关键字</Button>
                        <Link to={`/browser/edit/${record.profileId}`}><Button size="sm" variant="ghost" title="配置" className="px-3" disabled={isBusy}><Settings className="w-4 h-4 mr-1.5" />配置</Button></Link>
                        <Button size="sm" variant="ghost" onClick={() => openCopyModal(record)} title="克隆" className="px-3" disabled={isBusy}><Copy className="w-4 h-4 mr-1.5" />克隆</Button>
                        <Button size="sm" variant="ghost" onClick={() => handleDelete(record.profileId)} title="删除" className="px-3 text-red-500 hover:text-red-600 hover:bg-red-50" disabled={isBusy}><Trash2 className="w-4 h-4 mr-1.5" />删除</Button>
                      </div>
                    </div>

                    {/* Body Grid: Key-Value Pairs */}
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-4 py-2 shrink-0">
                      <div className="flex flex-col gap-0.5">
                        <span className="text-xs text-[var(--color-text-muted)] font-medium">内核版本</span>
                        <span className="text-xs text-[var(--color-text-primary)]">{core?.coreName || getProfileCoreLabel(record)}</span>
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <span className="text-xs text-[var(--color-text-muted)] font-medium">代理配置</span>
                        <span className="text-xs text-[var(--color-text-primary)]">{proxy?.proxyName || record.proxyId || '-'}</span>
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <span className="text-xs text-[var(--color-text-muted)] font-medium">快捷配置码</span>
                        <div className="mt-0.5"><LaunchCodeCell profileId={record.profileId} code={record.launchCode || ''} onRefresh={loadProfiles} /></div>
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <span className="text-xs text-[var(--color-text-muted)] font-medium">上次更新时间</span>
                        <span className="text-xs text-[var(--color-text-primary)]">{formatTime(record.updatedAt)}</span>
                      </div>
                    </div>

                    {/* Footer: Keywords */}
                    <div className="border-t border-[var(--color-border-muted)]/50 pt-2 flex items-start gap-2 flex-1 min-h-0">
                      <span className="text-xs font-medium text-[var(--color-text-primary)] shrink-0 pt-0.5">系统关键字</span>
                      <div className="flex-1 min-h-0 overflow-y-auto pr-1">
                        <KeywordInlineRow keywords={record.keywords || []} />
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </Card>

      {/* 删除实例弹窗 */}
      <Modal
        open={deleteModal.open}
        onClose={closeDeleteModal}
        title={deleteModal.ids.length > 1 ? '批量删除实例' : '删除实例'}
        width="480px"
        closable={!deletingProfiles}
        footer={
          <>
            <Button variant="secondary" onClick={closeDeleteModal} disabled={deletingProfiles}>取消</Button>
            <Button variant="danger" onClick={confirmDeleteProfiles} loading={deletingProfiles}>确认删除</Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="rounded-lg border border-red-500/20 bg-red-500/5 p-3 text-sm text-[var(--color-text-secondary)]">
            <p className="font-medium text-red-500 mb-1">
              确定删除 {deleteModal.ids.length} 个实例配置吗？
            </p>
            <p>默认只删除列表里的配置记录，不删除浏览器缓存、插件、Cookie、书签等本地数据。</p>
          </div>
          {deleteModal.names.length > 0 && (
            <div className="max-h-28 overflow-y-auto rounded-lg border border-[var(--color-border)] bg-[var(--color-bg-secondary)] p-2 text-xs text-[var(--color-text-muted)]">
              {deleteModal.names.slice(0, 8).map(name => <div key={name} className="truncate">{name}</div>)}
              {deleteModal.names.length > 8 && <div>另有 {deleteModal.names.length - 8} 个实例...</div>}
            </div>
          )}
          <label className="flex items-start gap-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg-secondary)] p-3 cursor-pointer">
            <input
              type="checkbox"
              className="mt-1"
              checked={deleteCache}
              onChange={e => setDeleteCache(e.target.checked)}
              disabled={deletingProfiles}
            />
            <span className="text-sm">
              <span className="block font-medium text-[var(--color-text-primary)]">同时删除缓存文件/用户数据目录</span>
              <span className="block mt-1 text-xs leading-5 text-[var(--color-text-muted)]">
                勾选后会删除该实例的数据文件夹，包括浏览器缓存、Cookie、已安装插件、书签等；未勾选则保留这些文件，后续仍可手动备份或清理。
              </span>
            </span>
          </label>
        </div>
      </Modal>

      {/* 扩展导入弹窗 */}
      <Modal open={extensionModalOpen} onClose={() => !importingExtension && setExtensionModalOpen(false)} title="导入扩展程序" width="640px"
        footer={
          <>
            <Button variant="secondary" onClick={() => setExtensionModalOpen(false)} disabled={importingExtension}>取消</Button>
            <Button onClick={handleImportExtension} loading={importingExtension}>下载并导入</Button>
          </>
        }>
        <div className="space-y-4">
          <div className="text-sm text-[var(--color-text-secondary)]">
            已选择 <span className="font-semibold text-[var(--color-accent)]">{selectedIds.size}</span> 个实例。导入后会写入这些实例的启动参数，实例需要重启后扩展才会加载。
          </div>
          <FormItem label="扩展程序下载地址" hint="支持 Chrome Web Store 详情页、32 位扩展 ID、直接 .crx/.zip 下载地址">
            <Textarea
              value={extensionUrl}
              onChange={e => setExtensionUrl(e.target.value)}
              rows={4}
              placeholder="例如：https://chromewebstore.google.com/detail/.../扩展ID 或 https://.../extension.crx"
            />
          </FormItem>
          <div className="rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-secondary)] p-3 text-xs leading-5 text-[var(--color-text-muted)]">
            会自动下载扩展、解包到本程序 extensions/imported 目录，并合并到 <code>--load-extension</code>，不会切换官方 Chrome，不影响原指纹核心。
          </div>
        </div>
      </Modal>

      {/* 基础配置弹窗 */}
      <Modal open={settingsModalOpen} onClose={() => setSettingsModalOpen(false)} title="基础配置" width="700px"
        footer={<><Button variant="secondary" onClick={() => setSettingsModalOpen(false)}>取消</Button><Button onClick={handleSaveSettings} loading={savingSettings}>保存</Button></>}>
        <div className="space-y-6">
          {/* 内核管理 */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <span className="text-sm font-medium text-[var(--color-text-primary)]">内核管理</span>
              <div className="flex gap-2">
                <Button size="sm" onClick={() => handleOpenCoreModal()}><Plus className="w-4 h-4" />新增内核</Button>
              </div>
            </div>
            <Card padding="none">
              <Table columns={coreColumns} data={cores} rowKey="coreId" />
            </Card>
          </div>

          {/* 其他设置 */}
          <FormItem label="用户数据根目录">
            <Input value={settings.userDataRoot} onChange={e => setSettings(prev => ({ ...prev, userDataRoot: e.target.value }))} placeholder="data" />
          </FormItem>
          <FormItem label="默认指纹参数（每行一个）">
            <Textarea value={fingerprintText} onChange={e => setFingerprintText(e.target.value)} rows={3} placeholder="--fingerprint-brand=Chrome" />
          </FormItem>
          <FormItem label="默认启动参数（每行一个）">
            <Textarea value={launchText} onChange={e => setLaunchText(e.target.value)} rows={3} placeholder="--disable-sync" />
          </FormItem>
          <FormItem label="默认代理">
            <Input value={settings.defaultProxy} onChange={e => setSettings(prev => ({ ...prev, defaultProxy: e.target.value }))} placeholder="http://127.0.0.1:7890" />
          </FormItem>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormItem label="启动就绪超时（毫秒）" hint="默认 3000，慢机器可调到 5000-10000">
              <Input
                type="number"
                min={1000}
                step={500}
                value={settings.startReadyTimeoutMs}
                onChange={e => setSettings(prev => ({ ...prev, startReadyTimeoutMs: Math.max(1000, Number(e.target.value) || 3000) }))}
                placeholder="3000"
              />
            </FormItem>
            <FormItem label="启动稳定窗口（毫秒）" hint="建议 1200-3000">
              <Input
                type="number"
                min={0}
                step={100}
                value={settings.startStableWindowMs}
                onChange={e => setSettings(prev => ({ ...prev, startStableWindowMs: Math.max(0, Number(e.target.value) || 1200) }))}
                placeholder="1200"
              />
            </FormItem>
          </div>
        </div>
      </Modal>

      {/* 内核编辑弹窗 */}
      <Modal open={coreModalOpen} onClose={() => setCoreModalOpen(false)} title={coreForm.coreId ? '编辑内核' : '新增内核'} width="500px"
        footer={<><Button variant="secondary" onClick={() => setCoreModalOpen(false)}>取消</Button><Button onClick={handleSaveCore} loading={savingCore}>保存</Button></>}>
        <div className="space-y-4">
          <FormItem label="内核名称" required>
            <Input value={coreForm.coreName} onChange={e => setCoreForm(prev => ({ ...prev, coreName: e.target.value }))} placeholder="Chrome 142" />
          </FormItem>
          <FormItem label="内核路径" required>
            <div className="flex gap-2">
              <Input value={coreForm.corePath} onChange={e => { setCoreForm(prev => ({ ...prev, corePath: e.target.value })); setCoreValidation(null) }} placeholder="chrome 或 D:/browsers/chrome-120" className="flex-1" />
              <Button variant="secondary" onClick={handleValidateCorePath}>验证</Button>
            </div>
            {coreValidation && (
              <div className={`flex items-center gap-1 mt-1 text-sm ${coreValidation.valid ? 'text-green-600' : 'text-red-600'}`}>
                {coreValidation.valid ? <CheckCircle className="w-4 h-4" /> : <XCircle className="w-4 h-4" />}
                {coreValidation.message}
              </div>
            )}
          </FormItem>
        </div>
      </Modal>

      {/* 代理不支持弹窗 */}
      <Modal
        open={proxyErrorModal}
        onClose={() => { setProxyErrorModal(false); setPendingStartId(null) }}
        title="代理链路不可用"
        width="420px"
        footer={
          <>
            <Button variant="secondary" onClick={() => { setProxyErrorModal(false); setPendingStartId(null) }}>取消</Button>
            {pendingStartId && (
              <Link to={`/browser/edit/${pendingStartId}`}>
                <Button onClick={() => setProxyErrorModal(false)}>去修改代理</Button>
              </Link>
            )}
          </>
        }
      >
        <div className="space-y-3">
          <div className="flex items-start gap-3 p-3 rounded-lg bg-[var(--color-bg-secondary)]">
            <XCircle className="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
            <p className="text-sm text-[var(--color-text-primary)]">{proxyErrorMsg}</p>
          </div>
          <p className="text-sm text-[var(--color-text-muted)]">请前往编辑页面重新选择可用链路；如果是订阅导入，先刷新订阅并确认该节点仍存在。</p>
        </div>
      </Modal>

      {/* 关键字弹窗 */}
      {kwModal.profile && (
        <KeywordsModal
          open={kwModal.open}
          profileId={kwModal.profile.profileId}
          profileName={kwModal.profile.profileName}
          initialKeywords={kwModal.profile.keywords || []}
          onClose={closeKwModal}
          onSaved={(keywords) => {
            updateProfilesState(prev => prev.map(p =>
              p.profileId === kwModal.profile!.profileId ? { ...p, keywords } : p
            ))
          }}
        />
      )}

      {/* 复制实例弹窗 */}
      <Modal
        open={copyModal.open}
        onClose={closeCopyModal}
        title="复制实例"
        width="420px"
        footer={
          <>
            <Button variant="secondary" onClick={closeCopyModal}>取消</Button>
            <Button onClick={() => copyModal.profile && handleCopy(copyModal.profile.profileId)} loading={copying}>确认复制</Button>
          </>
        }
      >
        <div className="space-y-4">
          <p className="text-sm text-[var(--color-text-muted)]">
            复制实例将保留原有的代理、内核、启动参数、标签等配置，但会生成新的指纹种子。
          </p>
          <FormItem label="新实例名称" required>
            <Input
              value={copyName}
              onChange={e => setCopyName(e.target.value)}
              placeholder="请输入新实例名称"
              autoFocus
            />
          </FormItem>
        </div>
      </Modal>

      {/* 操作错误弹窗 */}
      <Modal
        open={!!opError}
        onClose={() => setOpError('')}
        title="操作失败"
        width="420px"
        footer={<Button onClick={() => setOpError('')}>知道了</Button>}
      >
        <div className="text-[var(--color-text-secondary)] whitespace-pre-line">{opError}</div>
      </Modal>

      {/* 分组新建/重命名弹窗 */}
      <Modal
        open={groupModal.open}
        onClose={closeGroupModal}
        title={groupModal.mode === 'create' ? '新建分组' : '重命名分组'}
        width="420px"
        closable={!groupSubmitting}
        footer={
          <>
            <Button variant="secondary" onClick={closeGroupModal} disabled={groupSubmitting}>取消</Button>
            <Button onClick={submitGroupModal} loading={groupSubmitting}>
              {groupModal.mode === 'create' ? '创建' : '保存'}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <FormItem label="分组名称" required>
            <Input
              value={groupModal.name}
              onChange={e => setGroupModal(prev => ({ ...prev, name: e.target.value }))}
              placeholder="例如：钱包项目 / X 矩阵 / 测试组"
              autoFocus
              onKeyDown={e => { if (e.key === 'Enter') submitGroupModal() }}
            />
          </FormItem>
          {groupModal.mode === 'create' && groups.length > 0 && (
            <FormItem label="父级分组" hint="不选则为根级分组">
              <select
                className="w-full px-3 py-2 text-sm rounded-md border border-[var(--color-border-default)] bg-[var(--color-bg-surface)]"
                value={groupModal.parentId}
                onChange={e => setGroupModal(prev => ({ ...prev, parentId: e.target.value }))}
              >
                <option value="">根级分组</option>
                {groups.map(g => (
                  <option key={g.groupId} value={g.groupId}>{g.groupName}</option>
                ))}
              </select>
            </FormItem>
          )}
        </div>
      </Modal>
    </div>
  )
}
