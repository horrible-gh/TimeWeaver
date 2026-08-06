<template>
  <section>
    <div class="waiting" :class="outcome">
      <div class="radar"><i :class="radarIcon"></i></div>
      <h3>{{ $t(headingKey) }}</h3>
      <p class="stage-intro">
        {{ $t('enroll_watch_desc') }}
        <template v-if="flowState === 'succeeded' && listReflectState"> {{ $t('enroll_list_' + listReflectState) }}</template>
      </p>
      <span class="badge" :class="badgeClass">{{ $t(stateKey) }}</span>
    </div>

    <template v-if="cleanupCommand">
      <div class="divider"></div>
      <h4>{{ $t('enroll_cleanup_title') }}</h4>
      <div class="codebox">{{ cleanupCommand }}<button
        class="mini copy-code"
        :aria-label="$t('btn_copy')"
        @click="$emit('copy', cleanupCommand)"
      ><i class="ph ph-copy"></i></button></div>
      <p v-if="copyFeedback === 'success'" class="stage-intro" style="color: var(--green); margin: 6px 0 0">{{ $t('msg_copied') }}</p>
      <p v-if="copyFeedback === 'failed'" class="stage-intro" style="color: var(--red); margin: 6px 0 0">{{ $t('msg_copy_failed') }}</p>
    </template>

    <div class="stage-actions">
      <button v-if="vault" class="btn" @click="$emit('show-command')">{{ $t('btn_show_command') }}</button>
      <button v-if="flowState === 'watchFailed' || flowState === 'watchPaused'" class="btn secondary" @click="$emit('recheck')">
        <i class="ph ph-arrow-clockwise"></i> {{ $t('btn_recheck') }}
      </button>
      <button v-if="flowState === 'expired' || flowState === 'revoked'" class="btn secondary" @click="$emit('reissue')">
        <i class="ph ph-key"></i> {{ $t('btn_reissue') }}
      </button>
      <button class="btn primary" @click="$emit('close')">{{ $t('btn_close') }}</button>
    </div>
  </section>
</template>

<script setup>
import { computed, defineEmits, defineProps } from "vue";
import { buildCleanupCommand } from "@/dashboard/utils/enrollmentCommand";

const props = defineProps({
  flowState: { type: String, required: true },
  listReflectState: { type: String, default: null },
  method: { type: String, default: "interactive" },
  vault: { type: Object, default: null },
  copyFeedback: { type: String, default: null },
});
defineEmits(["recheck", "reissue", "show-command", "close", "copy"]);

const FAILED = ["expired", "revoked", "watchFailed", "forbidden"];

const cleanupCommand = computed(() => (props.flowState === "succeeded" ? buildCleanupCommand(props.method) : null));
const outcome = computed(() => {
  if (props.flowState === "succeeded") return "success";
  return FAILED.includes(props.flowState) ? "failed" : "";
});
const radarIcon = computed(() => {
  if (props.flowState === "succeeded") return "ph ph-check";
  return FAILED.includes(props.flowState) ? "ph ph-warning" : "ph ph-broadcast";
});
const badgeClass = computed(() => {
  if (props.flowState === "succeeded") return "online";
  if (props.flowState === "watchPaused") return "offline";
  return FAILED.includes(props.flowState) ? "danger" : "warning";
});
const stateKey = computed(() => ({
  waiting: "enroll_state_waiting",
  succeeded: "enroll_state_succeeded",
  expired: "enroll_state_expired",
  revoked: "enroll_state_revoked",
  watchFailed: "enroll_state_watch_failed",
  watchPaused: "enroll_state_watch_paused",
  forbidden: "err_admin_required",
}[props.flowState] || "enroll_state_waiting"));
const headingKey = computed(() => {
  if (props.flowState === "succeeded") return "enroll_watch_heading_done";
  return FAILED.includes(props.flowState) || props.flowState === "watchPaused"
    ? stateKey.value
    : "enroll_watch_heading";
});
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). `.badge` / `.btn` /
  `.mini` / `.divider` come from components.css; `.waiting` / `.radar` /
  `.codebox` / `.copy-code` / `.stage-intro` / `.stage-actions` from devices.css.

  The enrollment summary and the progress timeline that used to sit in this
  component now live in EnrollmentPanel's side rail, where the deck puts them
  and where they stay visible on all three steps. Props `form`, `groupName` and
  `enrollmentId` moved with them.
-->
