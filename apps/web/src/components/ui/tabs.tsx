/**
 * Tabs Component
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Built on Radix UI Tabs following shadcn/ui patterns.
 */

import * as React from "react"
import * as TabsPrimitive from "@radix-ui/react-tabs"

import { cn } from "@/lib/utils"

function Tabs({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn("flex flex-col", className)}
      {...props}
    />
  )
}

function TabsList({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(
        "inline-flex h-10 items-center justify-start gap-1 rounded-lg bg-gray-100 p-1 dark:bg-gray-800",
        className
      )}
      {...props}
    />
  )
}

function TabsTrigger({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium ring-offset-white transition-all",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2",
        "disabled:pointer-events-none disabled:opacity-50",
        "data-[state=active]:bg-white data-[state=active]:text-gray-900 data-[state=active]:shadow-sm",
        "data-[state=inactive]:text-gray-600 data-[state=inactive]:hover:text-gray-900",
        "dark:ring-offset-gray-900 dark:data-[state=active]:bg-gray-900 dark:data-[state=active]:text-gray-100",
        "dark:data-[state=inactive]:text-gray-400 dark:data-[state=inactive]:hover:text-gray-100",
        className
      )}
      {...props}
    />
  )
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn(
        "mt-4 ring-offset-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2",
        "dark:ring-offset-gray-900",
        className
      )}
      {...props}
    />
  )
}

export { Tabs, TabsList, TabsTrigger, TabsContent }
