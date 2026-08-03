<template>
  <section class="step-card">
    <h3>{{ $t('enroll_watch_heading') }}</h3>
    <p>{{ $t('enroll_watch_desc') }}</p>
    <div class="watch-grid">
      <div>
        <h4>{{ $t('enroll_summary_title') }}</h4>
        <dl>
          <div><dt>{{ $t('enroll_label_device_name') }}</dt><dd>{{ form.deviceName }}</dd></div>
          <div><dt>{{ $t('enroll_label_group') }}</dt><dd>{{ groupName || form.groupId }}</dd></div>
          <div><dt>{{ $t('enroll_label_ttl') }}</dt><dd>{{ form.ttlHours }}</dd></div>
          <div><dt>{{ $t('enroll_label_enrollment_id') }}</dt><dd :title="String(enrollmentId || '')">{{ shortId }}</dd></div>
        </dl>
      </div>
      <div>
        <h4>{{ $t('enroll_progress_title') }}</h4>
        <ol class="progress">
          <li class="done">{{ $t('enroll_progress_admin') }}</li>
          <li class="done">{{ $t('enroll_progress_issue') }}</li>
          <li :class="{ done: flowState === 'succeeded' }">{{ $t('enroll_progress_connect') }}</li>
          <li :class="{ done: listReflectState === 'done' }">{{ $t('enroll_progress_list') }}</li>
        </ol>
      </div>
    </div>
    <p class="state-message" :class="'state-' + flowState">{{ $t(stateKey) }}</p>
    <p v-if="flowState === 'succeeded' && listReflectState">{{ $t('enroll_list_' + listReflectState) }}</p>
    <div v-if="cleanupCommand" class="cleanup">
      <h4>{{ $t('enroll_cleanup_title') }}</h4>
      <textarea :value="cleanupCommand" readonly rows="2"></textarea>
      <button class="ghost" @click="$emit('copy', cleanupCommand)">{{ $t('btn_copy') }}</button>
    </div>
    <div class="actions">
      <button v-if="flowState === 'watchFailed' || flowState === 'watchPaused'" class="primary" @click="$emit('recheck')">{{ $t('btn_recheck') }}</button>
      <button v-if="flowState === 'expired' || flowState === 'revoked'" class="primary" @click="$emit('reissue')">{{ $t('btn_reissue') }}</button>
      <button v-if="vault" class="ghost" @click="$emit('show-command')">{{ $t('btn_show_command') }}</button>
      <button class="ghost" @click="$emit('close')">{{ $t('btn_close') }}</button>
    </div>
  </section>
</template>

<script setup>
import { computed, defineEmits, defineProps } from "vue";
import { buildCleanupCommand } from "@/dashboard/utils/enrollmentCommand";

const props = defineProps({
  flowState: { type: String, required: true },
  form: { type: Object, required: true },
  enrollmentId: { type: [String, Number], default: null },
  groupName: { type: [String, Number], default: "" },
  listReflectState: { type: String, default: null },
  method: { type: String, default: "interactive" },
  vault: { type: Object, default: null },
});
defineEmits(["recheck", "reissue", "show-command", "close", "copy"]);

const shortId = computed(() => String(props.enrollmentId || "—").slice(0, 8));
const cleanupCommand = computed(() => props.flowState === "succeeded" ? buildCleanupCommand(props.method) : null);
const stateKey = computed(() => ({
  waiting: "enroll_state_waiting",
  succeeded: "enroll_state_succeeded",
  expired: "enroll_state_expired",
  revoked: "enroll_state_revoked",
  watchFailed: "enroll_state_watch_failed",
  watchPaused: "enroll_state_watch_paused",
  forbidden: "err_admin_required",
}[props.flowState] || "enroll_state_waiting"));
</script>

<style scoped>
.step-card { padding: 24px; border: 1px solid #375575; border-radius: 12px; background: rgba(5, 25, 45, .72); }
.watch-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 22px; }
dl div { display: flex; justify-content: space-between; gap: 15px; padding: 7px 0; border-bottom: 1px solid rgba(90, 120, 150, .25); }
dd { margin: 0; text-align: right; overflow-wrap: anywhere; }
.progress { list-style: none; padding: 0; }
.progress li { margin: 9px 0; color: #90a4b8; }
.progress li::before { content: "○"; margin-right: 8px; }
.progress li.done { color: #6ee7b7; } .progress li.done::before { content: "●"; }
.state-message { padding: 12px; border-left: 3px solid #4da3df; background: rgba(77, 163, 223, .08); }
.cleanup textarea { width: 100%; color: #ddecfb; background: #020b14; border: 1px solid #49647f; }
.actions { display: flex; gap: 10px; margin-top: 18px; flex-wrap: wrap; }
.ghost, .primary { padding: 9px 13px; border: 1px solid #3f91d4; border-radius: 6px; color: #fff; background: transparent; cursor: pointer; }
.primary { background: #0b73b9; }
@media (max-width: 767px) { .watch-grid { grid-template-columns: 1fr; } }
</style>
