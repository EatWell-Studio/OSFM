import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { CredentialsSection } from '@/credentials/CredentialsSection'
import { AccountSection } from '@/me/AccountSection'
import { InvitesSection } from '@/me/InvitesSection'
import { LayerExport } from '@/me/LayerExport'
import { LeaveSection } from '@/me/LeaveSection'
import { NoticesSection } from '@/me/NoticesSection'
import { PersonalExport } from '@/me/PersonalExport'
import { ProfileSection } from '@/me/ProfileSection'

/**
 * The member's own page, one section per file (src/me/, and the credentials of D5 in src/credentials/): notices first
 * (M2-Q14), the profile, invites, ways to sign in, the exports, signing out (and the administration for maintainers),
 * and leaving the circle last. After a recovery, the page says to check the old passkeys.
 */
export function MePage() {
  const { t } = useTranslation(['me', 'auth'])
  const location = useLocation()
  const recovered = (location.state as { recovered?: boolean } | null)?.recovered === true
  return (
    <>
      <PageHeading>{t('title')}</PageHeading>
      <div className="flex flex-col gap-8 px-4 pb-4">
        {recovered && (
          <p role="status" className="rounded-lg border border-primary/40 p-3 text-sm">
            {t('auth:credentials.recover.checkPasskeys')}
          </p>
        )}
        <NoticesSection />
        <ProfileSection />
        <InvitesSection />
        <CredentialsSection />
        <PersonalExport />
        <LayerExport />
        <AccountSection />
        <LeaveSection />
      </div>
    </>
  )
}
