import { Download } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { rateLimitMessage } from '@/lib/rateLimit'
import { downloadPersonalExport, PersonalExportError } from './personalExportApi'

/** The download of all of the member's own data on the "me" page (E6), as one JSON file. */
export function PersonalExport() {
  const { t } = useTranslation('personalExport')
  const headingId = useId()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const download = async () => {
    setBusy(true)
    setError(null)
    try {
      await downloadPersonalExport()
    } catch (e) {
      setError(
        e instanceof PersonalExportError && e.status === 429 ? rateLimitMessage(e.retryAfter) : t('downloadError'),
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3">
      <h2 id={headingId} className="text-lg font-medium">
        {t('title')}
      </h2>
      <p className="text-sm">{t('explanation')}</p>
      <ul className="list-disc pl-5 text-sm text-muted-foreground">
        <li>{t('contents.account')}</li>
        <li>{t('contents.recommendations')}</li>
        <li>{t('contents.activity')}</li>
      </ul>
      <p className="text-sm text-muted-foreground">{t('privacy')}</p>
      <div>
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => {
            void download()
          }}
        >
          <Download aria-hidden="true" />
          {busy ? t('downloading') : t('download')}
        </Button>
      </div>
      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}
    </section>
  )
}
