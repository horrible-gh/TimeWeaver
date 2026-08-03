<template>
  <section class="summary-grid" :aria-label="$t('devices_tab_status')">
    <article v-for="card in cards" :key="card.key" class="summary-card" :class="'summary-' + card.key">
      <span class="summary-label">{{ $t(card.label) }}</span>
      <strong v-if="loading">—</strong>
      <strong v-else>{{ $t('devices_summary_unit', { n: card.value }) }}</strong>
      <span v-if="card.key === 'online'" class="summary-detail">
        {{ summary.onlineRatio == null ? $t('devices_summary_no_ratio') : $t('devices_summary_ratio', { n: summary.onlineRatio }) }}
      </span>
      <span v-else-if="card.key === 'attention' && summary.attention" class="summary-detail">
        {{ $t('device_reason_' + attentionReason) }}
      </span>
    </article>
  </section>
</template>

<script setup>
import { computed, defineProps } from "vue";
import { livenessReason, summarize } from "@/dashboard/utils/deviceStatus";

const props = defineProps({
  devices: { type: Array, default: () => [] },
  now: { type: [Date, Number], default: () => new Date() },
  loading: Boolean,
});

const summary = computed(() => summarize(props.devices, props.now));
const attentionReason = computed(() => {
  const device = props.devices.find((item) => livenessReason(item, props.now) === "heartbeat_stale")
    || props.devices.find((item) => livenessReason(item, props.now) === "login_stale");
  return device ? livenessReason(device, props.now) : "heartbeat_stale";
});
const cards = computed(() => [
  { key: "total", label: "devices_summary_total", value: summary.value.total },
  { key: "online", label: "devices_summary_online", value: summary.value.online },
  { key: "attention", label: "devices_summary_attention", value: summary.value.attention },
  { key: "offline", label: "devices_summary_offline", value: summary.value.offline },
]);
</script>

<style scoped>
.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
  margin: 20px 0;
}
.summary-card {
  min-height: 112px;
  padding: 18px;
  border: 1px solid rgba(112, 148, 191, .35);
  border-radius: 14px;
  background: linear-gradient(145deg, rgba(14, 48, 82, .92), rgba(6, 24, 46, .94));
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.summary-label, .summary-detail { color: #a9bdd2; }
.summary-card strong { color: #fff; font-size: 28px; }
.summary-online { border-color: rgba(49, 196, 141, .55); }
.summary-attention { border-color: rgba(248, 183, 74, .55); }
.summary-offline { border-color: rgba(236, 100, 113, .45); }
@media (max-width: 1200px) {
  .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 767px) {
  .summary-grid { grid-template-columns: 1fr; }
}
</style>
