import { Check, Copy } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

/**
 * A link that is shown this once (an invite or reset link: the server keeps only the hash of its code), with a copy
 * button.
 */
export function ShownOnceLink({ link, label, note }: { link: string; label: string; note: string }) {
  const { t } = useTranslation()
  const linkId = useId()
  const [copied, setCopied] = useState<boolean | null>(null)
  return (
    <div className="flex flex-col gap-2 rounded-lg border bg-muted/40 p-3">
      <label htmlFor={linkId} className="text-sm font-medium">
        {label}
      </label>
      <div className="flex gap-2">
        <input
          id={linkId}
          readOnly
          value={link}
          onFocus={(e) => {
            e.target.select()
          }}
          className="h-8 min-w-0 flex-1 rounded-lg border bg-background px-2 font-mono text-xs"
        />
        <Button
          variant="outline"
          onClick={() => {
            navigator.clipboard.writeText(link).then(
              () => {
                setCopied(true)
              },
              () => {
                setCopied(false)
              },
            )
          }}
        >
          {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
          {copied ? t('copied') : t('copy')}
        </Button>
      </div>
      {copied === false && (
        <p role="alert" className="text-sm text-destructive">
          {t('copyError')}
        </p>
      )}
      <p className="text-sm text-muted-foreground">{note}</p>
    </div>
  )
}
