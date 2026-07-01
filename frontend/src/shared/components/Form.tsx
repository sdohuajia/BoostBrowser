import { ReactNode, InputHTMLAttributes, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react'
import clsx from 'clsx'

interface FormItemProps {
  label?: string
  required?: boolean
  hint?: string
  error?: string
  children: ReactNode
  className?: string
}

export function FormItem({ label, required, hint, error, children, className }: FormItemProps) {
  return (
    <div className={clsx('space-y-1.5', className)}>
      {label && (
        <label className="block text-sm font-medium text-[var(--color-text-secondary)]">
          {label}
          {required && <span className="text-[var(--color-error)] ml-0.5">*</span>}
          {hint && <span className="text-xs font-normal text-[var(--color-text-muted)] ml-1">({hint})</span>}
        </label>
      )}
      {children}
      {error && <p className="text-xs text-[var(--color-error)]">{error}</p>}
    </div>
  )
}

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  error?: boolean
}

export function Input({ error, className, ...props }: InputProps) {
  return (
    <input
      className={clsx(
        'block h-9 px-3 text-sm',
        'bg-[var(--color-bg-surface)] text-[var(--color-text-primary)]',
        'border border-[var(--color-border-default)] rounded-lg',
        'placeholder:text-[var(--color-text-muted)]',
        'focus:outline-none focus:border-[var(--color-border-strong)] focus:ring-1 focus:ring-[var(--color-border-strong)]',
        'disabled:bg-[var(--color-bg-muted)] disabled:text-[var(--color-text-muted)] disabled:cursor-not-allowed',
        'transition-colors duration-150',
        error && 'border-[var(--color-error)] focus:border-[var(--color-error)] focus:ring-[var(--color-error)]',
        // 默认宽度自适应，可通过 className 覆盖
        !className?.includes('w-') && 'w-full',
        className
      )}
      {...props}
    />
  )
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  error?: boolean
  options: { value: string; label: string }[]
}

export function Select({ error, options, className, ...props }: SelectProps) {
  return (
    <select
      className={clsx(
        'block h-9 px-3 text-sm',
        'bg-[var(--color-bg-surface)] text-[var(--color-text-primary)]',
        'border border-[var(--color-border-default)] rounded-lg',
        'focus:outline-none focus:border-[var(--color-border-strong)] focus:ring-1 focus:ring-[var(--color-border-strong)]',
        'disabled:bg-[var(--color-bg-muted)] disabled:text-[var(--color-text-muted)] disabled:cursor-not-allowed',
        'transition-colors duration-150',
        'cursor-pointer',
        error && 'border-[var(--color-error)] focus:border-[var(--color-error)] focus:ring-[var(--color-error)]',
        // 默认宽度自适应，可通过 className 覆盖
        !className?.includes('w-') && 'w-full',
        className
      )}
      {...props}
    >
      {options.map((opt) => (
        <option key={opt.value} value={opt.value}>
          {opt.label}
        </option>
      ))}
    </select>
  )
}

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  error?: boolean
}

export function Textarea({ error, className, ...props }: TextareaProps) {
  return (
    <textarea
      className={clsx(
        'block w-full px-3 py-2 text-sm',
        'bg-[var(--color-bg-surface)] text-[var(--color-text-primary)]',
        'border border-[var(--color-border-default)] rounded-lg',
        'placeholder:text-[var(--color-text-muted)]',
        'focus:outline-none focus:border-[var(--color-border-strong)] focus:ring-1 focus:ring-[var(--color-border-strong)]',
        'disabled:bg-[var(--color-bg-muted)] disabled:text-[var(--color-text-muted)] disabled:cursor-not-allowed',
        'transition-colors duration-150 resize-none',
        error && 'border-[var(--color-error)] focus:border-[var(--color-error)] focus:ring-[var(--color-error)]',
        className
      )}
      {...props}
    />
  )
}

interface SwitchProps {
  checked: boolean
  onChange: (checked: boolean) => void
  disabled?: boolean
}

export function Switch({ checked, onChange, disabled }: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={clsx(
        'relative inline-flex h-5 w-9 items-center rounded-full transition-colors duration-150',
        'focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)] focus-visible:ring-offset-2',
        checked ? 'bg-[var(--color-accent)]' : 'bg-[var(--color-border-strong)]',
        disabled && 'opacity-50 cursor-not-allowed'
      )}
    >
      <span
        className={clsx(
          'inline-block h-4 w-4 transform rounded-full bg-white shadow-sm transition-transform duration-150',
          checked ? 'translate-x-4' : 'translate-x-0.5'
        )}
      />
    </button>
  )
}
