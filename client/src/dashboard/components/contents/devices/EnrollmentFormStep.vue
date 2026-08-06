<template>
  <section>
    <h3>{{ $t('enroll_form_heading') }}</h3>
    <p class="stage-intro">{{ $t('enroll_form_desc') }} {{ $t('enroll_form_once_notice') }}</p>

    <div class="form-grid">
      <div class="field half">
        <label>{{ $t('enroll_label_group') }} <span>*</span></label>
        <select
          :value="form.groupId"
          :disabled="groups.length === 0"
          @change="$emit('update-group', Number($event.target.value))"
        >
          <option v-if="groups.length === 0 && form.groupId === 0" :value="0">Unknown</option>
          <option v-for="group in groups" :key="group.group_id" :value="group.group_id">{{ group.group_name }}</option>
        </select>
        <small v-if="errors.groupId" class="error">{{ $t(errors.groupId) }}</small>
        <small v-else-if="groups.length === 0">{{ $t('msg_hidden_group_fallback') }}</small>
      </div>

      <div class="field half">
        <label>{{ $t('enroll_label_ttl') }} <span>*</span></label>
        <select :value="form.ttlHours" @change="$emit('update-ttl', Number($event.target.value))">
          <option v-for="hours in TTL_OPTIONS" :key="hours" :value="hours">{{ ttlLabel(hours) }}</option>
        </select>
        <small v-if="errors.ttlHours" class="error">{{ $t(errors.ttlHours) }}</small>
      </div>

      <div class="field full">
        <label>{{ $t('enroll_label_device_mode') }} <span>*</span></label>
        <div class="mode-toggle" role="group" :aria-label="$t('enroll_label_device_mode')">
          <button
            type="button"
            :class="{ active: !isExisting }"
            :aria-pressed="isExisting ? 'false' : 'true'"
            @click="$emit('update-mode', 'new')"
          >
            <i class="ph ph-plus-circle"></i> {{ $t('enroll_device_mode_new') }}
          </button>
          <button
            type="button"
            :class="{ active: isExisting }"
            :aria-pressed="isExisting ? 'true' : 'false'"
            :disabled="existingDevices.length === 0"
            @click="$emit('update-mode', 'existing')"
          >
            <i class="ph ph-key"></i> {{ $t('enroll_device_mode_existing') }}
          </button>
        </div>
        <small v-if="existingDevices.length === 0">{{ $t('msg_no_existing_devices') }}</small>
      </div>

      <div class="field full">
        <label>{{ $t('enroll_label_device_name') }} <span>*</span></label>
        <select
          v-if="isExisting"
          :value="form.deviceName"
          :disabled="existingDevices.length === 0"
          @change="$emit('update-device', $event.target.value)"
        >
          <option value="">{{ $t('enroll_device_select_placeholder') }}</option>
          <option v-for="device in existingDevices" :key="device.device_id" :value="device.device_name">
            {{ device.device_name }}
          </option>
        </select>
        <input
          v-else
          :value="form.deviceName"
          type="text"
          :maxlength="DEVICE_NAME_MAX_LEN"
          @input="$emit('update-device', $event.target.value)"
        />
        <small>{{ isExisting ? $t('enroll_hint_device_existing') : $t('enroll_hint_device_name') }}</small>
        <small v-if="errors.deviceName" class="error">{{ $t(errors.deviceName, { n: DEVICE_NAME_MAX_LEN }) }}</small>
      </div>
    </div>

    <div class="security-callout">
      <i class="ph ph-warning"></i>
      <span>{{ $t('enroll_warn_secret') }}</span>
    </div>

    <div class="stage-actions">
      <button class="btn primary" :disabled="!canIssue" @click="$emit('issue')">
        <i class="ph ph-arrow-right"></i>
        {{ flowState === 'issuing' ? $t('msg_loading') : $t('btn_issue_token') }}
      </button>
    </div>
  </section>
</template>

<script setup>
import { computed, defineEmits, defineProps } from "vue";
import { useI18n } from "vue-i18n";
import { DEVICE_NAME_MAX_LEN, TTL_OPTIONS } from "@/dashboard/constants/enrollment";

const props = defineProps({
  groups: { type: Array, default: () => [] },
  devices: { type: Array, default: () => [] },
  form: { type: Object, required: true },
  errors: { type: Object, default: () => ({}) },
  canIssue: Boolean,
  flowState: { type: String, required: true },
});
defineEmits(["update-group", "update-ttl", "update-mode", "update-device", "issue"]);
const { t } = useI18n();

const isExisting = computed(() => props.form.deviceMode === "existing");

// Reissue targets are picked, never typed: enroll reuses the existing device_id
// only on an exact device_name match, so a free-text field is what orphans a
// device's schedules (NR0012-0003 section 3.3). Rows without a group_id are kept
// because the device endpoint only ever returns the caller's own group anyway.
const existingDevices = computed(() => {
  const groupId = props.form.groupId;
  const rows = props.devices.filter((device) => (
    device
    && device.device_name
    && (groupId == null || device.group_id == null || Number(device.group_id) === Number(groupId))
  ));
  return rows.slice().sort((a, b) => String(a.device_name).localeCompare(String(b.device_name)));
});

function ttlLabel(hours) {
  const days = hours / 24;
  return hours > 24 && hours % 24 === 0
    ? t("enroll_ttl_days", { n: days })
    : t("enroll_ttl_hours", { n: hours });
}
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). The deck's two-column
  enrollment form is the canonical four-column `.form-grid` with two `.half`
  fields and two `.full` fields, so no new grid is declared here.
  `.stage-intro` / `.stage-actions` / `.security-callout` / `.mode-toggle` come
  from devices.css.
-->
