import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { SignatureLinks } from '@/components/SignatureLinks'
import { zhCN } from '@/i18n/resources'
import { displayNameProblem, isValidName } from '@/lib/names'
import { createQueryClient } from '@/lib/queryClient'
import { ProfileSection } from './ProfileSection'
import { hasPseudonym, isValidLinkURL, parseProfile } from './profileApi'

// Made-up values only. The fields are those of GET/POST /api/osfm/me/profile.
const t = zhCN.profile

type Body = Record<string, unknown>

const saved = {
  displayName: 'Test Member',
  pseudonym: '',
  externalLinks: [{ url: 'https://example.test/test-member', label: 'Test Page' }],
  pseudonymInUse: false,
  displayNameIsLoginName: false,
}

/** A server holding the profile; POST merges the body into it, or answers what post returns. */
function stubServer(initial: Body, post?: (body: Body) => Response | undefined) {
  let profile = initial
  const posted: Body[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>((input, init) => {
      const method = init?.method ?? 'GET'
      if (input === '/api/osfm/me/profile' && method === 'GET') {
        return Promise.resolve(Response.json(profile))
      }
      if (input === '/api/osfm/me/profile' && method === 'POST') {
        const body = JSON.parse(init?.body as string) as Body
        posted.push(body)
        const answer = post?.(body)
        if (answer) {
          return Promise.resolve(answer)
        }
        profile = { ...profile, ...body }
        return Promise.resolve(Response.json(profile))
      }
      return Promise.resolve(Response.json({}, { status: 404 }))
    }),
  )
  return { posted }
}

function renderSection() {
  render(
    <QueryClientProvider client={createQueryClient()}>
      <ProfileSection />
    </QueryClientProvider>,
  )
}

function problem(status: number, code: string, field?: string): Response {
  return Response.json({ status, message: 'x', code, data: field ? { field } : {} }, { status })
}

describe('names', () => {
  it('checks names as the server does, roughly', () => {
    expect(isValidName(' Test Name ')).toBe(true)
    expect(isValidName('')).toBe(false)
    expect(isValidName('x'.repeat(65))).toBe(false)
    expect(isValidName('Test\u202eName')).toBe(false)
    expect(displayNameProblem('', 'test_member')).toBe('invalid')
    expect(displayNameProblem(' TEST_Member ', 'test_member')).toBe('login')
    expect(displayNameProblem('\uff34\uff25\uff33\uff34\uff3fmember', 'test_member')).toBe('login')
    expect(displayNameProblem('Test Member', 'test_member')).toBeNull()
  })

  it('accepts https links only', () => {
    expect(isValidLinkURL('https://example.test/x')).toBe(true)
    expect(isValidLinkURL('http://example.test/x')).toBe(false)
    expect(isValidLinkURL('javascript:alert(1)')).toBe(false)
    expect(isValidLinkURL('https://user:pw@example.test/')).toBe(false)
  })

  it('reads a profile and tells whether there is a pseudonym', () => {
    expect(hasPseudonym(parseProfile(saved))).toBe(false)
    expect(hasPseudonym(parseProfile({ ...saved, pseudonym: 'Test Pen' }))).toBe(true)
    expect(() => parseProfile({ displayName: 'x' })).toThrow()
  })
})

describe('ProfileSection', () => {
  it('shows the saved profile and saves changes', async () => {
    const { posted } = stubServer(saved)
    renderSection()
    const name = await screen.findByLabelText(t.displayName)
    expect(name).toHaveValue('Test Member')
    expect(screen.getByLabelText(t.linkURL.replace('{{n}}', '1'))).toHaveValue('https://example.test/test-member')

    await userEvent.type(screen.getByLabelText(t.pseudonym), 'Test Pen')
    await userEvent.click(screen.getByRole('button', { name: t.addLink }))
    await userEvent.type(screen.getByLabelText(t.linkURL.replace('{{n}}', '2')), 'https://example.test/second')
    await userEvent.click(screen.getByRole('button', { name: t.save }))

    expect(await screen.findByText(t.saved)).toBeInTheDocument()
    expect(posted).toEqual([
      {
        displayName: 'Test Member',
        pseudonym: 'Test Pen',
        externalLinks: [
          { url: 'https://example.test/test-member', label: 'Test Page' },
          { url: 'https://example.test/second' },
        ],
      },
    ])
  })

  it('checks the form before sending', async () => {
    const { posted } = stubServer(saved)
    renderSection()
    const name = await screen.findByLabelText(t.displayName)
    await userEvent.clear(name)
    await userEvent.clear(screen.getByLabelText(t.linkURL.replace('{{n}}', '1')))
    await userEvent.type(screen.getByLabelText(t.linkURL.replace('{{n}}', '1')), 'http://example.test/')
    await userEvent.click(screen.getByRole('button', { name: t.save }))
    expect(await screen.findByText(t.errors.nameInvalid)).toBeInTheDocument()
    expect(screen.getByText(t.errors.linksInvalid.replace('{{max}}', '5'))).toBeInTheDocument()
    expect(posted).toEqual([])
  })

  it('puts a taken name on the field the server names, without saying whose it is', async () => {
    stubServer(saved, () => problem(409, 'name_taken', 'pseudonym'))
    renderSection()
    await userEvent.type(await screen.findByLabelText(t.pseudonym), 'Test Taken')
    await userEvent.click(screen.getByRole('button', { name: t.save }))
    const error = await screen.findByText(t.errors.nameTaken)
    expect(screen.getByLabelText(t.pseudonym)).toHaveAttribute('aria-describedby', expect.stringContaining(error.id))
  })

  it('does not clear a pseudonym in use, and flags a main name that is the login name', async () => {
    const { posted } = stubServer({
      ...saved,
      pseudonym: 'Test Pen',
      pseudonymInUse: true,
      displayNameIsLoginName: true,
    })
    renderSection()
    expect(await screen.findByText(t.loginNameNotice)).toBeInTheDocument()
    await userEvent.clear(screen.getByLabelText(t.pseudonym))
    await userEvent.click(screen.getByRole('button', { name: t.save }))
    expect(await screen.findByText(t.errors.pseudonymInUse)).toBeInTheDocument()
    expect(posted).toEqual([])
  })
})

describe('SignatureLinks', () => {
  it('renders nothing without links (a pseudonym signature has none)', () => {
    const { container } = render(<SignatureLinks links={undefined} name="Test Pen" />)
    expect(container).toBeEmptyDOMElement()
    const empty = render(<SignatureLinks links={[]} name="Test Pen" />)
    expect(empty.container).toBeEmptyDOMElement()
  })

  it('opens the https links behind a toggle, labels as plain text', async () => {
    render(
      <SignatureLinks
        name="Test Author"
        links={[
          { url: 'https://example.test/blog', label: '<b>Test Blog</b>' },
          { url: 'javascript:alert(1)', label: 'bad' },
          { url: 'https://example.test/' },
        ]}
      />,
    )
    const toggle = screen.getByRole('button', { name: /Test Author/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('link')).toBeNull()
    await userEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const list = screen.getByRole('list')
    const links = within(list).getAllByRole('link')
    expect(links.map((a) => a.textContent)).toEqual(['<b>Test Blog</b>', 'example.test'])
    for (const a of links) {
      expect(a).toHaveAttribute('target', '_blank')
      expect(a).toHaveAttribute('rel', 'noopener noreferrer nofollow')
    }
    expect(list.querySelector('b')).toBeNull()
    await waitFor(() => {
      expect(screen.queryByText('bad')).toBeNull()
    })
  })
})
