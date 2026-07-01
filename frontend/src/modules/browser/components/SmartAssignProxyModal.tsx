import { useEffect, useMemo, useState } from 'react'
import { Button, FormItem, Input, Modal, Select, Switch, toast } from '../../../shared/components'
import { Loader2 } from 'lucide-react'
import type { BrowserProfile, BrowserProxy, BrowserGroup } from '../types'
import { fetchBrowserProfiles, fetchGroups, updateBrowserProfile } from '../api'

const ALL_GROUPS = '__all__'
const NO_GROUP = '__none__'

// 代理范围
type ProxyScope = 'all' | 'filtered' | 'selected'
// 实例范围
type ProfileScope = 'all' | 'group' | 'unassigned'

interface SmartAssignProxyModalProps {
  open: boolean
  /** 全部可用代理（已经过过滤掉内置 direct/local 之外的，按调用方决定） */
  allProxies: BrowserProxy[]
  /** 当前页面筛选后的代理（用户在代理池表格里筛出来的那批） */
  filteredProxies: BrowserProxy[]
  /** 已勾选的代理 ID */
  selectedProxyIds: Set<string>
  onClose: () => void
  /** 分配完成后通知调用方刷新（可选） */
  onDone?: () => void
}

interface AssignmentRow {
  profile: BrowserProfile
  proxy: BrowserProxy
}

export function SmartAssignProxyModal({
  open,
  allProxies,
  filteredProxies,
  selectedProxyIds,
  onClose,
  onDone,
}: SmartAssignProxyModalProps) {
  const [perProxyCount, setPerProxyCount] = useState('1')
  const [proxyScope, setProxyScope] = useState<ProxyScope>('all')
  const [profileScope, setProfileScope] = useState<ProfileScope>('all')
  const [groupId, setGroupId] = useState<string>(ALL_GROUPS)
  const [skipAlreadyAssigned, setSkipAlreadyAssigned] = useState(false)
  const [includeBuiltin, setIncludeBuiltin] = useState(false)

  const [profiles, setProfiles] = useState<BrowserProfile[]>([])
  const [groups, setGroups] = useState<BrowserGroup[]>([])
  const [loading, setLoading] = useState(false)
  const [running, setRunning] = useState(false)
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)

  // 打开时拉数据
  useEffect(() => {
    if (!open) return
    setProgress(null)
    void (async () => {
      setLoading(true)
      try {
        const [profileList, groupList] = await Promise.all([fetchBrowserProfiles(), fetchGroups()])
        setProfiles(profileList)
        setGroups(groupList)
      } finally {
        setLoading(false)
      }
    })()
  }, [open])

  // 候选代理（按当前选择的范围决定，并按是否包含内置代理过滤）
  const candidateProxies = useMemo<BrowserProxy[]>(() => {
    let pool: BrowserProxy[] = []
    if (proxyScope === 'all') pool = allProxies
    else if (proxyScope === 'filtered') pool = filteredProxies
    else pool = allProxies.filter(p => selectedProxyIds.has(p.proxyId))

    if (!includeBuiltin) {
      pool = pool.filter(p => p.proxyId !== '__direct__' && p.proxyId !== '__local__')
    }
    // 保持调用方传入的顺序（代理池表格当前展示顺序），更符合"顺序分配"语义
    return pool
  }, [proxyScope, allProxies, filteredProxies, selectedProxyIds, includeBuiltin])

  // 候选实例（按分组等条件筛选；按 profileName 字典序稳定排序）
  const candidateProfiles = useMemo<BrowserProfile[]>(() => {
    let pool = profiles
    if (profileScope === 'group') {
      if (groupId === NO_GROUP) {
        pool = pool.filter(p => !p.groupId)
      } else if (groupId !== ALL_GROUPS) {
        pool = pool.filter(p => p.groupId === groupId)
      }
    } else if (profileScope === 'unassigned') {
      pool = pool.filter(p => !p.proxyId || p.proxyId === '__direct__')
    }
    if (skipAlreadyAssigned) {
      pool = pool.filter(p => !p.proxyId || p.proxyId === '__direct__')
    }
    return [...pool].sort((a, b) =>
      (a.profileName || a.profileId).localeCompare(b.profileName || b.profileId, 'zh-Hans-CN', { numeric: true })
    )
  }, [profiles, profileScope, groupId, skipAlreadyAssigned])

  // 分配预览
  const assignments = useMemo<AssignmentRow[]>(() => {
    const n = Math.max(1, Number(perProxyCount) || 1)
    const proxies = candidateProxies
    if (proxies.length === 0) return []
    return candidateProfiles.map((profile, idx) => {
      // 顺序填满每个代理 N 个实例后再下一个；超出代理池长度则循环回来
      const proxyIdx = Math.floor(idx / n) % proxies.length
      return { profile, proxy: proxies[proxyIdx] }
    })
  }, [candidateProfiles, candidateProxies, perProxyCount])

  // 每个代理被分到几个实例（统计）
  const distribution = useMemo<Map<string, number>>(() => {
    const m = new Map<string, number>()
    for (const row of assignments) {
      m.set(row.proxy.proxyId, (m.get(row.proxy.proxyId) || 0) + 1)
    }
    return m
  }, [assignments])

  const reset = () => {
    setPerProxyCount('1')
    setProxyScope('all')
    setProfileScope('all')
    setGroupId(ALL_GROUPS)
    setSkipAlreadyAssigned(false)
    setIncludeBuiltin(false)
    setProgress(null)
  }

  const handleClose = () => {
    if (running) return
    reset()
    onClose()
  }

  const handleApply = async () => {
    if (assignments.length === 0) {
      toast.error('没有可分配的实例或代理')
      return
    }
    setRunning(true)
    setProgress({ done: 0, total: assignments.length })
    let okCount = 0
    let failCount = 0
    try {
      for (let i = 0; i < assignments.length; i += 1) {
        const { profile, proxy } = assignments[i]
        try {
          await updateBrowserProfile(profile.profileId, {
            profileName: profile.profileName,
            userDataDir: profile.userDataDir,
            coreId: profile.coreId,
            fingerprintArgs: profile.fingerprintArgs || [],
            proxyId: proxy.proxyId,
            proxyConfig: proxy.proxyConfig,
            launchArgs: profile.launchArgs || [],
            tags: profile.tags || [],
            keywords: profile.keywords || [],
            groupId: profile.groupId || '',
          })
          okCount += 1
        } catch {
          failCount += 1
        }
        setProgress({ done: i + 1, total: assignments.length })
      }
      if (failCount === 0) {
        toast.success(`已为 ${okCount} 个实例分配代理`)
      } else {
        toast.success(`完成：成功 ${okCount} 个，失败 ${failCount} 个`)
      }
      onDone?.()
      reset()
      onClose()
    } finally {
      setRunning(false)
    }
  }

  const groupOptions = useMemo(() => {
    const opts = [
      { value: ALL_GROUPS, label: '全部分组' },
      { value: NO_GROUP, label: '未分组' },
    ]
    for (const g of groups) {
      opts.push({ value: g.groupId, label: g.groupName })
    }
    return opts
  }, [groups])

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="智能分配代理"
      width="720px"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose} disabled={running}>
            取消
          </Button>
          <Button
            onClick={handleApply}
            loading={running}
            disabled={running || loading || assignments.length === 0 || candidateProxies.length === 0}
          >
            {running && progress
              ? `分配中 ${progress.done}/${progress.total}`
              : `应用分配（${assignments.length}）`}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-xs text-[var(--color-text-muted)] bg-[var(--color-bg-secondary)] px-3 py-2 rounded">
          顺序为每个代理分配指定数量的实例。当实例数大于「代理数 × 每代理实例数」时，会从头循环复用代理。
        </p>

        <div className="grid grid-cols-2 gap-4">
          <FormItem label="每个代理分配多少个实例">
            <Input
              type="number"
              min={1}
              max={9999}
              value={perProxyCount}
              onChange={e => setPerProxyCount(e.target.value)}
              disabled={running}
            />
          </FormItem>

          <FormItem label="代理范围">
            <Select
              value={proxyScope}
              onChange={e => setProxyScope(e.target.value as ProxyScope)}
              disabled={running}
              options={[
                { value: 'all', label: `全部代理（${allProxies.length}）` },
                { value: 'filtered', label: `当前筛选后的代理（${filteredProxies.length}）` },
                { value: 'selected', label: `已勾选的代理（${selectedProxyIds.size}）` },
              ]}
            />
          </FormItem>

          <FormItem label="实例范围">
            <Select
              value={profileScope}
              onChange={e => setProfileScope(e.target.value as ProfileScope)}
              disabled={running}
              options={[
                { value: 'all', label: `全部实例（${profiles.length}）` },
                { value: 'group', label: '按分组筛选' },
                { value: 'unassigned', label: '只分配尚未设置代理的实例' },
              ]}
            />
          </FormItem>

          {profileScope === 'group' && (
            <FormItem label="选择分组">
              <Select
                value={groupId}
                onChange={e => setGroupId(e.target.value)}
                disabled={running}
                options={groupOptions}
              />
            </FormItem>
          )}
        </div>

        <div className="flex items-center gap-6 text-sm">
          <label className="flex items-center gap-2 cursor-pointer">
            <Switch checked={skipAlreadyAssigned} onChange={setSkipAlreadyAssigned} />
            <span className="text-[var(--color-text-muted)]">跳过已设置代理的实例</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <Switch checked={includeBuiltin} onChange={setIncludeBuiltin} />
            <span className="text-[var(--color-text-muted)]">包含内置代理（直连/本地）</span>
          </label>
        </div>

        {/* 概要 */}
        <div className="grid grid-cols-3 gap-3 text-sm">
          <SummaryCell label="参与代理" value={candidateProxies.length} />
          <SummaryCell label="参与实例" value={candidateProfiles.length} />
          <SummaryCell
            label="预计分配"
            value={assignments.length}
            tone={assignments.length > 0 ? 'primary' : 'muted'}
          />
        </div>

        {/* 校验提示 */}
        {!loading && candidateProxies.length === 0 && (
          <div className="text-xs text-red-500 bg-red-500/10 border border-red-500/30 rounded px-3 py-2">
            当前条件下没有可用代理。请调整代理范围或导入代理。
          </div>
        )}
        {!loading && candidateProxies.length > 0 && candidateProfiles.length === 0 && (
          <div className="text-xs text-amber-500 bg-amber-500/10 border border-amber-500/30 rounded px-3 py-2">
            当前条件下没有可分配的实例。请调整实例范围。
          </div>
        )}

        {/* 预览 */}
        {assignments.length > 0 && (
          <div className="border border-[var(--color-border)] rounded overflow-hidden">
            <div className="px-3 py-2 bg-[var(--color-bg-secondary)] text-xs text-[var(--color-text-muted)] flex items-center justify-between">
              <span>分配预览（前 {Math.min(assignments.length, 12)} 条）</span>
              <span>
                平均每代理 ≈ {(assignments.length / Math.max(1, candidateProxies.length)).toFixed(1)} 个实例
              </span>
            </div>
            <div className="max-h-[260px] overflow-y-auto">
              <table className="w-full text-xs">
                <thead className="bg-[var(--color-bg-secondary)]/60 sticky top-0">
                  <tr>
                    <th className="px-3 py-1.5 text-left font-medium text-[var(--color-text-muted)] w-10">#</th>
                    <th className="px-3 py-1.5 text-left font-medium text-[var(--color-text-muted)]">实例</th>
                    <th className="px-3 py-1.5 text-left font-medium text-[var(--color-text-muted)]">代理</th>
                    <th className="px-3 py-1.5 text-right font-medium text-[var(--color-text-muted)] w-24">该代理共分到</th>
                  </tr>
                </thead>
                <tbody>
                  {assignments.slice(0, 12).map((row, idx) => (
                    <tr key={row.profile.profileId} className="border-t border-[var(--color-border)]/40">
                      <td className="px-3 py-1.5 text-[var(--color-text-muted)]">{idx + 1}</td>
                      <td className="px-3 py-1.5 text-[var(--color-text-primary)] truncate max-w-[200px]">
                        {row.profile.profileName || row.profile.profileId}
                      </td>
                      <td className="px-3 py-1.5 text-[var(--color-text-primary)] truncate max-w-[260px]">
                        {row.proxy.proxyName || row.proxy.proxyId}
                        {row.proxy.groupName && (
                          <span className="ml-1.5 text-[var(--color-primary)]/70">[{row.proxy.groupName}]</span>
                        )}
                      </td>
                      <td className="px-3 py-1.5 text-right text-[var(--color-text-muted)]">
                        {distribution.get(row.proxy.proxyId) || 0}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {loading && (
          <div className="flex items-center justify-center py-4 text-[var(--color-text-muted)] text-sm gap-2">
            <Loader2 className="w-4 h-4 animate-spin" />
            加载实例数据...
          </div>
        )}
      </div>
    </Modal>
  )
}

function SummaryCell({
  label,
  value,
  tone = 'default',
}: {
  label: string
  value: number
  tone?: 'default' | 'primary' | 'muted'
}) {
  const valueClass =
    tone === 'primary'
      ? 'text-[var(--color-primary)]'
      : tone === 'muted'
      ? 'text-[var(--color-text-muted)]'
      : 'text-[var(--color-text-primary)]'
  return (
    <div className="border border-[var(--color-border)] rounded px-3 py-2">
      <div className="text-xs text-[var(--color-text-muted)]">{label}</div>
      <div className={`text-lg font-semibold mt-0.5 ${valueClass}`}>{value}</div>
    </div>
  )
}
