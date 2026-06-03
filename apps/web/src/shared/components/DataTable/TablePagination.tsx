import { ChevronLeft, ChevronRight } from 'lucide-react'

interface TablePaginationProps {
  currentPage: number
  totalPages: number
  hasMore: boolean
  onPageChange: (page: number) => void
}

export function TablePagination({ currentPage, totalPages, hasMore, onPageChange }: TablePaginationProps) {
  if (totalPages <= 1) return null
  return (
    <div className="flex items-center justify-between flex-wrap gap-3">
      <span className="text-sm text-muted-foreground">
        Page {currentPage} of {totalPages}
      </span>
      <div className="flex gap-2">
        <button
          type="button"
          className="inline-flex items-center gap-1 px-3.5 py-1.5 border border-border rounded-md bg-background text-foreground text-sm font-medium cursor-pointer transition-colors hover:bg-muted disabled:opacity-45 disabled:cursor-not-allowed"
          onClick={() => onPageChange(currentPage - 1)}
          disabled={currentPage <= 1}
          aria-label="Previous page"
        >
          <ChevronLeft size={16} />
          Previous
        </button>
        <button
          type="button"
          className="inline-flex items-center gap-1 px-3.5 py-1.5 border border-border rounded-md bg-background text-foreground text-sm font-medium cursor-pointer transition-colors hover:bg-muted disabled:opacity-45 disabled:cursor-not-allowed"
          onClick={() => onPageChange(currentPage + 1)}
          disabled={!hasMore}
          aria-label="Next page"
        >
          Next
          <ChevronRight size={16} />
        </button>
      </div>
    </div>
  )
}
