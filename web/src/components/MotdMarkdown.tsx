import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

/**
 * MOTD banner body. Kept in its own module so the markdown renderer (and its
 * remark dependency chain) loads lazily: most pages render no MOTD at all.
 */
export default function MotdMarkdown({ content }: { content: string }) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
}
