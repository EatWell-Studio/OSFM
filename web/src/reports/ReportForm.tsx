import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { MaxReportNoteLength, ReportReasonOther } from '@/api/types.gen'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { type ReportReason, reportReasons } from './reportsApi'

/**
 * The report form of one recommendation (D11, M2-Q8): a reason (required) and a note (required for "other"). Inline,
 * not a modal dialog.
 */
export function ReportForm({
  pending,
  error,
  onSubmit,
  onCancel,
}: {
  pending: boolean
  error: string | null
  onSubmit: (reason: ReportReason, note: string) => void
  onCancel: () => void
}) {
  const { t } = useTranslation('reports')
  const headingId = useId()
  const name = useId()
  const noteId = useId()
  const [reason, setReason] = useState<ReportReason | null>(null)
  const [note, setNote] = useState('')
  const trimmed = note.trim()
  const noteRequired = reason === ReportReasonOther
  const ready = reason !== null && (!noteRequired || trimmed !== '')
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (reason !== null && ready) {
      onSubmit(reason, trimmed)
    }
  }
  return (
    <form
      aria-labelledby={headingId}
      className="flex basis-full flex-col gap-3 rounded-lg border bg-muted/40 p-3"
      onSubmit={submit}
    >
      <h4 id={headingId} className="font-medium">
        {t('report.title')}
      </h4>
      <p className="text-muted-foreground">{t('report.intro')}</p>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      <fieldset className="flex flex-col gap-2">
        <legend className="font-medium">{t('report.reasonLegend')}</legend>
        {reportReasons.map((value) => (
          <label key={value} className="flex items-start gap-2">
            <input
              type="radio"
              name={name}
              value={value}
              checked={reason === value}
              onChange={() => {
                setReason(value)
              }}
              className="mt-1"
            />
            <span>{t(`reason.${value}`)}</span>
          </label>
        ))}
      </fieldset>
      <div className="flex flex-col gap-1">
        <label htmlFor={noteId} className="font-medium">
          {noteRequired ? t('report.noteRequired') : t('report.note')}
        </label>
        <Textarea
          id={noteId}
          value={note}
          maxLength={MaxReportNoteLength}
          required={noteRequired}
          onChange={(e) => {
            setNote(e.target.value)
          }}
        />
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={pending || !ready}>
          {t('report.submit')}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          {t('report.cancel')}
        </Button>
      </div>
    </form>
  )
}
