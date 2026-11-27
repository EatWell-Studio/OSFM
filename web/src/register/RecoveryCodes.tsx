import { Check, Copy, Download } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

const fileName = 'osfm-recovery-codes.txt'

/**
 * The recovery codes of a new member (D3): shown once, with copy and download, and a confirmation that they were saved
 * before the member goes on. Used after registering with password and TOTP and after registering a passkey.
 */
export function RecoveryCodes({
  codes,
  onConfirmed,
  continueLabel,
}: {
  codes: string[]
  onConfirmed: () => void
  /** The label of the button that continues, when it does not lead to the map. */
  continueLabel?: string
}) {
  const { t } = useTranslation()
  const headingId = useId()
  const savedId = useId()
  const [saved, setSaved] = useState(false)
  const [copied, setCopied] = useState<boolean | null>(null)
  const text = `${t('recoveryCodes.fileHeader')}\n\n${codes.join('\n')}\n`

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  // A blob: URL, not a data: URL: the content security policy allows neither external nor data: URLs, and a download
  // of a same-document blob is not a fetch it restricts.
  const download = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
    const a = document.createElement('a')
    a.href = url
    a.download = fileName
    document.body.append(a)
    a.click()
    a.remove()
    setTimeout(() => {
      URL.revokeObjectURL(url)
    }, 0)
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('recoveryCodes.title')}
      </h2>
      <p>{t('recoveryCodes.explanation')}</p>
      <ol
        aria-label={t('recoveryCodes.title')}
        className="grid grid-cols-[repeat(auto-fill,minmax(9.5rem,1fr))] gap-x-3 gap-y-2 rounded-lg border bg-muted/40 p-3 font-mono text-[0.8rem] tabular-nums"
      >
        {codes.map((code) => (
          <li key={code} className="whitespace-nowrap select-all">
            {code}
          </li>
        ))}
      </ol>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          onClick={() => {
            void copy()
          }}
        >
          {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
          {copied ? t('recoveryCodes.copied') : t('recoveryCodes.copy')}
        </Button>
        <Button variant="outline" onClick={download}>
          <Download aria-hidden="true" />
          {t('recoveryCodes.download')}
        </Button>
      </div>
      {copied === false && (
        <p role="alert" className="text-sm text-destructive">
          {t('recoveryCodes.copyError')}
        </p>
      )}
      <p className="text-sm text-muted-foreground">{t('recoveryCodes.shownOnce')}</p>
      <label htmlFor={savedId} className="flex items-start gap-2">
        <input
          id={savedId}
          type="checkbox"
          checked={saved}
          onChange={(e) => {
            setSaved(e.target.checked)
          }}
          className="mt-1 size-4"
        />
        <span>{t('recoveryCodes.confirm')}</span>
      </label>
      <Button size="lg" disabled={!saved} onClick={onConfirmed} className="self-start">
        {continueLabel ?? t('recoveryCodes.continue')}
      </Button>
    </section>
  )
}
