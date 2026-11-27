import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type SyntheticEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate, useNavigate } from 'react-router'
import { Field } from '@/components/Field'
import { PageHeading } from '@/components/PageHeading'
import { TopBar } from '@/components/TopBar'
import { Button } from '@/components/ui/button'
import { instanceQueryKey } from '@/instance/instance'
import { PasskeyRegistration } from '@/register/PasskeyRegistration'
import { PasswordTotpRegistration } from '@/register/PasswordTotpRegistration'
import { type Me, sessionQueryKey } from '@/session/session'
import { CitiesStep } from './CitiesStep'
import { checkSetupCode, normalizeSetupCode, setupQuery, setupQueryKey } from './setup'

/**
 * The first-start setup wizard (G3): the setup code and the first maintainer's account (the same registration forms
 * as with an invite, password and TOTP first, passkey second), then, signed in, the instance's name and its cities.
 * Which step applies comes from the server (GET /api/osfm/setup); once there is nothing left to set up, the page
 * leads to the map.
 */
export function SetupPage() {
  const { t } = useTranslation()
  const setup = useQuery(setupQuery)
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  let content
  if (setup.isPending) {
    content = (
      <p role="status" className="text-muted-foreground">
        {t('setup.loading')}
      </p>
    )
  } else if (setup.isError) {
    content = (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>{t('setup.loadError')}</p>
        <Button
          onClick={() => {
            void setup.refetch()
          }}
        >
          {t('setup.retry')}
        </Button>
      </div>
    )
  } else if (setup.data === null) {
    return <Navigate to="/" replace />
  } else if (setup.data === 'maintainer') {
    content = (
      <MaintainerStep
        onDone={(me) => {
          if (me) {
            queryClient.setQueryData(sessionQueryKey, me)
          } else {
            void queryClient.invalidateQueries({ queryKey: sessionQueryKey })
          }
          void queryClient.invalidateQueries({ queryKey: setupQueryKey })
        }}
      />
    )
  } else {
    content = (
      <InstanceStep
        onDone={() => {
          queryClient.setQueryData(setupQueryKey, null)
          void queryClient.invalidateQueries({ queryKey: instanceQueryKey })
          void navigate('/', { replace: true })
        }}
      />
    )
  }

  return (
    <div className="flex min-h-dvh flex-col">
      <TopBar />
      <main className="flex-1 pb-8">
        <PageHeading>{t('setup.title')}</PageHeading>
        <div className="flex flex-col gap-4 px-4">{content}</div>
      </main>
    </div>
  )
}

function StepHeading({ n, name }: { n: 1 | 2 | 3; name: 'account' | 'name' | 'cities' }) {
  const { t } = useTranslation()
  return <h2 className="text-lg font-medium">{t('setup.step', { n, name: t(`setup.steps.${name}`) })}</h2>
}

/** Step 1: the setup code, then the first maintainer's account. onDone gets the member when the method returns it. */
function MaintainerStep({ onDone }: { onDone: (me?: Me) => void }) {
  const { t } = useTranslation()
  const [code, setCode] = useState<string | null>(null)
  const [method, setMethod] = useState<'password' | 'passkey'>('password')

  if (code === null) {
    return (
      <>
        <StepHeading n={1} name="account" />
        <p>{t('setup.code.intro')}</p>
        <SetupCodeForm onValid={setCode} />
      </>
    )
  }
  return (
    <>
      <StepHeading n={1} name="account" />
      <p>{t('setup.account.intro')}</p>
      <div role="group" aria-label={t('setup.account.method')} className="flex flex-wrap gap-2">
        {(['password', 'passkey'] as const).map((m) => (
          <Button
            key={m}
            variant={m === method ? 'default' : 'outline'}
            aria-pressed={m === method}
            onClick={() => {
              setMethod(m)
            }}
          >
            {t(`pages.register.methods.${m}`)}
          </Button>
        ))}
      </div>
      {method === 'password' ? (
        <PasswordTotpRegistration
          entitlement={{ setupCode: code }}
          onRegistered={onDone}
          continueLabel={t('setup.account.continue')}
        />
      ) : (
        <PasskeyRegistration
          entitlement={{ setupCode: code }}
          continueLabel={t('setup.account.continue')}
          onDone={() => {
            onDone()
          }}
        />
      )}
    </>
  )
}

function SetupCodeForm({ onValid }: { onValid: (code: string) => void }) {
  const { t } = useTranslation()
  const [input, setInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const check = useMutation({
    mutationFn: checkSetupCode,
    onSuccess: (valid, code) => {
      if (valid) {
        onValid(code)
      } else {
        setError(t('setup.code.invalid'))
      }
    },
    onError: () => {
      setError(t('setup.code.network'))
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (normalizeSetupCode(input) === null) {
      setError(t('setup.code.form'))
      return
    }
    setError(null)
    check.mutate(input.trim())
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <Field
        label={t('setup.code.label')}
        hint={t('setup.code.hint')}
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
        {check.isPending ? t('setup.code.checking') : t('setup.code.check')}
      </Button>
    </form>
  )
}

/** Steps 2 and 3, for the signed-in first maintainer: the instance's name, then its cities. */
function InstanceStep({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [stage, setStage] = useState<'name' | 'cities'>('name')
  const [error, setError] = useState<string | null>(null)

  if (stage === 'cities') {
    return (
      <>
        <StepHeading n={3} name="cities" />
        <CitiesStep
          name={name.trim()}
          onBack={() => {
            setStage('name')
          }}
          onDone={onDone}
        />
      </>
    )
  }
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const trimmed = name.trim()
    if (trimmed === '' || Array.from(trimmed).length > 100) {
      setError(t('setup.name.required'))
      return
    }
    setError(null)
    setStage('cities')
  }
  return (
    <>
      <StepHeading n={2} name="name" />
      <form onSubmit={submit} noValidate className="flex flex-col gap-4">
        <Field
          label={t('setup.name.label')}
          hint={t('setup.name.hint')}
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
          {t('setup.name.next')}
        </Button>
      </form>
    </>
  )
}
