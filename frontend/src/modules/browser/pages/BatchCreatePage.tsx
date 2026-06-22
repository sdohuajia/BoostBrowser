import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Layers } from 'lucide-react'
import { Button, Card, ConfirmModal, FormItem, Input, Modal, Select, Textarea, toast } from '../../../shared/components'
import type { BrowserCore, BrowserProfileInput, BrowserProxy, BrowserGroup } from '../types'
import { batchCreateBrowserProfiles, fetchAllTags, fetchBrowserCores, fetchBrowserProxies, fetchBrowserSettings, fetchGroups } from '../api'
import { FingerprintPanel } from '../components/FingerprintPanel'
import { TagInput } from '../components/TagInput'
import { GroupSelector } from '../components/GroupSelector'

const fallbackLowLaunchArgs = ['--disable-sync', '--no-first-run']

function normalizeLaunchArgs(args: string[]): string[] {
  return (args || []).map(item => item.trim()).filter(Boolean)
}

function resolveDefaultLaunchArgs(args: string[]): string[] {
  const normalized = normalizeLaunchArgs(args)
  return normalized.length > 0 ? normalized : fallbackLowLaunchArgs
}

export function BatchCreatePage() {
  const navigate = useNavigate()
  const [prefix, setPrefix] = useState('实例')
  const [startIndex, setStartIndex] = useState(1)
  const [count, setCount] = useState(5)
  const [formData, setFormData] = useState<BrowserProfileInput>({
    profileName: '',
    userDataDir: '',
    coreId: '',
    fingerprintArgs: [],
    proxyId: '',
    proxyConfig: '',
    launchArgs: [],
    tags: [],
    keywords: [],
    groupId: '',
  })
  const [cores, setCores] = useState<BrowserCore[]>([])
  const [proxies, setProxies] = useState<BrowserProxy[]>([])
  const [groups, setGroups] = useState<BrowserGroup[]>([])
  const [launchArgsText, setLaunchArgsText] = useState('')
  const [allTags, setAllTags] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [isDirty, setIsDirty] = useState(false)
  const [leaveConfirm, setLeaveConfirm] = useState(false)
  const [saveError, setSaveError] = useState('')

  useEffect(() => {
    const loadData = async () => {
      const [coreList, proxyList, tagList, groupList, settings] = await Promise.all([
        fetchBrowserCores(),
        fetchBrowserProxies(),
        fetchAllTags(),
        fetchGroups(),
        fetchBrowserSettings(),
      ])
      setCores(coreList)
      setProxies(proxyList)
      setAllTags(tagList)
      setGroups(groupList)
      setLaunchArgsText(resolveDefaultLaunchArgs(settings.defaultLaunchArgs || []).join('\n'))
    }
    loadData()
  }, [])

  const handleChange = (field: keyof BrowserProfileInput, value: string | string[]) => {
    setIsDirty(true)
    setFormData(prev => ({ ...prev, [field]: value }))
  }

  const handleSave = async () => {
    if (!prefix.trim()) {
      toast.error('请输入名称前缀')
      return
    }
    if (count < 1 || count > 200) {
      toast.error('批量创建数量需在 1~200 之间')
      return
    }
    if (startIndex < 0) {
      toast.error('起始序号不能为负数')
      return
    }

    setSaving(true)
    const payload: BrowserProfileInput = {
      ...formData,
      launchArgs: normalizeLaunchArgs(launchArgsText.split('\n')),
    }
    try {
      const created = await batchCreateBrowserProfiles(prefix.trim(), startIndex, count, payload)
      toast.success(`成功创建 ${created.length} 个实例`)
      setIsDirty(false)
      navigate('/browser/list')
    } catch (error: any) {
      setSaveError(typeof error === 'string' ? error : error?.message || '批量创建失败')
    } finally {
      setSaving(false)
    }
  }

  const handleBack = () => {
    if (isDirty) { setLeaveConfirm(true) } else { navigate('/browser/list') }
  }

  const defaultCore = cores.find(c => c.isDefault)

  // 名字序列预览
  const previewNames: string[] = []
  for (let i = 0; i < Math.min(count, 10); i++) {
    previewNames.push(`${prefix}-${startIndex + i}`)
  }
  const hasMore = count > 10

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">批量创建实例</h1>
          <p className="text-sm text-[var(--color-text-muted)] mt-1">按前缀+序号批量创建多个指纹浏览器实例</p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" onClick={handleBack}>返回列表</Button>
          <Button size="sm" onClick={handleSave} loading={saving}>
            <Layers className="w-4 h-4" />
            批量创建
          </Button>
        </div>
      </div>

      <Card title="批量设置" subtitle="设置名称前缀、起始终号和数量">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <FormItem label="名称前缀" required>
            <Input
              value={prefix}
              onChange={e => setPrefix(e.target.value)}
              placeholder="实例"
            />
          </FormItem>
          <FormItem label="起始序号" hint="第一个实例的编号">
            <Input
              type="number"
              min={0}
              value={startIndex}
              onChange={e => setStartIndex(Math.max(0, parseInt(e.target.value) || 1))}
            />
          </FormItem>
          <FormItem label="创建数量" hint="1~200">
            <Input
              type="number"
              min={1}
              max={200}
              value={count}
              onChange={e => setCount(Math.min(200, Math.max(1, parseInt(e.target.value) || 1)))}
            />
          </FormItem>
        </div>
        {previewNames.length > 0 && (
          <div className="mt-3 p-3 bg-[var(--color-bg-base)] rounded-lg border border-[var(--color-border-default)]">
            <p className="text-xs text-[var(--color-text-muted)] mb-1.5">预览名称</p>
            <div className="flex flex-wrap gap-1.5">
              {previewNames.map(name => (
                <span key={name} className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-[var(--color-accent)]/10 text-[var(--color-accent)]">
                  {name}
                </span>
              ))}
              {hasMore && (
                <span className="inline-flex items-center px-2 py-0.5 rounded text-xs text-[var(--color-text-muted)]">
                  ... 共 {count} 个
                </span>
              )}
            </div>
          </div>
        )}
      </Card>

      <Card title="通用配置" subtitle="所有实例共用以下配置">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <FormItem label="内核">
            <Select
              value={formData.coreId}
              onChange={e => handleChange('coreId', e.target.value)}
              options={
                cores.length > 0 ? [
                  { value: '', label: defaultCore ? `使用默认 (${defaultCore.coreName})` : '使用默认内核' },
                  ...cores.map(c => ({ value: c.coreId, label: c.coreName })),
                ] : [
                  { value: '', label: '暂无内核，请添加内核' }
                ]
              }
            />
          </FormItem>
          <FormItem label="分组">
            <GroupSelector
              groups={groups}
              value={formData.groupId || ''}
              onChange={groupId => handleChange('groupId', groupId)}
              placeholder="未分组"
              className="w-full"
            />
          </FormItem>
          <FormItem label="标签">
            <TagInput
              value={formData.tags}
              onChange={tags => handleChange('tags', tags)}
              suggestions={allTags}
              placeholder="输入标签后按回车，所有实例共用此标签"
            />
          </FormItem>
        </div>
      </Card>

      <Card title="代理配置" subtitle="所有实例共用代理（可选）">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <FormItem label="代理池选择">
            <Select
              value={formData.proxyId}
              onChange={e => handleChange('proxyId', e.target.value)}
              options={[
                { value: '', label: '不使用代理池' },
                ...proxies.map(p => ({ value: p.proxyId, label: p.proxyName || p.proxyId })),
              ]}
            />
          </FormItem>
          <FormItem label="手动代理配置">
            <Input
              value={formData.proxyConfig}
              onChange={e => handleChange('proxyConfig', e.target.value)}
              placeholder="http://127.0.0.1:7890"
              disabled={!!formData.proxyId}
            />
          </FormItem>
        </div>
        {formData.proxyId && (
          <p className="text-xs text-[var(--color-text-muted)] mt-2">已选择代理池代理，手动配置将被忽略</p>
        )}
      </Card>

      <Card title="指纹配置" subtitle="所有实例共用指纹参数">
        <FingerprintPanel
          value={formData.fingerprintArgs}
          onChange={args => handleChange('fingerprintArgs', args)}
          defaultPresetId="win-chrome-office"
        />
      </Card>

      <Card title="启动参数" subtitle="每行一个参数，所有实例共用">
        <div className="space-y-2">
          <Textarea
            value={launchArgsText}
            onChange={e => { setLaunchArgsText(e.target.value); setIsDirty(true) }}
            rows={6}
            placeholder="--disable-sync"
          />
          <p className="text-xs text-[var(--color-text-muted)]">这里默认就是轻量参数模板；需要更复杂的参数，直接在此基础上修改。</p>
        </div>
      </Card>

      <ConfirmModal
        open={leaveConfirm}
        onClose={() => setLeaveConfirm(false)}
        onConfirm={() => navigate('/browser/list')}
        title="放弃未保存的更改？"
        content="当前页面有未保存的修改，离开后将丢失这些更改。"
        confirmText="放弃并离开"
        cancelText="继续编辑"
        danger
      />

      <Modal
        open={!!saveError}
        onClose={() => setSaveError('')}
        title="批量创建失败"
        width="420px"
        footer={<Button onClick={() => setSaveError('')}>知道了</Button>}
      >
        <div className="text-[var(--color-text-secondary)]">{saveError}</div>
      </Modal>
    </div>
  )
}