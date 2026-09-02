import * as React from "react"
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { TableHead } from "@/components/ui/table"

export type SortDirection = "asc" | "desc"

export type SortState<K extends string = string> = {
  key: K
  direction: SortDirection
}

export type SortableTableHeadProps = React.ComponentProps<typeof TableHead> & {
  sortKey: string
  sortState: SortState
  onSort: (key: string) => void
}

/**
 * Drop-in replacement for `TableHead` on columns that should be sortable.
 * Fully controlled: it renders the current `sortState` and reports clicks via
 * `onSort`, but never tracks sort state itself — the consumer owns the
 * `SortState` and is responsible for updating it (e.g. toggling asc/desc).
 */
function SortableTableHead({
  sortKey,
  sortState,
  onSort,
  className,
  children,
  ...props
}: SortableTableHeadProps) {
  const active = sortState.key === sortKey
  const direction = active ? sortState.direction : undefined

  let Icon = ArrowUpDown
  if (direction === "asc") Icon = ArrowUp
  else if (direction === "desc") Icon = ArrowDown

  return (
    <TableHead
      className={cn(className)}
      aria-sort={
        active ? (direction === "asc" ? "ascending" : "descending") : "none"
      }
      {...props}
    >
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => onSort(sortKey)}
        className="-ml-2 h-7 cursor-pointer gap-1 px-2 text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        {children}
        <Icon className={cn("h-3.5 w-3.5", !active && "opacity-50")} />
      </Button>
    </TableHead>
  )
}

export { SortableTableHead }
