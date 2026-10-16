import { describe, test, expect } from 'vitest'
import { resourceHref } from './resources'

describe('resourceHref', () => {
  test('a notebook without a matched cell keeps its plain route', () => {
    expect(resourceHref('notebook', 'nb-1')).toBe('/notebooks/nb-1')
  })

  test('a notebook content hit deep-links to the matched cell', () => {
    expect(resourceHref('notebook', 'nb-1', 'a1b2c3')).toBe('/notebooks/nb-1#cell-a1b2c3')
  })

  test('an empty cell id is treated as absent', () => {
    expect(resourceHref('notebook', 'nb-1', '')).toBe('/notebooks/nb-1')
  })

  test('other types ignore a cell id and keep their routes', () => {
    expect(resourceHref('dashboard', 'd-1', 'a1b2c3')).toBe('/dashboards/d-1/view')
    expect(resourceHref('folder', 'f-1', 'a1b2c3')).toBe('/?folder=f-1')
    expect(resourceHref('connector', 'c-1', 'a1b2c3')).toBe('/connectors')
    expect(resourceHref('agent', 'a-1', 'a1b2c3')).toBe('/agents')
    expect(resourceHref('model_config', 'm-1', 'a1b2c3')).toBe('/models')
    expect(resourceHref('skill', 's-1', 'a1b2c3')).toBe('/skills')
    expect(resourceHref('tool', 't-1', 'a1b2c3')).toBe('/tools')
    expect(resourceHref('mcp_server', 'x-1', 'a1b2c3')).toBe('/mcps')
  })
})
