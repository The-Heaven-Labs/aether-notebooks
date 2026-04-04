import type { StoryObj } from '@storybook/react-vite'
import { Skeleton, SkeletonRow, SkeletonCard } from './Skeleton'

const meta = {
  title: 'Components/Skeleton',
}

export default meta

export const BasicSkeleton: StoryObj<typeof Skeleton> = {
  render: () => <Skeleton width={200} height={16} />,
}

export const SkeletonVariants: StoryObj<typeof Skeleton> = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Skeleton width="100%" height={20} />
      <Skeleton width="80%" height={16} />
      <Skeleton width="60%" height={14} />
    </div>
  ),
}

export const TableRow: StoryObj<typeof SkeletonRow> = {
  render: () => (
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <tbody>
        <SkeletonRow columns={4} />
        <SkeletonRow columns={4} />
        <SkeletonRow columns={4} />
      </tbody>
    </table>
  ),
}

export const CardGrid: StoryObj<typeof SkeletonCard> = {
  render: () => (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16 }}>
      <SkeletonCard />
      <SkeletonCard />
      <SkeletonCard />
    </div>
  ),
}