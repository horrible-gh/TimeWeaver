// src/i18n.js
import { createI18n } from 'vue-i18n'

const userLocale = navigator.language.split('-')[0] || 'en';

import en_msg from './en/dashboard'
import ja_msg from './ja/dashboard'
import ko_msg from './ko/dashboard'


const messages = {
  en: {...en_msg},
  ko: {...ko_msg},
  ja: {...ja_msg}
};

// Create i18n instance
const i18n = createI18n({
  locale: userLocale,       // Default locale, such as en or the browser language
  fallbackLocale: 'en',     // Fallback locale
  messages                // Pass only the language-specific message objects.
})

export default i18n
