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

// i18n 인스턴스 생성
const i18n = createI18n({
  locale: userLocale,       // 기본 로케일 (예: 'en' 또는 브라우저의 언어)
  fallbackLocale: 'en',     // fallback 로케일
  messages                // 언어별 메시지 객체만 전달합니다.
})

export default i18n
