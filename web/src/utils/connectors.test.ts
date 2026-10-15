import { describe, test, expect } from 'vitest'
import { connectorOptionLabel, executableConnectorOptions, isBlockedProvisioner } from './connectors'

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

describe('isBlockedProvisioner', () => {
  test('is true only for a provisioner with can_use false', () => {
    expect(isBlockedProvisioner({ is_provisioner: true, can_use: false })).toBe(true)
  })

  test('is false for an allowed provisioner', () => {
    expect(isBlockedProvisioner({ is_provisioner: true, can_use: true })).toBe(false)
  })

  test('is false for a non-provisioner that cannot be used', () => {
    expect(isBlockedProvisioner({ is_provisioner: false, can_use: false })).toBe(false)
  })

  test('is false when can_use is undefined', () => {
    expect(isBlockedProvisioner({ is_provisioner: true })).toBe(false)
  })
})

describe('connectorOptionLabel', () => {
  test('labels a blocked provisioner', () => {
    expect(connectorOptionLabel({ name: 'Provisioner', is_provisioner: true, can_use: false }))
      .toBe('Provisioner (provisioner — no access)')
  })

  test('labels an allowed provisioner', () => {
    expect(connectorOptionLabel({ name: 'Provisioner', is_provisioner: true, can_use: true }))
      .toBe('Provisioner (provisioner)')
  })

  test('labels a view-only connector', () => {
    expect(connectorOptionLabel({ name: 'Read replica', is_provisioner: false, can_use: false }))
      .toBe('Read replica (view only)')
  })

  test('leaves a usable connector unlabelled', () => {
    expect(connectorOptionLabel({ name: 'Service', can_use: true })).toBe('Service')
  })
})

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
