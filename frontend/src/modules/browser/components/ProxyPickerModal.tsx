import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, Loader2, Search, Users, Wifi, X } from 'lucide-react'
import type { BrowserProxy } from '../types'
import { browserProxyBatchTestSpeed, browserProxyTestSpeed, fetchBrowserProfiles, fetchBrowserProxies, fetchBrowserProxyGroups } from '../api'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

interface ProxyPickerModalProps {
  open: boolean
  currentProxyId: string
  /** 当前正在编辑的实例ID，统计占用时会被排除（避免显示"自己用了自己"的噪音） */
  currentProfileId?: string
  onSelect: (proxy: BrowserProxy) => void
  onClose: () => void
}

type SpeedResult = { ok: boolean; latencyMs: number; error: string }

const ALL_GROUP = '__all__'
const BATCH_TEST_CONCURRENCY = 8

export function ProxyPickerModal({ open, currentProxyId, currentProfileId, onSelect, onClose }: ProxyPickerModalProps) {
  const [groups, setGroups] = useState<string[]>([])
  const [allProxies, setAllProxies] = useState<BrowserProxy[]>([])
  const [displayProxies, setDisplayProxies] = useState<BrowserProxy[]>([])
  const [selectedGroup, setSelectedGroup] = useState<string>(ALL_GROUP)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  // proxyId -> 占用它的实例名字列表（不含当前正在编辑的实例）
  const [usageMap, setUsageMap] = useState<Record<string, string[]>>({})
  // proxyId -> speed result
  const [speedMap, setSpeedMap] = useState<Record<string, SpeedResult>>({})
  const [testingIds, setTestingIds] = useState<Set<string>>(new Set())
  const abortRef = useRef(false)

  useEffect(() => {
    if (!open) return
    setSelectedGroup(ALL_GROUP)
    setSearch('')
    setSpeedMap({})
    setTestingIds(new Set())
    abortRef.current = false
    loadData()
    return () => { abortRef.current = true }
  }, [open])

  const loadData = async () => {
    setLoading(true)
    try {
      const [groupList, proxyList, profileList] = await Promise.all([
        fetchBrowserProxyGroups(),
        fetchBrowserProxies(),
        fetchBrowserProfiles(),
      ])
      setGroups(groupList)
      setAllProxies(proxyList)
      // 从代理数据初始化已有测速结果
      const initMap: Record<string, SpeedResult> = {}
      proxyList.forEach(p => {
        if (p.lastTestedAt) {
          initMap[p.proxyId] = { ok: p.lastTestOk ?? false, latencyMs: p.lastLatencyMs ?? -1, error: '' }
        }
      })
      setSpeedMap(initMap)
      // 统计代理占用情况：proxyId -> [实例名]
      const usage: Record<string, string[]> = {}
      profileList.forEach(profile => {
        if (!profile.proxyId) return
        if (currentProfileId && profile.profileId === currentProfileId) return
        if (!usage[profile.proxyId]) usage[profile.proxyId] = []
        usage[profile.proxyId].push(profile.profileName || profile.profileId)
      })
      setUsageMap(usage)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    let list = allProxies
    if (selectedGroup !== ALL_GROUP) {
      list = list.filter(p => p.groupName === selectedGroup)
    }
    if (search.trim()) {
      const q = search.trim().toLowerCase()
      list = list.filter(p =>
        (p.proxyName || '').toLowerCase().includes(q) ||
        (p.proxyConfig || '').toLowerCase().includes(q)
      )
    }

    const getSortTuple = (proxy: BrowserProxy): [number, number, string] => {
      const latest = speedMap[proxy.proxyId]
      const fromHistory = proxy.lastTestedAt
        ? { ok: proxy.lastTestOk ?? false, latencyMs: proxy.lastLatencyMs ?? -1 }
        : undefined
      const result = latest || fromHistory

      if (result?.ok && result.latencyMs >= 0) {
        return [0, result.latencyMs, proxy.proxyName || '']
      }
      if (proxy.proxyConfig === 'direct://') {
        return [2, Number.MAX_SAFE_INTEGER, proxy.proxyName || '']
      }
      if (result && !result.ok) {
        return [3, Number.MAX_SAFE_INTEGER, proxy.proxyName || '']
      }
      return [4, Number.MAX_SAFE_INTEGER, proxy.proxyName || '']
    }

    list = [...list].sort((a, b) => {
      const [rankA, latencyA, nameA] = getSortTuple(a)
      const [rankB, latencyB, nameB] = getSortTuple(b)
      if (rankA !== rankB) return rankA - rankB
      if (latencyA !== latencyB) return latencyA - latencyB
      return nameA.localeCompare(nameB, 'zh-CN')
    })

    setDisplayProxies(list)
  }, [selectedGroup, search, allProxies, speedMap])

  const testOne = async (proxyId: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (testingIds.has(proxyId)) return
    setTestingIds(prev => new Set(prev).add(proxyId))
    try {
      const result = await browserProxyTestSpeed(proxyId)
      if (!abortRef.current) {
        setSpeedMap(prev => ({ ...prev, [proxyId]: { ok: result.ok, latencyMs: result.latencyMs, error: result.error } }))
      }
    } finally {
      setTestingIds(prev => { const s = new Set(prev); s.delete(proxyId); return s })
    }
  }

  const testAll = async () => {
    const ids = displayProxies.map(p => p.proxyId).filter(id => id !== '__direct__')
    if (ids.length === 0) return
    abortRef.current = false
    setTestingIds(new Set(ids))
    const idSet = new Set(ids)
    const off = EventsOn('proxy:speed:result', (data: { proxyId: string; ok: boolean; latencyMs: number; error: string }) => {
      if (abortRef.current || !idSet.has(data.proxyId)) return
      setSpeedMap(prev => ({ ...prev, [data.proxyId]: { ok: data.ok, latencyMs: data.latencyMs, error: data.error } }))
      setTestingIds(prev => {
        const next = new Set(prev)
        next.delete(data.proxyId)
        return next
      })
    })
    try {
      const results = await browserProxyBatchTestSpeed(ids, BATCH_TEST_CONCURRENCY)
      if (!abortRef.current) {
        setSpeedMap(prev => {
          const next = { ...prev }
          results.forEach(result => {
            if (idSet.has(result.proxyId)) {
              next[result.proxyId] = { ok: result.ok, latencyMs: result.latencyMs, error: result.error }
            }
          })
          return next
        })
      }
    } finally {
      off()
      setTestingIds(prev => {
        const next = new Set(prev)
        ids.forEach(id => next.delete(id))
        return next
      })
    }
  }

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center" onClick={onClose}>
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" />
      <div
        className="relative bg-[var(--color-bg-elevated)] border border-[var(--color-border)] rounded-xl shadow-2xl w-[720px] max-h-[580px] flex flex-col"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-[var(--color-border)]">
          <span className="font-semibold text-[var(--color-text-primary)]">从代理池选择</span>
          <button onClick={onClose} className="text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)] transition-colors">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="flex flex-1 min-h-0">
          {/* Left: group list */}
          <div className="w-44 border-r border-[var(--color-border)] flex flex-col py-2 overflow-y-auto shrink-0 bg-[var(--color-bg-muted)]">
            <GroupItem label="全部" active={selectedGroup === ALL_GROUP} count={allProxies.length} onClick={() => setSelectedGroup(ALL_GROUP)} />
            {groups.map(g => (
              <GroupItem key={g} label={g} active={selectedGroup === g}
                count={allProxies.filter(p => p.groupName === g).length}
                onClick={() => setSelectedGroup(g)} />
            ))}
            {groups.length === 0 && <p className="text-xs text-[var(--color-text-muted)] px-3 py-2">暂无分组</p>}
          </div>

          {/* Right: proxy list */}
          <div className="flex-1 flex flex-col min-h-0 overflow-hidden">
            {/* Search + test all */}
            <div className="px-3 py-2 border-b border-[var(--color-border)] flex gap-2 items-center">
              <div className="relative flex-1">
                <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                <input
                  type="text"
                  value={search}
                  onChange={e => setSearch(e.target.value)}
                  placeholder="搜索代理名称或配置..."
                  className="w-full pl-8 pr-3 py-1.5 text-sm bg-[var(--color-bg-input)] border border-[var(--color-border)] rounded-lg text-[var(--color-text-primary)] placeholder-[var(--color-text-muted)] focus:outline-none focus:border-[var(--color-primary)]"
                />
              </div>
              <button
                onClick={testAll}
                disabled={testingIds.size > 0 || displayProxies.length === 0}
                className="shrink-0 flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border border-[var(--color-border)] text-[var(--color-text-secondary)] hover:text-[var(--color-primary)] hover:border-[var(--color-primary)] disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              >
                <Wifi className="w-3.5 h-3.5" />
                全部测速
              </button>
            </div>

            {/* List */}
            <div className="flex-1 overflow-y-auto">
              {loading ? (
                <div className="flex items-center justify-center h-24 text-sm text-[var(--color-text-muted)]">加载中...</div>
              ) : displayProxies.length === 0 ? (
                <div className="flex items-center justify-center h-24 text-sm text-[var(--color-text-muted)]">暂无代理</div>
              ) : (
                displayProxies.map(proxy => (
                  <ProxyRow
                    key={proxy.proxyId}
                    proxy={proxy}
                    selected={proxy.proxyId === currentProxyId}
                    testing={testingIds.has(proxy.proxyId)}
                    speedResult={speedMap[proxy.proxyId]}
                    usedBy={usageMap[proxy.proxyId]}
                    onSelect={() => { onSelect(proxy); onClose() }}
                    onTest={e => testOne(proxy.proxyId, e)}
                  />
                ))
              )}
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="px-5 py-3 border-t border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
          共 {displayProxies.length} 条，点击行即选中
        </div>
      </div>
    </div>,
    document.body
  )
}

function GroupItem({ label, active, count, onClick }: { label: string; active: boolean; count: number; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-3 py-2 text-sm flex items-center justify-between gap-2 transition-colors ${
        active
          ? 'bg-[var(--color-primary)]/10 text-[var(--color-primary)] font-medium'
          : 'text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-hover)]'
      }`}
    >
      <span className="truncate">{label}</span>
      <span className="text-xs opacity-60 shrink-0">{count}</span>
    </button>
  )
}

interface ProxyRowProps {
  proxy: BrowserProxy
  selected: boolean
  testing: boolean
  speedResult?: SpeedResult
  usedBy?: string[]
  onSelect: () => void
  onTest: (e: React.MouseEvent) => void
}

function SpeedBadge({ testing, result }: { testing: boolean; result?: SpeedResult }) {
  if (testing) return <Loader2 className="w-3.5 h-3.5 animate-spin text-[var(--color-text-muted)] shrink-0" />
  if (!result) return null
  if (!result.ok) return <span className="text-xs text-red-500 shrink-0">失败</span>
  const color = result.latencyMs < 200 ? 'text-green-500' : result.latencyMs < 500 ? 'text-yellow-500' : 'text-red-500'
  return <span className={`text-xs font-medium shrink-0 ${color}`}>{result.latencyMs}ms</span>
}

function UsageBadge({ usedBy }: { usedBy?: string[] }) {
  if (!usedBy || usedBy.length === 0) return null
  const preview = usedBy.slice(0, 3).join('、')
  const rest = usedBy.length > 3 ? `…等${usedBy.length}个` : ''
  const title = `已被以下实例使用：\n${usedBy.join('\n')}`
  return (
    <span
      title={title}
      className="shrink-0 inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-500/10 text-amber-600 border border-amber-500/30"
    >
      <Users className="w-3 h-3" />
      <span className="max-w-[180px] truncate">{preview}{rest}</span>
    </span>
  )
}

function ProxyRow({ proxy, selected, testing, speedResult, usedBy, onSelect, onTest }: ProxyRowProps) {
  return (
    <div
      onClick={onSelect}
      className={`w-full px-4 py-2.5 flex items-center gap-3 cursor-pointer transition-colors border-b border-[var(--color-border)]/40 last:border-0 overflow-hidden ${
        selected ? 'bg-[var(--color-primary)]/10' : 'hover:bg-[var(--color-bg-hover)]'
      }`}
    >
      <div className="flex-1 min-w-0 overflow-hidden">
        <div className="text-sm font-medium text-[var(--color-text-primary)] truncate flex items-center gap-2">
          <span className="truncate">{proxy.proxyName || proxy.proxyId}</span>
          {proxy.groupName && <span className="text-xs text-[var(--color-primary)]/70 font-normal shrink-0">[{proxy.groupName}]</span>}
          <UsageBadge usedBy={usedBy} />
        </div>
        <div className="text-xs text-[var(--color-text-muted)] truncate mt-0.5 w-0 min-w-full">
          {proxy.proxyConfig}
        </div>
      </div>
      <SpeedBadge testing={testing} result={speedResult} />
      <button
        onClick={onTest}
        disabled={testing}
        title="测速"
        className="shrink-0 p-1 rounded text-[var(--color-text-muted)] hover:text-[var(--color-primary)] hover:bg-[var(--color-primary)]/10 disabled:opacity-40 transition-colors"
      >
        <Wifi className="w-3.5 h-3.5" />
      </button>
      {selected && <Check className="w-4 h-4 text-[var(--color-primary)] shrink-0" />}
    </div>
  )
}
