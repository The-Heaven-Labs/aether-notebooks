import { describe, test, expect } from 'vitest'
import { executableConnectorOptions } from './connectors'

interface TestConnector {
  id: string
  name: string
  is_provisioner?: boolean
  can_use?: boolean
}

const CONNECTORS: TestConnector[] = [
  { id: 'svc', name: 'Service', can_use: true },
  { id: 'prov-blocked', name: 'Blocked provisioner', is_provisioner: true, can_use: false },
  { id: 'prov-allowed', name: 'Allowed provisioner', is_provisioner: true, can_use: true },
  { id: 'prov-unknown', name: 'Unknown provisioner', is_provisioner: true },
  { id: 'view-only', name: 'View only', can_use: false },
]

function ids(connectors: TestConnector[]): string[] {
  return connectors.map(c => c.id)
}

describe('executableConnectorOptions', () => {
  test('hides a non-executable provisioner', () => {
    expect(ids(executableConnectorOptions(CONNECTORS))).not.toContain('prov-blocked')
  })

  test('keeps an allowed provisioner (can_use true)', () => {
    expect(ids(executableConnectorOptions(CONNECTORS))).toContain('prov-allowed')
  })

  test('keeps a provisioner whose can_use is undefined', () => {
    expect(ids(executableConnectorOptions(CONNECTORS))).toContain('prov-unknown')
  })

  test('keeps a non-provisioner can_use false connector untouched', () => {
    expect(ids(executableConnectorOptions(CONNECTORS))).toContain('view-only')
  })

  test('keeps the non-executable provisioner when it is the current selection', () => {
    expect(ids(executableConnectorOptions(CONNECTORS, 'prov-blocked'))).toContain('prov-blocked')
  })

  test('hides other non-executable provisioners even when one is selected', () => {
    const connectors: TestConnector[] = [
      { id: 'prov-a', name: 'A', is_provisioner: true, can_use: false },
      { id: 'prov-b', name: 'B', is_provisioner: true, can_use: false },
    ]
    expect(ids(executableConnectorOptions(connectors, 'prov-a'))).toEqual(['prov-a'])
  })

  test('does not keep a hidden connector for a different selected id', () => {
    expect(ids(executableConnectorOptions(CONNECTORS, 'svc'))).not.toContain('prov-blocked')
  })

  test('does not keep a hidden connector for null/undefined selections', () => {
    expect(ids(executableConnectorOptions(CONNECTORS, null))).not.toContain('prov-blocked')
    expect(ids(executableConnectorOptions(CONNECTORS, undefined))).not.toContain('prov-blocked')
  })

  test('only can_use === false hides — falsey-but-not-false values stay', () => {
    const connectors = [{ id: 'weird', name: 'Weird', is_provisioner: true, can_use: undefined }]
    expect(ids(executableConnectorOptions(connectors as TestConnector[]))).toEqual(['weird'])
  })

  test('returns an empty list unchanged', () => {
    expect(executableConnectorOptions([])).toEqual([])
  })
})
