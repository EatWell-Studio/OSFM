import { ChevronDown, ExternalLink as ExternalLinkIcon } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ExternalLink } from '@/api/types.gen'
import { cn } from '@/lib/utils'

/** The links that may be shown: absolute https URLs with a host (the server sends nothing else; this is a backstop). */
function shownLinks(links: readonly ExternalLink[] | undefined): { href: string; text: string }[] {
  const out: { href: string; text: string }[] = []
  for (const link of links ?? []) {
    let url: URL
    try {
      url = new URL(link.url)
    } catch {
      continue
    }
    if (url.protocol !== 'https:' || url.hostname === '' || url.username !== '' || url.password !== '') {
      continue
    }
    const label = link.label?.trim() ?? ''
    out.push({ href: url.href, text: label || url.host + (url.pathname === '/' ? '' : url.pathname) })
  }
  return out
}

/**
 * The external links of a recommendation's signature (D8), behind a toggle next to the name: give it the
 * externalLinks of the RecommendationView. The server sends links only with a main-name signature, never with a
 * pseudonym (E3), so a pseudonym recommendation gets no toggle at all; without links this renders nothing. Labels are
 * plain text; the links open in a new tab without a referrer, and nothing is fetched before the member clicks one.
 *
 *   <SignatureName name={rec.displayName} />
 *   <SignatureLinks links={rec.externalLinks} name={rec.displayName} />
 */
export function SignatureLinks({
  links,
  name,
  className,
}: {
  links: readonly ExternalLink[] | undefined
  /** The signature the links belong to, for the toggle's accessible name. */
  name: string
  className?: string
}) {
  const { t } = useTranslation('profile')
  const [open, setOpen] = useState(false)
  const listId = useId()
  const shown = shownLinks(links)
  if (shown.length === 0) {
    return null
  }
  return (
    <span className={cn('inline-flex flex-col items-start gap-1', className)}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={t('signatureLinks.toggle', { name, count: shown.length })}
        onClick={() => {
          setOpen((o) => !o)
        }}
        className="inline-flex items-center gap-1 rounded text-xs text-muted-foreground underline-offset-2 hover:underline focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <ExternalLinkIcon aria-hidden="true" className="size-3" />
        {t('signatureLinks.label', { count: shown.length })}
        <ChevronDown aria-hidden="true" className={cn('size-3 transition-transform', open && 'rotate-180')} />
      </button>
      <ul id={listId} hidden={!open} className="flex flex-col gap-1 text-sm">
        {shown.map((link) => (
          <li key={link.href}>
            <a
              href={link.href}
              target="_blank"
              rel="noopener noreferrer nofollow"
              referrerPolicy="no-referrer"
              className="break-all text-primary underline underline-offset-2"
              translate="no"
            >
              {link.text}
            </a>
          </li>
        ))}
      </ul>
    </span>
  )
}
