import 'i18next'
import type zhCN from './locales/zh-CN.json'

// Typed translation keys: t('nav.map') is checked against the zh-CN locale at compile time.
declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'translation'
    resources: {
      translation: typeof zhCN
    }
  }
}
