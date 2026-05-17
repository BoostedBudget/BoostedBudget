import React from 'react'
import { cn } from '@/lib/utils'

const INPUT_BASE = 'w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground'

export interface DateFieldProps {
  name: string
  label: string
  value: string
  onChange: (e: React.ChangeEvent<HTMLInputElement>) => void
  min?: string
  className?: string
}

export function DateField({ name, label, value, onChange, min, className }: DateFieldProps) {
  return (
    <div>
      <label htmlFor={name} className="text-sm font-medium text-foreground">
        {label}
      </label>
      <input
        type="date"
        id={name}
        name={name}
        value={value}
        onChange={onChange}
        min={min}
        className={cn(INPUT_BASE, className)}
      />
    </div>
  )
}
