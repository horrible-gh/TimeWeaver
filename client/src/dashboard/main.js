// src/dashboard/main.js
import { createApp } from 'vue'
import App from './App.vue';
import router from "@/dashboard/router/index"
import i18n from './i18n/index' // 로그인용 i18n 인스턴스

const app = createApp(App);
app.use(router);
app.use(i18n);
app.mount('#app');
