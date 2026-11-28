// The form controls of the create flow: native radio buttons and checkboxes drawn as large buttons and chips (easy to
// hit with a thumb, B1), a labelled field with its problem, and a section that opens and closes. Native inputs rather
// than Radix popups: nothing here opens a layer, so nothing injects the <style> the content security policy refuses
// (docs/architecture.md §4.7), and the browser's own keyboard handling applies.

import { ChevronDownIcon } from 'lucide-react'
import { type ComponentProps, type ReactNode, useId, useState } from 'react'
import { cn } from '@/lib/utils'
import { inputClass, problemId } from './formUtils'

/** A problem under a field (nothing without one). */
export function ProblemText({ id, children }: { id: string; children: string | null }) {
  if (!children) {
    return null
  }
  return (
    <p id={problemId(id)} className="text-sm text-destructive">
      {children}
    </p>
  )
}

const chip =
  'inline-flex cursor-pointer items-center justify-center rounded-lg border bg-background px-3 text-sm select-none ' +
  'has-[:checked]:border-primary has-[:checked]:bg-primary has-[:checked]:text-primary-foreground ' +
  'has-[:focus-visible]:ring-3 has-[:focus-visible]:ring-ring/50 has-[:disabled]:cursor-not-allowed ' +
  'has-[:disabled]:opacity-50'

/**
 * One choice of several, as a row of buttons (native radios): nothing is chosen until the member picks (value null).
 * problem marks the group as invalid; its first button takes the focus then.
 */
export function RadioButtons<T extends string>({
  legend,
  name,
  values,
  value,
  label,
  onChange,
  problem,
  disabled,
  large,
  hint,
}: {
  legend: string
  name: string
  values: readonly T[]
  value: T | null
  label: (value: T) => ReactNode
  onChange: (value: T) => void
  problem?: string | null
  disabled?: (value: T) => boolean
  large?: boolean
  hint?: ReactNode
}) {
  const id = useId()
  return (
    <fieldset className="flex flex-col gap-2" aria-describedby={problem ? problemId(id) : undefined}>
      <legend className="mb-2 text-sm font-medium">{legend}</legend>
      <div className={cn('flex flex-wrap gap-2', large && 'grid grid-cols-4')}>
        {values.map((v, i) => (
          <label key={v} className={cn(chip, large ? 'h-12 text-base' : 'h-9')}>
            <input
              type="radio"
              name={name}
              value={v}
              checked={value === v}
              disabled={disabled?.(v)}
              aria-invalid={problem && i === 0 ? true : undefined}
              onChange={() => {
                onChange(v)
              }}
              className="sr-only"
            />
            {label(v)}
          </label>
        ))}
      </div>
      {hint}
      <ProblemText id={id}>{problem ?? null}</ProblemText>
    </fieldset>
  )
}

/** Any number of choices, as chips (native checkboxes), in the order of values. */
export function CheckChips<T extends string>({
  legend,
  values,
  chosen,
  label,
  onChange,
  problem,
}: {
  legend: string
  values: readonly T[]
  chosen: readonly T[]
  label: (value: T) => string
  onChange: (chosen: T[]) => void
  problem?: string | null
}) {
  const id = useId()
  return (
    <fieldset className="flex flex-col gap-2" aria-describedby={problem ? problemId(id) : undefined}>
      <legend className="mb-2 text-sm font-medium">{legend}</legend>
      <div className="flex flex-wrap gap-2">
        {values.map((v, i) => (
          <label key={v} className={cn(chip, 'h-8')}>
            <input
              type="checkbox"
              checked={chosen.includes(v)}
              aria-invalid={problem && i === 0 ? true : undefined}
              onChange={(e) => {
                const on = e.target.checked
                onChange(values.filter((x) => (x === v ? on : chosen.includes(x))))
              }}
              className="sr-only"
            />
            {label(v)}
          </label>
        ))}
      </div>
      <ProblemText id={id}>{problem ?? null}</ProblemText>
    </fieldset>
  )
}

/** A labelled one-line input with an optional hint and its problem. */
export function TextField({
  label,
  hint,
  problem,
  value,
  onChange,
  className,
  ...input
}: {
  label: string
  hint?: string
  problem?: string | null
  value: string
  onChange: (value: string) => void
  className?: string
} & Omit<ComponentProps<'input'>, 'value' | 'onChange'>) {
  const id = useId()
  const describedBy = [hint ? `${id}-hint` : null, problem ? problemId(id) : null].filter(Boolean).join(' ')
  return (
    <div className={cn('flex flex-col gap-1', className)}>
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      <input
        id={id}
        value={value}
        onChange={(e) => {
          onChange(e.target.value)
        }}
        aria-invalid={problem ? true : undefined}
        aria-describedby={describedBy || undefined}
        className={cn(inputClass, 'h-11')}
        {...input}
      />
      {hint && (
        <p id={`${id}-hint`} className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
      <ProblemText id={id}>{problem ?? null}</ProblemText>
    </div>
  )
}

/** A labelled text area that may span lines, with an optional hint and its problem. */
export function TextAreaField({
  label,
  hint,
  problem,
  value,
  onChange,
  ...textarea
}: {
  label: string
  hint?: string
  problem?: string | null
  value: string
  onChange: (value: string) => void
} & Omit<ComponentProps<'textarea'>, 'value' | 'onChange'>) {
  const id = useId()
  const describedBy = [hint ? `${id}-hint` : null, problem ? problemId(id) : null].filter(Boolean).join(' ')
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      <textarea
        id={id}
        value={value}
        onChange={(e) => {
          onChange(e.target.value)
        }}
        aria-invalid={problem ? true : undefined}
        aria-describedby={describedBy || undefined}
        className={cn(inputClass, 'field-sizing-content min-h-20 py-2')}
        {...textarea}
      />
      {hint && (
        <p id={`${id}-hint`} className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
      <ProblemText id={id}>{problem ?? null}</ProblemText>
    </div>
  )
}

/**
 * A section of optional fields, closed at first so that the shortest path (G2) does not scroll past it. It opens by
 * itself when one of its fields has a problem (invalid).
 */
export function Disclosure({ title, invalid, children }: { title: string; invalid: boolean; children: ReactNode }) {
  const id = useId()
  const [opened, setOpen] = useState(false)
  const open = opened || invalid
  return (
    <div className="flex flex-col gap-4 rounded-lg border">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => {
          setOpen(!open)
        }}
        className="flex min-h-11 items-center justify-between gap-2 px-3 text-left text-sm font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {title}
        <ChevronDownIcon
          aria-hidden="true"
          className={cn('size-4 shrink-0 transition-transform', open && 'rotate-180')}
        />
      </button>
      {open && (
        <div id={id} className="flex flex-col gap-5 px-3 pb-4">
          {children}
        </div>
      )}
    </div>
  )
}
