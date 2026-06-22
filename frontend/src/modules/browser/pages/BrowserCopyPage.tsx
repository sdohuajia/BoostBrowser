import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Button, Card, FormItem, Input, Select, toast } from '../../../shared/components'
import type { BrowserProfile } from '../types'
import { createBrowserProfile, fetchBrowserProfiles } from '../api'

export function BrowserCopyPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [profiles, setProfiles] = useState<BrowserProfile[]>([])
  const [sourceId, setSourceId] = useState(id || '')
  const [targetName, setTargetName] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    const loadProfiles = async () => {
      const list = await fetchBrowserProfiles()
      setProfiles(list)
      if (!sourceId && list.length > 0) {
        setSourceId(list[0].profileId)
      }
    }
    loadProfiles()
  }, [])

  const sourceProfile = profiles.find(item => item.profileId === sourceId)

  const handleCopy = async () => {
    if (!sourceProfile || !targetName) {
      toast.error('请填写目标名称')
      return
    }
    setSaving(true)
    try {
      await createBrowserProfile({
        profileName: targetName,
        userDataDir: `${sourceProfile.userDataDir}-copy`,
        coreId: sourceProfile.coreId,
        fingerprintArgs: sourceProfile.fingerprintArgs,
        proxyId: sourceProfile.proxyId,
        proxyConfig: sourceProfile.proxyConfig,
        launchArgs: sourceProfile.launchArgs,
        tags: sourceProfile.tags,
        keywords: sourceProfile.keywords || [],
      })
      toast.success('配置已复制')
      navigate('/browser/list')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">配置复制</h1>
          <p className="text-sm text-[var(--color-text-muted)] mt-1">基于现有配置快速创建</p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" onClick={() => navigate('/browser/list')}>返回列表</Button>
          <Button size="sm" onClick={handleCopy} loading={saving}>生成配置</Button>
        </div>
      </div>

      <Card title="复制设置" subtitle="选择源配置并设置名称">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <FormItem label="源配置">
            <Select
              value={sourceId}
              onChange={e => setSourceId(e.target.value)}
              options={profiles.map(item => ({ value: item.profileId, label: item.profileName }))}
            />
          </FormItem>
          <FormItem label="新配置名称">
            <Input value={targetName} onChange={e => setTargetName(e.target.value)} placeholder="请输入名称" />
          </FormItem>
        </div>
      </Card>
    </div>
  )
}
