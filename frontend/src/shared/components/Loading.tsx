import clsx from 'clsx'

type LoadingSize = 'sm' | 'md' | 'lg'

interface LoadingProps {
  size?: LoadingSize
  text?: string
  fullscreen?: boolean
  className?: string
}

const sizeStyles = {
  sm: 'w-4 h-4 border-2',
  md: 'w-6 h-6 border-2',
  lg: 'w-8 h-8 border-[3px]',
}

export function Loading({ 
  size = 'md', 
  text, 
  fullscreen = false,
  className 
}: LoadingProps) {
  const spinner = (
    <div className={clsx('flex flex-col items-center gap-3', className)}>
      <div
        className={clsx(
          'border-[var(--color-border-default)] border-t-[var(--color-accent)] rounded-full animate-spin',
          sizeStyles[size]
        )}
      />
      {text && (
        <span className="text-sm text-[var(--color-text-muted)]">{text}</span>
      )}
    </div>
  )

  if (fullscreen) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-[var(--color-bg-base)]/80 backdrop-blur-sm">
        {spinner}
      </div>
    )
  }

  return spinner
}

// 骨架屏组件
interface SkeletonProps {
  width?: string
  height?: string
  circle?: boolean
  className?: string
}

export function Skeleton({ 
  width = '100%', 
  height = '20px', 
  circle = false,
  className 
}: SkeletonProps) {
  return (
    <div
      className={clsx(
        'bg-[var(--color-bg-muted)] animate-pulse',
        circle ? 'rounded-full' : 'rounded',
        className
      )}
      style={{ width, height }}
    />
  )
}
