import { useState } from 'react'
import { Copy, Play, RefreshCw, Square, Trash2 } from 'lucide-react'
import { Button, toast } from '../../../shared/components'
import { regenerateBrowserProfileCode } from '../api'

// LaunchCode 单元格
export function LaunchCodeCell({ profileId, code, onRefresh }: { profileId: string; code: string; onRefresh: () => void }) {
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
    </div>
  )
}

// 批量操作工具栏
export function BatchToolbar({
  selectedCount,
  totalCount,
  onSelectAll,
  onDeselectAll,
  onBatchStart,
  onBatchStop,
  onBatchDelete,
  batchLoading,
}: {
  selectedCount: number
  totalCount: number
  onSelectAll: () => void
  onDeselectAll: () => void
  onBatchStart: () => void
  onBatchStop: () => void
  onBatchDelete: () => void
  batchLoading: boolean
}) {
  if (selectedCount === 0) return null
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 bg-[var(--color-accent)]/10 border border-[var(--color-accent)]/20 rounded-lg">
      <span className="text-sm font-medium text-[var(--color-accent)]">已选 {selectedCount} / {totalCount}</span>
      <div className="flex gap-1.5 ml-auto">
        <Button size="sm" variant="ghost" onClick={onSelectAll}>全选</Button>
        <Button size="sm" variant="ghost" onClick={onDeselectAll}>取消</Button>
        <Button size="sm" onClick={onBatchStart} loading={batchLoading}>
          <Play className="w-3.5 h-3.5" />启动
        </Button>
        <Button size="sm" variant="secondary" onClick={onBatchStop} loading={batchLoading}>
          <Square className="w-3.5 h-3.5" />停止
        </Button>
        <Button size="sm" variant="ghost" onClick={onBatchDelete} className="text-red-500 hover:text-red-600">
          <Trash2 className="w-3.5 h-3.5" />删除
        </Button>
      </div>
    </div>
  )
}
