interface Props {
  size?: number
  style?: React.CSSProperties
}

export function LoadingSpinner({ size = 8, style }: Props) {
  return (
    <div
      style={{
        width: size,
        height: size,
        borderRadius: '50%',
        background: 'var(--accent)',
        opacity: 0.5,
        ...style,
      }}
    />
  )
}