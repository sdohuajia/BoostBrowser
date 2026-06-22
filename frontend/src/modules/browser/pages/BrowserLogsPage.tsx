import { useEffect, useRef, useState } from 'react'
import { RefreshCw, Trash2 } from 'lucide-react'
import { Badge, Button, Card } from '../../../shared/components'

interface LogEntry {
  time: string
  level: string
  component: string
  message: string
  fields?: Record<string, any>
}

const LEVELS = ['ALL', 'DEBUG', 'INFO', 'WARN', 'ERROR']

const levelVariant = (level: string) => {
  switch (level) {
    case 'ERROR': return 'error'
    case 'WARN': return 'warning'
    case 'DEBUG': return 'default'
    default: return 'info'
  }
}

const levelColor = (level: string) => {
  switch (level) {
    case 'ERROR': return 'text-red-500'
    case 'WARN': return 'text-yellow-500'
    case 'DEBUG': return 'text-[var(--color-text-muted)]'
    default: return 'text-[var(--color-text-secondary)]'
  }
}

async function fetchLogs(): Promise<LogEntry[]> {
  try {
    const bindings: any = await import('../../../wailsjs/go/main/App')
    return (await bindings.GetAppLogs()) || []
  } catch { return [] }
}

async function clearLogs() {
  try {
    const bindings: any = await import('../../../wailsjs/go/main/App')
    await bindings.ClearAppLogs()
  } catch { /* ignore */ }
}

export function BrowserLogsPage() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [levelFilter, setLevelFilter] = useState('ALL')
  const [keyword, setKeyword] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const [loading, setLoading] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)

  const load = async () => {
    setLoading(true)
    try {
      const data = await fetchLogs()
      setLogs(data)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const timer = setInterval(load, 3000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    if (autoScroll) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logs, autoScroll])

  const handleClear = async () => {
    await clearLogs()
    setLogs([])
  }

  const filtered = logs.filter(entry => {
    if (levelFilter !== 'ALL' && entry.level !== levelFilter) return false
    if (keyword && !entry.message.toLowerCase().includes(keyword.toLowerCase()) &&
        !entry.component.toLowerCase().includes(keyword.toLowerCase())) return false
    return true
  })

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">日志查看</h1>
          <p className="text-sm text-[var(--color-text-muted)] mt-1">应用运行日志，每 3 秒自动刷新</p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" onClick={load} loading={loading}>
            <RefreshCw className="w-4 h-4" />刷新
          </Button>
          <Button variant="secondary" size="sm" onClick={handleClear}>
            <Trash2 className="w-4 h-4" />清空
          </Button>
        </div>
      </div>

      {/* 工具栏 */}
      <div className="flex items-center gap-3 flex-wrap">
        {/* 级别过滤 */}
        <div className="flex gap-1">
          {LEVELS.map(l => (
            <button
              key={l}
              onClick={() => setLevelFilter(l)}
              className={`px-2.5 py-1 text-xs rounded-md transition-colors ${
                levelFilter === l
                  ? 'bg-[var(--color-accent)] text-white'
                  : 'bg-[var(--color-bg-muted)] text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)]'
              }`}
            >
              {l}
            </button>
          ))}
        </div>

        <input
          value={keyword}
          onChange={e => setKeyword(e.target.value)}
          placeholder="搜索消息或组件..."
          className="px-3 py-1.5 text-sm rounded-md border border-[var(--color-border-default)] bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] w-48"
        />

        <label className="flex items-center gap-1.5 text-xs text-[var(--color-text-muted)] cursor-pointer select-none ml-auto">
          <input
            type="checkbox"
            checked={autoScroll}
            onChange={e => setAutoScroll(e.target.checked)}
            className="w-3.5 h-3.5"
          />
          自动滚动
        </label>

        <span className="text-xs text-[var(--color-text-muted)]">
          {filtered.length} / {logs.length} 条
        </span>
      </div>

      {/* 日志列表 */}
      <Card padding="none">
        <div
          className="overflow-auto font-mono text-xs"
          style={{ maxHeight: 'calc(100vh - 280px)' }}
        >
          {filtered.length === 0 ? (
            <div className="py-16 text-center text-sm text-[var(--color-text-muted)]">暂无日志</div>
          ) : (
            <table className="min-w-full">
              <thead className="sticky top-0 z-10 bg-[var(--color-bg-muted)]">
                <tr>
                  <th className="px-3 py-2 text-left text-[var(--color-text-muted)] font-semibold w-40">时间</th>
                  <th className="px-3 py-2 text-left text-[var(--color-text-muted)] font-semibold w-16">级别</th>
                  <th className="px-3 py-2 text-left text-[var(--color-text-muted)] font-semibold w-28">组件</th>
                  <th className="px-3 py-2 text-left text-[var(--color-text-muted)] font-semibold">消息</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--color-border-muted)]">
                {filtered.map((entry, i) => (
                  <tr key={i} className="hover:bg-[var(--color-bg-muted)]/40">
                    <td className="px-3 py-1.5 text-[var(--color-text-muted)] whitespace-nowrap">{entry.time}</td>
                    <td className="px-3 py-1.5">
                      <Badge variant={levelVariant(entry.level)} className="text-[10px]">{entry.level}</Badge>
                    </td>
                    <td className="px-3 py-1.5 text-[var(--color-text-muted)] truncate max-w-[112px]" title={entry.component}>
                      {entry.component}
                    </td>
                    <td className={`px-3 py-1.5 ${levelColor(entry.level)}`}>
                      <span>{entry.message}</span>
                      {entry.fields && Object.keys(entry.fields).length > 0 && (
                        <span className="ml-2 text-[var(--color-text-muted)]">
                          {Object.entries(entry.fields).map(([k, v]) => `${k}=${JSON.stringify(v)}`).join(' ')}
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <div ref={bottomRef} />
        </div>
      </Card>
    </div>
  )
}
