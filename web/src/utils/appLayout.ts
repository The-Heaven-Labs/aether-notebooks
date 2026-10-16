import type { CSSProperties } from 'react'

/**
 * The App surface's shared content width. The masthead, the filter strip, the
 * chapter tablist, and the widget body all constrain to this so the sections
 * align under one column.
 */
export const APP_CONTENT_MAX_WIDTH = 1200

/** The App surface's content-width mode. Absent means `fixed`. */
export type AppContentWidth = 'fixed' | 'full'

/**
 * The style that constrains an App surface section to the shared content width.
 * `fixed` (and an absent width, which defaults to fixed) centers the section at
 * `APP_CONTENT_MAX_WIDTH`; `full` spans the available width and returns
 * `undefined` so callers can spread it without overriding their own layout.
 */
export function appContentWidthStyle(
  width: AppContentWidth | undefined,
): CSSProperties | undefined {
  return width === 'full'
    ? undefined
    : { maxWidth: APP_CONTENT_MAX_WIDTH, margin: '0 auto' }
}
