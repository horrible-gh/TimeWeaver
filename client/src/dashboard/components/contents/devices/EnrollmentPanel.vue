<template>
  <div class="panel">
    <div class="enroll-head">
      <div class="panel-title">
        <div class="title-row">
          <h2>{{ $t('enroll_title') }}</h2>
          <span class="admin-pill">{{ $t('enroll_admin_only') }}</span>
        </div>
        <p>{{ $t('enroll_desc') }}</p>
      </div>
      <button class="btn" @click="closePanel">
        <i class="ph ph-arrow-left"></i> {{ $t('btn_back_to_devices') }}
      </button>
    </div>

    <ol class="stepper">
      <li v-for="entry in steps" :key="entry.no" class="step" :class="{ active: step === entry.no, done: step > entry.no }">
        <span class="step-no">{{ entry.no }}</span>
        <span class="step-copy">
          <strong>{{ $t(entry.title) }}</strong>
          <small>{{ $t(entry.sub) }}</small>
        </span>
      </li>
    </ol>

    <div class="workflow">
      <div class="workflow-card">
        <div v-if="notice && notice.key" class="security-callout" :class="notice.tone === 'info' ? 'info' : 'error'">
          <i class="ph ph-warning-circle"></i>
          <span>{{ $t(notice.key, notice.params) }}</span>
        </div>

        <EnrollmentFormStep
          v-if="step === 1"
          :groups="groups"
          :devices="devices"
          :form="form"
          :errors="formErrors"
          :canIssue="canIssue"
          :flowState="flowState"
          @update-group="form.groupId = $event"
          @update-ttl="form.ttlHours = $event"
          @update-mode="setDeviceMode"
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
          :listReflectState="listReflectState"
          :method="executionMethod"
          :vault="vault"
          :copyFeedback="copyFeedback"
          @copy="copy"
          @recheck="recheck"
          @reissue="reissue"
          @show-command="showCommand = true"
          @close="closePanel"
        />
      </div>

      <aside class="workflow-side">
        <div class="side-card">
          <h4>{{ $t('enroll_summary_title') }}</h4>
          <div class="summary">
            <div class="summary-row">
              <span>{{ $t('enroll_label_device_name') }}</span>
              <b>{{ form.deviceName || '—' }}</b>
            </div>
            <div class="summary-row">
              <span>{{ $t('enroll_label_device_mode') }}</span>
              <b>{{ form.deviceMode === 'existing' ? $t('enroll_device_mode_existing') : $t('enroll_device_mode_new') }}</b>
            </div>
            <div class="summary-row">
              <span>{{ $t('enroll_label_group') }}</span>
              <b>{{ selectedGroupName }}</b>
            </div>
            <div class="summary-row">
              <span>{{ $t('enroll_label_ttl') }}</span>
              <b>{{ ttlLabel }}</b>
            </div>
            <div class="summary-row">
              <span>{{ $t('enroll_label_method') }}</span>
              <b>{{ $t('enroll_method_' + executionMethod) }}</b>
            </div>
          </div>
          <div class="divider"></div>
          <div class="summary-row">
            <span>{{ $t('enroll_label_enrollment_id') }}</span>
            <b :title="String(enrollmentId || '')">{{ shortId }}</b>
          </div>
        </div>

        <div class="side-card">
          <h4>{{ $t('enroll_progress_title') }}</h4>
          <ol class="timeline">
            <li v-for="entry in timeline" :key="entry.key" class="time-row" :class="entry.state">
              <span class="time-dot"></span>
              <span>{{ $t(entry.label) }}</span>
            </li>
          </ol>
        </div>
      </aside>
    </div>
  </div>
</template>

<script setup>
import { computed, defineEmits, defineProps, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import EnrollmentDeliverStep from "@/dashboard/components/contents/devices/EnrollmentDeliverStep.vue";
import EnrollmentFormStep from "@/dashboard/components/contents/devices/EnrollmentFormStep.vue";
import EnrollmentWatchStep from "@/dashboard/components/contents/devices/EnrollmentWatchStep.vue";

const props = defineProps({
  flow: { type: Object, required: true },
  groups: { type: Array, default: () => [] },
  devices: { type: Array, default: () => [] },
});
const emit = defineEmits(["close"]);
// eslint-disable-next-line vue/no-setup-props-destructure
const {
  flowState, form, formErrors, vault, enrollmentId, revealed, remainingSeconds,
  listReflectState, notice, copyFeedback, executionMethod, expiryWarning, canIssue,
  issue, reveal, hide, copy, startWatch, recheck, reissue, closeFlow, setDeviceMode,
} = props.flow;
const { t } = useI18n();
const showCommand = ref(false);
watch(flowState, () => { if (flowState.value !== "waiting") showCommand.value = false; });

const steps = [
  { no: 1, title: "enroll_step1_title", sub: "enroll_step1_sub" },
  { no: 2, title: "enroll_step2_title", sub: "enroll_step2_sub" },
  { no: 3, title: "enroll_step3_title", sub: "enroll_step3_sub" },
];

const step = computed(() => {
  if (flowState.value === "form" || flowState.value === "issuing") return 1;
  if (flowState.value === "delivering" || (flowState.value === "waiting" && showCommand.value)) return 2;
  return 3;
});
const selectedGroupName = computed(() => {
  // Order matters: Number(null) is 0, so the not-yet-chosen case has to be
  // tested before the hidden-group-0 case or it renders as "Unknown".
  if (form.groupId == null) return "—";
  if (Number(form.groupId) === 0) return "Unknown";
  const group = props.groups.find((item) => Number(item.group_id) === Number(form.groupId));
  return group ? group.group_name : form.groupId;
});
const ttlLabel = computed(() => {
  const hours = Number(form.ttlHours);
  return hours > 24 && hours % 24 === 0
    ? t("enroll_ttl_days", { n: hours / 24 })
    : t("enroll_ttl_hours", { n: hours });
});
const shortId = computed(() => String(enrollmentId.value || "—").slice(0, 8));

// Deck timeline: admin check is always settled, the token row is current until
// it is issued, the agent row goes current while the flow is watching, and the
// list row only lights up once the new device actually appears.
const timeline = computed(() => {
  const issued = step.value > 1 || flowState.value === "waiting";
  return [
    { key: "admin", label: "enroll_progress_admin", state: "done" },
    { key: "token", label: "enroll_progress_issue", state: issued ? "done" : "current" },
    {
      key: "connect",
      label: "enroll_progress_connect",
      state: flowState.value === "succeeded" ? "done" : (flowState.value === "waiting" ? "current" : ""),
    },
    {
      key: "list",
      label: "enroll_progress_list",
      state: listReflectState.value === "done" ? "done" : "",
    },
  ];
});

function closePanel() {
  closeFlow();
  emit("close");
}
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). `.panel` / `.panel-title`
  / `.btn` / `.divider` come from components.css; `.enroll-head` / `.title-row`
  / `.admin-pill` / `.stepper` / `.step` / `.workflow` / `.workflow-card` /
  `.workflow-side` / `.side-card` / `.summary` / `.timeline` /
  `.security-callout` from devices.css.

  The deck keeps the summary and the progress timeline visible beside every
  stage, so both moved up here out of EnrollmentWatchStep (where they were only
  reachable on step 3). This is markup placement only: the flow composable, its
  state and every emit are untouched.
-->
