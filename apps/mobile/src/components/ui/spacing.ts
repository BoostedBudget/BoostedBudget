export type SpacingStep = 0 | 1 | 2 | 3 | 4 | 5 | 6 | 8 | 10 | 12 | 16

export type SpacingProp =
  | 'p'
  | 'px'
  | 'py'
  | 'pt'
  | 'pb'
  | 'pl'
  | 'pr'
  | 'm'
  | 'mx'
  | 'my'
  | 'mt'
  | 'mb'
  | 'ml'
  | 'mr'
  | 'gap'

export const SPACING_CLASSES: Record<SpacingProp, Record<SpacingStep, string>> = {
  p: { 0: 'p-bb-0', 1: 'p-bb-1', 2: 'p-bb-2', 3: 'p-bb-3', 4: 'p-bb-4', 5: 'p-bb-5', 6: 'p-bb-6', 8: 'p-bb-8', 10: 'p-bb-10', 12: 'p-bb-12', 16: 'p-bb-16' },
  px: { 0: 'px-bb-0', 1: 'px-bb-1', 2: 'px-bb-2', 3: 'px-bb-3', 4: 'px-bb-4', 5: 'px-bb-5', 6: 'px-bb-6', 8: 'px-bb-8', 10: 'px-bb-10', 12: 'px-bb-12', 16: 'px-bb-16' },
  py: { 0: 'py-bb-0', 1: 'py-bb-1', 2: 'py-bb-2', 3: 'py-bb-3', 4: 'py-bb-4', 5: 'py-bb-5', 6: 'py-bb-6', 8: 'py-bb-8', 10: 'py-bb-10', 12: 'py-bb-12', 16: 'py-bb-16' },
  pt: { 0: 'pt-bb-0', 1: 'pt-bb-1', 2: 'pt-bb-2', 3: 'pt-bb-3', 4: 'pt-bb-4', 5: 'pt-bb-5', 6: 'pt-bb-6', 8: 'pt-bb-8', 10: 'pt-bb-10', 12: 'pt-bb-12', 16: 'pt-bb-16' },
  pb: { 0: 'pb-bb-0', 1: 'pb-bb-1', 2: 'pb-bb-2', 3: 'pb-bb-3', 4: 'pb-bb-4', 5: 'pb-bb-5', 6: 'pb-bb-6', 8: 'pb-bb-8', 10: 'pb-bb-10', 12: 'pb-bb-12', 16: 'pb-bb-16' },
  pl: { 0: 'pl-bb-0', 1: 'pl-bb-1', 2: 'pl-bb-2', 3: 'pl-bb-3', 4: 'pl-bb-4', 5: 'pl-bb-5', 6: 'pl-bb-6', 8: 'pl-bb-8', 10: 'pl-bb-10', 12: 'pl-bb-12', 16: 'pl-bb-16' },
  pr: { 0: 'pr-bb-0', 1: 'pr-bb-1', 2: 'pr-bb-2', 3: 'pr-bb-3', 4: 'pr-bb-4', 5: 'pr-bb-5', 6: 'pr-bb-6', 8: 'pr-bb-8', 10: 'pr-bb-10', 12: 'pr-bb-12', 16: 'pr-bb-16' },
  m: { 0: 'm-bb-0', 1: 'm-bb-1', 2: 'm-bb-2', 3: 'm-bb-3', 4: 'm-bb-4', 5: 'm-bb-5', 6: 'm-bb-6', 8: 'm-bb-8', 10: 'm-bb-10', 12: 'm-bb-12', 16: 'm-bb-16' },
  mx: { 0: 'mx-bb-0', 1: 'mx-bb-1', 2: 'mx-bb-2', 3: 'mx-bb-3', 4: 'mx-bb-4', 5: 'mx-bb-5', 6: 'mx-bb-6', 8: 'mx-bb-8', 10: 'mx-bb-10', 12: 'mx-bb-12', 16: 'mx-bb-16' },
  my: { 0: 'my-bb-0', 1: 'my-bb-1', 2: 'my-bb-2', 3: 'my-bb-3', 4: 'my-bb-4', 5: 'my-bb-5', 6: 'my-bb-6', 8: 'my-bb-8', 10: 'my-bb-10', 12: 'my-bb-12', 16: 'my-bb-16' },
  mt: { 0: 'mt-bb-0', 1: 'mt-bb-1', 2: 'mt-bb-2', 3: 'mt-bb-3', 4: 'mt-bb-4', 5: 'mt-bb-5', 6: 'mt-bb-6', 8: 'mt-bb-8', 10: 'mt-bb-10', 12: 'mt-bb-12', 16: 'mt-bb-16' },
  mb: { 0: 'mb-bb-0', 1: 'mb-bb-1', 2: 'mb-bb-2', 3: 'mb-bb-3', 4: 'mb-bb-4', 5: 'mb-bb-5', 6: 'mb-bb-6', 8: 'mb-bb-8', 10: 'mb-bb-10', 12: 'mb-bb-12', 16: 'mb-bb-16' },
  ml: { 0: 'ml-bb-0', 1: 'ml-bb-1', 2: 'ml-bb-2', 3: 'ml-bb-3', 4: 'ml-bb-4', 5: 'ml-bb-5', 6: 'ml-bb-6', 8: 'ml-bb-8', 10: 'ml-bb-10', 12: 'ml-bb-12', 16: 'ml-bb-16' },
  mr: { 0: 'mr-bb-0', 1: 'mr-bb-1', 2: 'mr-bb-2', 3: 'mr-bb-3', 4: 'mr-bb-4', 5: 'mr-bb-5', 6: 'mr-bb-6', 8: 'mr-bb-8', 10: 'mr-bb-10', 12: 'mr-bb-12', 16: 'mr-bb-16' },
  gap: { 0: 'gap-bb-0', 1: 'gap-bb-1', 2: 'gap-bb-2', 3: 'gap-bb-3', 4: 'gap-bb-4', 5: 'gap-bb-5', 6: 'gap-bb-6', 8: 'gap-bb-8', 10: 'gap-bb-10', 12: 'gap-bb-12', 16: 'gap-bb-16' },
}

export type FlexAlign = 'start' | 'center' | 'end' | 'stretch' | 'baseline'

export const FLEX_ALIGN_CLASSES: Record<FlexAlign, string> = {
  start: 'items-start',
  center: 'items-center',
  end: 'items-end',
  stretch: 'items-stretch',
  baseline: 'items-baseline',
}

export type FlexJustify = 'start' | 'center' | 'end' | 'between' | 'around' | 'evenly'

export const FLEX_JUSTIFY_CLASSES: Record<FlexJustify, string> = {
  start: 'justify-start',
  center: 'justify-center',
  end: 'justify-end',
  between: 'justify-between',
  around: 'justify-around',
  evenly: 'justify-evenly',
}
