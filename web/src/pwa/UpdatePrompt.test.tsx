import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import { UpdateBanner } from './UpdatePrompt'

describe('UpdateBanner', () => {
  it('offers the new version and reloads into it', async () => {
    const onReload = vi.fn()
    render(<UpdateBanner onReload={onReload} />)
    expect(screen.getByRole('status')).toHaveTextContent(i18n.t('app.update.available'))
    await userEvent.click(screen.getByRole('button', { name: i18n.t('app.update.reload') }))
    expect(onReload).toHaveBeenCalledOnce()
  })
})
