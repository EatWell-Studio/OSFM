import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import { en, namespaces, zhCN } from './resources'

// Every visible string goes through a translation key (docs/architecture.md §4.1), in namespaces by feature
// (resources.ts).
export const defaultLanguage = 'zh-CN'
export const supportedLanguages = ['zh-CN', 'en'] as const

export const resources = {
  'zh-CN': zhCN,
  en,
} as const

void i18n.use(initReactI18next).init({
  resources,
  ns: namespaces,
  defaultNS: 'common',
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
