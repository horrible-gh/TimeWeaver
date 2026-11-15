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
      {{ $t('enroll_setup_location') }}
      <a href="https://github.com/horrible-gh/TimeWeaver-Agent" target="_blank" rel="noopener noreferrer">TimeWeaver-Agent</a>
    </p>
    <p class="stage-intro" style="margin: 12px 0 0">{{ $t('enroll_setup_start') }}</p>
    <div class="codebox">{{ runCommand }}<button
      class="mini copy-code"
      :aria-label="$t('btn_copy')"
      @click="$emit('copy', runCommand)"
    ><i class="ph ph-copy"></i></button></div>
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
import { computed, defineEmits, defineProps } from "vue";
import { buildCommandBundle, buildRunCommand, bundleToText } from "@/dashboard/utils/enrollmentCommand";

const props = defineProps({
  vault: { type: Object, default: null },
  revealed: Boolean,
  remainingSeconds: { type: Number, default: 0 },
  expiryWarning: Boolean,
  copyFeedback: { type: String, default: null },
  method: { type: String, default: "interactive" },
});
defineEmits(["reveal", "hide", "copy", "start-watch", "update-method"]);

// A fixed-width mask never exposes the token length. Copy reads vault.token.
const MASK = "••••••••••••••••••••••••••••";
const METHODS = ["interactive", "linux"];

const commandText = computed(() => (props.vault
  ? bundleToText(buildCommandBundle(props.method))
  : ""));
const runCommand = computed(() => buildRunCommand(props.method));
const countdown = computed(() => {
  const seconds = Math.max(0, props.remainingSeconds);
  const hh = String(Math.floor(seconds / 3600)).padStart(2, "0");
  const mm = String(Math.floor((seconds % 3600) / 60)).padStart(2, "0");
  const ss = String(seconds % 60).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
});
</script>

<!-- Existing design tokens/components are reused; no new layout or colours. -->
