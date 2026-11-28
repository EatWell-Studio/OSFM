import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import type { BranchPreview, TreeMember } from '@/api/types.gen'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'

// Made-up values only: the tree of GET /api/osfm/members/tree and the answers of the branch routes.
const maintainer: Me = {
  uuid: '0190a1b2-0000-7000-8000-000000000001',
  displayName: 'Test Maintainer',
  role: 'maintainer',
}
const t = zhCN.members

function node(uuid: string, displayName: string, invited: TreeMember[] = [], role = 'member'): TreeMember {
  return {
    uuid,
    displayName,
    role,
    joinedAt: '2026-01-01T00:00:00Z',
    leftAt: null,
    canInvite: true,
    inviteCooldown: false,
    invited,
  }
}

const grandchild = node('0190a1b2-0000-7000-8000-000000000004', 'Test Grandchild')
const deputy = node('0190a1b2-0000-7000-8000-000000000005', 'Test Deputy', [], 'maintainer')
const child = node('0190a1b2-0000-7000-8000-000000000003', 'Test Child', [grandchild, deputy])
const tree = [node(maintainer.uuid, maintainer.displayName, [child], 'maintainer')]

function preview(affected: TreeMember[]): BranchPreview {
  return {
    action: 'stop_invites',
    affected: affected.map((m, i) => ({ uuid: m.uuid, displayName: m.displayName, role: m.role, depth: i })),
    skipped: [{ uuid: deputy.uuid, displayName: deputy.displayName, role: 'maintainer', depth: 1 }],
  }
}

/** A server whose branch grows between the first preview and the first execution. */
function stubServer() {
  let previews = 0
  const executed: unknown[] = []
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (url === '/api/osfm/me') {
      return Promise.resolve(Response.json(maintainer))
    }
    if (url === '/api/osfm/me/credentials') {
      return Promise.resolve(
        Response.json({
          passkeys: [],
          passwordTotp: { createdAt: '2026-01-01T00:00:00Z', lastUsedAt: null },
          recoveryCodesLeft: 10,
          verifiedUntil: new Date(Date.now() + 600_000).toISOString(),
        }),
      )
    }
    if (url === '/api/osfm/members/tree') {
      return Promise.resolve(Response.json({ roots: tree }))
    }
    if (url === `/api/osfm/members/${child.uuid}/branch/preview`) {
      previews++
      return Promise.resolve(Response.json(preview(previews === 1 ? [child] : [child, grandchild])))
    }
    if (url === `/api/osfm/members/${child.uuid}/branch`) {
      const body = JSON.parse(init?.body as string) as { affected: string[] }
      executed.push(body)
      if (body.affected.length === 1) {
        return Promise.resolve(
          Response.json({ code: 'branch_changed', status: 409, message: '', data: {} }, { status: 409 }),
        )
      }
      return Promise.resolve(Response.json(preview([child, grandchild])))
    }
    return Promise.resolve(Response.json({}, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return { executed }
}

describe('the invite tree', () => {
  it('shows who invited whom, previews a branch and asks again when it changed', async () => {
    const user = userEvent.setup()
    const { executed } = stubServer()
    render(
      <App
        router={createMemoryRouter(routes, { initialEntries: ['/admin/tree'] })}
        queryClient={createQueryClient()}
      />,
    )

    const childRow = await screen.findByRole('group', { name: 'Test Child' })
    const below = screen.getByRole('list', { name: t.tree.invitedBy.replace('{{name}}', 'Test Child') })
    expect(within(below).getByRole('group', { name: 'Test Grandchild' })).toBeInTheDocument()

    // Collapsing hides the members below.
    await user.click(within(childRow).getByRole('button', { name: t.tree.collapse }))
    expect(screen.queryByRole('group', { name: 'Test Grandchild' })).not.toBeInTheDocument()

    await user.click(within(childRow).getByRole('button', { name: t.tree.stopInvites }))
    const affected = await screen.findByRole('list', { name: t.tree.preview.affectedList })
    expect(
      within(affected)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Test Child'])
    const skipped = screen.getByRole('list', { name: t.tree.preview.skippedList })
    expect(within(skipped).getByRole('listitem')).toHaveTextContent('Test Deputy')

    // The branch grew meanwhile: the server refuses, the page shows the new list.
    await user.click(screen.getByRole('button', { name: t.tree.preview.confirmStop_one.replace('{{count}}', '1') }))
    expect(await screen.findByText(t.errors.branchChanged)).toBeInTheDocument()
    const confirm = await screen.findByRole('button', {
      name: t.tree.preview.confirmStop_other.replace('{{count}}', '2'),
    })
    await user.click(confirm)
    expect(
      await screen.findByText(t.tree.stopped_other.replace('{{count}}', '2').replace('{{name}}', 'Test Child')),
    ).toBeInTheDocument()
    expect(executed).toEqual([
      { action: 'stop_invites', affected: [child.uuid] },
      { action: 'stop_invites', affected: [child.uuid, grandchild.uuid] },
    ])
  })
})
