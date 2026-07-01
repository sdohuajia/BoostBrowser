import { useState } from 'react'
import { ChevronDown, ChevronRight, Filter, FolderPlus, Pencil, Trash2, X } from 'lucide-react'
import { Input, Select } from '../../../shared/components'
import { TagFilterBar } from './TagFilterBar'
import type { BrowserCore, BrowserProxy, BrowserGroupWithCount } from '../types'

export interface InstanceFilters {
  keyword: string
  status: '' | 'running' | 'stopped'
  proxyId: string
  coreId: string
  tags: Set<string>
  kwSearch: string
  groupId: string   // '' = 全部, '__ungrouped__' = 未分组, 其他 = 具体分组ID
}

export const EMPTY_FILTERS: InstanceFilters = {
  keyword: '',
  status: '',
  proxyId: '',
  coreId: '',
  tags: new Set(),
  kwSearch: '',
  groupId: '',
}

export function isFiltersEmpty(f: InstanceFilters) {
  return !f.keyword && !f.status && !f.proxyId && !f.coreId && f.tags.size === 0 && !f.kwSearch && !f.groupId
}

interface Props {
  filters: InstanceFilters
  onChange: (f: InstanceFilters) => void
  proxies: BrowserProxy[]
  cores: BrowserCore[]
  allTags: string[]
  groups: BrowserGroupWithCount[]
  onCreateGroup?: () => void
  onRenameGroup?: (groupId: string) => void
  onDeleteGroup?: (groupId: string) => void
}

export function InstanceFilterBar({ filters, onChange, proxies, cores, allTags, groups, onCreateGroup, onRenameGroup, onDeleteGroup }: Props) {
  const [collapsed, setCollapsed] = useState(false)

  const set = <K extends keyof InstanceFilters>(key: K, value: InstanceFilters[K]) =>
    onChange({ ...filters, [key]: value })

  const hasFilter = !isFiltersEmpty(filters)
  const activeCount = [filters.keyword, filters.status, filters.proxyId, filters.coreId, filters.kwSearch, filters.groupId].filter(Boolean).length + filters.tags.size

  // 当前选中的真实分组（不含 '' / '__ungrouped__'）
  const selectedRealGroup = filters.groupId && filters.groupId !== '__ungrouped__' ? filters.groupId : ''

  return (
    <div className="space-y-2">
      <div
        className="flex items-center gap-1.5 cursor-pointer select-none text-xs text-[var(--color-text-muted)] hover:text-[var(--color-text-primary)] transition-colors"
        onClick={() => setCollapsed(prev => !prev)}
      >
        {collapsed ? <ChevronRight className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
        <Filter className="w-3.5 h-3.5" />
        <span>筛选</span>
        {collapsed && activeCount > 0 && (
          <span className="ml-1 px-1.5 py-0.5 text-[10px] font-medium bg-[var(--color-accent)]/10 text-[var(--color-accent)] rounded-full">
            {activeCount}
          </span>
        )}
      </div>

      {!collapsed && (
        <>
          <div className="flex items-center gap-2 flex-wrap">
            <Input
              value={filters.keyword}
              onChange={e => set('keyword', e.target.value)}
              placeholder="搜索名称 / 分组 / 标签 / 导入关键字..."
              style={{ width: '260px' }}
            />
            <Select
              value={filters.status}
              onChange={e => set('status', e.target.value as InstanceFilters['status'])}
              options={[
                { value: '', label: '全部状态' },
                { value: 'running', label: '运行中' },
                { value: 'stopped', label: '已停止' },
              ]}
              style={{ width: '120px' }}
            />
            <Select
              value={filters.proxyId}
              onChange={e => set('proxyId', e.target.value)}
              options={[
                { value: '', label: '全部代理' },
                { value: '__none__', label: '无代理' },
                ...proxies.map(p => ({ value: p.proxyId, label: p.proxyName || p.proxyId })),
              ]}
              style={{ width: '150px' }}
            />
            <Select
              value={filters.coreId}
              onChange={e => set('coreId', e.target.value)}
              options={[
                { value: '', label: '全部内核' },
                ...cores.map(c => ({ value: c.coreId, label: c.coreName })),
              ]}
              style={{ width: '140px' }}
            />
            <Select
              value={filters.groupId}
              onChange={e => set('groupId', e.target.value)}
              options={[
                { value: '', label: '全部分组' },
                { value: '__ungrouped__', label: '未分组' },
                ...groups.map(g => ({ value: g.groupId, label: `${g.groupName}（${g.instanceCount}）` })),
              ]}
              style={{ width: '160px' }}
            />
            {onCreateGroup && (
              <button
                onClick={onCreateGroup}
                title="新建分组"
                className="p-1.5 rounded text-[var(--color-text-muted)] hover:text-[var(--color-accent)] hover:bg-[var(--color-bg-muted)] transition-colors"
              >
                <FolderPlus className="w-3.5 h-3.5" />
              </button>
            )}
            {onRenameGroup && selectedRealGroup && (
              <button
                onClick={() => onRenameGroup(selectedRealGroup)}
                title="重命名当前分组"
                className="p-1.5 rounded text-[var(--color-text-muted)] hover:text-[var(--color-accent)] hover:bg-[var(--color-bg-muted)] transition-colors"
              >
                <Pencil className="w-3.5 h-3.5" />
              </button>
            )}
            {onDeleteGroup && selectedRealGroup && (
              <button
                onClick={() => onDeleteGroup(selectedRealGroup)}
                title="删除当前分组"
                className="p-1.5 rounded text-[var(--color-text-muted)] hover:text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors"
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            )}
            <Input
              value={filters.kwSearch}
              onChange={e => set('kwSearch', e.target.value)}
              placeholder="仅关键字精筛（如 Profile ID / 账号 / 域名）..."
              className="flex-1 min-w-[220px]"
            />
            {hasFilter && (
              <button
                onClick={() => onChange({ ...EMPTY_FILTERS, tags: new Set() })}
                className="flex items-center gap-1 px-2 py-1 text-xs text-[var(--color-text-muted)] hover:text-[var(--color-error)] hover:bg-[var(--color-bg-muted)] rounded transition-colors"
              >
                <X className="w-3.5 h-3.5" />
                清除
              </button>
            )}
          </div>
          <TagFilterBar
            tags={allTags}
            selected={filters.tags}
            onChange={tags => set('tags', tags)}
          />
        </>
      )}
    </div>
  )
}
