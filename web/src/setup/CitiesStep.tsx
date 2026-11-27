import { useMutation, useQuery } from '@tanstack/react-query'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { type InstanceCity, instanceQuery } from '@/instance/instance'
import { authErrorCode, authRetryAfter, rateLimitedCode } from '@/lib/authApi'
import { rateLimitMessage } from '@/lib/rateLimit'
import { type Bounds, citiesQueryKey, cityLabel, fetchCities } from '@/map/cities'
import { completeSetup, isValidInstanceName, parseBbox } from './setup'

const slugPattern = /^[a-z0-9-]{1,64}$/
const maxCityName = 64

/** Rounds a coordinate for display: 5 decimal places are about a metre. */
function round(n: number): number {
  return Math.round(n * 1e5) / 1e5
}

function bboxText(b: Bounds): string {
  return b.map(round).join(',')
}

function validName(name: string): boolean {
  const trimmed = name.trim()
  return trimmed !== '' && Array.from(trimmed).length <= maxCityName
}

/** A city entered by hand, for a basemap that may not be on the server yet. */
interface ManualCity {
  slug: string
  name: string
  bbox: Bounds
}

/**
 * The last step of the setup, for the signed-in first maintainer: the cities. The instance name saved with the
 * maintainer is shown and can still be corrected; left as it is, the request carries no name and the server keeps it. The basemap archives already in the server's tiles directory (GET /tiles/) are
 * offered with their bounds, each to be used or not under a display name; cities whose basemap is not there yet can be
 * added with a slug and a bounding box, and for those the step shows the deploy/tiles.sh command that extracts the
 * basemap, and a button to look for the archives again. At least one city; then the setup is complete. Reached again
 * after signing in when the wizard was left before this step.
 */
export function CitiesStep({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const instance = useQuery(instanceQuery)
  // The name as edited here; null while it is the saved one.
  const [name, setName] = useState<string | null>(null)
  const archives = useQuery({ queryKey: citiesQueryKey, queryFn: ({ signal }) => fetchCities(signal) })
  // Per archive id: whether it is used and under which name; archives not in here are used under their label.
  const [edits, setEdits] = useState<Record<string, { use: boolean; name: string }>>({})
  const [manual, setManual] = useState<ManualCity[]>([])
  const [error, setError] = useState<string | null>(null)

  const manualSlugs = new Set(manual.map((m) => m.slug))
  const archiveIds = new Set((archives.data ?? []).map((a) => a.id))
  // An archive that appeared for a city added by hand belongs to that city.
  const offered = (archives.data ?? []).filter((a) => !manualSlugs.has(a.id))
  const edit = (id: string) => edits[id] ?? { use: true, name: cityLabel({ id }) }

  const save = useMutation({
    mutationFn: completeSetup,
    onSuccess: onDone,
    onError: (err) => {
      switch (authErrorCode(err)) {
        case rateLimitedCode:
          setError(rateLimitMessage(t, authRetryAfter(err)))
          break
        case 'cities_invalid':
        case 'instance_name_invalid':
          setError(t('setup.cities.errors.invalid'))
          break
        case 'setup_done':
          setError(t('setup.errors.done'))
          break
        default:
          setError(t('setup.cities.errors.network'))
      }
    },
  })

  const finish = () => {
    const cities: InstanceCity[] = []
    for (const a of offered) {
      const e = edit(a.id)
      if (e.use) {
        cities.push({ slug: a.id, name: e.name.trim(), bbox: a.bounds, center: [a.center[0], a.center[1]] })
      }
    }
    for (const m of manual) {
      const [minLon, minLat, maxLon, maxLat] = m.bbox
      cities.push({ slug: m.slug, name: m.name, bbox: m.bbox, center: [(minLon + maxLon) / 2, (minLat + maxLat) / 2] })
    }
    if (cities.length === 0) {
      setError(t('setup.cities.errors.none'))
      return
    }
    if (!cities.every((c) => validName(c.name))) {
      setError(t('setup.cities.errors.name'))
      return
    }
    if (name !== null && !isValidInstanceName(name)) {
      setError(t('setup.name.required'))
      return
    }
    setError(null)
    save.mutate({ name: name?.trim() ?? '', cities })
  }

  return (
    <div className="flex flex-col gap-4">
      <Field
        label={t('setup.name.label')}
        hint={t('setup.cities.nameHint')}
        name="instance-name"
        autoComplete="off"
        maxLength={100}
        value={name ?? instance.data?.name ?? ''}
        onChange={(e) => {
          setName(e.target.value)
        }}
      />
      <p>{t('setup.cities.intro')}</p>

      <section className="flex flex-col gap-3">
        <h3 className="font-medium">{t('setup.cities.archives')}</h3>
        {archives.isPending && <p className="text-muted-foreground">{t('app.loading')}</p>}
        {archives.isError && <p role="alert">{t('map.loadError')}</p>}
        {archives.data && offered.length === 0 && <p>{t('setup.cities.noArchives')}</p>}
        {offered.map((a) => {
          const e = edit(a.id)
          return (
            <ArchiveRow
              key={a.id}
              slug={a.id}
              use={e.use}
              name={e.name}
              onChange={(next) => {
                setEdits((all) => ({ ...all, [a.id]: next }))
              }}
            />
          )
        })}
        <Button
          variant="outline"
          className="self-start"
          disabled={archives.isFetching}
          onClick={() => {
            void archives.refetch()
          }}
        >
          {t('setup.cities.recheck')}
        </Button>
      </section>

      {manual.map((m) => (
        <section key={m.slug} className="flex flex-col gap-2 rounded-lg border p-3">
          <p className="font-medium">{m.name}</p>
          <p className="text-sm text-muted-foreground">{t('setup.cities.slugLabel', { slug: m.slug })}</p>
          {archiveIds.has(m.slug) ? (
            <p className="text-sm">{t('setup.cities.hasTiles')}</p>
          ) : (
            <>
              <p className="text-sm">{t('setup.cities.noTiles')}</p>
              <code className="rounded border bg-muted/40 px-2 py-1 font-mono text-sm break-all select-all">
                {`deploy/tiles.sh ${m.slug} ${bboxText(m.bbox)}`}
              </code>
            </>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="self-start"
            onClick={() => {
              setManual((all) => all.filter((x) => x.slug !== m.slug))
            }}
          >
            {t('setup.cities.add.remove')}
          </Button>
        </section>
      ))}

      <AddCityForm
        taken={(slug) => manualSlugs.has(slug) || offered.some((a) => a.id === slug)}
        onAdd={(city) => {
          setManual((all) => [...all, city])
        }}
      />

      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button size="lg" disabled={save.isPending} onClick={finish}>
          {save.isPending ? t('setup.cities.working') : t('setup.cities.finish')}
        </Button>
      </div>
    </div>
  )
}

function ArchiveRow({
  slug,
  use,
  name,
  onChange,
}: {
  slug: string
  use: boolean
  name: string
  onChange: (next: { use: boolean; name: string }) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  return (
    <div className="flex flex-col gap-2 rounded-lg border p-3">
      <div className="flex items-center gap-2">
        <input
          id={id}
          type="checkbox"
          checked={use}
          onChange={(e) => {
            onChange({ use: e.target.checked, name })
          }}
          className="size-4"
        />
        <label htmlFor={id}>{t('setup.cities.include')}</label>
        <span className="text-sm text-muted-foreground">{t('setup.cities.slugLabel', { slug })}</span>
      </div>
      {use && (
        <Field
          label={t('setup.cities.name')}
          maxLength={maxCityName}
          value={name}
          onChange={(e) => {
            onChange({ use, name: e.target.value })
          }}
        />
      )}
    </div>
  )
}

function AddCityForm({ taken, onAdd }: { taken: (slug: string) => boolean; onAdd: (city: ManualCity) => void }) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [bbox, setBbox] = useState('')
  const [errors, setErrors] = useState<{ name?: string; slug?: string; bbox?: string }>({})

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const s = slug.trim().toLowerCase()
    const box = parseBbox(bbox)
    const next: typeof errors = {}
    if (!validName(name)) {
      next.name = t('setup.cities.errors.name')
    }
    if (!slugPattern.test(s)) {
      next.slug = t('setup.cities.errors.slug')
    } else if (taken(s)) {
      next.slug = t('setup.cities.errors.slugTaken')
    }
    if (box === null) {
      next.bbox = t('setup.cities.errors.bbox')
    }
    setErrors(next)
    if (Object.keys(next).length > 0 || box === null) {
      return
    }
    onAdd({ slug: s, name: name.trim(), bbox: box })
    setName('')
    setSlug('')
    setBbox('')
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-3 rounded-lg border border-dashed p-3">
      <h3 className="font-medium">{t('setup.cities.add.title')}</h3>
      <Field
        label={t('setup.cities.add.name')}
        error={errors.name}
        maxLength={maxCityName}
        value={name}
        onChange={(e) => {
          setName(e.target.value)
        }}
      />
      <Field
        label={t('setup.cities.add.slug')}
        hint={t('setup.cities.add.slugHint')}
        error={errors.slug}
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        maxLength={64}
        value={slug}
        onChange={(e) => {
          setSlug(e.target.value)
        }}
      />
      <Field
        label={t('setup.cities.add.bbox')}
        hint={t('setup.cities.add.bboxHint')}
        error={errors.bbox}
        inputMode="decimal"
        autoCorrect="off"
        spellCheck={false}
        value={bbox}
        onChange={(e) => {
          setBbox(e.target.value)
        }}
      />
      <Button type="submit" variant="outline" className="self-start">
        {t('setup.cities.add.submit')}
      </Button>
    </form>
  )
}
