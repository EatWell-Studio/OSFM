import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

/**
 * The recovery codes a member receives at registration (D3), shown this once: the server keeps only their hashes. The
 * member has to confirm they saved them before going on.
 */
export function RecoveryCodes({ codes, onConfirmed }: { codes: string[]; onConfirmed: () => void }) {
  const { t } = useTranslation()
  const id = useId()
  const [saved, setSaved] = useState(false)
  return (
    <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-3">
      <h2 id={`${id}-heading`} className="text-lg font-medium">
        {t('recoveryCodes.title')}
      </h2>
      <p>{t('recoveryCodes.explain')}</p>
      <ol className="grid grid-cols-1 gap-1 rounded-lg border p-3 font-mono text-sm whitespace-nowrap sm:grid-cols-2">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ol>
      <label className="flex items-start gap-2">
        <input
          type="checkbox"
          checked={saved}
          onChange={(e) => {
            setSaved(e.target.checked)
          }}
          className="mt-1"
        />
        <span>{t('recoveryCodes.confirm')}</span>
      </label>
      <Button size="lg" disabled={!saved} onClick={onConfirmed} className="self-start">
        {t('recoveryCodes.continue')}
      </Button>
    </section>
  )
}
