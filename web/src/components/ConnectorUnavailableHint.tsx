/**
 * Muted inline note for execution pickers whose stored connector reference is
 * not among the rendered options (a non-executable provisioner is hidden from
 * every execution picker — see `executableConnectorOptions`). The picker falls
 * back to its placeholder; this hint makes that placeholder state legible
 * without offering the unusable connector as a choice.
 *
 * It is a sentence, not an instrument label, so it stays DM Sans (DESIGN.md's
 * Two Scripts Rule) at the muted 11px metadata size.
 */
interface ConnectorUnavailableHintProps {
  /** Optional id so callers can wire `aria-describedby` on their select. */
  id?: string
  title?: string
}

export function ConnectorUnavailableHint({
  id,
  title = "The stored connector can't be used for execution — pick another",
}: ConnectorUnavailableHintProps) {
  return (
    <span id={id} title={title} style={styles.hint}>
      Current connector is unavailable
    </span>
  )
}

const styles: Record<string, React.CSSProperties> = {
  hint: {
    fontSize: 11,
    fontFamily: 'var(--font-sans)',
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
    flexShrink: 0,
  },
}
