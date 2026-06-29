import { render, screen } from '@testing-library/react'
import { HierarchyTreeModule } from '../charts/HierarchyTreeChart'
import { test, expect, vi } from 'vitest'

// ECharts uses ResizeObserver
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const treeData = {
  columns: [
    { name: 'pid', type: 'int4' },
    { name: 'ppid', type: 'int4' },
    { name: 'name', type: 'text' },
    { name: 'cpu', type: 'float' },
  ],
  rows: [
    [1, 0, 'init', 0.1],
    [2, 1, 'sshd', 0.5],
    [3, 1, 'nginx', 1.2],
    [4, 2, 'sshd-session', 0.3],
  ],
}

test('renders hierarchy tree', () => {
  render(
    <HierarchyTreeModule.Component
      data={treeData}
      config={{
        chartType: 'hierarchy_tree',
        idColumn: 'pid',
        parentIdColumn: 'ppid',
        labelColumn: 'name',
        metricColumns: ['cpu'],
        layout: 'top-down',
      }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('auto-detects parent-child columns', () => {
  const detected = HierarchyTreeModule.detectColumns(treeData.columns, treeData.rows)
  expect(detected.idColumn).toBe('pid')
  expect(detected.parentIdColumn).toBe('ppid')
})

test('config panel renders layout selector', () => {
  const onChange = vi.fn()
  render(
    <HierarchyTreeModule.ConfigPanel
      config={{ chartType: 'hierarchy_tree' }}
      columns={['pid', 'ppid', 'name', 'cpu']}
      onChange={onChange}
    />
  )
  expect(screen.getByText('Layout')).toBeInTheDocument()
  expect(screen.getByText('ID column')).toBeInTheDocument()
  expect(screen.getByText('Parent ID column')).toBeInTheDocument()
})
