import type { CSSProperties, ReactNode, Ref } from 'react'
import type { WidgetAppearance } from '../utils/widgetAppearance'

interface WidgetFrameProps {
  appearance: WidgetAppearance
  children: ReactNode
  /**
   * Pass-through for layout hosts that clone their child — react-grid-layout
   * supplies `className`, `style`, and `ref` to its direct child, so the frame
   * merges them (host styles win) instead of dropping grid positioning.
   */
  className?: string
  style?: CSSProperties
  ref?: Ref<HTMLDivElement>
}

const PADDING: Record<WidgetAppearance['padding'], string> = { none: '0px', sm: '8px', md: '16px' }

/**
 * Outer chrome for a dashboard widget.
 *
 * The defaults reproduce the pre-appearance widget card exactly
 * (`var(--bg-card)`, `1px solid var(--border-light)`, 4px radius, no outer
 * padding), so wiring this in changes nothing until an author opts into
 * `config.appearance`.
 *
 * `padding` is *outer* padding around the widget content: `none` (the default)
 * preserves content widgets' own inner padding, while `sm` (8px) and `md`
 * (16px) add breathing room around the content. A border is only drawn on the
 * `card` background — transparent widgets never get one, regardless of the
 * `border` flag.
 */
export function WidgetFrame({ appearance, children, className, style, ref }: WidgetFrameProps) {
  return (
    <div ref={ref} className={className} style={{ ...frameStyle(appearance), ...style }}>
      {children}
    </div>
  )
}

function frameStyle(a: WidgetAppearance): CSSProperties {
  return {
    background: a.background === 'card' ? 'var(--bg-card)' : 'transparent',
    border: a.border && a.background === 'card' ? '1px solid var(--border-light)' : 'none',
    padding: PADDING[a.padding],
    textAlign: a.align,
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    borderRadius: 4,
  }
}
