import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TextWidget, textConfigFrom } from './TextWidget'
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
  it('interpolates declared variables from props', () => {
    const variables: DashboardVariable[] = [{ name: 'region', label: 'Region', type: 'text' }]
    render(<TextWidget markdown={'## {{region}} overview'} values={{ region: 'EMEA' }} variables={variables} />)
    expect(screen.getByRole('heading', { name: 'EMEA overview' })).toBeInTheDocument()
  })
  it('shows a muted hint when there is nothing to render', () => {
    render(<TextWidget markdown={'  '} />)
    expect(screen.getByText('Empty text panel — edit it in the dashboard editor.')).toBeInTheDocument()
  })
})

describe('textConfigFrom', () => {
  it('reads config.text.markdown and defaults to empty', () => {
    expect(textConfigFrom({ text: { markdown: '## Hi' } })).toEqual({ markdown: '## Hi' })
    expect(textConfigFrom(undefined)).toEqual({ markdown: '' })
    expect(textConfigFrom({})).toEqual({ markdown: '' })
  })
})
