import { calendarDateRange, historyDateKey } from './utils'

/**
 * Today's UTC calendar date as a YYYY-MM-DD key.
 *
 * UTC, because every history series it bounds is keyed on the UTC calendar date the backend
 * derived (`(snapshot_at AT TIME ZONE 'UTC')::date`), and the date presets resolve "today" the
 * same way. `toISOString` is UTC by definition, so there is no local-field variant of this to
 * get wrong.
 */
export function todayDateKeyUTC(now: Date = new Date()): string {
  return now.toISOString().slice(0, 10)
}

/**
 * The last calendar day a forward-fill may fabricate: the newest real observation, or today,
 * whichever is EARLIER. Returns null when there is nothing to fill from.
 *
 * Both halves of that minimum matter. Filling to the requested end date is what made the
 * Balance History chart plot a flat line out to Sep 29 while the newest snapshot was Sep 8
 * (#1626 Phase 3, Defect 1) — a chart that ends where the data ends tells the truth, one that
 * runs past it implies we know a balance we have never observed. And stopping merely at "the
 * array's last row" is not enough on its own: a snapshot dated in the future would raise that
 * cutoff and take the whole fabricated tail with it.
 *
 * Keys are YYYY-MM-DD, so lexicographic comparison IS chronological comparison.
 */
export function forwardFillCutoff(
  dateKeys: string[],
  todayKey: string = todayDateKeyUTC()
): string | null {
  let newest: string | null = null
  for (const key of dateKeys) {
    if (newest === null || key > newest) newest = key
  }
  if (newest === null) return null
  return newest < todayKey ? newest : todayKey
}

/**
 * Forward-fill a daily history series so a chart gets a continuous line.
 *
 * Each day in [startDate, cutoff] without an observation inherits the previous day's values via
 * `carry`; `cutoff` is forwardFillCutoff's minimum of the newest observation and today, further
 * bounded by `endDate`. Days BEFORE the first observation are skipped rather than back-filled —
 * we have no earlier value to carry, and inventing one would fabricate history.
 *
 * A real observation beyond the cutoff is kept as itself: never dropped (it is data), never
 * turned into the head of a fabricated ramp (the days leading up to it are not data).
 *
 * @param rows - observations ordered by date ascending, each keyed by an RFC3339 or date-only
 *               `date`.
 * @param carry - builds the filled row for `dateKey` from the previous observation.
 */
export function forwardFillByDay<T extends { date: string }>(
  rows: T[],
  startDate: string,
  endDate: string,
  carry: (previous: T, dateKey: string) => T,
  todayKey: string = todayDateKeyUTC()
): T[] {
  const byDay = new Map<string, T>()
  for (const row of rows) {
    byDay.set(historyDateKey(row.date), row)
  }

  const cutoff = forwardFillCutoff([...byDay.keys()], todayKey)
  if (cutoff === null) return []

  const endKey = historyDateKey(endDate)
  const fillEnd = endKey < cutoff ? endKey : cutoff

  const filled: T[] = []
  let previous: T | null = null

  for (const dateKey of calendarDateRange(startDate, fillEnd)) {
    const observed = byDay.get(dateKey)
    if (observed) {
      filled.push(observed)
      previous = observed
    } else if (previous) {
      filled.push(carry(previous, dateKey))
    }
  }

  for (const row of rows) {
    if (historyDateKey(row.date) > fillEnd) filled.push(row)
  }

  return filled
}
