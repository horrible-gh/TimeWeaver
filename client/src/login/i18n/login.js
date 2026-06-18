// src/i18n.js
import { createI18n } from 'vue-i18n'

const userLocale = navigator.language.split('-')[0] || 'en';

// 각 언어별 메시지 객체만 정의합니다.
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
  ko: {
    title: 'TimeWeaver-UI',
    login: "로그인",
    join: "회원가입",
    forgot: "패스워드 찾기",
    password: "패스워드"
  }
}

// i18n 인스턴스 생성
const i18n = createI18n({
  locale: userLocale,       // 기본 로케일 (예: 'en' 또는 브라우저의 언어)
  fallbackLocale: 'en',     // fallback 로케일
  messages                // 언어별 메시지 객체만 전달합니다.
})

export default i18n
