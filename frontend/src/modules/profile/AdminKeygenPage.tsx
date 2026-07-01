import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Button, Input, Modal, toast, Textarea } from '../../shared/components'
import { Key } from 'lucide-react'
import { generateCDKeys } from '../dashboard/api'

const ADMIN_PAGE_PASSWORD = '志字辈小蚂蚁'

export function AdminKeygenPage() {
    const navigate = useNavigate()
    const [count, setCount] = useState<number>(10)
    const [keys, setKeys] = useState<string[]>([])
    const [loading, setLoading] = useState(false)
    const [accessGranted, setAccessGranted] = useState(false)
    const [passwordInput, setPasswordInput] = useState('')

    const handleGenerate = async () => {
        if (count <= 0 || count > 1000) {
            toast.error('生成数量必须在 1 ~ 1000 之间')
            return
        }

        setLoading(true)
        const res = await generateCDKeys(count)
        setLoading(false)

        if (res.success) {
            setKeys(res.keys)
            toast.success(`成功生成 ${res.keys.length} 个兑换码`)
        } else {
            toast.error(res.message || '生成失败')
        }
    }

    const handleCopyAll = async () => {
        if (keys.length === 0) return
        try {
            await navigator.clipboard.writeText(keys.join('\n'))
            toast.success('已复制全部兑换码到剪贴板')
        } catch {
            toast.error('复制失败，请手动选择复制')
        }
    }

    const handleVerifyPassword = (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()

        if (passwordInput.trim() === ADMIN_PAGE_PASSWORD) {
            setAccessGranted(true)
            setPasswordInput('')
            toast.success('验证通过，已进入兑换码生成页面')
            return
        }

        toast.error('密码错误，请重试')
        setPasswordInput('')
    }

    return (
        <>
            <Modal
                open={!accessGranted}
                onClose={() => navigate('/profile')}
                title="管理员验证"
                width="420px"
                closable={false}
            >
                <form className="space-y-4" onSubmit={handleVerifyPassword}>
                    <p className="text-sm text-[var(--color-text-secondary)]">
                        请输入访问密码后继续。
                    </p>
                    <Input
                        type="text"
                        value={passwordInput}
                        onChange={(e) => setPasswordInput(e.target.value)}
                        placeholder="请输入密码"
                        autoFocus
                        autoComplete="off"
                        inputMode="text"
                        spellCheck={false}
                    />
                    <div className="flex justify-end gap-3 pt-1">
                        <Button type="button" variant="secondary" onClick={() => navigate('/profile')}>
                            取消
                        </Button>
                        <Button type="submit">
                            确认进入
                        </Button>
                    </div>
                </form>
            </Modal>

            {accessGranted && (
                <div className="space-y-6 animate-fade-in max-w-4xl mx-auto">
                    <div>
                        <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">系统核心管理 - CDKey 生成器</h1>
                        <p className="text-sm text-[var(--color-text-muted)] mt-1">隐藏管理员工具。生成的每个兑换码均可为客户端增加 10 个永久额度。</p>
                    </div>

                    <Card>
                        <div className="flex flex-col gap-4">
                            <div className="flex items-end gap-3">
                                <div className="flex-1">
                                    <label className="block text-sm font-medium text-[var(--color-text-primary)] mb-1">生成数量</label>
                                    <Input
                                        type="number"
                                        min={1}
                                        max={100}
                                        value={count}
                                        onChange={(e) => setCount(parseInt(e.target.value) || 0)}
                                        placeholder="10"
                                    />
                                </div>
                                <Button onClick={handleGenerate} loading={loading} className="w-32">
                                    <Key className="w-4 h-4 mr-2" />
                                    立即生成
                                </Button>
                            </div>

                            <div className="mt-4">
                                <div className="flex items-center justify-between mb-2">
                                    <span className="text-sm font-medium text-[var(--color-text-primary)]">生成结果</span>
                                    <Button size="sm" variant="secondary" onClick={handleCopyAll} disabled={keys.length === 0}>
                                        一键复制
                                    </Button>
                                </div>
                                <Textarea
                                    value={keys.length > 0 ? keys.join('\n') : '点击上方按钮生成...'}
                                    readOnly
                                    rows={15}
                                    className="font-mono text-sm leading-relaxed"
                                />
                            </div>
                        </div>
                    </Card>
                </div>
            )}
        </>
    )
}
