import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { zhCN } from '@/i18n/resources'
import { attitudes } from '@/lib/enums'
import { AttitudeBadge } from './AttitudeBadge'
import { SignatureName } from './SignatureName'
import { TimeSlotChips } from './TimeSlotChips'

describe('AttitudeBadge', () => {
  it.each(attitudes)('labels %s', (attitude) => {
    render(<AttitudeBadge attitude={attitude} />)
    expect(screen.getByText(zhCN.common.attitude[attitude])).toHaveAttribute('data-attitude', attitude)
  })
})

describe('TimeSlotChips', () => {
  it('lists the slots in the order of the day, without unknown values', () => {
    render(<TimeSlotChips slots={['late_night', 'nonsense', 'breakfast', 'lunch']} label="slots" />)
    const items = within(screen.getByRole('list', { name: 'slots' })).getAllByRole('listitem')
    expect(items.map((li) => li.textContent)).toEqual([
      zhCN.common.timeSlot.breakfast,
      zhCN.common.timeSlot.lunch,
      zhCN.common.timeSlot.late_night,
    ])
  })

  it('renders nothing without slots', () => {
    const { container } = render(<TimeSlotChips slots={[]} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('SignatureName', () => {
  it('renders the name as plain text, markup included', () => {
    const name = '<b>Test</b> [link](https://example.org)'
    const { container } = render(<SignatureName name={name} />)
    expect(screen.getByText(name)).toBeInTheDocument()
    expect(container.querySelector('b')).toBeNull()
    expect(container.querySelector('a')).toBeNull()
  })
})
