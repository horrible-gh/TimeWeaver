<template>
  <section v-if="vault">
    <h3>{{ $t('enroll_deliver_heading') }}</h3>
    <p class="stage-intro">{{ $t('enroll_deliver_desc') }}</p>

    <div class="token-box">
      <div class="token-label" :class="{ warning: expiryWarning }">
        <span>{{ $t('enroll_label_token') }} · {{ $t(expiryWarning ? 'enroll_expiry_warning' : 'enroll_remaining', { t: countdown }) }}</span>
        <button class="text-btn" @click="$emit(revealed ? 'hide' : 'reveal')">
          {{ $t(revealed ? 'btn_hide' : 'btn_reveal') }}
        </button>
      </div>
      <div class="token-value">
        <code :title="revealed ? vault.token : ''">{{ revealed ? vault.token : MASK }}</code>
        <button class="btn" @click="$emit('copy', vault.token)">
          <i class="ph ph-copy"></i> {{ $t('btn_copy') }}
        </button>
      </div>
    </div>

    <div class="security-callout">
      <i class="ph ph-warning"></i>
      <span>{{ $t('enroll_warn_secret') }}</span>
    </div>

    <div class="command-tabs" role="tablist" :aria-label="$t('enroll_label_command')">
      <button
        v-for="option in METHODS"
        :key="option"
        class="command-tab"
        :class="{ active: method === option }"
        role="tab"
        :aria-selected="method === option"
        @click="$emit('update-method', option)"
      >
        {{ $t('enroll_method_' + option) }}
      </button>
    </div>

    <div class="codebox">{{ commandText }}<button
      class="mini copy-code"
      :aria-label="$t('btn_copy_all')"
      @click="$emit('copy', commandText)"
    ><i class="ph ph-copy"></i></button></div>

    <p class="stage-intro" style="margin: 12px 0 0">
      {{ $t('enroll_hint_install_dir', { dir: installDir }) }}
      <template v-if="method === 'system_task'"> {{ $t('enroll_hint_system_task') }}</template>
    </p>
    <p v-if="copyFeedback === 'success'" class="stage-intro" style="color: var(--green); margin: 6px 0 0">{{ $t('msg_copied') }}</p>
    <p v-if="copyFeedback === 'failed'" class="stage-intro" style="color: var(--red); margin: 6px 0 0">{{ $t('msg_copy_failed') }}</p>

    <div class="stage-actions">
      <button class="btn primary" @click="$emit('start-watch')">
        <i class="ph ph-broadcast"></i> {{ $t('btn_start_watch') }}
      </button>
    </div>
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

// Fixed-width mask: the deck shows bullets, and a mask that tracked the real
// length would leak it. Copy always reads vault.token, never this string.
const MASK = "••••••••••••••••••••••••••••";
const METHODS = ["interactive", "system_task"];

const installDir = ref(DEFAULT_INSTALL_DIR);
const commandText = computed(() => (props.vault
  ? bundleToText(buildCommandBundle(props.method, props.vault.token, installDir.value))
  : ""));
const countdown = computed(() => {
  const seconds = Math.max(0, props.remainingSeconds);
  const hh = String(Math.floor(seconds / 3600)).padStart(2, "0");
  const mm = String(Math.floor((seconds % 3600) / 60)).padStart(2, "0");
  const ss = String(seconds % 60).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
});
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). `.btn` / `.mini` /
  `.text-btn` / `.command-tabs` / `.command-tab` come from components.css;
  `.token-box` / `.token-value` / `.codebox` / `.copy-code` / `.stage-intro` /
  `.stage-actions` / `.security-callout` from devices.css. The two inline
  colours are token references, not literals.

  The `<div class="codebox">` opening and closing tags are deliberately kept
  tight around the text: `.codebox` is `white-space: pre-wrap`, so a newline in
  the template would render as a blank first line of the command.
-->
