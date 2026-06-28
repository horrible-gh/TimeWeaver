// src/login/main.js
import { createApp } from 'vue'
import Login from "@/login/componenets/LoginForm.vue";  // Login page
import i18n from './i18n/login' // Login i18n instance

createApp(Login).use(i18n).mount('#app')
