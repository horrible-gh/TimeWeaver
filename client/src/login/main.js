// src/login/main.js
import { createApp } from 'vue'
import Login from "@/login/componenets/LoginForm.vue";  // 로그인 페이지
import i18n from './i18n/login' // 로그인용 i18n 인스턴스

createApp(Login).use(i18n).mount('#app')
