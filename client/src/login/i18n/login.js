// src/i18n.js
import { createI18n } from 'vue-i18n'
import ko_msg from './ko/login'

const userLocale = navigator.language.split('-')[0] || 'en';

// Define only the message objects for each language.
const messages = {
  en: {
    title: 'TimeWeaver-UI',
    login: "Login",
    join: "Join",
    forgot: "Forgot Password",
    password: "Password"
  },
  ja: {
    title: 'TimeWeaver-UI',
    login: "ログイン",
    join: "会員登録",
    forgot: "パスワードのお忘れ",
    password: "パスワード"
  },
  ko: {...ko_msg}
}

// Create i18n instance
const i18n = createI18n({
  locale: userLocale,       // Default locale, such as en or the browser language
  fallbackLocale: 'en',     // Fallback locale
  messages                // Pass only the language-specific message objects.
})

export default i18n
