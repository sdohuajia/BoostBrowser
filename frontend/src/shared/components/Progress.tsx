import clsx from 'clsx'

type ProgressStatus = 'normal' | 'success' | 'error' | 'warning'

interface ProgressProps {
  percent: number
  status?: ProgressStatus
  showInfo?: boolean
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

const statusColors = {
  normal: 'bg-[var(--color-accent)]',
  success: 'bg-[var(--color-success)]',
  error: 'bg-[var(--color-error)]',
  warning: 'bg-[var(--color-warning)]',
}

const sizeStyles = {
  sm: 'h-1',
  md: 'h-2',
  lg: 'h-3',
}

export function Progress({
  percent,
  status = 'normal',
  showInfo = true,
  size = 'md',
  className,
}: ProgressProps) {
  const validPercent = Math.min(100, Math.max(0, percent))

  return (
    <div className={clsx('flex items-center gap-3', className)}>
      <div className={clsx('flex-1 bg-[var(--color-bg-muted)] rounded-full overflow-hidden', sizeStyles[size])}>
        <div
          className={clsx('h-full transition-all duration-300 rounded-full', statusColors[status])}
          style={{ width: `${validPercent}%` }}
        />
      </div>
      {showInfo && (
        <span className="text-sm text-[var(--color-text-muted)] min-w-[3ch] text-right">
          {validPercent}%
        </span>
      )}
    </div>
  )
}

// 圆形进度条
interface CircleProgressProps {
  percent: number
  size?: number
  strokeWidth?: number
  status?: ProgressStatus
  showInfo?: boolean
}

export function CircleProgress({
  percent,
  size = 120,
  strokeWidth = 8,
  status = 'normal',
  showInfo = true,
}: CircleProgressProps) {
  const validPercent = Math.min(100, Math.max(0, percent))
  const radius = (size - strokeWidth) / 2
  const circumference = 2 * Math.PI * radius
  const offset = circumference - (validPercent / 100) * circumference

  const colors = {
    normal: 'var(--color-accent)',
    success: 'var(--color-success)',
    error: 'var(--color-error)',
    warning: 'var(--color-warning)',
  }

  return (
    <div className="relative inline-flex items-center justify-center">
      <svg width={size} height={size} className="transform -rotate-90">
        {/* 背景圆 */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="var(--color-bg-muted)"
          strokeWidth={strokeWidth}
        />
        {/* 进度圆 */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={colors[status]}
          strokeWidth={strokeWidth}
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          strokeLinecap="round"
          className="transition-all duration-300"
        />
      </svg>
      {showInfo && (
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-lg font-semibold text-[var(--color-text-primary)]">
            {validPercent}%
          </span>
        </div>
      )}
    </div>
  )
}
