<template>
  <section class="enrollment-panel">
    <button class="back-button" @click="closePanel">← {{ $t('btn_back_to_devices') }}</button>
    <header>
      <span class="admin-label">{{ $t('enroll_admin_only') }}</span>
      <h2>{{ $t('enroll_title') }}</h2>
      <p>{{ $t('enroll_desc') }}</p>
    </header>
    <ol class="stepper">
      <li :class="{ active: step === 1, done: step > 1 }"><strong>1</strong><span>{{ $t('enroll_step1_title') }}<small>{{ $t('enroll_step1_sub') }}</small></span></li>
      <li :class="{ active: step === 2, done: step > 2 }"><strong>2</strong><span>{{ $t('enroll_step2_title') }}<small>{{ $t('enroll_step2_sub') }}</small></span></li>
      <li :class="{ active: step === 3 }"><strong>3</strong><span>{{ $t('enroll_step3_title') }}<small>{{ $t('enroll_step3_sub') }}</small></span></li>
    </ol>

    <p v-if="notice && notice.key" class="panel-notice">{{ $t(notice.key, notice.params) }}</p>

    <EnrollmentFormStep
      v-if="step === 1"
      :groups="groups"
      :form="form"
      :errors="formErrors"
      :canIssue="canIssue"
      :flowState="flowState"
      @update-group="form.groupId = $event"
      @update-ttl="form.ttlHours = $event"
      @update-device="form.deviceName = $event"
      @issue="issue"
    />
    <EnrollmentDeliverStep
      v-else-if="step === 2"
      :vault="vault"
      :revealed="revealed"
      :remainingSeconds="remainingSeconds"
      :expiryWarning="expiryWarning"
      :copyFeedback="copyFeedback"
      :method="executionMethod"
      @reveal="reveal"
      @hide="hide"
      @copy="copy"
      @update-method="executionMethod = $event"
      @start-watch="startWatch"
    />
    <EnrollmentWatchStep
      v-else
      :flowState="flowState"
      :form="form"
      :enrollmentId="enrollmentId"
      :groupName="selectedGroupName"
      :listReflectState="listReflectState"
      :method="executionMethod"
      :vault="vault"
      @copy="copy"
      @recheck="recheck"
      @reissue="reissue"
      @show-command="showCommand = true"
      @close="closePanel"
    />
  </section>
</template>

<script setup>
import { computed, defineEmits, defineProps, ref, watch } from "vue";
import EnrollmentDeliverStep from "@/dashboard/components/contents/devices/EnrollmentDeliverStep.vue";
import EnrollmentFormStep from "@/dashboard/components/contents/devices/EnrollmentFormStep.vue";
import EnrollmentWatchStep from "@/dashboard/components/contents/devices/EnrollmentWatchStep.vue";

const props = defineProps({
  flow: { type: Object, required: true },
  groups: { type: Array, default: () => [] },
});
const emit = defineEmits(["close"]);
// eslint-disable-next-line vue/no-setup-props-destructure
const {
  flowState, form, formErrors, vault, enrollmentId, revealed, remainingSeconds,
  listReflectState, notice, copyFeedback, executionMethod, expiryWarning, canIssue,
  issue, reveal, hide, copy, startWatch, recheck, reissue, closeFlow,
} = props.flow;
const showCommand = ref(false);
watch(flowState, () => { if (flowState.value !== "waiting") showCommand.value = false; });

const step = computed(() => {
  if (flowState.value === "form" || flowState.value === "issuing") return 1;
  if (flowState.value === "delivering" || (flowState.value === "waiting" && showCommand.value)) return 2;
  return 3;
});
const selectedGroupName = computed(() => {
  if (Number(form.groupId) === 0) return "Unknown";
  const group = props.groups.find((item) => Number(item.group_id) === Number(form.groupId));
  return group ? group.group_name : form.groupId;
});
function closePanel() {
  closeFlow();
  emit("close");
}
</script>

<style scoped>
.enrollment-panel { max-width: 1120px; margin: 0 auto; }
.back-button { color: #9ed6ff; border: 0; background: transparent; cursor: pointer; padding: 8px 0; }
.enrollment-panel header { margin: 14px 0 22px; }
.enrollment-panel header p { color: #a9bdd2; }
.admin-label { display: inline-block; padding: 4px 8px; border: 1px solid #f0b84d; border-radius: 999px; color: #fcd34d; font-size: 12px; }
.stepper { display: grid; grid-template-columns: repeat(3, 1fr); list-style: none; padding: 0; margin: 0 0 20px; gap: 10px; }
.stepper li { display: flex; gap: 10px; align-items: center; padding: 12px; color: #8196aa; border-bottom: 2px solid #344b62; }
.stepper li.active, .stepper li.done { color: #fff; border-color: #3ca7e8; }
.stepper strong { display: grid; place-items: center; width: 28px; height: 28px; border-radius: 50%; background: #173956; }
.stepper small { display: block; color: #91a7ba; }
.panel-notice { padding: 11px; border-left: 3px solid #e27b87; color: #fecdd3; background: rgba(225, 73, 94, .08); }
@media (max-width: 1200px) { .stepper small { display: none; } }
@media (max-width: 767px) { .stepper li { padding: 8px 4px; font-size: 12px; } }
</style>
