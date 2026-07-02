export function slugify(text: string): string | null {
  if (!text?.trim()) return null
  return text.toLowerCase().replace(/\s+/g, '-').replace(/[^\w-]/g, '')
}

export function getFirstHeadingSlug(source: string): string | null {
  const match = source.match(/^#{1,6}\s+(.+)$/m)
  if (!match) return null
  return slugify(match[1])
}

export function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  return h
}
