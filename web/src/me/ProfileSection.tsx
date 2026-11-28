import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { isValidName } from '@/lib/names'
import { rateLimitMessage } from '@/lib/rateLimit'
import { sessionQueryKey } from '@/session/session'
import {
  isValidLinkLabel,
  isValidLinkURL,
  MaxExternalLinks,
  type Profile,
  ProfileError,
  profileQuery,
  profileQueryKey,
  saveProfile,
} from './profileApi'

/**
 * The member's main name, pseudonym and external links on the "me" page (D8, C3, M2-Q6). The main name signs
 * recommendations and shows the external links; the pseudonym signs the recommendations the member chooses to sign with
 * it, never with links, and no other member can tell whose it is.
 */
export function ProfileSection() {
  const { t } = useTranslation(['profile', 'common'])
  const headingId = useId()
  const query = useQuery(profileQuery)
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('title')}
      </h2>
      {query.isPending ? (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      ) : query.isError ? (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>{t('loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void query.refetch()
            }}
          >
            {t('common:retry')}
          </Button>
        </div>
      ) : (
        <ProfileForm saved={query.data} />
      )}
    </section>
  )
}

type Errors = Partial<Record<'displayName' | 'pseudonym' | 'links' | 'form', string>>

interface LinkRow {
  key: number
  url: string
  label: string
}

let nextRowKey = 0

function rowsOf(profile: Profile): LinkRow[] {
  return profile.externalLinks.map((l) => ({ key: nextRowKey++, url: l.url, label: l.label ?? '' }))
}

function ProfileForm({ saved }: { saved: Profile }) {
  const { t } = useTranslation('profile')
  const queryClient = useQueryClient()
  const [displayName, setDisplayName] = useState(saved.displayName)
  const [pseudonym, setPseudonym] = useState(saved.pseudonym)
  const [rows, setRows] = useState<LinkRow[]>(() => rowsOf(saved))
  const [errors, setErrors] = useState<Errors>({})
  const [notice, setNotice] = useState<string | null>(null)

  const save = useMutation({
    mutationFn: saveProfile,
    onSuccess: (profile) => {
      queryClient.setQueryData(profileQueryKey, profile)
      // The main name is part of the session's member (the top bar, the "me" page).
      void queryClient.invalidateQueries({ queryKey: sessionQueryKey })
      setDisplayName(profile.displayName)
      setPseudonym(profile.pseudonym)
      setRows(rowsOf(profile))
      setNotice(t('saved'))
    },
    onError: (error) => {
      setErrors(serverErrors(error))
    },
  })

  function serverErrors(error: unknown): Errors {
    if (!(error instanceof ProfileError)) {
      return { form: t('errors.network') }
    }
    const fieldName = error.field ?? 'displayName'
    switch (error.code) {
      case 'name_invalid':
        return { [fieldName]: t('errors.nameInvalid') }
      case 'name_is_login_name':
        return { [fieldName]: t('errors.nameIsLoginName') }
      case 'name_taken':
        return { [fieldName]: t('errors.nameTaken') }
      case 'pseudonym_in_use':
        return { pseudonym: t('errors.pseudonymInUse') }
      case 'external_links_invalid':
        return { links: t('errors.linksInvalid', { max: MaxExternalLinks }) }
      case 'rate_limited':
        return { form: rateLimitMessage(error.retryAfter) }
      default:
        return { form: t('errors.network') }
    }
  }

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    setNotice(null)
    const found: Errors = {}
    if (!isValidName(displayName)) {
      found.displayName = t('errors.nameInvalid')
    }
    if (pseudonym.trim() !== '' && !isValidName(pseudonym)) {
      found.pseudonym = t('errors.nameInvalid')
    } else if (pseudonym.trim() === '' && saved.pseudonymInUse) {
      found.pseudonym = t('errors.pseudonymInUse')
    }
    const links = rows
      .filter((r) => r.url.trim() !== '' || r.label.trim() !== '')
      .map((r) => ({ url: r.url.trim(), label: r.label.trim() }))
    if (links.some((l) => !isValidLinkURL(l.url) || !isValidLinkLabel(l.label))) {
      found.links = t('errors.linksInvalid', { max: MaxExternalLinks })
    }
    setErrors(found)
    if (Object.keys(found).length > 0) {
      return
    }
    save.mutate({
      displayName: displayName.trim(),
      pseudonym: pseudonym.trim(),
      externalLinks: links.map((l) => (l.label ? l : { url: l.url })),
    })
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {saved.displayNameIsLoginName && (
        <p role="note" className="rounded-lg border border-primary/40 p-3 text-sm">
          {t('loginNameNotice')}
        </p>
      )}
      {notice && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {notice}
        </p>
      )}
      <Field
        label={t('displayName')}
        hint={t('displayNameHint')}
        error={errors.displayName}
        name="nickname"
        autoComplete="nickname"
        required
        maxLength={64}
        value={displayName}
        onChange={(e) => {
          setDisplayName(e.target.value)
        }}
      />
      <Field
        label={t('pseudonym')}
        hint={saved.pseudonymInUse ? t('pseudonymHintInUse') : t('pseudonymHint')}
        error={errors.pseudonym}
        name="pseudonym"
        autoComplete="off"
        maxLength={64}
        value={pseudonym}
        onChange={(e) => {
          setPseudonym(e.target.value)
        }}
      />
      <LinksEditor
        rows={rows}
        error={errors.links}
        onChange={(next) => {
          setRows(next)
        }}
      />
      {errors.form && (
        <p role="alert" className="text-sm text-destructive">
          {errors.form}
        </p>
      )}
      <Button type="submit" className="self-start" disabled={save.isPending}>
        {save.isPending ? t('working') : t('save')}
      </Button>
    </form>
  )
}

function LinksEditor({
  rows,
  error,
  onChange,
}: {
  rows: LinkRow[]
  error: string | undefined
  onChange: (rows: LinkRow[]) => void
}) {
  const { t } = useTranslation('profile')
  const groupId = useId()
  const update = (key: number, change: Partial<LinkRow>) => {
    onChange(rows.map((r) => (r.key === key ? { ...r, ...change } : r)))
  }
  return (
    <fieldset aria-describedby={`${groupId}-hint`} className="flex flex-col gap-3">
      <legend className="text-sm font-medium">{t('links')}</legend>
      <p id={`${groupId}-hint`} className="text-xs text-muted-foreground">
        {t('linksHint', { max: MaxExternalLinks })}
      </p>
      {rows.map((row, i) => (
        <div key={row.key} className="flex flex-col gap-2 rounded-lg border p-3">
          <Field
            label={t('linkURL', { n: i + 1 })}
            type="url"
            inputMode="url"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            placeholder="https://"
            value={row.url}
            onChange={(e) => {
              update(row.key, { url: e.target.value })
            }}
          />
          <Field
            label={t('linkLabel', { n: i + 1 })}
            maxLength={64}
            value={row.label}
            onChange={(e) => {
              update(row.key, { label: e.target.value })
            }}
          />
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="self-start"
            onClick={() => {
              onChange(rows.filter((r) => r.key !== row.key))
            }}
          >
            <Trash2 aria-hidden="true" />
            {t('removeLink', { n: i + 1 })}
          </Button>
        </div>
      ))}
      {rows.length < MaxExternalLinks && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="self-start"
          onClick={() => {
            onChange([...rows, { key: nextRowKey++, url: '', label: '' }])
          }}
        >
          <Plus aria-hidden="true" />
          {t('addLink')}
        </Button>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </fieldset>
  )
}
