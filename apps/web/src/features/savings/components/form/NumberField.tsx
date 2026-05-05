import React from 'react'
import { cn } from '@/lib/utils'

const INPUT_BASE = 'w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground'
const INPUT_ERROR = 'border-destructive focus:border-destructive focus:ring-destructive/10'

export interface NumberFieldProps {
  name: string
  label: string
  value: string
  onChange: (e: React.ChangeEvent<HTMLInputElement>) => void
  error?: string
  placeholder?: string
  min?: string
  step?: string
  prefix?: string
  className?: string
}

export function NumberField({ name, label, value, onChange, error, placeholder, min, step, prefix, className }: NumberFieldProps) {
  return (
    <div>
      <label htmlFor={name} className="text-sm font-medium text-foreground">
        {label}
      </label>
      <div className="relative">
        {prefix && (
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground">
            {prefix}
          </span>
        )}
        <input
          type="number"
          id={name}
          name={name}
          value={value}
          onChange={onChange}
          placeholder={placeholder}
          min={min}
          step={step}
          className={cn(INPUT_BASE, prefix && 'pl-7', error && INPUT_ERROR, className)}
        />
      </div>
      {error && <p className="mt-1 text-sm text-destructive">{error}</p>}
    </div>
  )
}
