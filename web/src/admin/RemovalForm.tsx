import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { MaxRemovalReasonLength, RecommendationsHide, RecommendationsKeep } from '@/api/types.gen'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

export type RecommendationsChoice = typeof RecommendationsHide | typeof RecommendationsKeep

const choices = [
  { value: RecommendationsHide, label: 'remove.hide', hint: 'remove.hideHint' },
  { value: RecommendationsKeep, label: 'remove.keep', hint: 'remove.keepHint' },
] as const satisfies readonly { value: RecommendationsChoice; label: string; hint: string }[]

/**
 * What happens to a removed member's recommendations (hide or keep) and why, before a removal of one member or of a
 * branch (D7). Both are required; the reason is plain text for the audit log and the maintainers.
 */
export function RemovalForm({
  confirmLabel,
  pending,
  onConfirm,
  onCancel,
}: {
  confirmLabel: string
  pending: boolean
  onConfirm: (choice: RecommendationsChoice, reason: string) => void
  onCancel: () => void
}) {
  const { t } = useTranslation('members')
  const name = useId()
  const reasonId = useId()
  const [choice, setChoice] = useState<RecommendationsChoice | null>(null)
  const [reason, setReason] = useState('')
  const trimmed = reason.trim()
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (choice && trimmed) {
      onConfirm(choice, trimmed)
    }
  }
  return (
    <form className="flex flex-col gap-3" onSubmit={submit}>
      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-medium">{t('remove.recommendations')}</legend>
        {choices.map(({ value, label, hint }) => (
          <label key={value} className="flex items-start gap-2 text-sm">
            <input
              type="radio"
              name={name}
              value={value}
              checked={choice === value}
              onChange={() => {
                setChoice(value)
              }}
              className="mt-1"
            />
            <span>
              {t(label)}
              <span className="block text-muted-foreground">{t(hint)}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <div className="flex flex-col gap-1">
        <label htmlFor={reasonId} className="text-sm font-medium">
          {t('remove.reason')}
        </label>
        <Textarea
          id={reasonId}
          value={reason}
          maxLength={MaxRemovalReasonLength}
          onChange={(e) => {
            setReason(e.target.value)
          }}
        />
        <p className="text-xs text-muted-foreground">{t('remove.reasonHint')}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="destructive" disabled={pending || !choice || !trimmed}>
          {confirmLabel}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          {t('cancel')}
        </Button>
      </div>
    </form>
  )
}
