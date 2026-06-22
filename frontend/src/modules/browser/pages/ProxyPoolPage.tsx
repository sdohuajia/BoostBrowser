import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, Card, ConfirmModal, FormItem, Input, Modal, Select, Switch, Table, Textarea, toast } from '../../../shared/components'
import type { SortOrder, TableColumn } from '../../../shared/components/Table'
import type { BrowserProxy, ProxyIPHealthResult } from '../types'
import { fetchBrowserProxies, fetchBrowserProxyGroups, saveBrowserProxies, browserProxyTestSpeed, browserProxyBatchTestSpeed, browserProxyCheckIPHealth, browserProxyBatchCheckIPHealth, fetchClashImportFromURL } from '../api'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import { SmartAssignProxyModal } from '../components/SmartAssignProxyModal'
import yaml from 'js-yaml'

// 内置代理 ID，不可删除、不可编辑
const BUILTIN_PROXY_IDS = new Set(['__direct__', '__local__'])
const PROXY_LATENCY_CACHE_KEY = 'browser:proxyPool:latencyMap:v1'
const PROXY_IP_HEALTH_CACHE_KEY = 'browser:proxyPool:ipHealthMap:v1'
const PROXY_SOURCE_IGNORED_NAMES_KEY = 'browser:proxyPool:sourceIgnoredProxyNames:v1'
const PROXY_GLOBAL_AUTO_REFRESH_KEY = 'browser:proxyPool:globalAutoRefreshEnabled:v1'
const PROXY_GLOBAL_REFRESH_INTERVAL_KEY = 'browser:proxyPool:globalRefreshIntervalM:v1'
const PROXY_LATENCY_CACHE_TTL_MS = 12 * 60 * 60 * 1000
const PROXY_IP_HEALTH_CACHE_TTL_MS = 12 * 60 * 60 * 1000

const BUILTIN_PROXIES: BrowserProxy[] = [
  { proxyId: '__direct__', proxyName: '直连（不走代理）', proxyConfig: 'direct://' },
  { proxyId: '__local__', proxyName: '本地代理', proxyConfig: 'http://127.0.0.1:7890' },
]

function ensureBuiltinProxies(proxies: BrowserProxy[]): BrowserProxy[] {
  const result = [...proxies]
  for (const builtin of BUILTIN_PROXIES) {
    if (!result.find(p => p.proxyId === builtin.proxyId)) {
      result.unshift(builtin)
    }
  }
  return result
}

interface ClashProxy {
  name: string
  type: string
  server: string
  port: number
  [key: string]: any
}

type ProxyImportMode = 'clash' | 'direct'

interface DirectImportForm {
  proxyName: string
  protocol: 'http' | 'https' | 'socks5'
  server: string
  port: string
  username: string
  password: string
}

const DIRECT_PROXY_PROTOCOL_OPTIONS = [
  { value: 'http', label: 'HTTP' },
  { value: 'https', label: 'HTTPS' },
  { value: 'socks5', label: 'SOCKS5' },
] as const

const INITIAL_DIRECT_IMPORT_FORM: DirectImportForm = {
  proxyName: '',
  protocol: 'http',
  server: '',
  port: '',
  username: '',
  password: '',
}

interface ImportCandidate {
  proxyName: string
  proxyConfig: string
}

interface ProxyDisplayInfo {
  proxyId: string
  proxyName: string
  proxyConfig: string
  groupName: string
  sourceId: string
  sourceUrl: string
  sourceAutoRefresh: boolean
  sourceRefreshIntervalM: number
  sourceLastRefreshAt: string
  type: string
  server: string
  port: number
  latencyMs?: number
}

interface URLImportSourceMeta {
  sourceId: string
  sourceUrl: string
  sourceNamePrefix: string
  sourceGroupName: string
  sourceDnsServers: string
  sourceAutoRefresh: boolean
  sourceRefreshIntervalM: number
  sourceLastRefreshAt: string
}

function parseProxyInfo(proxyConfig: string): { type: string; server: string; port: number } {
  const cfg = proxyConfig.trim()
  if (cfg === 'direct://') return { type: 'direct', server: '-', port: 0 }
  const urlMatch = cfg.match(/^([a-zA-Z0-9+\-]+):\/\//)
  if (urlMatch) {
    const scheme = urlMatch[1].toLowerCase()
    try {
      const u = new URL(cfg)
      return { type: scheme, server: u.hostname, port: parseInt(u.port) || 0 }
    } catch {
      return { type: scheme, server: '-', port: 0 }
    }
  }
  try {
    const parsed = yaml.load(cfg) as ClashProxy[] | ClashProxy
    const proxy = Array.isArray(parsed) ? parsed[0] : parsed
    return { type: proxy?.type || '-', server: proxy?.server || '-', port: proxy?.port || 0 }
  } catch {
    return { type: '-', server: '-', port: 0 }
  }
}

function toDisplayList(proxies: BrowserProxy[]): ProxyDisplayInfo[] {
  return proxies.map(p => {
    const info = parseProxyInfo(p.proxyConfig)
    return {
      proxyId: p.proxyId,
      proxyName: p.proxyName,
      proxyConfig: p.proxyConfig,
      groupName: p.groupName || '',
      sourceId: p.sourceId || '',
      sourceUrl: p.sourceUrl || '',
      sourceAutoRefresh: !!p.sourceAutoRefresh,
      sourceRefreshIntervalM: Math.max(0, Number(p.sourceRefreshIntervalM || 0)),
      sourceLastRefreshAt: p.sourceLastRefreshAt || '',
      ...info,
    }
  })
}

function proxyToYaml(proxy: ClashProxy): string {
  return yaml.dump([proxy], { flowLevel: -1, lineWidth: -1 }).trim()
}

function quoteYamlScalar(value: string): string {
  const v = value.trim()
  if (!v) return "''"
  return `'${v.replace(/'/g, "''")}'`
}

function normalizeImportedProxyArray(payload: unknown): ClashProxy[] | null {
  const asArray = (input: unknown): ClashProxy[] => {
    if (!Array.isArray(input)) return []
    return input.filter((item): item is ClashProxy => !!item && typeof item === 'object')
  }

  if (Array.isArray(payload)) {
    return asArray(payload)
  }
  if (!payload || typeof payload !== 'object') {
    return null
  }

  const record = payload as Record<string, unknown>
  if (Array.isArray(record.proxies)) {
    return asArray(record.proxies)
  }
  if (Array.isArray(record.proxy)) {
    return asArray(record.proxy)
  }
  if (Array.isArray(record.Proxy)) {
    return asArray(record.Proxy)
  }
  return null
}

function normalizeLooseClashImportText(raw: string): string {
  const normalizedNewline = raw.replace(/\uFEFF/g, '').replace(/\r\n/g, '\n').trim()
  if (!normalizedNewline) return normalizedNewline

  const lines = normalizedNewline.split('\n')
  const fixedLines = lines.map(line => {
    // 容错: -节点名, type: vless, server: ... => - { name: '节点名', type: vless, server: ... }
    const m = line.match(/^(\s*)-\s*([^,{][^,]*?)\s*,\s*(type\s*:.*)$/i)
    if (!m) return line
    const indent = m[1] || ''
    const name = m[2] || ''
    const tail = m[3] || ''
    return `${indent}- { name: ${quoteYamlScalar(name)}, ${tail.trim()} }`
  })

  const hasProxiesRoot = fixedLines.some(line => /^\s*proxies\s*:/.test(line))
  if (hasProxiesRoot) {
    return fixedLines.join('\n')
  }

  const looksLikeProxyList = fixedLines.some(line => /^\s*-\s*/.test(line))
  if (!looksLikeProxyList) {
    return fixedLines.join('\n')
  }

  const indented = fixedLines.map(line => {
    if (!line.trim()) return line
    return `  ${line}`
  })
  return `proxies:\n${indented.join('\n')}`
}

function parseClashImportText(raw: string): ClashProxy[] {
  const input = raw.trim()
  if (!input) {
    throw new Error('请输入 YAML 内容')
  }

  const attempts = [input]
  const normalized = normalizeLooseClashImportText(input)
  if (normalized && normalized !== input) {
    attempts.push(normalized)
  }

  let lastError: unknown = null
  for (const text of attempts) {
    try {
      const parsed = yaml.load(text)
      const proxies = normalizeImportedProxyArray(parsed)
      if (proxies) {
        return proxies
      }
    } catch (error) {
      lastError = error
    }
  }

  if (lastError && typeof lastError === 'object' && lastError !== null && 'message' in lastError) {
    throw new Error(String((lastError as { message?: string }).message || '解析失败'))
  }
  throw new Error('无效的 YAML 格式，需要包含 proxies 数组')
}

function normalizeDirectProxyConfig(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return ''
  if (/^socket:\/\//i.test(trimmed)) {
    return trimmed.replace(/^socket:\/\//i, 'socks5://')
  }
  if (/^socks:\/\//i.test(trimmed)) {
    return trimmed.replace(/^socks:\/\//i, 'socks5://')
  }
  return trimmed
}

function resolveDirectProxyName(rawName: string, scheme: string, server: string, port: number, index: number, prefix: string): string {
  const name = rawName.trim()
  const fallbackName = server
    ? `${scheme.toUpperCase()}-${server}${port > 0 ? `:${port}` : ''}`
    : `导入代理 ${index + 1}`
  const finalName = name || fallbackName
  return prefix ? `${prefix}-${finalName}` : finalName
}

function formatDirectProxyHost(raw: string): string {
  const host = raw.trim()
  if (!host) return ''
  if (host.startsWith('[') && host.endsWith(']')) {
    return host
  }
  return host.includes(':') ? `[${host}]` : host
}

function buildDirectImportCandidate(form: DirectImportForm): ImportCandidate {
  const serverInput = form.server.trim()
  if (!serverInput) {
    throw new Error('请输入代理地址')
  }
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(serverInput)) {
    throw new Error('代理地址只需要填写主机名或 IP，不需要协议头')
  }

  const portInput = form.port.trim()
  if (!portInput) {
    throw new Error('请输入代理端口')
  }
  if (!/^\d+$/.test(portInput)) {
    throw new Error('代理端口必须为数字')
  }

  const port = Number(portInput)
  if (port < 1 || port > 65535) {
    throw new Error('代理端口必须在 1-65535 之间')
  }

  const username = form.username.trim()
  const password = form.password
  if (password && !username) {
    throw new Error('填写密码时请同时填写账号')
  }

  const auth = username
    ? `${encodeURIComponent(username)}${password ? `:${encodeURIComponent(password)}` : ''}@`
    : ''
  const rawConfig = `${form.protocol}://${auth}${formatDirectProxyHost(serverInput)}:${port}`

  let parsedURL: URL
  try {
    parsedURL = new URL(rawConfig)
  } catch {
    throw new Error('请输入有效的代理地址')
  }

  if (!parsedURL.hostname) {
    throw new Error('请输入有效的代理地址')
  }

  const normalizedConfig = normalizeDirectProxyConfig(parsedURL.toString()).replace(/\/$/, '')
  const normalizedServer = parsedURL.hostname.replace(/^\[(.*)\]$/, '$1')

  return {
    proxyName: resolveDirectProxyName(form.proxyName, form.protocol, normalizedServer, port, 0, ''),
    proxyConfig: normalizedConfig,
  }
}

function normalizeDirectProxyLine(raw: string): string {
  let line = raw.trim()
  if (!line || line.startsWith('#')) return ''
  line = normalizeDirectProxyConfig(line)
  if (!/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(line)) {
    line = `http://${line}`
  }
  return line
}

function parseDirectProxyLine(raw: string, index: number, prefix: string): ImportCandidate | null {
  const normalized = normalizeDirectProxyLine(raw)
  if (!normalized) return null

  let parsedURL: URL
  try {
    parsedURL = new URL(normalized)
  } catch {
    throw new Error(`第 ${index + 1} 行代理格式无效`)
  }

  const scheme = parsedURL.protocol.replace(':', '').toLowerCase()
  if (!['http', 'https', 'socks5'].includes(scheme)) {
    throw new Error(`第 ${index + 1} 行协议不支持，仅支持 http / https / socks5`)
  }
  if (!parsedURL.hostname) {
    throw new Error(`第 ${index + 1} 行缺少代理地址`)
  }
  if (!parsedURL.port || !/^\d+$/.test(parsedURL.port)) {
    throw new Error(`第 ${index + 1} 行缺少代理端口`)
  }

  const port = Number(parsedURL.port)
  if (port < 1 || port > 65535) {
    throw new Error(`第 ${index + 1} 行端口必须在 1-65535 之间`)
  }

  const normalizedConfig = normalizeDirectProxyConfig(parsedURL.toString()).replace(/\/$/, '')
  const normalizedServer = parsedURL.hostname.replace(/^\[(.*)\]$/, '$1')
  return {
    proxyName: resolveDirectProxyName('', scheme, normalizedServer, port, index, prefix),
    proxyConfig: normalizedConfig,
  }
}

function buildDirectImportCandidatesFromText(raw: string, prefix: string): ImportCandidate[] {
  const lines = raw.split(/\r?\n/)
  const result: ImportCandidate[] = []
  lines.forEach((line, index) => {
    const item = parseDirectProxyLine(line, index, prefix)
    if (item) result.push(item)
  })
  return result
}

function buildImportCandidatesFromClash(parsedProxies: ClashProxy[], prefix: string): ImportCandidate[] {
  return parsedProxies.map((proxy, index) => ({
    proxyName: resolveImportedProxyName(proxy, index, prefix),
    proxyConfig: proxyToYaml(proxy),
  }))
}

function buildImportPreview(candidates: ImportCandidate[], groupName: string): ProxyDisplayInfo[] {
  return candidates.map((candidate, index) => {
    const info = parseProxyInfo(candidate.proxyConfig)
    return {
      proxyId: `preview-${index}`,
      proxyName: candidate.proxyName,
      proxyConfig: candidate.proxyConfig,
      groupName,
      sourceId: '',
      sourceUrl: '',
      sourceAutoRefresh: false,
      sourceRefreshIntervalM: 0,
      sourceLastRefreshAt: '',
      type: info.type || '-',
      server: info.server || '-',
      port: info.port || 0,
    }
  })
}

function parseTimestampMs(value: string): number {
  const v = (value || '').trim()
  if (!v) return 0
  const t = Date.parse(v)
  return Number.isFinite(t) ? t : 0
}

function normalizeRefreshIntervalM(value: number): number {
  if (!Number.isFinite(value)) return 0
  if (value <= 0) return 0
  if (value < 5) return 5
  if (value > 24 * 60) return 24 * 60
  return Math.round(value)
}

function sourceHostLabel(sourceURL: string): string {
  const raw = (sourceURL || '').trim()
  if (!raw) return ''
  try {
    const u = new URL(raw)
    return u.host || raw
  } catch {
    return raw
  }
}

function normalizeSourceURL(sourceURL: string): string {
  const raw = (sourceURL || '').trim()
  if (!raw) return ''
  try {
    const parsed = new URL(raw)
    parsed.hash = ''
    return parsed.toString()
  } catch {
    return raw
  }
}

function buildStableSourceID(sourceURL: string, sourceNamePrefix: string): string {
  const key = `${normalizeSourceURL(sourceURL)}|||${sourceNamePrefix.trim()}`
  // djb2 变体，输出稳定且实现简单。
  let hash = 5381
  for (let i = 0; i < key.length; i += 1) {
    hash = ((hash << 5) + hash) ^ key.charCodeAt(i)
  }
  const unsigned = hash >>> 0
  return `src-${unsigned.toString(36)}`
}

function resolveImportSourceID(list: BrowserProxy[], sourceURL: string, sourceNamePrefix: string): string {
  const normalizedURL = normalizeSourceURL(sourceURL)
  const normalizedPrefix = sourceNamePrefix.trim()
  const existing = list.find(item =>
    normalizeSourceURL(item.sourceUrl || '') === normalizedURL &&
    (item.sourceNamePrefix || '').trim() === normalizedPrefix &&
    (item.sourceId || '').trim() !== ''
  )
  if (existing?.sourceId?.trim()) {
    return existing.sourceId.trim()
  }
  return buildStableSourceID(sourceURL, sourceNamePrefix)
}

function collectURLImportSources(list: BrowserProxy[]): URLImportSourceMeta[] {
  const sourceMap = new Map<string, URLImportSourceMeta>()
  for (const item of list) {
    const sourceId = (item.sourceId || '').trim()
    const sourceUrl = (item.sourceUrl || '').trim()
    if (!sourceId || !sourceUrl) continue

    const last = sourceMap.get(sourceId)
    const currentLastRefreshAt = item.sourceLastRefreshAt || ''
    if (!last) {
      sourceMap.set(sourceId, {
        sourceId,
        sourceUrl,
        sourceNamePrefix: (item.sourceNamePrefix || '').trim(),
        sourceGroupName: (item.groupName || '').trim(),
        sourceDnsServers: (item.dnsServers || '').trim(),
        sourceAutoRefresh: !!item.sourceAutoRefresh,
        sourceRefreshIntervalM: normalizeRefreshIntervalM(Number(item.sourceRefreshIntervalM || 0)),
        sourceLastRefreshAt: currentLastRefreshAt,
      })
      continue
    }

    if (
      parseTimestampMs(currentLastRefreshAt) > parseTimestampMs(last.sourceLastRefreshAt) &&
      currentLastRefreshAt.trim()
    ) {
      last.sourceLastRefreshAt = currentLastRefreshAt
    }
  }
  return Array.from(sourceMap.values())
}

function nextProxyID(): string {
  return `proxy-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

function resolveImportedProxyName(proxy: ClashProxy, index: number, prefix: string): string {
  const rawName = (proxy.name || '').trim() || `导入代理 ${index + 1}`
  return prefix ? `${prefix}-${rawName}` : rawName
}

function createExistingProxyIDPicker(oldSourceProxies: BrowserProxy[]) {
  const exactMap = new Map<string, BrowserProxy[]>()
  const nameMap = new Map<string, BrowserProxy[]>()
  oldSourceProxies.forEach(item => {
    const exactKey = `${item.proxyName}|||${item.proxyConfig}`
    const exactList = exactMap.get(exactKey) || []
    exactList.push(item)
    exactMap.set(exactKey, exactList)

    const nameKey = item.proxyName
    const nameList = nameMap.get(nameKey) || []
    nameList.push(item)
    nameMap.set(nameKey, nameList)
  })

  return (name: string, configText: string): string | null => {
    const exactKey = `${name}|||${configText}`
    const exactList = exactMap.get(exactKey)
    if (exactList && exactList.length > 0) {
      const item = exactList.shift()
      if (item?.proxyId) return item.proxyId
    }

    const nameList = nameMap.get(name)
    if (nameList && nameList.length > 0) {
      const item = nameList.shift()
      if (item?.proxyId) return item.proxyId
    }
    return null
  }
}

function buildRefreshedSourceProxies(
  parsedProxies: ClashProxy[],
  oldSourceProxies: BrowserProxy[],
  meta: URLImportSourceMeta,
  refreshedAt: string
): BrowserProxy[] {
  const pickExisting = createExistingProxyIDPicker(oldSourceProxies)

  const prefix = meta.sourceNamePrefix.trim()
  const sourceGroupName = meta.sourceGroupName.trim()
  const sourceDnsServers = meta.sourceDnsServers.trim()
  const refreshed: BrowserProxy[] = []

  parsedProxies.forEach((proxy, idx) => {
    const proxyName = resolveImportedProxyName(proxy, idx, prefix)
    const proxyConfig = proxyToYaml(proxy)
    const proxyId = pickExisting(proxyName, proxyConfig) || nextProxyID()

    refreshed.push({
      proxyId,
      proxyName,
      proxyConfig,
      dnsServers: sourceDnsServers || undefined,
      groupName: sourceGroupName || undefined,
      sourceId: meta.sourceId,
      sourceUrl: meta.sourceUrl,
      sourceNamePrefix: prefix || undefined,
      sourceAutoRefresh: meta.sourceAutoRefresh,
      sourceRefreshIntervalM: meta.sourceRefreshIntervalM,
      sourceLastRefreshAt: refreshedAt,
    })
  })

  return refreshed
}

function readSourceIgnoredProxyNames(): Record<string, string[]> {
  try {
    const raw = localStorage.getItem(PROXY_SOURCE_IGNORED_NAMES_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return {}
    const cleaned: Record<string, string[]> = {}
    Object.entries(parsed as Record<string, unknown>).forEach(([sourceId, value]) => {
      if (!sourceId.trim() || !Array.isArray(value)) return
      const names = value
        .map(item => (typeof item === 'string' ? item.trim() : ''))
        .filter(Boolean)
      if (names.length > 0) {
        cleaned[sourceId] = names
      }
    })
    return cleaned
  } catch {
    return {}
  }
}

function writeSourceIgnoredProxyNames(data: Record<string, string[]>) {
  try {
    const cleaned: Record<string, string[]> = {}
    Object.entries(data).forEach(([sourceId, names]) => {
      const key = sourceId.trim()
      if (!key || !Array.isArray(names)) return
      const validNames = names.map(name => (name || '').trim()).filter(Boolean)
      if (validNames.length > 0) {
        cleaned[key] = validNames
      }
    })
    localStorage.setItem(PROXY_SOURCE_IGNORED_NAMES_KEY, JSON.stringify(cleaned))
  } catch {
    // ignore write failures
  }
}

function appendSourceIgnoredProxyNames(sourceId: string, names: string[]) {
  const sourceKey = sourceId.trim()
  if (!sourceKey || names.length === 0) return
  const cleaned = names.map(name => name.trim()).filter(Boolean)
  if (cleaned.length === 0) return

  const existing = readSourceIgnoredProxyNames()
  existing[sourceKey] = [...(existing[sourceKey] || []), ...cleaned]
  writeSourceIgnoredProxyNames(existing)
}

function applyIgnoredProxyNamesForSource(
  parsedProxies: ClashProxy[],
  sourceNamePrefix: string,
  ignoredProxyNames: string[]
): ClashProxy[] {
  if (ignoredProxyNames.length === 0) return parsedProxies
  const ignoredCounter = new Map<string, number>()
  ignoredProxyNames.forEach(name => {
    const key = name.trim()
    if (!key) return
    ignoredCounter.set(key, (ignoredCounter.get(key) || 0) + 1)
  })
  if (ignoredCounter.size === 0) return parsedProxies

  return parsedProxies.filter((proxy, idx) => {
    const proxyName = resolveImportedProxyName(proxy, idx, sourceNamePrefix)
    const count = ignoredCounter.get(proxyName) || 0
    if (count <= 0) return true
    if (count === 1) {
      ignoredCounter.delete(proxyName)
    } else {
      ignoredCounter.set(proxyName, count - 1)
    }
    return false
  })
}

function readGlobalRefreshConfig(): { enabled: boolean; intervalM: number } {
  try {
    const rawEnabled = localStorage.getItem(PROXY_GLOBAL_AUTO_REFRESH_KEY)
    const rawInterval = localStorage.getItem(PROXY_GLOBAL_REFRESH_INTERVAL_KEY)
    const enabled = rawEnabled === '1'
    const interval = normalizeRefreshIntervalM(Number(rawInterval || 0))
    return {
      enabled,
      intervalM: interval > 0 ? interval : 60,
    }
  } catch {
    return { enabled: false, intervalM: 60 }
  }
}

function writeGlobalRefreshConfig(enabled: boolean, intervalM: number) {
  try {
    localStorage.setItem(PROXY_GLOBAL_AUTO_REFRESH_KEY, enabled ? '1' : '0')
    localStorage.setItem(PROXY_GLOBAL_REFRESH_INTERVAL_KEY, String(intervalM))
  } catch {
    // ignore write failures
  }
}

function toLatencyValue(ok: boolean, latencyMs: number, error?: string): number {
  if (ok) return latencyMs
  return error?.includes('不支持') ? -3 : -2
}

function readLatencyCache(): Record<string, number> {
  try {
    const raw = localStorage.getItem(PROXY_LATENCY_CACHE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as { timestamp?: number; data?: Record<string, number> }
    if (!parsed?.timestamp || !parsed?.data) return {}
    if (Date.now() - parsed.timestamp > PROXY_LATENCY_CACHE_TTL_MS) return {}
    const cleaned: Record<string, number> = {}
    Object.entries(parsed.data).forEach(([proxyId, latency]) => {
      if (typeof latency === 'number' && Number.isFinite(latency) && latency !== -1) {
        cleaned[proxyId] = latency
      }
    })
    return cleaned
  } catch {
    return {}
  }
}

function writeLatencyCache(data: Record<string, number>) {
  try {
    const cleaned: Record<string, number> = {}
    Object.entries(data).forEach(([proxyId, latency]) => {
      if (typeof latency === 'number' && Number.isFinite(latency) && latency !== -1) {
        cleaned[proxyId] = latency
      }
    })
    localStorage.setItem(PROXY_LATENCY_CACHE_KEY, JSON.stringify({
      timestamp: Date.now(),
      data: cleaned,
    }))
  } catch {
    // ignore write failures
  }
}

function readIPHealthCache(): Record<string, ProxyIPHealthResult> {
  try {
    const raw = localStorage.getItem(PROXY_IP_HEALTH_CACHE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as { timestamp?: number; data?: Record<string, ProxyIPHealthResult> }
    if (!parsed?.timestamp || !parsed?.data) return {}
    if (Date.now() - parsed.timestamp > PROXY_IP_HEALTH_CACHE_TTL_MS) return {}
    const cleaned: Record<string, ProxyIPHealthResult> = {}
    Object.entries(parsed.data).forEach(([proxyId, item]) => {
      if (item && typeof item === 'object') {
        cleaned[proxyId] = item
      }
    })
    return cleaned
  } catch {
    return {}
  }
}

function writeIPHealthCache(data: Record<string, ProxyIPHealthResult>) {
  try {
    localStorage.setItem(PROXY_IP_HEALTH_CACHE_KEY, JSON.stringify({
      timestamp: Date.now(),
      data,
    }))
  } catch {
    // ignore write failures
  }
}

export function ProxyPoolPage() {
  const [proxies, setProxies] = useState<BrowserProxy[]>([])
  const [displayList, setDisplayList] = useState<ProxyDisplayInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [groups, setGroups] = useState<string[]>([])

  const [filterProtocol, setFilterProtocol] = useState<string>('all')
  const [filterKeyword, setFilterKeyword] = useState('')
  const [filterGroup, setFilterGroup] = useState<string>('all')
  const [sortColumn, setSortColumn] = useState<string>('') // 默认不排序
  const [sortOrder, setSortOrder] = useState<SortOrder>(undefined)

  const [latencyMap, setLatencyMap] = useState<Record<string, number>>({})
  const [testingAll, setTestingAll] = useState(false)
  const [ipHealthMap, setIPHealthMap] = useState<Record<string, ProxyIPHealthResult>>({})
  const [checkingIPHealthIds, setCheckingIPHealthIds] = useState<Set<string>>(new Set())
  const [checkingAllIPHealth, setCheckingAllIPHealth] = useState(false)

  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [batchDeleteConfirmOpen, setBatchDeleteConfirmOpen] = useState(false)
  const [smartAssignOpen, setSmartAssignOpen] = useState(false)

  const [importModalOpen, setImportModalOpen] = useState(false)
  const [importMode, setImportMode] = useState<ProxyImportMode>('clash')
  const [importUrl, setImportUrl] = useState('')
  const [importResolvedUrl, setImportResolvedUrl] = useState('')
  const [importText, setImportText] = useState('')
  const [importDnsServers, setImportDnsServers] = useState('')
  const [importNamePrefix, setImportNamePrefix] = useState('')
  const [importGroupName, setImportGroupName] = useState('')
  const [directImportForm, setDirectImportForm] = useState<DirectImportForm>(() => ({ ...INITIAL_DIRECT_IMPORT_FORM }))
  const [directImportUrl, setDirectImportUrl] = useState('')
  const [directImportBatchText, setDirectImportBatchText] = useState('')
  const [previewModalOpen, setPreviewModalOpen] = useState(false)
  const [previewList, setPreviewList] = useState<ProxyDisplayInfo[]>([])
  const [removedPreviewProxyNames, setRemovedPreviewProxyNames] = useState<string[]>([])
  const [importing, setImporting] = useState(false)
  const [fetchingImportUrl, setFetchingImportUrl] = useState(false)
  const [refreshingAllSources, setRefreshingAllSources] = useState(false)
  const [refreshingSourceIds, setRefreshingSourceIds] = useState<Set<string>>(new Set())
  const [globalAutoRefreshEnabled, setGlobalAutoRefreshEnabled] = useState(false)
  const [globalRefreshIntervalM, setGlobalRefreshIntervalM] = useState('60')

  const [editModalOpen, setEditModalOpen] = useState(false)
  const [editingProxy, setEditingProxy] = useState<BrowserProxy | null>(null)
  const [editForm, setEditForm] = useState({ proxyName: '', proxyConfig: '', dnsServers: '', groupName: '' })
  const [saving, setSaving] = useState(false)

  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [deletingId, setDeletingId] = useState<string | null>(null)
  const [ipHealthDetailOpen, setIPHealthDetailOpen] = useState(false)
  const [currentIPHealthDetail, setCurrentIPHealthDetail] = useState<ProxyIPHealthResult | null>(null)
  const proxiesRef = useRef<BrowserProxy[]>([])
  const refreshingSourceIdsRef = useRef<Set<string>>(new Set())
  const autoRefreshRunningRef = useRef(false)
  const globalRefreshInterval = useMemo(() => {
    const interval = normalizeRefreshIntervalM(Number(globalRefreshIntervalM || 0))
    return interval > 0 ? interval : 60
  }, [globalRefreshIntervalM])

  useEffect(() => {
    const cfg = readGlobalRefreshConfig()
    setGlobalAutoRefreshEnabled(cfg.enabled)
    setGlobalRefreshIntervalM(String(cfg.intervalM))
    setLatencyMap(readLatencyCache())
    setIPHealthMap(readIPHealthCache())
    loadProxies()
  }, [])

  useEffect(() => {
    writeLatencyCache(latencyMap)
  }, [latencyMap])

  useEffect(() => {
    writeIPHealthCache(ipHealthMap)
  }, [ipHealthMap])

  useEffect(() => {
    writeGlobalRefreshConfig(globalAutoRefreshEnabled, globalRefreshInterval)
  }, [globalAutoRefreshEnabled, globalRefreshInterval])

  useEffect(() => {
    proxiesRef.current = proxies
  }, [proxies])

  useEffect(() => {
    refreshingSourceIdsRef.current = refreshingSourceIds
  }, [refreshingSourceIds])

  useEffect(() => {
    if (!proxies.length) return
    const validIds = new Set(proxies.map(p => p.proxyId))
    setLatencyMap(prev => {
      let changed = false
      const next: Record<string, number> = {}
      Object.entries(prev).forEach(([proxyId, latency]) => {
        if (validIds.has(proxyId)) {
          next[proxyId] = latency
        } else {
          changed = true
        }
      })
      return changed ? next : prev
    })

    setIPHealthMap(prev => {
      let changed = false
      const next: Record<string, ProxyIPHealthResult> = {}
      Object.entries(prev).forEach(([proxyId, health]) => {
        if (validIds.has(proxyId)) {
          next[proxyId] = health
        } else {
          changed = true
        }
      })
      return changed ? next : prev
    })
  }, [proxies])

  const loadProxies = async () => {
    setLoading(true)
    try {
      const raw = await fetchBrowserProxies()
      const proxyList = ensureBuiltinProxies(raw)
      const persistedLatency: Record<string, number> = {}
      const persistedIPHealth: Record<string, ProxyIPHealthResult> = {}
      proxyList.forEach(proxy => {
        if (proxy.lastTestedAt) {
          persistedLatency[proxy.proxyId] = (proxy.lastTestOk ?? false)
            ? (proxy.lastLatencyMs ?? -2)
            : -2
        }
        if (proxy.lastIPHealthJson) {
          try {
            const parsed = JSON.parse(proxy.lastIPHealthJson) as ProxyIPHealthResult
            if (parsed && typeof parsed === 'object' && parsed.proxyId) {
              persistedIPHealth[proxy.proxyId] = parsed
            }
          } catch {
            // ignore bad historical json
          }
        }
      })

      setProxies(proxyList)
      setDisplayList(toDisplayList(proxyList))
      setLatencyMap(prev => ({ ...persistedLatency, ...prev }))
      setIPHealthMap(prev => ({ ...persistedIPHealth, ...prev }))
      const grps = await fetchBrowserProxyGroups()
      setGroups(grps)
    } finally {
      setLoading(false)
    }
  }

  // 直接保存完整列表，内置代理保护由后端负责
  const saveProxies = useCallback(async (list: BrowserProxy[]) => {
    await saveBrowserProxies(list)
    setProxies(list)
    setDisplayList(toDisplayList(list))
    // 刷新分组列表（可能有新分组加入）
    const grps = await fetchBrowserProxyGroups()
    setGroups(grps)
  }, [])

  const sourceMetas = useMemo(() => collectURLImportSources(proxies), [proxies])
  const hasURLImportSources = sourceMetas.length > 0

  const refreshSingleSource = useCallback(async (sourceId: string, silent: boolean) => {
    const currentList = proxiesRef.current
    const metas = collectURLImportSources(currentList)
    const meta = metas.find(item => item.sourceId === sourceId)
    if (!meta) return false

    if (refreshingSourceIdsRef.current.has(sourceId)) return false
    setRefreshingSourceIds(prev => {
      const next = new Set(prev)
      next.add(sourceId)
      return next
    })

    try {
      const result = await fetchClashImportFromURL(meta.sourceUrl)
      const parsed = parseClashImportText(result.content || '')
      if (!parsed.length) {
        throw new Error('订阅内容未解析到可用代理')
      }
      const ignoredNameMap = readSourceIgnoredProxyNames()
      const sourceIgnoredNames = ignoredNameMap[sourceId] || []
      const filteredParsed = applyIgnoredProxyNamesForSource(parsed, meta.sourceNamePrefix, sourceIgnoredNames)

      const latest = proxiesRef.current
      const oldSourceProxies = latest.filter(item => (item.sourceId || '').trim() === sourceId)
      const refreshedAt = new Date().toISOString()
      const effectiveMeta: URLImportSourceMeta = {
        ...meta,
        sourceAutoRefresh: globalAutoRefreshEnabled,
        sourceRefreshIntervalM: globalRefreshInterval,
      }
      const refreshedSourceProxies = buildRefreshedSourceProxies(filteredParsed, oldSourceProxies, effectiveMeta, refreshedAt)

      const merged = latest
        .filter(item => (item.sourceId || '').trim() !== sourceId)
        .concat(refreshedSourceProxies)

      await saveProxies(merged)
      if (!silent) {
        toast.success(`订阅刷新成功：${meta.sourceUrl}（${refreshedSourceProxies.length} 条）`)
      }
      return true
    } catch (error: any) {
      if (!silent) {
        toast.error(error?.message || '订阅刷新失败')
      }
      return false
    } finally {
      setRefreshingSourceIds(prev => {
        const next = new Set(prev)
        next.delete(sourceId)
        return next
      })
    }
  }, [globalAutoRefreshEnabled, globalRefreshInterval, saveProxies])

  const handleRefreshAllSources = useCallback(async (silent = false) => {
    const metas = collectURLImportSources(proxiesRef.current)
    if (metas.length === 0) {
      if (!silent) {
        toast.info('当前没有 URL 导入订阅')
      }
      return
    }

    setRefreshingAllSources(true)
    let successCount = 0
    for (const meta of metas) {
      // 串行刷新，避免并发保存导致覆盖
      // eslint-disable-next-line no-await-in-loop
      const ok = await refreshSingleSource(meta.sourceId, true)
      if (ok) successCount += 1
    }
    setRefreshingAllSources(false)

    if (!silent) {
      if (successCount === metas.length) {
        toast.success(`订阅刷新完成：${successCount}/${metas.length}`)
      } else {
        toast.warning(`订阅刷新完成：成功 ${successCount}/${metas.length}`)
      }
    }
  }, [refreshSingleSource])

  useEffect(() => {
    const runAutoRefresh = async () => {
      if (autoRefreshRunningRef.current || refreshingAllSources) {
        return
      }
      if (!globalAutoRefreshEnabled) {
        return
      }
      const intervalMs = globalRefreshInterval * 60 * 1000
      const metas = collectURLImportSources(proxiesRef.current).filter(meta => {
        if (!meta.sourceUrl.trim()) return false
        const last = parseTimestampMs(meta.sourceLastRefreshAt)
        return last <= 0 || Date.now() - last >= intervalMs
      })
      if (metas.length === 0) {
        return
      }

      autoRefreshRunningRef.current = true
      try {
        for (const meta of metas) {
          // eslint-disable-next-line no-await-in-loop
          await refreshSingleSource(meta.sourceId, true)
        }
      } finally {
        autoRefreshRunningRef.current = false
      }
    }

    void runAutoRefresh()
    const timer = window.setInterval(() => {
      void runAutoRefresh()
    }, 60 * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [globalAutoRefreshEnabled, globalRefreshInterval, refreshingAllSources, refreshSingleSource])

  const protocolOptions = useMemo(
    () => ['all', ...Array.from(new Set(displayList.map(p => p.type).filter(t => t !== '-')))],
    [displayList]
  )

  const getLatencySortTuple = (proxyId: string): [number, number] => {
    const v = latencyMap[proxyId]
    if (v === undefined) return [4, Number.MAX_SAFE_INTEGER]
    if (v === -1) return [1, Number.MAX_SAFE_INTEGER] // 测试中
    if (v === -2) return [2, Number.MAX_SAFE_INTEGER] // 超时
    if (v === -3) return [3, Number.MAX_SAFE_INTEGER] // 不支持
    return [0, v] // 正常延迟
  }

  const compareText = (a: string, b: string) => a.localeCompare(b, 'zh-CN')

  const compareByColumn = (a: ProxyDisplayInfo, b: ProxyDisplayInfo, column: string) => {
    switch (column) {
      case 'proxyName':
        return compareText(a.proxyName || '', b.proxyName || '')
      case 'groupName':
        return compareText(a.groupName || '', b.groupName || '')
      case 'type':
        return compareText(a.type || '', b.type || '')
      case 'server':
        return compareText(a.server || '', b.server || '')
      case 'port':
        return (a.port || 0) - (b.port || 0)
      case 'latency': {
        const [rankA, valA] = getLatencySortTuple(a.proxyId)
        const [rankB, valB] = getLatencySortTuple(b.proxyId)
        if (rankA !== rankB) return rankA - rankB
        if (valA !== valB) return valA - valB
        return compareText(a.proxyName || '', b.proxyName || '')
      }
      default:
        return 0
    }
  }

  const filteredList = useMemo(() => {
    const filtered = displayList.filter(p => {
      const matchProtocol = filterProtocol === 'all' || p.type === filterProtocol
      const matchKeyword = !filterKeyword || p.proxyName.toLowerCase().includes(filterKeyword.toLowerCase()) || p.server.toLowerCase().includes(filterKeyword.toLowerCase())
      const matchGroup = filterGroup === 'all' || p.groupName === filterGroup
      return matchProtocol && matchKeyword && matchGroup
    })

    if (!sortColumn || !sortOrder) return filtered

    return [...filtered].sort((a, b) => {
      const cmp = compareByColumn(a, b, sortColumn)
      return sortOrder === 'asc' ? cmp : -cmp
    })
  }, [displayList, filterProtocol, filterKeyword, filterGroup, sortColumn, sortOrder, latencyMap])

  const allFilteredSelected = filteredList.length > 0 && filteredList.every(p => selectedIds.has(p.proxyId))
  const someFilteredSelected = filteredList.some(p => selectedIds.has(p.proxyId))

  const handleToggleAll = () => {
    if (allFilteredSelected) {
      setSelectedIds(prev => {
        const next = new Set(prev)
        filteredList.forEach(p => next.delete(p.proxyId))
        return next
      })
    } else {
      setSelectedIds(prev => {
        const next = new Set(prev)
        filteredList.filter(p => !BUILTIN_PROXY_IDS.has(p.proxyId)).forEach(p => next.add(p.proxyId))
        return next
      })
    }
  }

  const handleToggleOne = (proxyId: string) => {
    if (BUILTIN_PROXY_IDS.has(proxyId)) return
    setSelectedIds(prev => {
      const next = new Set(prev)
      next.has(proxyId) ? next.delete(proxyId) : next.add(proxyId)
      return next
    })
  }

  const handleBatchDeleteConfirm = async () => {
    try {
      const newProxies = proxies.filter(p => !selectedIds.has(p.proxyId))
      await saveProxies(newProxies)
      toast.success(`已删除 ${selectedIds.size} 个代理`)
      setSelectedIds(new Set())
    } catch (error: any) {
      toast.error(error?.message || '删除失败')
    }
  }

  const handleTestOne = async (record: ProxyDisplayInfo) => {
    if (record.proxyConfig === 'direct://') {
      toast.info('直连模式无需测速')
      return
    }
    setLatencyMap(prev => ({ ...prev, [record.proxyId]: -1 }))
    const result = await browserProxyTestSpeed(record.proxyId)
    const val = toLatencyValue(result.ok, result.latencyMs, result.error)
    setLatencyMap(prev => ({ ...prev, [record.proxyId]: val }))
  }

  const handleTestAll = async () => {
    const testable = filteredList.filter(p => p.proxyConfig !== 'direct://')
    if (testable.length === 0) return
    setTestingAll(true)
    const init: Record<string, number> = {}
    testable.forEach(p => { init[p.proxyId] = -1 })
    setLatencyMap(prev => ({ ...prev, ...init }))

    // 监听后端实时推送的单个测速结果
    const off = EventsOn('proxy:speed:result', (data: { proxyId: string; ok: boolean; latencyMs: number; error: string }) => {
      const val = toLatencyValue(data.ok, data.latencyMs, data.error)
      setLatencyMap(prev => ({ ...prev, [data.proxyId]: val }))
    })

    try {
      const proxyIds = testable.map(p => p.proxyId)
      const results = await browserProxyBatchTestSpeed(proxyIds, 4)
      setLatencyMap(prev => {
        const next = { ...prev }
        results.forEach(result => {
          next[result.proxyId] = toLatencyValue(result.ok, result.latencyMs, result.error)
        })
        return next
      })
    } finally {
      off()
      setTestingAll(false)
    }
  }

  const handleCheckOneIPHealth = async (record: ProxyDisplayInfo) => {
    if (record.proxyConfig === 'direct://') {
      toast.info('直连模式无需检测')
      return
    }
    if (checkingIPHealthIds.has(record.proxyId)) return

    setCheckingIPHealthIds(prev => new Set(prev).add(record.proxyId))
    try {
      const result = await browserProxyCheckIPHealth(record.proxyId)
      setIPHealthMap(prev => ({ ...prev, [record.proxyId]: result }))
      if (!result.ok) {
        toast.error(result.error || `${record.proxyName} 检测失败`)
      }
    } finally {
      setCheckingIPHealthIds(prev => {
        const next = new Set(prev)
        next.delete(record.proxyId)
        return next
      })
    }
  }

  const handleCheckAllIPHealth = async () => {
    const testable = filteredList.filter(p => p.proxyConfig !== 'direct://')
    if (testable.length === 0) return
    setCheckingAllIPHealth(true)

    const ids = testable.map(p => p.proxyId)
    const idSet = new Set(ids)
    setCheckingIPHealthIds(prev => new Set([...Array.from(prev), ...ids]))

    const off = EventsOn('proxy:iphealth:result', (data: ProxyIPHealthResult) => {
      if (!data?.proxyId || !idSet.has(data.proxyId)) return
      setIPHealthMap(prev => ({ ...prev, [data.proxyId]: data }))
      setCheckingIPHealthIds(prev => {
        const next = new Set(prev)
        next.delete(data.proxyId)
        return next
      })
    })

    try {
      const results = await browserProxyBatchCheckIPHealth(ids, 4)
      setIPHealthMap(prev => {
        const next = { ...prev }
        results.forEach(result => {
          if (result?.proxyId && idSet.has(result.proxyId)) {
            next[result.proxyId] = result
          }
        })
        return next
      })
      const failed = results.filter(r => !r.ok).length
      if (failed > 0) {
        toast.info(`IP 健康检测完成：成功 ${results.length - failed}，失败 ${failed}`)
      } else {
        toast.success(`IP 健康检测完成：共 ${results.length} 条`)
      }
    } finally {
      off()
      setCheckingIPHealthIds(prev => {
        const next = new Set(prev)
        ids.forEach(id => next.delete(id))
        return next
      })
      setCheckingAllIPHealth(false)
    }
  }

  const renderLatency = (record: ProxyDisplayInfo) => {
    if (record.proxyConfig === 'direct://') {
      return <span className="text-[var(--color-text-muted)] text-xs">不适用</span>
    }
    const val = latencyMap[record.proxyId]
    if (val === undefined) return <span className="text-[var(--color-text-muted)] text-xs">-</span>
    if (val === -1) return <span className="text-[var(--color-text-muted)] text-xs animate-pulse">测试中...</span>
    if (val === -2) return <span className="text-red-500 text-xs">超时</span>
    if (val === -3) return <span className="text-gray-400 text-xs">不支持</span>
    const color = val < 200 ? 'text-green-500' : val < 500 ? 'text-yellow-500' : 'text-red-500'
    return <span className={`text-xs font-medium ${color}`}>{val} ms</span>
  }

  const openIPHealthDetail = (proxyId: string) => {
    const result = ipHealthMap[proxyId]
    if (!result) return
    setCurrentIPHealthDetail(result)
    setIPHealthDetailOpen(true)
  }

  const renderIPHealth = (record: ProxyDisplayInfo) => {
    if (record.proxyConfig === 'direct://') {
      return <span className="text-[var(--color-text-muted)] text-xs">不适用</span>
    }
    if (checkingIPHealthIds.has(record.proxyId)) {
      return <span className="text-[var(--color-text-muted)] text-xs animate-pulse">检测中...</span>
    }

    const result = ipHealthMap[record.proxyId]
    if (!result) return <span className="text-[var(--color-text-muted)] text-xs">-</span>
    if (!result.ok) {
      return (
        <div className="flex items-center gap-2">
          <span className="text-xs text-red-500 truncate max-w-[120px]" title={result.error || '检测失败'}>失败</span>
          <Button size="sm" variant="ghost" onClick={(e) => { e.stopPropagation(); openIPHealthDetail(record.proxyId) }}>原始</Button>
        </div>
      )
    }

    const location = [result.country, result.region, result.city].filter(Boolean).join(' / ')
    return (
      <div className="flex items-center gap-2 min-w-0">
        <div className="min-w-0">
          <div className="text-xs text-[var(--color-text-primary)] truncate">{result.ip || '-'}</div>
          <div className="text-[11px] text-[var(--color-text-muted)] truncate">
            {`fraud ${result.fraudScore} | ${result.isResidential ? '住宅' : '机房'}${location ? ` | ${location}` : ''}`}
          </div>
        </div>
        <Button size="sm" variant="ghost" onClick={(e) => { e.stopPropagation(); openIPHealthDetail(record.proxyId) }}>原始</Button>
      </div>
    )
  }

  const columns: TableColumn<ProxyDisplayInfo>[] = [
    {
      key: 'checkbox',
      title: '',
      width: '40px',
      render: (_, record) => (
        <input
          type="checkbox"
          checked={selectedIds.has(record.proxyId)}
          disabled={BUILTIN_PROXY_IDS.has(record.proxyId)}
          onChange={() => handleToggleOne(record.proxyId)}
          onClick={e => e.stopPropagation()}
          className="w-4 h-4 rounded border-[var(--color-border)] accent-[var(--color-primary)] cursor-pointer disabled:opacity-30 disabled:cursor-not-allowed"
        />
      ),
    },
    { key: 'proxyName', title: '代理名称', width: '180px', sortable: true },
    { key: 'groupName', title: '分组', width: '100px', sortable: true, render: (val) => val ? <span className="px-1.5 py-0.5 text-xs rounded bg-[var(--color-accent)]/10 text-[var(--color-accent)]">{val}</span> : '-' },
    {
      key: 'source',
      title: '来源',
      width: '180px',
      render: (_, record) => {
        if (!record.sourceUrl) return '-'
        const host = sourceHostLabel(record.sourceUrl)
        return (
          <div className="text-xs leading-5">
            <div className="text-[var(--color-text-primary)] truncate" title={record.sourceUrl}>{host}</div>
            <div className="text-[var(--color-text-muted)]">
              {globalAutoRefreshEnabled ? `自动刷新 ${globalRefreshInterval} 分钟（全局）` : '手动刷新'}
            </div>
          </div>
        )
      },
    },
    { key: 'type', title: '类型', width: '90px', sortable: true },
    { key: 'server', title: '服务器', width: '180px', sortable: true },
    { key: 'port', title: '端口', width: '80px', sortable: true, render: (val) => val || '-' },
    {
      key: 'latency',
      title: '延迟',
      width: '90px',
      sortable: true,
      render: (_, record) => renderLatency(record),
    },
    {
      key: 'ipHealth',
      title: 'IP健康',
      width: '280px',
      render: (_, record) => renderIPHealth(record),
    },
    {
      key: 'actions',
      title: '操作',
      width: '320px',
      render: (_, record) => {
        const isBuiltin = BUILTIN_PROXY_IDS.has(record.proxyId)
        const hasSource = !!record.sourceId && !!record.sourceUrl
        return (
          <div className="flex gap-2">
            {hasSource && (
              <Button
                size="sm"
                variant="secondary"
                onClick={(e) => { e.stopPropagation(); void refreshSingleSource(record.sourceId, false) }}
                loading={refreshingSourceIds.has(record.sourceId)}
              >
                刷新订阅
              </Button>
            )}
            <Button
              size="sm" variant="ghost"
              onClick={(e) => { e.stopPropagation(); handleTestOne(record) }}
              loading={latencyMap[record.proxyId] === -1}
              disabled={record.proxyConfig === 'direct://'}
            >测速</Button>
            <Button
              size="sm" variant="ghost"
              onClick={(e) => { e.stopPropagation(); handleCheckOneIPHealth(record) }}
              loading={checkingIPHealthIds.has(record.proxyId)}
              disabled={record.proxyConfig === 'direct://'}
            >IP健康</Button>
            <Button
              size="sm" variant="ghost"
              disabled={isBuiltin}
              title={isBuiltin ? '内置代理不可编辑' : undefined}
              onClick={(e) => { e.stopPropagation(); if (!isBuiltin) handleEdit(record) }}
            >编辑</Button>
            <Button
              size="sm" variant="danger"
              disabled={isBuiltin}
              title={isBuiltin ? '内置代理不可删除' : undefined}
              onClick={(e) => { e.stopPropagation(); if (!isBuiltin) handleDeleteClick(record.proxyId) }}
            >删除</Button>
          </div>
        )
      },
    },
  ]

  const handleRemovePreviewProxy = (proxyId: string) => {
    const target = previewList.find(item => item.proxyId === proxyId)
    if (!target) return
    setPreviewList(prev => prev.filter(item => item.proxyId !== proxyId))
    setRemovedPreviewProxyNames(prev => [...prev, target.proxyName])
  }

  const previewColumns: TableColumn<ProxyDisplayInfo>[] = [
    { key: 'proxyName', title: '代理名称', width: '200px' },
    { key: 'type', title: '类型', width: '100px' },
    { key: 'server', title: '服务器', width: '200px' },
    { key: 'port', title: '端口', width: '100px', render: (val) => val || '-' },
    {
      key: 'actions',
      title: '操作',
      width: '96px',
      render: (_, record) => (
        <Button
          size="sm"
          variant="danger"
          onClick={() => handleRemovePreviewProxy(record.proxyId)}
        >
          删除
        </Button>
      ),
    },
  ]

  const handleEdit = (record: ProxyDisplayInfo) => {
    const proxy = proxies.find(p => p.proxyId === record.proxyId)
    if (proxy) {
      setEditingProxy(proxy)
      setEditForm({ proxyName: proxy.proxyName, proxyConfig: proxy.proxyConfig, dnsServers: proxy.dnsServers || '', groupName: proxy.groupName || '' })
      setEditModalOpen(true)
    }
  }

  const handleSaveProxy = async () => {
    if (!editForm.proxyName.trim()) { toast.error('请输入代理名称'); return }
    if (!editingProxy) return
    setSaving(true)
    try {
      const newProxies = proxies.map(p =>
        p.proxyId === editingProxy.proxyId
          ? { ...p, proxyName: editForm.proxyName, proxyConfig: editForm.proxyConfig, dnsServers: editForm.dnsServers, groupName: editForm.groupName }
          : p
      )
      await saveProxies(newProxies)
      setEditModalOpen(false)
      toast.success('代理已更新')
    } catch (error: any) {
      toast.error(error?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteClick = (proxyId: string) => {
    setDeletingId(proxyId)
    setDeleteConfirmOpen(true)
  }

  const handleDeleteConfirm = async () => {
    if (!deletingId) return
    try {
      const newProxies = proxies.filter(p => p.proxyId !== deletingId)
      await saveProxies(newProxies)
      setSelectedIds(prev => { const next = new Set(prev); next.delete(deletingId); return next })
      toast.success('代理已删除')
    } catch (error: any) {
      toast.error(error?.message || '删除失败')
    }
    setDeletingId(null)
  }

  const handleImportModeChange = (nextMode: ProxyImportMode) => {
    setImportMode(nextMode)
    setImportResolvedUrl('')
    setDirectImportUrl('')
    setDirectImportBatchText('')
    if (nextMode !== 'clash') {
      setImportUrl('')
      setImportDnsServers('')
    }
    if (nextMode === 'direct') {
      setDirectImportForm({ ...INITIAL_DIRECT_IMPORT_FORM })
    }
  }

  const handleFetchImportURL = async () => {
    const targetURL = importUrl.trim()
    if (!targetURL) {
      toast.error('请输入订阅 URL')
      return
    }

    setFetchingImportUrl(true)
    try {
      const result = await fetchClashImportFromURL(targetURL)
      const content = (result?.content || '').trim()
      if (!content) {
        throw new Error('订阅内容为空')
      }

      setImportResolvedUrl((result?.url || targetURL).trim())
      setImportText(content)

      if (!importDnsServers.trim() && typeof result?.dnsServers === 'string' && result.dnsServers.trim()) {
        setImportDnsServers(result.dnsServers.trim())
      }
      if (!importGroupName.trim() && typeof result?.suggestedGroup === 'string' && result.suggestedGroup.trim()) {
        setImportGroupName(result.suggestedGroup.trim())
      }

      toast.success(`URL 获取成功，检测到 ${Math.max(0, Number(result?.proxyCount || 0))} 个代理`)
    } catch (error: any) {
      setImportResolvedUrl('')
      toast.error(error?.message || 'URL 获取失败')
    } finally {
      setFetchingImportUrl(false)
    }
  }

  const handleParseImport = () => {
    try {
      const prefix = importNamePrefix.trim()
      const candidates = importMode === 'clash'
        ? buildImportCandidatesFromClash(parseClashImportText(importText), prefix)
        : directImportBatchText.trim()
          ? buildDirectImportCandidatesFromText(directImportBatchText, prefix)
          : [buildDirectImportCandidate(directImportForm)]
      if (!candidates.length) {
        toast.error('未解析到可导入代理')
        return
      }
      const preview = buildImportPreview(candidates, importGroupName.trim())
      setRemovedPreviewProxyNames([])
      setPreviewList(preview)
      setImportModalOpen(false)
      setPreviewModalOpen(true)
    } catch (error: any) {
      toast.error(`解析失败: ${error?.message || '未知错误'}`)
    }
  }

  const handleConfirmImport = async () => {
    if (previewList.length === 0) {
      toast.error('请至少保留 1 个代理后再导入')
      return
    }
    setImporting(true)
    try {
      const sourceURL = importMode === 'clash' ? importResolvedUrl.trim() : ''
      const isURLImport = !!sourceURL
      const sourceNamePrefix = importMode === 'clash' ? importNamePrefix.trim() : ''
      const sourceID = isURLImport ? resolveImportSourceID(proxies, sourceURL, sourceNamePrefix) : ''
      const sourceAutoRefresh = isURLImport ? globalAutoRefreshEnabled : false
      const sourceRefreshIntervalM = sourceAutoRefresh ? globalRefreshInterval : 0
      const sourceLastRefreshAt = isURLImport ? new Date().toISOString() : ''
      const oldSourceProxies = isURLImport
        ? proxies.filter(item => (item.sourceId || '').trim() === sourceID)
        : []
      const pickExistingID = createExistingProxyIDPicker(oldSourceProxies)

      const newProxies: BrowserProxy[] = previewList.map((p) => ({
        proxyId: pickExistingID(p.proxyName, p.proxyConfig) || nextProxyID(),
        proxyName: p.proxyName,
        proxyConfig: p.proxyConfig,
        dnsServers: importMode === 'clash' ? importDnsServers.trim() || undefined : undefined,
        groupName: importGroupName.trim() || undefined,
        sourceId: sourceID || undefined,
        sourceUrl: sourceURL || undefined,
        sourceNamePrefix: sourceNamePrefix || undefined,
        sourceAutoRefresh,
        sourceRefreshIntervalM,
        sourceLastRefreshAt: sourceLastRefreshAt || undefined,
      }))
      const allProxies = isURLImport
        ? proxies.filter(item => (item.sourceId || '').trim() !== sourceID).concat(newProxies)
        : [...proxies, ...newProxies]
      await saveProxies(allProxies)
      if (isURLImport && removedPreviewProxyNames.length > 0) {
        appendSourceIgnoredProxyNames(sourceID, removedPreviewProxyNames)
      }
      setPreviewModalOpen(false)
      setImportUrl('')
      setImportResolvedUrl('')
      setImportText('')
      setImportDnsServers('')
      setImportNamePrefix('')
      setImportGroupName('')
      setDirectImportForm({ ...INITIAL_DIRECT_IMPORT_FORM })
      setDirectImportUrl('')
      setDirectImportBatchText('')
      setPreviewList([])
      setRemovedPreviewProxyNames([])
      toast.success(`成功导入 ${newProxies.length} 个代理`)
    } catch (error: any) {
      toast.error(error?.message || '导入失败')
    } finally {
      setImporting(false)
    }
  }

  const selectedCount = selectedIds.size
  const canParseImport = importMode === 'clash'
    ? !!importText.trim()
    : !!directImportBatchText.trim() || (!!directImportForm.server.trim() && !!directImportForm.port.trim())

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">代理池配置</h1>
          <p className="text-sm text-[var(--color-text-muted)] mt-1">管理代理配置，支持 Clash 订阅、HTTP、HTTPS、SOCKS5</p>
        </div>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void handleRefreshAllSources(false)}
            loading={refreshingAllSources}
            disabled={!hasURLImportSources}
          >
            刷新订阅
          </Button>
          <Button size="sm" variant="secondary" onClick={handleCheckAllIPHealth} loading={checkingAllIPHealth} disabled={filteredList.length === 0}>检测IP健康</Button>
          <Button size="sm" variant="secondary" onClick={handleTestAll} loading={testingAll} disabled={filteredList.length === 0}>测试全部</Button>
          <Button size="sm" variant="secondary" onClick={() => setSmartAssignOpen(true)} disabled={proxies.length === 0}>智能分配</Button>
          <Button size="sm" onClick={() => setImportModalOpen(true)}>导入代理</Button>
        </div>
      </div>

      <Card>
        <div className="flex items-center gap-3 mb-4">
          <Input
            value={filterKeyword}
            onChange={e => setFilterKeyword(e.target.value)}
            placeholder="搜索名称或服务器..."
            style={{ width: '220px' }}
          />
          <select
            value={filterProtocol}
            onChange={e => setFilterProtocol(e.target.value)}
            className="px-3 py-1.5 text-sm rounded-md border border-[var(--color-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)] focus:outline-none focus:ring-1 focus:ring-[var(--color-primary)]"
          >
            {protocolOptions.map(p => (
              <option key={p} value={p}>{p === 'all' ? '全部协议' : p.toUpperCase()}</option>
            ))}
          </select>
          <select
            value={filterGroup}
            onChange={e => setFilterGroup(e.target.value)}
            className="px-3 py-1.5 text-sm rounded-md border border-[var(--color-border)] bg-[var(--color-bg-secondary)] text-[var(--color-text-primary)] focus:outline-none focus:ring-1 focus:ring-[var(--color-primary)]"
          >
            <option value="all">全部分组</option>
            {groups.map(g => <option key={g} value={g}>{g}</option>)}
          </select>
          {(filterProtocol !== 'all' || filterKeyword || filterGroup !== 'all') && (
            <Button size="sm" variant="ghost" onClick={() => { setFilterProtocol('all'); setFilterKeyword(''); setFilterGroup('all') }}>清除筛选</Button>
          )}
          <div className="flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-bg-secondary)] px-2 py-1.5">
            <span className="text-xs text-[var(--color-text-muted)]">全局自动刷新</span>
            <Switch
              checked={globalAutoRefreshEnabled}
              onChange={(checked) => setGlobalAutoRefreshEnabled(checked)}
            />
            <Input
              type="number"
              min={5}
              max={1440}
              value={globalRefreshIntervalM}
              onChange={e => setGlobalRefreshIntervalM(e.target.value)}
              className="w-24"
              disabled={!globalAutoRefreshEnabled}
            />
            <span className="text-xs text-[var(--color-text-muted)]">分钟</span>
          </div>
          <div className="flex-1" />
          {filteredList.length > 0 && (
            <label className="flex items-center gap-1.5 text-sm text-[var(--color-text-muted)] cursor-pointer select-none">
              <input
                type="checkbox"
                checked={allFilteredSelected}
                ref={el => { if (el) el.indeterminate = someFilteredSelected && !allFilteredSelected }}
                onChange={handleToggleAll}
                className="w-4 h-4 rounded border-[var(--color-border)] accent-[var(--color-primary)] cursor-pointer"
              />
              全选
            </label>
          )}
          {selectedCount > 0 && (
            <Button size="sm" variant="danger" onClick={() => setBatchDeleteConfirmOpen(true)}>
              删除所选 ({selectedCount})
            </Button>
          )}
        </div>
        <Table
          columns={columns}
          data={filteredList}
          rowKey="proxyId"
          loading={loading}
          emptyText="暂无代理配置，点击上方按钮添加或导入"
          sortColumn={sortColumn}
          sortOrder={sortOrder}
          onSort={({ column, order }) => {
            setSortColumn(column)
            setSortOrder(order)
          }}
        />
      </Card>

      <Modal open={importModalOpen} onClose={() => setImportModalOpen(false)} title="导入代理配置" width="600px"
        footer={
          <>
            <Button variant="secondary" onClick={() => setImportModalOpen(false)} disabled={fetchingImportUrl}>取消</Button>
            <Button onClick={handleParseImport} disabled={fetchingImportUrl || !canParseImport}>解析</Button>
          </>
        }>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-2">
            <Button
              variant={importMode === 'clash' ? undefined : 'secondary'}
              onClick={() => handleImportModeChange('clash')}
            >
              Clash 订阅 / YAML
            </Button>
            <Button
              variant={importMode === 'direct' ? undefined : 'secondary'}
              onClick={() => handleImportModeChange('direct')}
            >
              HTTP / SOCKS5
            </Button>
          </div>
          <p className="text-sm text-[var(--color-text-muted)]">
            {importMode === 'clash'
              ? '支持粘贴 Clash YAML，或通过订阅 URL 自动拉取并解析（含 proxies、dns、proxy-groups）'
              : '支持单条录入或批量粘贴 HTTP / HTTPS / SOCKS5 代理，一行一个，导入后直接生效，不走 Clash 桥接'}
          </p>
          {importMode === 'clash' && (
            <>
              <FormItem label="订阅 URL（可选）">
                <div className="flex gap-2">
                  <Input
                    value={importUrl}
                    onChange={e => {
                      const next = e.target.value
                      setImportUrl(next)
                      if (importResolvedUrl.trim() && next.trim() !== importResolvedUrl.trim()) {
                        setImportResolvedUrl('')
                      }
                    }}
                    placeholder="https://example.com/clash/subscription"
                    className="flex-1"
                  />
                  <Button
                    variant="secondary"
                    onClick={handleFetchImportURL}
                    loading={fetchingImportUrl}
                    disabled={!importUrl.trim()}
                  >
                    从 URL 获取
                  </Button>
                </div>
                {importResolvedUrl.trim() && (
                  <p className="text-xs text-[var(--color-success)] mt-1 break-all">
                    已绑定订阅：{importResolvedUrl}
                  </p>
                )}
                <p className="text-xs text-[var(--color-text-muted)] mt-1">获取成功后会自动回填 YAML 文本，并尝试自动填充 DNS 与建议分组；自动刷新时间请在列表顶部统一配置</p>
              </FormItem>
              <Textarea
                value={importText}
                onChange={e => setImportText(e.target.value)}
                rows={12}
                placeholder={`proxies:\n  - name: vless-v6\n    type: vless\n    server: example.com\n    port: 443\n    uuid: your-uuid\n    ...`}
              />
            </>
          )}
          {importMode === 'direct' && (
            <div className="space-y-4">
              <FormItem label="批量导入代理（可选）">
                <Textarea
                  value={directImportBatchText}
                  onChange={e => setDirectImportBatchText(e.target.value)}
                  rows={7}
                  placeholder={`一行一个代理，例如：\nsocks5://user:pass@116.212.124.13:28513\nhttp://user:pass@1.2.3.4:8080\nhttps://5.6.7.8:8443\n127.0.0.1:7890`}
                />
                <p className="text-xs text-[var(--color-text-muted)] mt-1">
                  支持 http://、https://、socks5://、socks://、socket://；未写协议时默认按 HTTP 导入。填写批量内容后，下方单条表单会被忽略。
                </p>
              </FormItem>
              <FormItem label="粘贴单条代理链接（自动识别）">
                <Input
                  value={directImportUrl}
                  disabled={!!directImportBatchText.trim()}
                  onChange={e => {
                    const url = e.target.value
                    setDirectImportUrl(url)
                    if (!url.trim()) return
                    try {
                      const normalized = normalizeDirectProxyLine(url)
                      if (!normalized) return
                      const parsed = new URL(normalized)
                      const protocol = parsed.protocol.replace(':', '').toLowerCase()
                      const validProtocols = ['http', 'https', 'socks5']
                      const matchedProtocol = validProtocols.includes(protocol) ? protocol : 'http'
                      setDirectImportForm(prev => ({
                        ...prev,
                        protocol: matchedProtocol as DirectImportForm['protocol'],
                        server: parsed.hostname || '',
                        port: parsed.port || '',
                        username: decodeURIComponent(parsed.username || ''),
                        password: decodeURIComponent(parsed.password || ''),
                      }))
                    } catch {
                      // Not a valid URL, let user fill fields manually
                    }
                  }}
                  placeholder="例如：socks5://user:pass@116.212.124.13:28513 或 http://127.0.0.1:7890"
                />
                <p className="text-xs text-[var(--color-text-muted)] mt-1">粘贴完整代理链接后自动填写下方字段；也可手动逐项填写</p>
              </FormItem>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <FormItem label="代理协议" required>
                <Select
                  options={[...DIRECT_PROXY_PROTOCOL_OPTIONS]}
                  value={directImportForm.protocol}
                  onChange={e => setDirectImportForm(prev => ({ ...prev, protocol: e.target.value as DirectImportForm['protocol'] }))}
                />
              </FormItem>
              <FormItem label="代理名称（可选）">
                <Input
                  value={directImportForm.proxyName}
                  onChange={e => setDirectImportForm(prev => ({ ...prev, proxyName: e.target.value }))}
                  placeholder="例如：香港节点"
                />
              </FormItem>
              <FormItem label="代理地址" required>
                <Input
                  value={directImportForm.server}
                  onChange={e => setDirectImportForm(prev => ({ ...prev, server: e.target.value }))}
                  placeholder="例如：127.0.0.1 或 hk.example.com"
                />
              </FormItem>
              <FormItem label="代理端口" required>
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={directImportForm.port}
                  onChange={e => setDirectImportForm(prev => ({ ...prev, port: e.target.value }))}
                  placeholder="例如：1080"
                />
              </FormItem>
              <FormItem label="账号（可选）">
                <Input
                  value={directImportForm.username}
                  onChange={e => setDirectImportForm(prev => ({ ...prev, username: e.target.value }))}
                  placeholder="留空则不使用认证"
                />
              </FormItem>
              <FormItem label="密码（可选）">
                <Input
                  type="password"
                  value={directImportForm.password}
                  onChange={e => setDirectImportForm(prev => ({ ...prev, password: e.target.value }))}
                  placeholder="留空则不使用密码"
                />
              </FormItem>
            </div>
            </div>
          )}
          <FormItem label="分组名称（可选）">
            <Input
              value={importGroupName}
              onChange={e => setImportGroupName(e.target.value)}
              placeholder="例如：香港、美国、机场A"
              list="proxy-groups-datalist"
            />
            {groups.length > 0 && (
              <datalist id="proxy-groups-datalist">
                {groups.map(g => <option key={g} value={g} />)}
              </datalist>
            )}
            <p className="text-xs text-[var(--color-text-muted)] mt-1">填写后本次导入的代理将归入该分组，可按分组筛选</p>
          </FormItem>
          {importMode !== 'clash' && directImportBatchText.trim() && (
            <FormItem label="名称前缀（可选）">
              <Input
                value={importNamePrefix}
                onChange={e => setImportNamePrefix(e.target.value)}
                placeholder="例如：HK、US、机场A"
              />
              <p className="text-xs text-[var(--color-text-muted)] mt-1">
                批量导入时代理名称将变为 <code className="px-1 bg-[var(--color-bg-secondary)] rounded">前缀-协议-地址:端口</code>，留空则自动按协议和地址命名
              </p>
            </FormItem>
          )}
          {importMode === 'clash' && (
            <FormItem label="名称前缀（可选）">
              <Input
                value={importNamePrefix}
                onChange={e => setImportNamePrefix(e.target.value)}
                placeholder="例如：HK、US、机场A"
              />
              <p className="text-xs text-[var(--color-text-muted)] mt-1">
                填写后代理名称将变为 <code className="px-1 bg-[var(--color-bg-secondary)] rounded">前缀-原名称</code>，留空则保持原名
              </p>
            </FormItem>
          )}
          {importMode === 'clash' && (
            <FormItem label="批量 DNS 配置（可选）">
              <Textarea value={importDnsServers} onChange={e => setImportDnsServers(e.target.value)} rows={5}
                placeholder={`dns:\n  enable: true\n  nameserver:\n    - 119.29.29.29\n    - 223.5.5.5`} />
              <p className="text-xs text-[var(--color-text-muted)] mt-1">留空则不配置 DNS，填写后将应用到本次导入的所有代理</p>
            </FormItem>
          )}
        </div>
      </Modal>

      <Modal open={previewModalOpen} onClose={() => setPreviewModalOpen(false)} title="确认导入以下代理" width="700px"
        footer={<><Button variant="secondary" onClick={() => { setPreviewModalOpen(false); setImportModalOpen(true) }}>返回修改</Button><Button onClick={handleConfirmImport} loading={importing} disabled={previewList.length === 0}>确认导入</Button></>}>
        <div className="space-y-3">
          {importMode === 'clash' && importDnsServers.trim() && (
            <p className="text-xs text-[var(--color-text-muted)] bg-[var(--color-bg-secondary)] px-3 py-2 rounded">已配置批量 DNS，将应用到以下所有代理</p>
          )}
          <p className="text-xs text-[var(--color-text-muted)]">
            保留 {previewList.length} 条，删除 {removedPreviewProxyNames.length} 条。删除项不会进入后续比较环节。
          </p>
          <Table columns={previewColumns} data={previewList} rowKey="proxyId" maxHeight="380px" emptyText="无代理数据" />
        </div>
      </Modal>

      <Modal open={editModalOpen} onClose={() => setEditModalOpen(false)} title="编辑代理" width="500px"
        footer={<><Button variant="secondary" onClick={() => setEditModalOpen(false)}>取消</Button><Button onClick={handleSaveProxy} loading={saving}>保存</Button></>}>
        <div className="space-y-4">
          <FormItem label="代理名称" required>
            <Input value={editForm.proxyName} onChange={e => setEditForm(prev => ({ ...prev, proxyName: e.target.value }))} placeholder="例如：香港节点" />
          </FormItem>
          <FormItem label="分组名称（可选）">
            <Input value={editForm.groupName} onChange={e => setEditForm(prev => ({ ...prev, groupName: e.target.value }))} placeholder="例如：香港、美国" list="edit-proxy-groups-datalist" />
            <datalist id="edit-proxy-groups-datalist">
              {groups.map(g => <option key={g} value={g} />)}
            </datalist>
          </FormItem>
          <FormItem label="代理配置">
            <Textarea value={editForm.proxyConfig} onChange={e => setEditForm(prev => ({ ...prev, proxyConfig: e.target.value }))} rows={10} placeholder="支持 Clash YAML、http://、https://、socks5:// 代理配置" />
          </FormItem>
          <FormItem label="DNS 服务器（可选）">
            <Textarea value={editForm.dnsServers} onChange={e => setEditForm(prev => ({ ...prev, dnsServers: e.target.value }))} rows={6}
              placeholder={`dns:\n  enable: true\n  nameserver:\n    - 119.29.29.29\n    - 223.5.5.5`} />
            <p className="text-xs text-[var(--color-text-muted)] mt-1">支持 Clash dns: YAML 格式，主要用于 Clash / 桥接代理；直连 HTTP/SOCKS5 通常不会使用这里的 DNS 配置</p>
          </FormItem>
        </div>
      </Modal>

      <Modal
        open={ipHealthDetailOpen}
        onClose={() => setIPHealthDetailOpen(false)}
        title="IP健康原始返回"
        width="760px"
        footer={<Button variant="secondary" onClick={() => setIPHealthDetailOpen(false)}>关闭</Button>}
      >
        <div className="space-y-3">
          {currentIPHealthDetail && (
            <>
              <div className="text-xs text-[var(--color-text-muted)]">
                代理ID：{currentIPHealthDetail.proxyId} | 来源：{currentIPHealthDetail.source} | 时间：{currentIPHealthDetail.updatedAt}
              </div>
              {!currentIPHealthDetail.ok && (
                <div className="text-sm text-red-500">{currentIPHealthDetail.error || '检测失败'}</div>
              )}
              <pre className="max-h-[420px] overflow-auto text-xs leading-5 rounded-lg bg-[var(--color-bg-secondary)] border border-[var(--color-border)] p-3">
                {JSON.stringify(currentIPHealthDetail.rawData || {}, null, 2)}
              </pre>
            </>
          )}
        </div>
      </Modal>

      <ConfirmModal open={deleteConfirmOpen} onClose={() => setDeleteConfirmOpen(false)} onConfirm={handleDeleteConfirm}
        title="确认删除" content="确定要删除这个代理吗？此操作不可恢复。" confirmText="删除" danger />

      <ConfirmModal open={batchDeleteConfirmOpen} onClose={() => setBatchDeleteConfirmOpen(false)} onConfirm={handleBatchDeleteConfirm}
        title="批量删除" content={`确定要删除选中的 ${selectedCount} 个代理吗？此操作不可恢复。`} confirmText="删除" danger />

      <SmartAssignProxyModal
        open={smartAssignOpen}
        allProxies={proxies}
        filteredProxies={filteredList.map(p => proxies.find(item => item.proxyId === p.proxyId)!).filter(Boolean)}
        selectedProxyIds={selectedIds}
        onClose={() => setSmartAssignOpen(false)}
      />
    </div>
  )
}
