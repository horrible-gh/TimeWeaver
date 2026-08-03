<template>
  <section v-if="vault" class="step-card">
    <h3>{{ $t('enroll_deliver_heading') }}</h3>
    <p>{{ $t('enroll_deliver_desc') }}</p>
    <p class="countdown" :class="{ warning: expiryWarning }">
      {{ $t(expiryWarning ? 'enroll_expiry_warning' : 'enroll_remaining', { t: countdown }) }}
    </p>

    <label class="block-label">{{ $t('enroll_label_token') }}</label>
    <div class="secret-row">
      <input class="secret" :type="revealed ? 'text' : 'password'" :value="vault.token" readonly />
      <button class="ghost" @click="$emit(revealed ? 'hide' : 'reveal')">{{ $t(revealed ? 'btn_hide' : 'btn_reveal') }}</button>
      <button class="ghost" @click="$emit('copy', vault.token)">{{ $t('btn_copy') }}</button>
    </div>

    <div class="method-grid">
      <label><input type="radio" value="interactive" :checked="method === 'interactive'" @change="$emit('update-method', 'interactive')" /> {{ $t('enroll_method_interactive') }}</label>
      <label><input type="radio" value="system_task" :checked="method === 'system_task'" @change="$emit('update-method', 'system_task')" /> {{ $t('enroll_method_system_task') }}</label>
    </div>

    <label class="block-label">{{ $t('enroll_label_command') }}</label>
    <textarea class="command" :value="commandText" readonly rows="5"></textarea>
    <div class="command-actions">
      <button class="ghost" @click="$emit('copy', commandText)">{{ $t('btn_copy_all') }}</button>
      <span v-if="copyFeedback === 'success'" class="success">{{ $t('msg_copied') }}</span>
      <span v-if="copyFeedback === 'failed'" class="error">{{ $t('msg_copy_failed') }}</span>
    </div>
    <p>{{ $t('enroll_hint_install_dir', { dir: installDir }) }}</p>
    <p v-if="method === 'system_task'" class="warning-text">{{ $t('enroll_hint_system_task') }}</p>
    <p class="secret-warning">{{ $t('enroll_warn_secret') }}</p>
    <button class="primary" @click="$emit('start-watch')">{{ $t('btn_start_watch') }}</button>
  </section>
</template>

<script setup>
import { computed, defineEmits, defineProps, ref } from "vue";
import { DEFAULT_INSTALL_DIR } from "@/dashboard/constants/enrollment";
import { buildCommandBundle, bundleToText } from "@/dashboard/utils/enrollmentCommand";

const props = defineProps({
  vault: { type: Object, default: null },
  revealed: Boolean,
  remainingSeconds: { type: Number, default: 0 },
  expiryWarning: Boolean,
  copyFeedback: { type: String, default: null },
  method: { type: String, default: "interactive" },
});
defineEmits(["reveal", "hide", "copy", "start-watch", "update-method"]);

const installDir = ref(DEFAULT_INSTALL_DIR);
const commandText = computed(() => props.vault ? bundleToText(buildCommandBundle(props.method, props.vault.token, installDir.value)) : "");
const countdown = computed(() => {
  const seconds = Math.max(0, props.remainingSeconds);
  const hh = String(Math.floor(seconds / 3600)).padStart(2, "0");
  const mm = String(Math.floor((seconds % 3600) / 60)).padStart(2, "0");
  const ss = String(seconds % 60).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
});
</script>

<style scoped>
.step-card { padding: 24px; border: 1px solid #375575; border-radius: 12px; background: rgba(5, 25, 45, .72); }
.secret-row, .command-actions, .method-grid { display: flex; gap: 10px; align-items: center; }
.secret-row { margin-bottom: 18px; }
.secret, .command { width: 100%; color: #ddecfb; background: #020b14; border: 1px solid #49647f; border-radius: 7px; padding: 11px; font-family: monospace; }
.command { resize: vertical; white-space: pre; overflow: auto; }
.method-grid { margin: 18px 0; flex-wrap: wrap; }
.block-label { display: block; margin: 13px 0 7px; }
.ghost, .primary { white-space: nowrap; padding: 9px 13px; border: 1px solid #3f91d4; border-radius: 6px; color: #fff; background: transparent; cursor: pointer; }
.primary { margin-top: 12px; background: #0b73b9; }
.countdown { font-weight: 700; color: #83d8ff; }
.warning, .warning-text { color: #fcd34d; }
.secret-warning { border-left: 3px solid #f0b84d; padding: 10px 12px; color: #c8d6e5; }
.success { color: #6ee7b7; } .error { color: #fda4af; }
@media (max-width: 767px) { .secret-row { align-items: stretch; flex-wrap: wrap; } .secret-row .secret { flex-basis: 100%; } }
</style>
