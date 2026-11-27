import { useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { instanceQueryKey } from '@/instance/instance'
import { CitiesStep } from '@/setup/CitiesStep'
import { updateInstance } from './instance'

/**
 * The instance's name and cities after the setup (maintainers): the setup wizard's cities step, starting from the
 * saved settings. The top bar and the map's city switcher follow once saved.
 */
export function InstanceSettings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const headingId = useId()
  const [saved, setSaved] = useState(false)
  // A new key after saving starts the form again from the saved settings.
  const [round, setRound] = useState(0)
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('pages.admin.instance.title')}
      </h2>
      {saved && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {t('pages.admin.instance.saved')}
        </p>
      )}
      <CitiesStep
        key={round}
        editing
        save={updateInstance}
        finishLabel={t('pages.admin.instance.save')}
        onDone={(instance) => {
          queryClient.setQueryData(instanceQueryKey, instance)
          setSaved(true)
          setRound((r) => r + 1)
        }}
      />
    </section>
  )
}
