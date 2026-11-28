// Messages for the refusals of the governance routes (removal, branches, roles), by the server's error code.

import type { TFunction } from 'i18next'
import {
  CodeBranchChanged,
  CodeCannotDemoteSelf,
  CodeCannotRemoveMaintainer,
  CodeCannotRemoveSelf,
  CodeLastMaintainer,
} from '@/api/types.gen'
import { errorCode } from '@/credentials/api'

export function governanceErrorMessage(t: TFunction<'members'>, error: unknown): string {
  switch (errorCode(error)) {
    case 'not_found':
      return t('errors.notFound')
    case CodeCannotRemoveSelf:
      return t('errors.removeSelf')
    case CodeCannotRemoveMaintainer:
      return t('errors.removeMaintainer')
    case CodeCannotDemoteSelf:
      return t('errors.demoteSelf')
    case CodeLastMaintainer:
      return t('errors.lastMaintainer')
    case CodeBranchChanged:
      return t('errors.branchChanged')
    case 'forbidden':
      return t('errors.forbidden')
    default:
      return t('errors.generic')
  }
}
