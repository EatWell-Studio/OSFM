import { useQuery } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { instanceQuery } from '@/instance/instance'
import { retryAfterMinutes } from '@/lib/rateLimit'
import { fetchLayerExport, type Layer, LayerExportError, saveFile } from './layerExportApi'

const layers: Layer[] = ['places', 'recommendations']

/**
 * The GeoJSON downloads of the places and recommendations layers on the "me" page (F1, M2-Q17): what each file holds
 * and under which licence, a choice of the whole map or one city (when the instance has several), and a button per
 * layer. The files hold only what every member sees: no private or hidden recommendations, no member data in the
 * places layer.
 */
export function LayerExport() {
  const { t } = useTranslation('layerExport')
  const headingId = useId()
  const cityId = useId()
  const instance = useQuery(instanceQuery)
  const cities = instance.data?.cities ?? []
  const [city, setCity] = useState('')
  const [pending, setPending] = useState<Layer | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState<string | null>(null)
  // A city that is no longer configured falls back to the whole map.
  const chosen = cities.some((c) => c.slug === city) ? city : ''

  async function download(layer: Layer) {
    setPending(layer)
    setError(null)
    setSaved(null)
    try {
      const { blob, fileName } = await fetchLayerExport(layer, chosen)
      saveFile(blob, fileName)
      setSaved(fileName)
    } catch (err) {
      if (err instanceof LayerExportError && err.status === 429) {
        setError(
          err.retryAfter === null
            ? t('rateLimitedLater')
            : t('rateLimited', { count: retryAfterMinutes(err.retryAfter) }),
        )
      } else {
        setError(t('error'))
      }
    } finally {
      setPending(null)
    }
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3">
      <h2 id={headingId} className="text-lg font-medium">
        {t('title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('intro')}</p>
      {cities.length > 1 && (
        <label htmlFor={cityId} className="flex flex-col gap-1 self-start text-sm">
          {t('scope')}
          <select
            id={cityId}
            value={chosen}
            onChange={(e) => {
              setCity(e.target.value)
            }}
            className="h-8 rounded-lg border bg-background px-2"
          >
            <option value="">{t('allCities')}</option>
            {cities.map((c) => (
              <option key={c.slug} value={c.slug}>
                {c.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <ul className="flex flex-col gap-3">
        {layers.map((layer) => (
          <li key={layer} className="flex flex-col gap-2 rounded-lg border p-3">
            <p className="text-sm">{t(`${layer}.description`)}</p>
            <p className="text-xs text-muted-foreground">{t(`${layer}.license`)}</p>
            <Button
              variant="outline"
              className="self-start"
              disabled={pending !== null}
              onClick={() => {
                void download(layer)
              }}
            >
              {pending === layer ? t('downloading') : t(`${layer}.download`)}
            </Button>
          </li>
        ))}
      </ul>
      {error !== null && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {saved !== null && (
        <p role="status" className="text-sm text-muted-foreground">
          {t('saved', { fileName: saved })}
        </p>
      )}
    </section>
  )
}
