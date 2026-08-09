import { useEffect, useMemo, useState } from 'react'
import { KeyRound, Play, RefreshCw } from 'lucide-react'
import { Button, Card, FormItem, Input, Textarea, toast } from '../../../shared/components'
import { fetchBrowserProfiles, importOKXWalletBatch } from '../api'
import type { BrowserProfile } from '../types'

const natural = (items: BrowserProfile[]) => [...items].sort((a, b) =>
  (a.profileName || a.profileId).localeCompare(b.profileName || b.profileId, 'zh-CN', { numeric: true }))

export function OKXWalletImportPage() {
  const [profiles, setProfiles] = useState<BrowserProfile[]>([])
  const [mnemonics, setMnemonics] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<any>(null)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const reload = () => fetchBrowserProfiles().then(items => setProfiles(natural(items))).catch(() => setProfiles([]))
  useEffect(() => { reload() }, [])
  const count = useMemo(() => mnemonics.split(/\r?\n/).filter(line => line.trim() && !line.trim().startsWith('#')).length, [mnemonics])
  const run = async () => {
    if (selectedIds.length === 0) return toast.warning('请至少选择一个实例')
    if (count < selectedIds.length) return toast.warning(`助记词 ${count} 条，已选实例 ${selectedIds.length} 个`)
    if (password.length < 8) return toast.warning('请输入至少 8 位的钱包密码')
    setBusy(true); setResult(null)
    try { setResult(await importOKXWalletBatch(mnemonics, password, selectedIds)); toast.success('批量任务已完成') }
    catch (error: any) { toast.error(error?.message || '批量导入失败') }
    finally { setBusy(false); setPassword('') }
  }
  return <div className="p-6 space-y-5 overflow-auto h-full bg-[var(--color-bg-layout)]">
    <div className="flex items-center justify-between"><div className="flex gap-3"><KeyRound className="w-6 h-6 text-[var(--color-accent)]"/><div><h1 className="text-lg font-semibold">OKX 钱包批量导入</h1><p className="text-sm text-[var(--color-text-muted)]">按实例-1 至实例-N 的顺序，一行助记词对应一个实例。</p></div></div><Button variant="secondary" onClick={reload} disabled={busy}><RefreshCw className="w-4 h-4"/>刷新实例</Button></div>
    <Card><div className="grid grid-cols-2 gap-4"><FormItem label="助记词文本" required hint="每行一条，不保存"><Textarea rows={12} value={mnemonics} onChange={e => setMnemonics(e.target.value)} disabled={busy}/></FormItem><div className="space-y-4"><FormItem label="统一钱包密码" required hint="仅本次任务内存使用"><Input type="password" value={password} onChange={e => setPassword(e.target.value)} disabled={busy}/></FormItem><div className="rounded-lg bg-[var(--color-bg-muted)] p-4 text-sm"><div className="flex justify-between"><span>已选：<b>{selectedIds.length}</b>　助记词：<b>{count}</b></span><button type="button" className="text-[var(--color-accent)]" onClick={() => setSelectedIds(selectedIds.length === profiles.length ? [] : profiles.map(p => p.profileId))}>{selectedIds.length === profiles.length ? '取消全选' : '全选'}</button></div><div className="max-h-52 overflow-auto mt-2 space-y-1">{profiles.map((profile, index) => <label key={profile.profileId} className="flex gap-2 cursor-pointer"><input type="checkbox" checked={selectedIds.includes(profile.profileId)} onChange={() => setSelectedIds(ids => ids.includes(profile.profileId) ? ids.filter(id => id !== profile.profileId) : [...ids, profile.profileId])}/>{index + 1}. {profile.profileName || profile.profileId}</label>)}</div></div><Button onClick={run} loading={busy} className="w-full"><Play className="w-4 h-4"/>开始逐个导入</Button></div></div></Card>
    {result && <Card><h2 className="font-semibold mb-3">任务结果：成功 {result.succeeded} / 未安装或失败 {result.failed}</h2>{result.items?.map((item: any) => <div key={item.profileId} className={item.status === 'success' ? 'text-green-600 text-sm' : 'text-red-600 text-sm'}>{item.profileName}：{item.status === 'success' ? '成功' : item.error}</div>)}</Card>}
  </div>
}
