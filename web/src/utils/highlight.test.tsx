import { describe, test, expect } from 'vitest'
import { render } from '@testing-library/react'
import { highlight, HighlightedText } from './highlight'

function renderNodes(text: string, query: string) {
  const { container } = render(<HighlightedText text={text} query={query} />)
  return container
}

function markTexts(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('mark.search-hit')).map((mark) => mark.textContent ?? '')
}

describe('highlight', () => {
  test('is case-insensitive and wraps the match in a search-hit mark', () => {
    const container = renderNodes('Revenue by region', 'revenue')
    expect(markTexts(container)).toEqual(['Revenue'])
    expect(container.textContent).toBe('Revenue by region')
  })

  test('highlights every whitespace-separated term', () => {
    const container = renderNodes('Revenue by region and status', 'revenue status')
    expect(markTexts(container)).toEqual(['Revenue', 'status'])
    expect(container.textContent).toBe('Revenue by region and status')
  })

  test('treats regex metacharacters as literal text', () => {
    const container = renderNodes('metrics a.b and a_b and (parent)', 'a.b (parent)')
    expect(markTexts(container)).toEqual(['a.b', '(parent)'])
  })

  test('a longer term wins over a shorter prefix', () => {
    const container = renderNodes('queries about query planning', 'query queries')
    expect(markTexts(container)).toEqual(['queries', 'query'])
  })

  test('no match returns the text as one plain text node', () => {
    const container = renderNodes('Revenue by region', 'zzz')
    expect(container.querySelectorAll('mark')).toHaveLength(0)
    expect(container.childNodes).toHaveLength(1)
    expect(container.childNodes[0].nodeType).toBe(Node.TEXT_NODE)
    expect(container.textContent).toBe('Revenue by region')
  })

  test('an empty query never wraps anything', () => {
    expect(highlight('abc', '   ')).toEqual(['abc'])
    expect(highlight('abc', '')).toEqual(['abc'])
  })

  test('markup in the text stays text, never HTML', () => {
    const evil = '<img src=x onerror=alert(1)>'
    const container = renderNodes(evil, 'img')
    expect(container.querySelector('img')).toBeNull()
    expect(container.textContent).toBe(evil)
    expect(markTexts(container)).toEqual(['img'])
  })
})
