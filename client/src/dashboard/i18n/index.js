// src/i18n.js
import { createI18n } from 'vue-i18n'
import en_msg from './en/dashboard'
import ja_msg from './ja/dashboard'
import ko_msg from './ko/dashboard'
import agentSetup from './agentSetup'
import housekeeping from './housekeeping'
import access from './access'

const userLocale = navigator.language.split('-')[0] || 'en';

const messages = {
  en: {...en_msg, ...agentSetup.en, ...housekeeping.en, ...access.en},
  ko: {...ko_msg, ...agentSetup.ko, ...housekeeping.ko, ...access.ko},
  ja: {...ja_msg, ...agentSetup.ja, ...housekeeping.ja, ...access.ja}
};

const i18n = createI18n({
  locale: userLocale,
  fallbackLocale: 'en',
  messages
})

export default i18n
