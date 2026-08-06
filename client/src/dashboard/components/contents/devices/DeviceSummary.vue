<template>
  <section class="metric-grid" :aria-label="$t('devices_tab_status')">
    <article v-for="card in cards" :key="card.key" class="metric">
      <div class="metric-top">
        <span>{{ $t(card.label) }}</span>
        <span class="metric-icon" :style="card.tint ? { color: card.tint } : null">
          <i :class="card.icon"></i>
        </span>
      </div>
      <div class="metric-value">
        <strong>{{ loading ? '—' : card.value }}</strong>
        <small>{{ card.meta }}</small>
      </div>
    </article>
  </section>
</template>

<script setup>
import { computed, defineProps } from "vue";
import { useI18n } from "vue-i18n";
import { livenessReason, summarize } from "@/dashboard/utils/deviceStatus";

const props = defineProps({
  devices: { type: Array, default: () => [] },
  now: { type: [Date, Number], default: () => new Date() },
  loading: Boolean,
});
const { t } = useI18n();

const summary = computed(() => summarize(props.devices, props.now));
const attentionReason = computed(() => {
  const device = props.devices.find((item) => livenessReason(item, props.now) === "heartbeat_stale")
    || props.devices.find((item) => livenessReason(item, props.now) === "login_stale");
  return device ? livenessReason(device, props.now) : "heartbeat_stale";
});

// The deck puts a short qualifier under each figure (`대` / `75%` / `업데이트 3
// · 지연 1` / `24시간 이상 1`). Only the online ratio and the attention reason
// are computable here, so the other two carry the unit.
const cards = computed(() => [
  {
    key: "total",
    label: "devices_summary_total",
    icon: "ph ph-desktop",
    tint: null,
    value: summary.value.total,
    meta: t("metric_devices_unit"),
  },
  {
    key: "online",
    label: "devices_summary_online",
    icon: "ph ph-check-circle",
    tint: "var(--green)",
    value: summary.value.online,
    meta: summary.value.onlineRatio == null
      ? t("devices_summary_no_ratio")
      : t("devices_summary_ratio", { n: summary.value.onlineRatio }),
  },
  {
    key: "attention",
    label: "devices_summary_attention",
    icon: "ph ph-warning",
    tint: "var(--amber)",
    value: summary.value.attention,
    meta: summary.value.attention
      ? t(`device_reason_${attentionReason.value}`)
      : t("metric_devices_unit"),
  },
  {
    key: "offline",
    label: "devices_summary_offline",
    icon: "ph ph-wifi-slash",
    tint: null,
    value: summary.value.offline,
    meta: t("metric_devices_unit"),
  },
]);
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). `.metric-grid` /
  `.metric` / `.metric-top` / `.metric-icon` / `.metric-value` are the canonical
  rules in components.css section 9; the icon tint is a token reference, not a
  literal, which is the pattern T3 used on ScheduleList's metric row.
-->
