import { describe, test, expect } from 'vitest'
import { looksLikeEmail, normalizeEmail } from './email'

describe('looksLikeEmail', () => {
  test('accepts a plain address', () => expect(looksLikeEmail('future@example.com')).toBe(true))
  test('accepts surrounding whitespace', () => expect(looksLikeEmail('  future@example.com  ')).toBe(true))
  test('rejects a missing domain', () => expect(looksLikeEmail('future@')).toBe(false))
  test('rejects a missing local part', () => expect(looksLikeEmail('@example.com')).toBe(false))
  test('rejects inner whitespace', () => expect(looksLikeEmail('fu ture@example.com')).toBe(false))
  test('rejects a plain name', () => expect(looksLikeEmail('Alice')).toBe(false))
})

describe('normalizeEmail', () => {
  test('trims and lowercases', () =>
    expect(normalizeEmail('  Future.User@Example.COM ')).toBe('future.user@example.com'))
})
