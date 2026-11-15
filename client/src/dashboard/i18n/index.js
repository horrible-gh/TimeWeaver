// src/i18n.js
import { createI18n } from 'vue-i18n'
import en_msg from './en/dashboard'
import ja_msg from './ja/dashboard'
import ko_msg from './ko/dashboard'
import agentSetup from './agentSetup'

const userLocale = navigator.language.split('-')[0] || 'en';

const messages = {
  en: {...en_msg, ...agentSetup.en},
  ko: {...ko_msg, ...agentSetup.ko},
  ja: {...ja_msg, ...agentSetup.ja}
};

const i18n = createI18n({
  locale: userLocale,
  fallbackLocale: 'en',
  messages
})

export default i18n
