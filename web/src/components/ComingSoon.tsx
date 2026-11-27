import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { PageHeading } from './PageHeading'

// Placeholders for the parts of M2 that are not built yet: each later task replaces its own file (a page or a
// section), which renders one of these until then.

/** A page that is still to come: its heading and a short note. */
export function ComingSoonPage({ title }: { title: string }) {
  const { t } = useTranslation()
  return (
    <>
      <PageHeading>{title}</PageHeading>
      <p className="px-4 text-muted-foreground">{t('comingSoon')}</p>
    </>
  )
}

/** A section of a page that is still to come. */
export function ComingSoonSection({ title }: { title: string }) {
  const { t } = useTranslation()
  const headingId = useId()
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-2">
      <h2 id={headingId} className="text-lg font-medium">
        {title}
      </h2>
      <p className="text-sm text-muted-foreground">{t('comingSoon')}</p>
    </section>
  )
}

/** For a route whose parameters are not valid (for example a place that is not a UUID). */
export function InvalidLinkPage() {
  const { t } = useTranslation()
  return (
    <>
      <PageHeading>{t('notFound.title')}</PageHeading>
      <p role="alert" className="px-4 text-muted-foreground">
        {t('invalidLink')}
      </p>
    </>
  )
}
