import { PanelHeader } from './PanelHeader'

export default {
  component: PanelHeader,
}

export const WithClose = {
  args: {
    title: 'Schema Browser',
    onClose: () => {},
  },
}

export const WithoutClose = {
  args: {
    title: 'Schedules',
  },
}

export const CustomStyle = {
  args: {
    title: 'Cell History',
    onClose: () => {},
    style: {
      background: 'white',
      borderBottom: '1px solid var(--border-light)',
      position: 'relative' as const,
    },
  },
}
