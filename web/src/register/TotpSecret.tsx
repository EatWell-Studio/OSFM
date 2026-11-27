import { Check, Copy } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { groupSecret } from '@/lib/passwordTotp'

/**
 * A new TOTP secret for the authenticator app: the QR code (an inline SVG made here, no external service), a link that
 * opens the app on this device, and the secret to type in or copy. Used at registration and wherever a member sets up
 * or replaces the authenticator app.
 */
export function TotpSecret({ otpauthUri, secret }: { otpauthUri: string; secret: string }) {
  const { t } = useTranslation('auth')
  const [copied, setCopied] = useState(false)
  return (
    <>
      <div className="self-center rounded-lg bg-white p-2">
        <QRCodeSVG value={otpauthUri} size={208} level="M" title={t('register.password.qrTitle')} />
      </div>
      <Button asChild variant="outline" className="self-start">
        <a href={otpauthUri}>{t('register.password.openApp')}</a>
      </Button>
      <div className="flex flex-col gap-1">
        <p className="text-sm">{t('register.password.secretLabel')}</p>
        <div className="flex flex-wrap items-center gap-2">
          <code className="rounded border bg-muted/40 px-2 py-1 font-mono text-sm break-all select-all">
            {groupSecret(secret)}
          </code>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              navigator.clipboard.writeText(secret).then(
                () => {
                  setCopied(true)
                },
                () => {
                  setCopied(false)
                },
              )
            }}
          >
            {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
            {copied ? t('register.password.copied') : t('register.password.copySecret')}
          </Button>
        </div>
      </div>
    </>
  )
}
