import React from 'react'
import { cn } from '@/lib/utils'

const INPUT_BASE = 'w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground'
const INPUT_ERROR = 'border-destructive focus:border-destructive focus:ring-destructive/10'

export interface TextFieldProps {
  name: string
  label: string
  value: string
  onChange: (e: React.ChangeEvent<HTMLInputElement>) => void
  error?: string
  placeholder?: string
  className?: string
}

export function TextField({ name, label, value, onChange, error, placeholder, className }: TextFieldProps) {
  return (
    <div>
      <label htmlFor={name} className="text-sm font-medium text-foreground">
        {label}
      </label>
      <input
        type="text"
        id={name}
        name={name}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        className={cn(INPUT_BASE, error && INPUT_ERROR, className)}
      />
      {error && <p className="mt-1 text-sm text-destructive">{error}</p>}
    </div>
  )
}
