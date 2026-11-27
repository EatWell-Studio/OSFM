import { cn } from '@/lib/utils'

/**
 * The name a recommendation is signed with, as the server gives it (displayName: the main name, the pseudonym or the
 * text for a member who left; the app never works out which). Rendered as plain text only: never as HTML or Markdown,
 * never turned into a link here (the main name's external links are a separate control, and a pseudonym has none, E3).
 */
export function SignatureName({ name, className }: { name: string; className?: string }) {
  return (
    <span className={cn('font-medium break-words', className)} translate="no">
      {name}
    </span>
  )
}
