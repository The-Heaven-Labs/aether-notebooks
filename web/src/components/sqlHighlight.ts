import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags } from '@lezer/highlight'

export const sqlHighlight = HighlightStyle.define([
  { tag: tags.keyword, class: 'cm-keyword' },
  { tag: tags.string, class: 'cm-string' },
  { tag: tags.comment, class: 'cm-comment' },
  { tag: tags.function(tags.name), class: 'cm-function' },
  { tag: tags.number, class: 'cm-number' },
])

export { syntaxHighlighting }
