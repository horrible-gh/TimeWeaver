// src/i18n.js
import { createI18n } from 'vue-i18n'
import en_msg from './en/login'
import ja_msg from './ja/login'
import ko_msg from './ko/login'

const userLocale = navigator.language.split('-')[0] || 'en';

// Define only the message objects for each language.
const messages = {
  en: {...en_msg},
  ja: {...ja_msg},
  ko: {...ko_msg}
}

// Create i18n instance
const i18n = createI18n({
  locale: userLocale,       // Default locale, such as en or the browser language
  fallbackLocale: 'en',     // Fallback locale
  messages                // Pass only the language-specific message objects.
})

export default i18n
