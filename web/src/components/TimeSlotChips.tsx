import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { type TimeSlot, timeSlots } from '@/lib/enums'

/**
 * The time slots of a place or recommendation as a row of chips, always in the order of the day whatever order they
 * come in; unknown values are left out. Nothing is rendered without any.
 */
export function TimeSlotChips({ slots, label }: { slots: readonly string[]; label?: string }) {
  const { t } = useTranslation()
  const shown = timeSlots.filter((s: TimeSlot) => slots.includes(s))
  if (shown.length === 0) {
    return null
  }
  return (
    <ul aria-label={label} className="flex flex-wrap gap-1">
      {shown.map((slot) => (
        <li key={slot}>
          <Badge variant="outline">{t(`timeSlot.${slot}`)}</Badge>
        </li>
      ))}
    </ul>
  )
}
