import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TextWidget } from './TextWidget'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import type { DashboardVariable } from '../types'

describe('TextWidget', () => {
  it('renders markdown headings', () => {
    render(<TextWidget markdown={'## What changed\n\nRevenue is up.'} />)
    expect(screen.getByRole('heading', { name: 'What changed' })).toBeInTheDocument()
  })
  it('renders raw HTML as text, never as markup', () => {
    render(<TextWidget markdown={'<img src=x onerror=alert(1)>'} />)
    expect(screen.queryByRole('img')).toBeNull()
  })
  it('keeps raw HTML as literal text instead of dropping it', () => {
    const { container } = render(<TextWidget markdown={'<img src=x onerror=alert(1)>'} />)
    expect(container.textContent).toContain('<img src=x onerror=alert(1)>')
  })
  it('interpolates declared variables from props', () => {
    const variables: DashboardVariable[] = [{ name: 'region', label: 'Region', type: 'text' }]
    render(<TextWidget markdown={'## {{region}} overview'} values={{ region: 'EMEA' }} variables={variables} />)
    expect(screen.getByRole('heading', { name: 'EMEA overview' })).toBeInTheDocument()
  })
  it('interpolates from the dashboard variables context when no props are given', () => {
    render(
      <MemoryRouter>
        <DashboardVariablesProvider dashboardId="text-widget-d1" variables={[{ name: 'region', label: 'Region', type: 'text', default: 'EMEA' }]}>
          <TextWidget markdown={'## {{region}} overview'} />
        </DashboardVariablesProvider>
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { name: 'EMEA overview' })).toBeInTheDocument()
  })
  it('shows a muted hint when there is nothing to render', () => {
    render(<TextWidget markdown={'  '} />)
    expect(screen.getByText('Empty text panel — edit it in the dashboard editor.')).toBeInTheDocument()
  })
})
