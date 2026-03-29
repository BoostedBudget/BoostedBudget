import { ChevronDownIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { formatCurrency } from '@/lib/utils'
import type { AccountTypesResponse } from '../types'

interface AccountTypeFilterProps {
  accountTypes: AccountTypesResponse
  selectedTypes: string[]
  onChange: (types: string[]) => void
}

/**
 * AccountTypeFilter component for filtering dashboard by account type
 *
 * Features:
 * - Compact dropdown trigger with selection summary
 * - Multi-select checkboxes for each account type
 * - Shows account count and total balance for each type
 * - "All" option to select/deselect all types
 *
 * Issue #353: Balance Tracking Dashboard
 * Issue #832: Design system migration (DropdownMenu)
 */
export function AccountTypeFilter({
  accountTypes,
  selectedTypes,
  onChange,
}: AccountTypeFilterProps) {
  const types = accountTypes.types ?? []
  const allTypes = types.map((t) => t.key)
  const allSelected = selectedTypes.length === 0 || selectedTypes.length === allTypes.length

  const triggerLabel = allSelected
    ? 'All Account Types'
    : `${selectedTypes.length} of ${types.length} types`

  const handleToggleAll = () => {
    if (allSelected) {
      onChange([])
    } else {
      onChange(allTypes)
    }
  }

  const handleToggleType = (typeKey: string) => {
    if (selectedTypes.includes(typeKey)) {
      onChange(selectedTypes.filter((t) => t !== typeKey))
    } else {
      onChange([...selectedTypes, typeKey])
    }
  }

  const isTypeSelected = (typeKey: string) =>
    selectedTypes.length === 0 || selectedTypes.includes(typeKey)

  return (
    <div data-testid="account-type-filter">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" className="gap-2">
            {triggerLabel}
            <ChevronDownIcon className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuLabel>Account Types</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuCheckboxItem
            checked={allSelected}
            onCheckedChange={handleToggleAll}
          >
            All Types
          </DropdownMenuCheckboxItem>
          <DropdownMenuSeparator />
          {types.map((type) => (
            <DropdownMenuCheckboxItem
              key={type.key}
              checked={isTypeSelected(type.key)}
              onCheckedChange={() => handleToggleType(type.key)}
            >
              <div className="flex w-full items-center justify-between gap-2">
                <div>
                  <p className="font-medium">{type.label}</p>
                  <p className="text-xs text-muted-foreground">
                    {type.count} account{type.count !== 1 ? 's' : ''}
                  </p>
                </div>
                <span className="text-sm font-semibold">
                  {formatCurrency(type.total_balance)}
                </span>
              </div>
            </DropdownMenuCheckboxItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
