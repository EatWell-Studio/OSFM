import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { instanceQueryKey } from '@/instance/instance'
import { authErrorCode } from '@/lib/authApi'
import {
  type CircleSettings as Settings,
  circleSettingsQuery,
  circleSettingsQueryKey,
  maxGeocoderContact,
  saveCircleSettings,
} from './circleSettingsApi'

/**
 * The report threshold (D11), the invite cooling-off period for new members (D12) and place search with its contact
 * (B7) on /admin/settings. A server that does not have these settings yet gets a note instead of the form.
 */
export function CircleSettings() {
  const { t } = useTranslation(['admin', 'common'])
  const headingId = useId()
  const query = useQuery(circleSettingsQuery)
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('circle.title')}
      </h2>
      {query.isPending ? (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      ) : query.isError ? (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>{t('circle.loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void query.refetch()
            }}
          >
            {t('common:retry')}
          </Button>
        </div>
      ) : query.data === null ? (
        <p className="text-sm text-muted-foreground">{t('circle.unavailable')}</p>
      ) : (
        <CircleSettingsForm saved={query.data} />
      )}
    </section>
  )
}

type FieldName = 'reportThreshold' | 'inviteCooldownDays' | 'geocoderContact'

/** A whole number of at least min typed into a field, or null. */
function wholeNumber(text: string, min: number): number | null {
  if (!/^\d+$/.test(text.trim())) {
    return null
  }
  const n = Number(text.trim())
  return Number.isSafeInteger(n) && n >= min ? n : null
}

function CircleSettingsForm({ saved }: { saved: Settings }) {
  const { t } = useTranslation('admin')
  const queryClient = useQueryClient()
  const geocoderId = useId()
  const [threshold, setThreshold] = useState(String(saved.reportThreshold))
  const [cooldown, setCooldown] = useState(String(saved.inviteCooldownDays))
  const [geocoderEnabled, setGeocoderEnabled] = useState(saved.geocoderEnabled)
  const [contact, setContact] = useState(saved.geocoderContact)
  const [errors, setErrors] = useState<Partial<Record<FieldName, string>>>({})
  const [notice, setNotice] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)

  const save = useMutation({
    mutationFn: saveCircleSettings,
    onSuccess: ({ settings, instance }) => {
      queryClient.setQueryData(circleSettingsQueryKey, settings)
      queryClient.setQueryData(instanceQueryKey, instance)
      setNotice(t('circle.saved'))
    },
    onError: (err) => {
      setFormError(authErrorCode(err) === null ? t('circle.errors.network') : t('circle.errors.invalid'))
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    setNotice(null)
    setFormError(null)
    const reportThreshold = wholeNumber(threshold, 1)
    const inviteCooldownDays = wholeNumber(cooldown, 0)
    const geocoderContact = contact.trim()
    const found: Partial<Record<FieldName, string>> = {}
    if (reportThreshold === null) {
      found.reportThreshold = t('circle.errors.reportThreshold')
    }
    if (inviteCooldownDays === null) {
      found.inviteCooldownDays = t('circle.errors.inviteCooldownDays')
    }
    if (Array.from(geocoderContact).length > maxGeocoderContact) {
      found.geocoderContact = t('circle.errors.geocoderContact')
    }
    setErrors(found)
    if (reportThreshold === null || inviteCooldownDays === null || Object.keys(found).length > 0) {
      return
    }
    save.mutate({ reportThreshold, inviteCooldownDays, geocoderEnabled, geocoderContact })
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {notice && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {notice}
        </p>
      )}
      <Field
        label={t('circle.reportThreshold')}
        hint={t('circle.reportThresholdHint')}
        error={errors.reportThreshold}
        inputMode="numeric"
        value={threshold}
        onChange={(e) => {
          setThreshold(e.target.value)
        }}
      />
      <Field
        label={t('circle.inviteCooldownDays')}
        hint={t('circle.inviteCooldownDaysHint')}
        error={errors.inviteCooldownDays}
        inputMode="numeric"
        value={cooldown}
        onChange={(e) => {
          setCooldown(e.target.value)
        }}
      />
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <Checkbox
            id={geocoderId}
            checked={geocoderEnabled}
            aria-describedby={`${geocoderId}-hint`}
            onCheckedChange={(checked) => {
              setGeocoderEnabled(checked === true)
            }}
          />
          <label htmlFor={geocoderId} className="text-sm font-medium">
            {t('circle.geocoderEnabled')}
          </label>
        </div>
        <p id={`${geocoderId}-hint`} className="text-xs text-muted-foreground">
          {t('circle.geocoderEnabledHint')}
        </p>
      </div>
      <Field
        label={t('circle.geocoderContact')}
        hint={t('circle.geocoderContactHint')}
        error={errors.geocoderContact}
        value={contact}
        onChange={(e) => {
          setContact(e.target.value)
        }}
      />
      {formError && (
        <p role="alert" className="text-sm text-destructive">
          {formError}
        </p>
      )}
      <Button type="submit" className="self-start" disabled={save.isPending}>
        {save.isPending ? t('circle.working') : t('circle.save')}
      </Button>
    </form>
  )
}
