import { useEffect, useMemo, useState } from 'react'
import { MoreHorizontal, PackagePlus, Puzzle, Search, ShieldCheck, UploadCloud, X } from 'lucide-react'
import { Button, Card, FormItem, Input, Modal, Select, Textarea, toast } from '../../../shared/components'
import { fetchBrowserProfiles, importExtensionToBrowserProfiles, removeExtensionFromBrowserProfiles } from '../api'
import type { BrowserProfile } from '../types'

type ExtensionPlatform = 'google' | 'firefox'
type DistributionMode = 'manual' | 'global'

interface ManagedExtension {
  id: string
  name: string
  description: string
  developer: string
  downloadAddress: string
  platform: ExtensionPlatform
  distributionMode: DistributionMode
  profileIds: string[]
  updatedAt: string
}

const STORAGE_KEY = 'boost-browser-managed-extensions'

const platformLabel: Record<ExtensionPlatform, string> = {
  google: 'Google',
  firefox: 'Firefox',
}

const modeLabel: Record<DistributionMode, string> = {
  manual: '手动分配',
  global: '全局使用',
}

const defaultExtensions: ManagedExtension[] = [
  {
    id: 'builtin-metamask',
    name: 'MetaMask',
    description: '以太坊/Web3 钱包扩展，可分配到指定浏览器实例',
    developer: 'metamask.io',
    downloadAddress: 'nkbihfbeogaeaoehlefnkodbefgpgknn',
    platform: 'google',
    distributionMode: 'manual',
    profileIds: [],
    updatedAt: new Date().toISOString(),
  },
  {
    id: 'builtin-okx',
    name: 'OKX Wallet',
    description: 'OKX Web3 钱包扩展，适合批量实例统一加载',
    developer: 'okx.com',
    downloadAddress: 'mcohilncbfahbmgdjkbpemcciiolgcge',
    platform: 'google',
    distributionMode: 'global',
    profileIds: [],
    updatedAt: new Date().toISOString(),
  },
]

function loadExtensions(): ManagedExtension[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return defaultExtensions
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : defaultExtensions
  } catch {
    return defaultExtensions
  }
}

function saveExtensions(items: ManagedExtension[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(items))
}

function extensionIcon(name: string) {
  const text = (name || 'E').trim().slice(0, 1).toUpperCase()
  const palettes = [
    'from-blue-500 to-cyan-400',
    'from-purple-500 to-pink-400',
    'from-emerald-500 to-lime-400',
    'from-orange-500 to-amber-400',
  ]
  const idx = Math.abs(name.split('').reduce((acc, ch) => acc + ch.charCodeAt(0), 0)) % palettes.length
  return (
    <div className={`w-10 h-10 rounded-xl bg-gradient-to-br ${palettes[idx]} text-white flex items-center justify-center font-bold shadow-sm`}>
      {text}
    </div>
  )
}

export function ExtensionManagementPage() {
  const [extensions, setExtensions] = useState<ManagedExtension[]>(() => loadExtensions())
  const [profiles, setProfiles] = useState<BrowserProfile[]>([])
  const [activePlatform, setActivePlatform] = useState<'all' | ExtensionPlatform>('all')
  const [keyword, setKeyword] = useState('')
  const [uploadOpen, setUploadOpen] = useState(false)
  const [configOpen, setConfigOpen] = useState(false)
  const [currentId, setCurrentId] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [form, setForm] = useState({
    name: '',
    description: '',
    developer: '',
    downloadAddress: '',
    platform: 'google' as ExtensionPlatform,
    distributionMode: 'manual' as DistributionMode,
    profileIds: [] as string[],
  })

  useEffect(() => {
    fetchBrowserProfiles().then(setProfiles).catch(() => setProfiles([]))
  }, [])

  useEffect(() => {
    saveExtensions(extensions)
  }, [extensions])

  const filtered = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    return extensions.filter(item => {
      if (activePlatform !== 'all' && item.platform !== activePlatform) return false
      if (!q) return true
      return [item.name, item.description, item.developer, item.downloadAddress]
        .some(value => value.toLowerCase().includes(q))
    })
  }, [extensions, activePlatform, keyword])

  const currentExtension = extensions.find(item => item.id === currentId) || null
  const selectedCount = form.distributionMode === 'global' ? profiles.length : form.profileIds.length

  const resetForm = () => {
    setForm({
      name: '',
      description: '',
      developer: '',
      downloadAddress: '',
      platform: 'google',
      distributionMode: 'manual',
      profileIds: [],
    })
  }

  const openUpload = () => {
    resetForm()
    setCurrentId(null)
    setUploadOpen(true)
  }

  const openConfig = (item: ManagedExtension) => {
    setCurrentId(item.id)
    setForm({
      name: item.name,
      description: item.description,
      developer: item.developer,
      downloadAddress: item.downloadAddress,
      platform: item.platform,
      distributionMode: item.distributionMode,
      profileIds: item.profileIds || [],
    })
    setConfigOpen(true)
  }

  const toggleProfile = (profileId: string) => {
    setForm(prev => {
      const exists = prev.profileIds.includes(profileId)
      return {
        ...prev,
        profileIds: exists
          ? prev.profileIds.filter(id => id !== profileId)
          : [...prev.profileIds, profileId],
      }
    })
  }

  const submitExtension = async () => {
    const name = form.name.trim()
    const downloadAddress = form.downloadAddress.trim()
    if (!name) {
      toast.warning('请输入扩展名称')
      return
    }
    if (!downloadAddress) {
      toast.warning('请输入扩展下载地址或扩展 ID')
      return
    }
    setSubmitting(true)
    try {
      const nextItem: ManagedExtension = {
        id: currentId || `ext-${Date.now()}`,
        name,
        description: form.description.trim() || '暂无描述',
        developer: form.developer.trim() || '未知开发者',
        downloadAddress,
        platform: form.platform,
        distributionMode: form.distributionMode,
        profileIds: form.distributionMode === 'global' ? [] : form.profileIds,
        updatedAt: new Date().toISOString(),
      }
      setExtensions(prev => currentId
        ? prev.map(item => item.id === currentId ? nextItem : item)
        : [nextItem, ...prev]
      )
      toast.success(currentId ? '扩展配置已保存' : '扩展已加入列表')
      setUploadOpen(false)
      setConfigOpen(false)
      resetForm()
      setCurrentId(null)
    } finally {
      setSubmitting(false)
    }
  }

  const distributeExtension = async (item: ManagedExtension) => {
    const targetIds = item.distributionMode === 'global'
      ? profiles.map(profile => profile.profileId)
      : item.profileIds
    if (targetIds.length === 0) {
      toast.warning('请先在配置里选择要分配的实例')
      return
    }
    setSubmitting(true)
    try {
      const result = await importExtensionToBrowserProfiles(targetIds, item.downloadAddress)
      toast.success(result?.message || `已分配到 ${targetIds.length} 个实例`)
      setExtensions(prev => prev.map(ext => ext.id === item.id ? { ...ext, updatedAt: new Date().toISOString() } : ext))
    } catch (error: any) {
      toast.error(error?.message || '扩展分配失败')
    } finally {
      setSubmitting(false)
    }
  }

  const removeExtension = async (item: ManagedExtension) => {
    const targetIds = item.distributionMode === 'global'
      ? profiles.map(profile => profile.profileId)
      : item.profileIds
    setSubmitting(true)
    try {
      if (targetIds.length > 0) {
        const result = await removeExtensionFromBrowserProfiles(targetIds, item.downloadAddress)
        toast.success(result?.message || '扩展已解绑')
      } else {
        toast.success('扩展已从列表移除')
      }
      setExtensions(prev => prev.filter(ext => ext.id !== item.id))
      if (currentId === item.id) {
        setConfigOpen(false)
        setCurrentId(null)
      }
    } catch (error: any) {
      toast.error(error?.message || '移除扩展失败')
    } finally {
      setSubmitting(false)
    }
  }

  const modalTitle = currentId ? '配置扩展' : '上传扩展'

  return (
    <div className="flex flex-col h-full bg-[var(--color-bg-layout)]">
      <div className="flex items-center justify-between px-6 py-4 border-b border-[var(--color-border-default)] bg-[var(--color-bg-surface)]">
        <div className="flex items-center gap-3">
          <Puzzle className="w-5 h-5 text-[var(--color-accent)]" />
          <div>
            <h1 className="text-lg font-semibold text-[var(--color-text-primary)]">扩展管理</h1>
            <p className="text-xs text-[var(--color-text-muted)] mt-0.5">统一添加扩展，并分配到指定浏览器实例</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button onClick={openUpload}><UploadCloud className="w-4 h-4" />上传扩展</Button>
          <Button variant="secondary" onClick={() => toast.info('扩展中心入口已预留，可继续接入在线扩展市场')}><PackagePlus className="w-4 h-4" />扩展中心</Button>
        </div>
      </div>

      <div className="px-6 py-3 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 text-sm border-b border-[var(--color-border-default)]">
        添加后的扩展可一键写入实例启动参数；已启动实例需要重启后生效。不会切换官方 Chrome，不影响当前指纹内核。
      </div>

      <div className="p-6 space-y-4 overflow-auto">
        <Card padding="none" className="shadow-sm">
          <div className="flex items-center justify-between gap-4 px-5 py-4 border-b border-[var(--color-border-muted)]">
            <div className="flex items-center gap-2">
              {[
                { key: 'all', label: '全部' },
                { key: 'google', label: 'Google' },
                { key: 'firefox', label: 'Firefox' },
              ].map(tab => (
                <button
                  key={tab.key}
                  onClick={() => setActivePlatform(tab.key as any)}
                  className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
                    activePlatform === tab.key
                      ? 'bg-[var(--color-accent)] text-[var(--color-text-inverse)]'
                      : 'text-[var(--color-text-secondary)] hover:bg-[var(--color-accent-muted)]'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>
            <div className="relative w-72">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--color-text-muted)]" />
              <Input value={keyword} onChange={e => setKeyword(e.target.value)} placeholder="请输入扩展名称" className="pl-9" />
            </div>
          </div>

          <table className="w-full">
            <thead className="bg-[var(--color-bg-muted)] border-b border-[var(--color-border-muted)]">
              <tr className="text-left text-xs font-semibold text-[var(--color-text-muted)]">
                <th className="px-5 py-3">扩展</th>
                <th className="px-5 py-3 w-48">开发者</th>
                <th className="px-5 py-3 w-36">分配方式</th>
                <th className="px-5 py-3 w-32">平台</th>
                <th className="px-5 py-3 w-48 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border-muted)]">
              {filtered.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-5 py-14 text-center text-sm text-[var(--color-text-muted)]">暂无扩展</td>
                </tr>
              ) : filtered.map(item => {
                const count = item.distributionMode === 'global' ? profiles.length : item.profileIds.length
                return (
                  <tr key={item.id} className="hover:bg-[var(--color-bg-hover)] transition-colors">
                    <td className="px-5 py-4">
                      <div className="flex items-center gap-3">
                        {extensionIcon(item.name)}
                        <div className="min-w-0">
                          <div className="font-medium text-[var(--color-text-primary)] truncate">{item.name}</div>
                          <div className="text-xs text-[var(--color-text-muted)] truncate max-w-xl">{item.description}</div>
                        </div>
                      </div>
                    </td>
                    <td className="px-5 py-4 text-sm text-[var(--color-text-secondary)]">{item.developer}</td>
                    <td className="px-5 py-4">
                      <span className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium ${
                        item.distributionMode === 'global'
                          ? 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-300'
                          : 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                      }`}>
                        {modeLabel[item.distributionMode]}{count > 0 ? ` · ${count}` : ''}
                      </span>
                    </td>
                    <td className="px-5 py-4 text-sm text-[var(--color-text-secondary)]">{platformLabel[item.platform]}</td>
                    <td className="px-5 py-4">
                      <div className="flex items-center justify-end gap-2">
                        <Button size="sm" variant="secondary" onClick={() => openConfig(item)}>配置</Button>
                        <Button size="sm" onClick={() => distributeExtension(item)} disabled={submitting}>分配</Button>
                        <button onClick={() => removeExtension(item)} className="p-2 rounded-lg text-[var(--color-text-muted)] hover:bg-red-50 hover:text-red-600" title="移除" disabled={submitting}>
                          <MoreHorizontal className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </Card>
      </div>

      <Modal
        open={uploadOpen || configOpen}
        onClose={() => { if (!submitting) { setUploadOpen(false); setConfigOpen(false) } }}
        title={modalTitle}
        width="760px"
        footer={
          <>
            <Button variant="secondary" onClick={() => { setUploadOpen(false); setConfigOpen(false) }} disabled={submitting}>取消</Button>
            <Button onClick={submitExtension} loading={submitting}>保存</Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <FormItem label="扩展名称" required>
              <Input value={form.name} onChange={e => setForm(prev => ({ ...prev, name: e.target.value }))} placeholder="例如 MetaMask" />
            </FormItem>
            <FormItem label="开发者">
              <Input value={form.developer} onChange={e => setForm(prev => ({ ...prev, developer: e.target.value }))} placeholder="开发者/团队/邮箱" />
            </FormItem>
          </div>
          <FormItem label="扩展说明">
            <Input value={form.description} onChange={e => setForm(prev => ({ ...prev, description: e.target.value }))} placeholder="显示在扩展列表中的简短描述" />
          </FormItem>
          <FormItem label="扩展程序下载地址" required hint="支持 Chrome Web Store 详情页、32位扩展ID、直接 .crx/.zip 下载地址">
            <Textarea rows={3} value={form.downloadAddress} onChange={e => setForm(prev => ({ ...prev, downloadAddress: e.target.value }))} placeholder="例如：https://chromewebstore.google.com/detail/.../扩展ID 或 nkbihfbeogaeaoehlefnkodbefgpgknn" />
          </FormItem>
          <div className="grid grid-cols-2 gap-4">
            <FormItem label="平台">
              <Select value={form.platform} onChange={e => setForm(prev => ({ ...prev, platform: e.target.value as ExtensionPlatform }))} options={[{ value: 'google', label: 'Google' }, { value: 'firefox', label: 'Firefox' }]} />
            </FormItem>
            <FormItem label="分配方式">
              <Select value={form.distributionMode} onChange={e => setForm(prev => ({ ...prev, distributionMode: e.target.value as DistributionMode }))} options={[{ value: 'manual', label: '手动分配' }, { value: 'global', label: '全局使用' }]} />
            </FormItem>
          </div>

          {form.distributionMode === 'manual' ? (
            <div className="rounded-xl border border-[var(--color-border-default)] overflow-hidden">
              <div className="flex items-center justify-between px-4 py-3 bg-[var(--color-bg-muted)] border-b border-[var(--color-border-muted)]">
                <div className="text-sm font-medium text-[var(--color-text-primary)]">选择分配实例</div>
                <div className="text-xs text-[var(--color-text-muted)]">已选 {selectedCount} 个</div>
              </div>
              <div className="max-h-56 overflow-auto divide-y divide-[var(--color-border-muted)]">
                {profiles.length === 0 ? (
                  <div className="px-4 py-8 text-sm text-center text-[var(--color-text-muted)]">暂无实例</div>
                ) : profiles.map(profile => (
                  <label key={profile.profileId} className="flex items-center justify-between px-4 py-2.5 cursor-pointer hover:bg-[var(--color-bg-hover)]">
                    <span className="flex items-center gap-2 text-sm text-[var(--color-text-primary)]">
                      <input type="checkbox" className="accent-[var(--color-accent)]" checked={form.profileIds.includes(profile.profileId)} onChange={() => toggleProfile(profile.profileId)} />
                      {profile.profileName || profile.profileId}
                    </span>
                    <span className={`text-xs ${profile.running ? 'text-green-600' : 'text-[var(--color-text-muted)]'}`}>{profile.running ? '运行中' : '未启动'}</span>
                  </label>
                ))}
              </div>
            </div>
          ) : (
            <div className="flex items-center gap-2 rounded-xl border border-green-200 bg-green-50 dark:bg-green-900/20 dark:border-green-900 px-4 py-3 text-sm text-green-700 dark:text-green-300">
              <ShieldCheck className="w-4 h-4" />
              全局使用会分配到当前全部 {profiles.length} 个实例；后续新建实例仍可再次点击“分配”写入。
            </div>
          )}

          {currentExtension && (
            <div className="flex items-center justify-between rounded-lg bg-[var(--color-bg-muted)] px-3 py-2 text-xs text-[var(--color-text-muted)]">
              <span>上次更新：{new Date(currentExtension.updatedAt).toLocaleString()}</span>
              <button onClick={() => removeExtension(currentExtension)} className="inline-flex items-center gap-1 text-red-500 hover:text-red-600" disabled={submitting}>
                <X className="w-3 h-3" />移除扩展
              </button>
            </div>
          )}
        </div>
      </Modal>
    </div>
  )
}
