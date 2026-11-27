import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type SyntheticEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate, useNavigate } from 'react-router'
import { Field } from '@/components/Field'
import { PageHeading } from '@/components/PageHeading'
import { TopBar } from '@/components/TopBar'
import { Button } from '@/components/ui/button'
import { instanceQuery, instanceQueryKey } from '@/instance/instance'
import { authErrorCode, authRetryAfter, rateLimitedCode } from '@/lib/authApi'
import { rateLimitMessage } from '@/lib/rateLimit'
import { PasskeyRegistration } from '@/register/PasskeyRegistration'
import { PasswordTotpRegistration } from '@/register/PasswordTotpRegistration'
import { type Me, sessionQueryKey } from '@/session/session'
import { useSession } from '@/session/useSession'
import { CitiesStep } from './CitiesStep'
import { checkSetupCode, isValidInstanceName, normalizeSetupCode, setupQuery, setupQueryKey } from './setup'

/**
 * The first-start setup wizard (G3): the setup code, the instance's name, the first maintainer's account (the same
 * registration forms as with an invite, password and TOTP first, passkey second), then, signed in, the cities. The
 * name is kept in the page and sent with the requests that create the maintainer: the server saves it together with
 * the maintainer, and it is the issuer of the maintainer's TOTP entry. Which step applies comes from the server
 * (GET /api/osfm/setup); once there is nothing left to set up, the page leads to the map.
 */
export function SetupPage() {
  const { t } = useTranslation(['setup', 'auth'])
  const session = useSession()
  const setup = useQuery({
    ...setupQuery(session.status === 'signed-in' ? session.me.uuid : null),
    enabled: session.status !== 'loading',
  })
  // Once the maintainer exists, the name saved with them (members only).
  const instance = useQuery({ ...instanceQuery, enabled: session.status === 'signed-in' })
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  let content
  if (setup.isPending || session.status === 'loading') {
    content = (
      <p role="status" className="text-muted-foreground">
        {t('loading')}
      </p>
    )
  } else if (setup.isError) {
    content = (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>{t('loadError')}</p>
        <Button
          onClick={() => {
            void setup.refetch()
          }}
        >
          {t('retry')}
        </Button>
      </div>
    )
  } else if (setup.data === null) {
    return <Navigate to="/" replace />
  } else if (setup.data === 'maintainer') {
    content = (
      <MaintainerSteps
        onDone={(me) => {
          if (me) {
            queryClient.setQueryData(sessionQueryKey, me)
          } else {
            void queryClient.invalidateQueries({ queryKey: sessionQueryKey })
          }
          void queryClient.invalidateQueries({ queryKey: instanceQueryKey })
          void queryClient.invalidateQueries({ queryKey: setupQueryKey })
        }}
      />
    )
  } else {
    content = (
      <>
        <StepHeading n={4} name="cities" />
        <CitiesStep
          onDone={() => {
            queryClient.setQueriesData({ queryKey: setupQueryKey }, null)
            void queryClient.invalidateQueries({ queryKey: instanceQueryKey })
            void navigate('/', { replace: true })
          }}
        />
      </>
    )
  }

  return (
    <div className="flex min-h-dvh flex-col">
      <TopBar name={instance.data?.name} />
      <main className="flex-1 pb-8">
        <PageHeading>{t('title')}</PageHeading>
        <div className="flex flex-col gap-4 px-4">{content}</div>
      </main>
    </div>
  )
}

function StepHeading({ n, name }: { n: 1 | 2 | 3 | 4; name: 'code' | 'name' | 'account' | 'cities' }) {
  const { t } = useTranslation(['setup', 'auth'])
  return <h2 className="text-lg font-medium">{t('step', { n, name: t(`steps.${name}`) })}</h2>
}

/**
 * Steps 1 to 3, while the instance has no maintainer: the setup code, the instance name, the first maintainer's
 * account. onDone gets the member when the method returns it.
 */
function MaintainerSteps({ onDone }: { onDone: (me?: Me) => void }) {
  const { t } = useTranslation(['setup', 'auth'])
  const [code, setCode] = useState<string | null>(null)
  const [name, setName] = useState<string | null>(null)
  const [method, setMethod] = useState<'password' | 'passkey'>('password')

  if (code === null) {
    return (
      <>
        <StepHeading n={1} name="code" />
        <p>{t('code.intro')}</p>
        <SetupCodeForm onValid={setCode} />
      </>
    )
  }
  if (name === null) {
    return (
      <>
        <StepHeading n={2} name="name" />
        <NameForm onDone={setName} />
      </>
    )
  }
  const entitlement = { setupCode: code, instanceName: name }
  return (
    <>
      <StepHeading n={3} name="account" />
      <p>{t('account.intro')}</p>
      <div role="group" aria-label={t('account.method')} className="flex flex-wrap gap-2">
        {(['password', 'passkey'] as const).map((m) => (
          <Button
            key={m}
            variant={m === method ? 'default' : 'outline'}
            aria-pressed={m === method}
            onClick={() => {
              setMethod(m)
            }}
          >
            {t(`auth:register.methods.${m}`)}
          </Button>
        ))}
      </div>
      {method === 'password' ? (
        <PasswordTotpRegistration
          entitlement={entitlement}
          onRegistered={onDone}
          continueLabel={t('account.continue')}
        />
      ) : (
        <PasskeyRegistration
          entitlement={entitlement}
          continueLabel={t('account.continue')}
          onDone={() => {
            onDone()
          }}
        />
      )}
      <Button
        variant="ghost"
        className="self-start"
        onClick={() => {
          setName(null)
        }}
      >
        {t('account.back')}
      </Button>
    </>
  )
}

function SetupCodeForm({ onValid }: { onValid: (code: string) => void }) {
  const { t } = useTranslation(['setup', 'auth'])
  const [input, setInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const check = useMutation({
    mutationFn: checkSetupCode,
    onSuccess: (valid, code) => {
      if (valid) {
        onValid(code)
      } else {
        setError(t('code.invalid'))
      }
    },
    onError: (err) => {
      setError(
        authErrorCode(err) === rateLimitedCode
          ? rateLimitMessage(authRetryAfter(err))
          : authErrorCode(err) === 'setup_done'
            ? t('errors.done')
            : t('code.network'),
      )
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (normalizeSetupCode(input) === null) {
      setError(t('code.form'))
      return
    }
    setError(null)
    check.mutate(input.trim())
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <Field
        label={t('code.label')}
        hint={t('code.hint')}
        error={error}
        name="setup-code"
        autoComplete="off"
        autoCapitalize="characters"
        autoCorrect="off"
        spellCheck={false}
        required
        maxLength={32}
        value={input}
        onChange={(e) => {
          setInput(e.target.value)
        }}
      />
      <Button type="submit" size="lg" disabled={check.isPending} className="self-start">
        {check.isPending ? t('code.checking') : t('code.check')}
      </Button>
    </form>
  )
}

/** The instance name, kept in the page until the maintainer is created with it. */
function NameForm({ onDone }: { onDone: (name: string) => void }) {
  const { t } = useTranslation(['setup', 'auth'])
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (!isValidInstanceName(name)) {
      setError(t('name.required'))
      return
    }
    setError(null)
    onDone(name.trim())
  }
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <Field
        label={t('name.label')}
        hint={t('name.hint')}
        error={error}
        name="instance-name"
        autoComplete="off"
        required
        maxLength={100}
        value={name}
        onChange={(e) => {
          setName(e.target.value)
        }}
      />
      <Button type="submit" size="lg" className="self-start">
        {t('name.next')}
      </Button>
    </form>
  )
}
