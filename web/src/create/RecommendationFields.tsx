// The recommendation form (C2–C4), for a new place, for a place that exists and for editing (T031):
//
//   <RecommendationForm initial={values} onSubmit={(input) => save.mutate(input)} saving={save.isPending}
//     error={save.error} submitLabel={t('save')} fixedSignature={rec.displayName} visibilityEditable={rec.isMine} />
//
// RecommendationFields is the fields alone, for a form that has more (the new place's). The attitude (four buttons,
// none chosen at first) and the text are required; the rest is optional and starts closed, so that the shortest path
// touches nothing else (G2). The signature and the visibility stay in view: they decide who sees what (C3, C4).

import { useQuery } from '@tanstack/react-query'
import { PlusIcon, Trash2Icon, XIcon } from 'lucide-react'
import { type ReactNode, type SyntheticEvent, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import type { RecommendationInput } from '@/api/types.gen'
import { Button } from '@/components/ui/button'
import { attitudes, signatures, sourceKinds, timeSlots, visibilities } from '@/lib/enums'
import { cn } from '@/lib/utils'
import { hasPseudonym, profileQuery } from '@/me/profileApi'
import { CheckChips, Disclosure, ProblemText, RadioButtons, TextAreaField, TextField } from './controls'
import { focusFirstProblem, selectClass, useCanUsePseudonym, useProblemText } from './formUtils'
import {
  limits,
  newSource,
  type Problems,
  recommendationProblems,
  type RecommendationValues,
  serverProblems,
  toRecommendationInput,
  today,
} from './recommendationForm'
import { FormMessage } from './FormMessage'

/** The fields of the closed "more" section. */
const moreFields = ['timeSlots', 'dishes', 'season', 'sources', 'visitedOn']

/** Whether one of the problems is about a field of the "more" section. */
function moreInvalid(problems: Problems): boolean {
  return Object.keys(problems).some((k) => moreFields.includes(k.split('.')[0] ?? ''))
}

export interface RecommendationFieldsProps {
  value: RecommendationValues
  onChange: (value: RecommendationValues) => void
  problems: Problems
  /**
   * The name the recommendation is signed with, when the signature cannot be chosen (editing: it is fixed once
   * created, M2-Q2). Without it the member chooses their main name or their pseudonym.
   */
  fixedSignature?: string
  /** Whether the visibility can be changed (not by a maintainer editing someone else's recommendation). */
  visibilityEditable?: boolean
}

export function RecommendationFields({
  value,
  onChange,
  problems,
  fixedSignature,
  visibilityEditable = true,
}: RecommendationFieldsProps) {
  const { t } = useTranslation(['create', 'common'])
  const problemText = useProblemText()
  const set = (patch: Partial<RecommendationValues>) => {
    onChange({ ...value, ...patch })
  }

  return (
    <div className="flex flex-col gap-5">
      <RadioButtons
        legend={t('recommendation.attitude')}
        name="attitude"
        values={attitudes}
        value={value.attitude}
        label={(a) => t(`common:attitude.${a}`)}
        onChange={(attitude) => {
          set({ attitude })
        }}
        problem={problems.attitude ? t('errors.attitude') : null}
        large
      />
      <TextAreaField
        label={t('recommendation.text')}
        hint={t('recommendation.textHint')}
        value={value.text}
        onChange={(text) => {
          set({ text })
        }}
        problem={problemText(problems.text, limits.text)}
        maxLength={limits.text}
      />

      <Disclosure title={t('recommendation.more')} invalid={moreInvalid(problems)}>
        <CheckChips
          legend={t('recommendation.timeSlots')}
          values={timeSlots}
          chosen={value.timeSlots}
          label={(s) => t(`common:timeSlot.${s}`)}
          onChange={(chosen) => {
            set({ timeSlots: chosen })
          }}
          problem={problemText(problems.timeSlots)}
        />
        <Dishes
          dishes={value.dishes}
          onChange={(dishes) => {
            set({ dishes })
          }}
          problem={problemText(problems.dishes, problems.dishes === 'tooMany' ? limits.dishes : limits.dish)}
        />
        <TextField
          label={t('recommendation.season')}
          hint={t('recommendation.seasonHint')}
          value={value.season}
          onChange={(season) => {
            set({ season })
          }}
          problem={problemText(problems.season, limits.season)}
          maxLength={limits.season}
        />
        <Sources value={value} onChange={onChange} problems={problems} />
        <TextField
          label={t('recommendation.visitedOn')}
          type="date"
          max={today()}
          value={value.visitedOn}
          onChange={(visitedOn) => {
            set({ visitedOn })
          }}
          problem={problemText(problems.visitedOn)}
        />
      </Disclosure>

      <Signature value={value} onChange={onChange} fixed={fixedSignature} problem={problemText(problems.displayAs)} />
      {visibilityEditable && (
        <RadioButtons
          legend={t('recommendation.visibility')}
          name="visibility"
          values={[...visibilities].reverse()}
          value={value.visibility}
          label={(v) => t(`common:visibility.${v}`)}
          onChange={(visibility) => {
            set({ visibility })
          }}
          problem={problemText(problems.visibility)}
        />
      )}
    </div>
  )
}

/** The dishes: typed one by one (Enter or the button adds one), each removable. */
function Dishes({
  dishes,
  onChange,
  problem,
}: {
  dishes: string[]
  onChange: (dishes: string[]) => void
  problem: string | null
}) {
  const { t } = useTranslation('create')
  const id = useId()
  const [draft, setDraft] = useState('')
  const add = () => {
    const dish = draft.trim()
    if (dish !== '' && !dishes.includes(dish)) {
      onChange([...dishes, dish])
    }
    setDraft('')
  }
  return (
    <div role="group" aria-labelledby={`${id}-label`} className="flex flex-col gap-2">
      <span id={`${id}-label`} className="text-sm font-medium">
        {t('recommendation.dishes')}
      </span>
      {dishes.length > 0 && (
        <ul className="flex flex-wrap gap-2">
          {dishes.map((dish) => (
            <li
              key={dish}
              className="flex items-center gap-1 rounded-lg border bg-secondary py-0.5 pr-0.5 pl-2 text-sm"
            >
              <span className="break-all">{dish}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={t('recommendation.removeDish', { dish })}
                onClick={() => {
                  onChange(dishes.filter((d) => d !== dish))
                }}
              >
                <XIcon aria-hidden="true" />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <div className="flex gap-2">
        <label htmlFor={`${id}-input`} className="sr-only">
          {t('recommendation.dish')}
        </label>
        <input
          id={`${id}-input`}
          value={draft}
          maxLength={limits.dish}
          placeholder={t('recommendation.dish')}
          enterKeyHint="done"
          aria-invalid={problem ? true : undefined}
          onChange={(e) => {
            setDraft(e.target.value)
          }}
          // A dish typed but not added yet is added when the field is left, so that it is not lost on saving.
          onBlur={add}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
          className={cn(selectClass, 'flex-1')}
        />
        <Button type="button" variant="outline" className="h-11" onClick={add}>
          <PlusIcon aria-hidden="true" />
          {t('recommendation.addDish')}
        </Button>
      </div>
      <ProblemText id={id}>{problem}</ProblemText>
    </div>
  )
}

/** The sources: each a link, a kind and an excerpt, several allowed (C2). */
function Sources({
  value,
  onChange,
  problems,
}: {
  value: RecommendationValues
  onChange: (value: RecommendationValues) => void
  problems: Problems
}) {
  const { t } = useTranslation(['create', 'common'])
  const problemText = useProblemText()
  const id = useId()
  const setSource = (i: number, patch: Partial<RecommendationValues['sources'][number]>) => {
    onChange({ ...value, sources: value.sources.map((s, j) => (j === i ? { ...s, ...patch } : s)) })
  }
  return (
    <div role="group" aria-labelledby={`${id}-label`} className="flex flex-col gap-3">
      <span id={`${id}-label`} className="text-sm font-medium">
        {t('recommendation.sources')}
      </span>
      {value.sources.map((source, i) => {
        const n = i + 1
        return (
          <fieldset key={source.key} className="flex flex-col gap-2 border-l-2 pl-3">
            <legend className="flex w-full items-center justify-between text-sm text-muted-foreground">
              {t('recommendation.source', { n })}
            </legend>
            <div className="flex flex-col gap-1">
              <label htmlFor={`${id}-${String(source.key)}-kind`} className="text-sm">
                {t('recommendation.sourceKind')}
              </label>
              <select
                id={`${id}-${String(source.key)}-kind`}
                value={source.kind}
                onChange={(e) => {
                  const kind = sourceKinds.find((k) => k === e.target.value)
                  if (kind) {
                    setSource(i, { kind })
                  }
                }}
                className={selectClass}
              >
                {sourceKinds.map((k) => (
                  <option key={k} value={k}>
                    {t(`common:sourceKind.${k}`)}
                  </option>
                ))}
              </select>
            </div>
            <TextField
              label={t('recommendation.sourceUrl')}
              type="url"
              inputMode="url"
              autoComplete="off"
              value={source.url}
              onChange={(url) => {
                setSource(i, { url })
              }}
              problem={problemText(problems[`sources.${String(i)}.url`])}
            />
            <TextAreaField
              label={t('recommendation.sourceExcerpt')}
              value={source.excerpt}
              onChange={(excerpt) => {
                setSource(i, { excerpt })
              }}
              problem={problemText(problems[`sources.${String(i)}.excerpt`], limits.excerpt)}
              maxLength={limits.excerpt}
            />
            <ProblemText id={`${id}-${String(source.key)}`}>
              {problemText(problems[`sources.${String(i)}.kind`])}
            </ProblemText>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="self-start"
              onClick={() => {
                onChange({ ...value, sources: value.sources.filter((_, j) => j !== i) })
              }}
            >
              <Trash2Icon aria-hidden="true" />
              {t('recommendation.removeSource', { n })}
            </Button>
          </fieldset>
        )
      })}
      {value.sources.length < limits.sources && (
        <Button
          type="button"
          variant="outline"
          className="h-10 self-start"
          onClick={() => {
            onChange({ ...value, sources: [...value.sources, newSource()] })
          }}
        >
          <PlusIcon aria-hidden="true" />
          {t('recommendation.addSource')}
        </Button>
      )}
      <ProblemText id={id}>{problemText(problems.sources, limits.sources)}</ProblemText>
    </div>
  )
}

/**
 * The signature: the main name or the pseudonym (C3). Without a pseudonym the choice is disabled, with a link to "me"
 * to set one. Fixed once created (M2-Q2): editing shows the name only.
 */
function Signature({
  value,
  onChange,
  fixed,
  problem,
}: {
  value: RecommendationValues
  onChange: (value: RecommendationValues) => void
  fixed?: string
  problem: string | null
}) {
  const { t } = useTranslation(['create', 'common'])
  const profile = useQuery(profileQuery)
  if (fixed !== undefined) {
    return (
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium">{t('recommendation.signature')}</span>
        <p className="text-sm break-words">{fixed}</p>
        <p className="text-xs text-muted-foreground">{t('recommendation.signatureFixed')}</p>
      </div>
    )
  }
  const canUsePseudonym = profile.data ? hasPseudonym(profile.data) : false
  return (
    <RadioButtons
      legend={t('recommendation.signature')}
      name="displayAs"
      values={signatures}
      value={value.displayAs}
      label={(s) =>
        s === 'main'
          ? profile.data
            ? t('recommendation.signedMain', { name: profile.data.displayName })
            : t('common:signature.main')
          : canUsePseudonym && profile.data
            ? t('recommendation.signedPseudonym', { name: profile.data.pseudonym })
            : t('common:signature.pseudonym')
      }
      disabled={(s) => s === 'pseudonym' && !canUsePseudonym}
      onChange={(displayAs) => {
        onChange({ ...value, displayAs })
      }}
      problem={problem}
      hint={
        profile.data && !canUsePseudonym ? (
          <p className="text-xs text-muted-foreground">
            {t('recommendation.noPseudonym')}{' '}
            <Link to="/me" className="text-primary underline underline-offset-2">
              {t('recommendation.setPseudonym')}
            </Link>
          </p>
        ) : null
      }
    />
  )
}

export interface RecommendationFormProps {
  /** The values the form starts with (emptyRecommendation for a new one). */
  initial: RecommendationValues
  /** Called with the request once the checks pass. */
  onSubmit: (input: RecommendationInput) => void
  saving: boolean
  /** The error of the last save: its field problems show at their fields, other errors above the button. */
  error: unknown
  submitLabel: string
  fixedSignature?: string
  visibilityEditable?: boolean
  /** Whether saving is possible at all (false while offline). */
  canSave?: boolean
}

/** The recommendation form on its own: the fields, the checks and the save button (/p/:place/recommend, T031). */
export function RecommendationForm({
  initial,
  onSubmit,
  saving,
  error,
  submitLabel,
  fixedSignature,
  visibilityEditable = true,
  canSave = true,
}: RecommendationFormProps) {
  const { t } = useTranslation('create')
  const formRef = useRef<HTMLFormElement>(null)
  const canUsePseudonym = useCanUsePseudonym()
  const [values, setValues] = useState(initial)
  const [problems, setProblems] = useState<Problems>({})
  // The server's problems with the last save, until the next one.
  const shown = { ...(serverProblems(error) ?? {}), ...problems }

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const found = recommendationProblems(values, { canUsePseudonym, signatureFixed: fixedSignature !== undefined })
    setProblems(found)
    if (Object.keys(found).length > 0) {
      focusFirstProblem(formRef.current)
      return
    }
    onSubmit(toRecommendationInput(values, { signature: fixedSignature === undefined, visibility: visibilityEditable }))
  }

  return (
    <form ref={formRef} onSubmit={submit} noValidate className="flex flex-col gap-5">
      <RecommendationFields
        value={values}
        onChange={setValues}
        problems={shown}
        fixedSignature={fixedSignature}
        visibilityEditable={visibilityEditable}
      />
      <SaveBar>
        <FormMessage problems={shown} error={error} />
        <Button type="submit" size="lg" className="h-12 w-full text-base" disabled={saving || !canSave}>
          {saving ? t('saving') : submitLabel}
        </Button>
      </SaveBar>
    </form>
  )
}

/** The save button and its messages, kept at the bottom of the screen above the navigation (one hand, B1). */
export function SaveBar({ children }: { children: ReactNode }) {
  return (
    <div className="sticky bottom-[calc(3.5rem+env(safe-area-inset-bottom))] -mx-4 flex flex-col gap-2 border-t bg-background px-4 py-3">
      {children}
    </div>
  )
}
