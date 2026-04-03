interface Props {
  width?: number | string
  height?: number
  borderRadius?: number
  style?: React.CSSProperties
}

export function Skeleton({ width = '100%', height = 16, borderRadius = 4, style }: Props) {
  return (
    <div
      style={{
        width,
        height,
        borderRadius,
        background: 'var(--bg-secondary)',
        animation: 'skeleton-pulse 1.5s ease-in-out infinite',
        ...style,
      }}
    />
  )
}

export function SkeletonRow({ columns = 3 }: { columns?: number }) {
  return (
    <tr style={styles.row}>
      {Array.from({ length: columns }).map((_, i) => (
        <td key={i} style={styles.cell}>
          <Skeleton width="80%" height={14} />
        </td>
      ))}
    </tr>
  )
}

export function SkeletonCard() {
  return (
    <div style={styles.card}>
      <Skeleton width={24} height={24} borderRadius={6} style={{ marginBottom: 12 }} />
      <Skeleton width="70%" height={18} style={{ marginBottom: 8 }} />
      <Skeleton width="50%" height={14} />
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  row: {
    borderBottom: '1px solid var(--border-light)',
  },
  cell: {
    padding: '12px 16px',
  },
  card: {
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: 16,
  },
}