import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enJson from './locales/en.json'
import zhCN from './locales/zh-CN.json'

// Every visible string goes through a translation key (docs/architecture.md §4.1). zh-CN is the source locale: its
// keys define the type of all others, so a key missing from en fails the type check.
export const defaultLanguage = 'zh-CN'
export const supportedLanguages = ['zh-CN', 'en'] as const

const en: typeof zhCN = enJson

export const resources = {
  'zh-CN': { translation: zhCN },
  en: { translation: en },
} as const

void i18n.use(initReactI18next).init({
  resources,
  lng: defaultLanguage,
  fallbackLng: defaultLanguage,
  supportedLngs: supportedLanguages,
  // The resources are bundled, so initialisation can finish synchronously: the first render already has the texts.
  initAsync: false,
  interpolation: {
    // React escapes rendered values already.
    escapeValue: false,
  },
  react: {
    useSuspense: false,
  },
})

i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
})

export { i18n }
