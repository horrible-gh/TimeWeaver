<template>
  <section class="step-card">
    <h3>{{ $t('enroll_form_heading') }}</h3>
    <p>{{ $t('enroll_form_desc') }}</p>
    <p class="notice">{{ $t('enroll_form_once_notice') }}</p>
    <div class="form-grid">
      <label>
        <span>{{ $t('enroll_label_group') }}</span>
        <select :value="form.groupId" :disabled="groups.length === 0" @change="$emit('update-group', Number($event.target.value))">
          <option v-if="groups.length === 0 && form.groupId === 0" :value="0">Unknown</option>
          <option v-for="group in groups" :key="group.group_id" :value="group.group_id">{{ group.group_name }}</option>
        </select>
        <small v-if="errors.groupId" class="error">{{ $t(errors.groupId) }}</small>
        <small v-else-if="groups.length === 0">{{ $t('msg_hidden_group_fallback') }}</small>
      </label>
      <label>
        <span>{{ $t('enroll_label_ttl') }}</span>
        <select :value="form.ttlHours" @change="$emit('update-ttl', Number($event.target.value))">
          <option v-for="hours in TTL_OPTIONS" :key="hours" :value="hours">{{ ttlLabel(hours) }}</option>
        </select>
        <small v-if="errors.ttlHours" class="error">{{ $t(errors.ttlHours) }}</small>
      </label>
      <label class="wide">
        <span>{{ $t('enroll_label_device_mode') }}</span>
        <div class="mode-toggle" role="radiogroup">
          <button type="button" :class="{ active: form.deviceMode !== 'existing' }" @click="$emit('update-device-mode', 'new')">{{ $t('enroll_device_mode_new') }}</button>
          <button type="button" :class="{ active: form.deviceMode === 'existing' }" :disabled="devices.length === 0" @click="$emit('update-device-mode', 'existing')">{{ $t('enroll_device_mode_existing') }}</button>
        </div>
      </label>
      <label class="wide">
        <span>{{ $t('enroll_label_device_name') }}</span>
        <select v-if="form.deviceMode === 'existing'" :value="form.deviceName" @change="$emit('update-device', $event.target.value)">
          <option v-for="device in devices" :key="device.device_id" :value="device.device_name">{{ device.device_name }}</option>
        </select>
        <input v-else :value="form.deviceName" type="text" :maxlength="DEVICE_NAME_MAX_LEN" @input="$emit('update-device', $event.target.value)" />
        <small v-if="form.deviceMode === 'existing'">{{ $t('enroll_hint_device_existing') }}</small>
        <small v-else>{{ $t('enroll_hint_device_name') }}</small>
        <small v-if="form.deviceMode === 'existing' && devices.length === 0" class="error">{{ $t('msg_no_existing_devices') }}</small>
        <small v-if="errors.deviceName" class="error">{{ $t(errors.deviceName, { n: DEVICE_NAME_MAX_LEN }) }}</small>
      </label>
    </div>
    <p class="secret-warning">{{ $t('enroll_warn_secret') }}</p>
    <button class="primary" :disabled="!canIssue" @click="$emit('issue')">
      {{ flowState === 'issuing' ? $t('msg_loading') : $t('btn_issue_token') }}
    </button>
  </section>
</template>

<script setup>
import { defineEmits, defineProps } from "vue";
import { useI18n } from "vue-i18n";
import { DEVICE_NAME_MAX_LEN, TTL_OPTIONS } from "@/dashboard/constants/enrollment";

defineProps({
  groups: { type: Array, default: () => [] },
  devices: { type: Array, default: () => [] },
  form: { type: Object, required: true },
  errors: { type: Object, default: () => ({}) },
  canIssue: Boolean,
  flowState: { type: String, required: true },
});
defineEmits(["update-group", "update-ttl", "update-device", "update-device-mode", "issue"]);
const { t } = useI18n();

function ttlLabel(hours) {
  const days = hours / 24;
  return hours > 24 && hours % 24 === 0
    ? t("enroll_ttl_days", { n: days })
    : t("enroll_ttl_hours", { n: hours });
}
</script>

<style scoped>
.step-card { padding: 24px; border: 1px solid #375575; border-radius: 12px; background: rgba(5, 25, 45, .72); }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 18px; margin: 20px 0; }
.form-grid label { display: grid; gap: 7px; }
.form-grid .wide { grid-column: 1 / -1; }
input, select { padding: 10px; border: 1px solid #54708b; border-radius: 6px; color: #fff; background: #071b30; }
.mode-toggle { display: flex; gap: 8px; }
.mode-toggle button { flex: 1; padding: 9px; border: 1px solid #54708b; border-radius: 6px; color: #adc0d2; background: #071b30; cursor: pointer; }
.mode-toggle button.active { color: #fff; background: #0b73b9; border-color: #3f91d4; }
.mode-toggle button:disabled { opacity: .4; cursor: not-allowed; }
small, p { color: #adc0d2; }
.notice, .secret-warning { border-left: 3px solid #f0b84d; padding: 10px 12px; background: rgba(240, 184, 77, .08); }
.error { color: #fda4af; }
.primary { padding: 10px 16px; border: 0; border-radius: 6px; color: #fff; background: #0b73b9; cursor: pointer; }
.primary:disabled { opacity: .45; cursor: not-allowed; }
@media (max-width: 1200px) { .form-grid { grid-template-columns: 1fr; } .form-grid .wide { grid-column: auto; } }
</style>
