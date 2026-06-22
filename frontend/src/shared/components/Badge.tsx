import { ReactNode } from 'react'
import clsx from 'clsx'

type BadgeVariant = 'default' | 'success' | 'error' | 'warning' | 'info'
type BadgeSize = 'sm' | 'md' | 'lg'

interface BadgeProps {
  children: ReactNode
  variant?: BadgeVariant
  size?: BadgeSize
  dot?: boolean
  dotClassName?: string
  className?: string
}

const variantStyles = {
  default: 'bg-[var(--color-bg-muted)] text-[var(--color-text-secondary)]',
  success: 'bg-[var(--color-success)]/15 text-[var(--color-success)]',
  error: 'bg-[var(--color-error)]/15 text-[var(--color-error)]',
  warning: 'bg-[var(--color-warning)]/15 text-[var(--color-warning)]',
  info: 'bg-[var(--color-accent)]/15 text-[var(--color-accent)]',
}

const sizeStyles = {
  sm: 'px-1.5 py-0.5 text-xs',
  md: 'px-2 py-1 text-xs',
  lg: 'px-2.5 py-1 text-sm',
}

const dotStyles = {
  default: 'bg-[var(--color-text-muted)]',
  success: 'bg-[var(--color-success)]',
  error: 'bg-[var(--color-error)]',
  warning: 'bg-[var(--color-warning)]',
  info: 'bg-[var(--color-accent)]',
}

export function Badge({
  children,
  variant = 'default',
  size = 'md',
  dot = false,
  dotClassName = 'w-1.5 h-1.5',
  className,
}: BadgeProps) {
  return (
    <span
      className={clsx(
        'inline-flex items-center gap-1.5 rounded-full font-medium',
        variantStyles[variant],
        sizeStyles[size],
        className
      )}
    >
      {dot && (
        <span className={clsx('rounded-full', dotClassName, dotStyles[variant])} />
      )}
      {children}
    </span>
  )
}
