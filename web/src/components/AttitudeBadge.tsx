import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { Attitude } from '@/lib/enums'

// A colour per attitude as well as the label, so that the four can be told apart at a glance; the label carries the
// meaning (colour is never the only cue).
const styles: Record<Attitude, string> = {
  must_try: 'bg-emerald-600 text-white dark:bg-emerald-500',
  good: 'bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-100',
  ok: 'bg-secondary text-secondary-foreground',
  avoid: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
}

/** A recommendation's attitude (C2) as a labelled badge: must try, good, OK or avoid. */
export function AttitudeBadge({ attitude, className }: { attitude: Attitude; className?: string }) {
  const { t } = useTranslation()
  return (
    <Badge data-attitude={attitude} className={cn(styles[attitude], className)}>
      {t(`attitude.${attitude}`)}
    </Badge>
  )
}
