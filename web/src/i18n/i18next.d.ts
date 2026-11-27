import 'i18next'
import type { Namespaces } from './resources'

// Typed translation keys: t('nav.map') is checked against the zh-CN locale at compile time. Without a namespace, keys
// are those of "common"; a component of a feature uses its own, useTranslation('admin'), and reaches the common texts
// with a prefix, t('common:retry'), after naming both: useTranslation(['admin', 'common']).
declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common'
    resources: Namespaces
  }
}
