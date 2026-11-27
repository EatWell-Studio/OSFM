import { Members } from './Members'
import { ResetLinks } from './ResetLinks'

/** The members and their reset links (D5), /admin/members. */
export function MembersAdmin() {
  return (
    <>
      <Members />
      <ResetLinks />
    </>
  )
}
