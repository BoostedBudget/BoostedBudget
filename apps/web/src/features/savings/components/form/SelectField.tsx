import React from 'react'
import { cn } from '@/lib/utils'

const INPUT_BASE = 'w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground'
const INPUT_ERROR = 'border-destructive focus:border-destructive focus:ring-destructive/10'

export interface SelectFieldProps {
  name: string
  label: string
  value: string
  onChange: (e: React.ChangeEvent<HTMLSelectElement>) => void
  error?: string
  disabled?: boolean
  children: React.ReactNode
  className?: string
}

export function SelectField({ name, label, value, onChange, error, disabled, children, className }: SelectFieldProps) {
  return (
    <div>
      <label htmlFor={name} className="text-sm font-medium text-foreground">
        {label}
      </label>
      <select
        id={name}
        name={name}
        value={value}
        onChange={onChange}
        disabled={disabled}
        className={cn(INPUT_BASE, error && INPUT_ERROR, className)}
      >
        {children}
      </select>
      {error && <p className="mt-1 text-sm text-destructive">{error}</p>}
    </div>
  )
}
