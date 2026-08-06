<template>
  <div class="auth">
    <div class="auth-card panel">
      <div class="auth-brand">
        <span class="brand-mark">
          <img src="@/assets/img/dashboard/logo-timeweaver.png" alt="TimeWeaver" />
        </span>
        <strong>TimeWeaver</strong>
        <span>{{ $t('title') }}</span>
      </div>

      <form class="form-grid" @submit.prevent="login">
        <div class="field full">
          <label for="auth-id">ID</label>
          <input
            id="auth-id"
            type="text"
            name="id"
            v-model="id"
            placeholder="ID"
            @keypress.enter="$refs.password.focus()"
          />
        </div>

        <div class="field full">
          <label for="auth-password">{{ $t('password') }}</label>
          <input
            id="auth-password"
            ref="password"
            type="password"
            name="pwd"
            v-model="password"
            :placeholder="$t('password')"
            @keypress.enter="login"
          />
        </div>

        <div class="field full" v-if="error">
          <small class="error">{{ error }}</small>
        </div>

        <div class="field full auth-actions">
          <button type="button" class="btn primary" :disabled="isLoading" @click="login">
            <i v-if="isLoading" class="ph ph-circle-notch auth-spinner"></i>
            <span v-else>{{ $t('login') }}</span>
          </button>
          <button type="button" class="btn" @click="register">
            {{ $t('join') }}
          </button>
        </div>
      </form>

      <div class="auth-foot">
        <button type="button" class="text-btn" @click="forgot">
          {{ $t('forgot') }}
        </button>
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
      error: "",
    };
  },
  methods: {
    async login() {
      this.error = "";
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
        this.error = this.$t('login_failed');
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
