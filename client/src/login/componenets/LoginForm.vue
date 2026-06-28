<template>
    <div class="limiter">
      <div
        class="container-login100"
        :style="{ backgroundImage: `url(${require('@/assets/login_form/images/bg-01.jpg')})` }"
      >
        <div class="wrap-login100 p-t-30 p-b-50">
          <span class="login100-form-title p-b-41">
            {{ $t('title') }}
          </span>
          <form name="loginForm" class="login100-form validate-form p-b-33 p-t-5" @submit.prevent="login">
            <div class="wrap-input100 validate-input" data-validate="Enter username">
              <input
                class="input100"
                type="text"
                name="id"
                v-model="id"
                placeholder="ID"
                @keypress.enter="$refs.password.focus()"
              />
              <span class="focus-input100" data-placeholder="&#xe82a;"></span>
            </div>

            <div class="wrap-input100 validate-input" data-validate="Enter password">
              <input
                class="input100"
                ref="password"
                type="password"
                name="pwd"
                v-model="password"
                :placeholder="$t('password')"
                @keypress.enter="login"
              />
              <span class="focus-input100" data-placeholder="&#xe80f;"></span>
            </div>

            <div class="container-login100-form-btn m-t-32">
              <button type="button" class="login100-form-btn m-r-8" @click="login">
                {{ $t('login') }}
              </button>
              <button type="button" class="login105-form-btn m-l-8" @click="register">
                {{ $t('join') }}
              </button>
              <br /><br />
              <a href="javascript:void(0);" class="login110-form-btn" @click="forgot">
                {{ $t('forgot') }}
              </a>
              <div id="process" v-if="isLoading">
                <i class="fa fa-spinner fa-spin"></i>
              </div>
            </div>
          </form>
        </div>
      </div>
    </div>
  </template>

<script>
import { postRequest } from "@api";

export default {
  name: 'LoginForm',
  data() {
    return {
      id: "",
      password: "",
      isLoading: false,
    };
  },
  methods: {
    async login() {
      try {
        const response = await postRequest("/login", {
          username: this.id,
          password: this.password,
        });

        const token = response.access_token;
        localStorage.setItem("access_token", token);
        const user = response.user;
        localStorage.setItem("user", JSON.stringify(user)); // ✅ Convert object to a JSON string before saving
        console.log("Login succeeded:", response);
        window.location.href = "/dashboard";
      } catch (error) {
        console.error("Login failed:", error);
      }
    },
    register() {
      console.log("Register clicked");
      alert("Go to the registration page");
    },
    forgot() {
      console.log("Forgot password clicked");
      alert("Show password recovery page");
    },
  }
};
</script>

<style>
@import "@/assets/login_form/css/util.css";
@import "@/assets/login_form/css/main.css";
@import "@/assets/login_form/fonts/Linearicons-Free-v1.0.0/icon-font.min.css";
</style>
