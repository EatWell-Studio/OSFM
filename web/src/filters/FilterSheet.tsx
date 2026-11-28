import { useQuery } from '@tanstack/react-query'
import { SlidersHorizontalIcon } from 'lucide-react'
import { type ReactNode, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { categories, cuisines, timeSlots } from '@/lib/enums'
import { cn } from '@/lib/utils'
import { signaturesQuery } from '@/map/places/placesApi'
import { filterCount, type Filters, noFilters } from './filters'
import { useFilters } from './useFilters'

/**
 * The filter button, with the number of chosen conditions, and the bottom sheet it opens (A3): time slots, categories,
 * cuisines, signatures (of the city, GET /api/osfm/signatures; none without a city the server knows) and the two
 * attitude switches. Every change applies at once, to the address and so to the map and the list. There is no way to
 * sort by attitude here or anywhere else (C11).
 */
export function FilterSheet({ city, className }: { city: string | null; className?: string }) {
  const { t } = useTranslation(['map', 'common'])
  const [filters, setFilters] = useFilters()
  const count = filterCount(filters)
  const change = (patch: Partial<Filters>) => {
    setFilters({ ...filters, ...patch })
  }

  // Not modal: a modal dialog locks the page's scrolling with an injected <style> element, which the content security
  // policy refuses (docs/architecture.md §4.7). A tap outside or Escape closes it.
  return (
    <Sheet modal={false}>
      <SheetTrigger asChild>
        <Button
          variant="outline"
          aria-label={count > 0 ? t('filters.buttonCount', { count }) : t('filters.button')}
          className={cn('bg-background shadow-sm', className)}
        >
          <SlidersHorizontalIcon aria-hidden="true" />
          <span aria-hidden="true">{t('filters.button')}</span>
          {count > 0 && <Badge aria-hidden="true">{count}</Badge>}
        </Button>
      </SheetTrigger>
      <SheetContent side="bottom" className="max-h-[85dvh] gap-0">
        <SheetHeader className="pr-12">
          <SheetTitle>{t('filters.title')}</SheetTitle>
          <SheetDescription>{t('filters.description')}</SheetDescription>
        </SheetHeader>
        <div className="flex flex-col gap-5 overflow-y-auto px-4 pb-2">
          <Choices
            legend={t('filters.slots')}
            values={timeSlots}
            chosen={filters.slots}
            label={(v) => t(`common:timeSlot.${v}`)}
            onChange={(slots) => {
              change({ slots })
            }}
          />
          <Choices
            legend={t('filters.categories')}
            values={categories}
            chosen={filters.categories}
            label={(v) => t(`common:category.${v}`)}
            onChange={(chosen) => {
              change({ categories: chosen })
            }}
          />
          <Choices
            legend={t('filters.cuisines')}
            values={cuisines}
            chosen={filters.cuisines}
            label={(v) => t(`common:cuisine.${v}`)}
            onChange={(chosen) => {
              change({ cuisines: chosen })
            }}
          />
          <Signatures
            city={city}
            chosen={filters.by}
            onChange={(by) => {
              change({ by })
            }}
          />
          <fieldset className="flex flex-col gap-3">
            <legend className="mb-2 text-sm font-medium">{t('filters.attitudes')}</legend>
            <Switch
              checked={filters.mustTry}
              onChange={(mustTry) => {
                change({ mustTry })
              }}
            >
              {t('filters.mustTry')}
            </Switch>
            <Switch
              checked={filters.noAvoid}
              onChange={(noAvoid) => {
                change({ noAvoid })
              }}
            >
              {t('filters.noAvoid')}
            </Switch>
          </fieldset>
        </div>
        <SheetFooter className="flex-row justify-between border-t">
          <Button
            variant="ghost"
            disabled={count === 0}
            onClick={() => {
              setFilters(noFilters)
            }}
          >
            {t('filters.clear')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

/** A group of chips that each turn a value on or off. */
function Choices<T extends string>({
  legend,
  values,
  chosen,
  label,
  onChange,
}: {
  legend: string
  values: readonly T[]
  chosen: readonly T[]
  label: (value: T) => string
  onChange: (chosen: T[]) => void
}) {
  const legendId = useId()
  return (
    <div role="group" aria-labelledby={legendId} className="flex flex-col gap-2">
      <span id={legendId} className="text-sm font-medium">
        {legend}
      </span>
      <ToggleGroup
        type="multiple"
        variant="outline"
        size="sm"
        value={[...chosen]}
        onValueChange={(next) => {
          onChange(values.filter((v) => next.includes(v)))
        }}
        className="w-full flex-wrap"
      >
        {values.map((v) => (
          <ToggleGroupItem key={v} value={v} className="data-[state=on]:border-primary">
            {label(v)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    </div>
  )
}

/**
 * The signatures of the city to filter by. A signature chosen before (say, in a link) that the city no longer lists
 * stays shown, so that it can be turned off.
 */
function Signatures({
  city,
  chosen,
  onChange,
}: {
  city: string | null
  chosen: string[]
  onChange: (by: string[]) => void
}) {
  const { t } = useTranslation('map')
  const legendId = useId()
  const signatures = useQuery({ ...signaturesQuery(city ?? ''), enabled: city !== null })
  const listed = signatures.data ?? []
  const names = [...listed, ...chosen.filter((n) => !listed.includes(n))]

  let body: ReactNode
  if (signatures.isError) {
    body = <p className="text-sm text-muted-foreground">{t('filters.signaturesError')}</p>
  } else if (city !== null && signatures.isPending && names.length === 0) {
    body = <p className="text-sm text-muted-foreground">{t('filters.signaturesLoading')}</p>
  } else if (names.length === 0) {
    body = <p className="text-sm text-muted-foreground">{t('filters.noSignatures')}</p>
  } else {
    body = (
      <ToggleGroup
        type="multiple"
        variant="outline"
        size="sm"
        value={chosen}
        onValueChange={(next) => {
          onChange(names.filter((n) => next.includes(n)))
        }}
        className="w-full flex-wrap"
      >
        {names.map((name) => (
          // The signature is the member's own text: shown as plain text.
          <ToggleGroupItem key={name} value={name} className="max-w-full data-[state=on]:border-primary">
            <span className="truncate">{name}</span>
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    )
  }
  return (
    <div role="group" aria-labelledby={legendId} className="flex flex-col gap-2">
      <span id={legendId} className="text-sm font-medium">
        {t('filters.by')}
      </span>
      {body}
    </div>
  )
}

function Switch({
  checked,
  onChange,
  children,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  children: ReactNode
}) {
  const id = useId()
  return (
    <div className="flex items-center gap-3">
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(value) => {
          onChange(value === true)
        }}
      />
      <label htmlFor={id} className="text-sm">
        {children}
      </label>
    </div>
  )
}
