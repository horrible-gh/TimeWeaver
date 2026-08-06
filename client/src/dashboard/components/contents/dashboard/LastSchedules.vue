<template>
  <div class="panel">
    <div class="panel-head">
      <div class="panel-title">
        <h2>Latest schedules</h2>
        <p>{{ $t('last_schedules_desc') }}</p>
      </div>
      <div class="actions">
        <button class="btn" @click="$emit('refresh')">
          <i class="ph ph-arrow-clockwise"></i> {{ $t('btn_refresh') }}
        </button>
      </div>
    </div>

    <div v-if="schedules.length === 0" class="empty-state">
      {{ $t('schedules_list_no_datas') }}
    </div>
    <div class="table-scroll" v-else>
      <table>
        <thead>
          <tr>
            <th>{{ $t('list_label_device') }}</th>
            <th>{{ $t('list_label_schedule') }}</th>
            <th>{{ $t('list_label_status') }}</th>
            <th>{{ $t('list_label_start_time') }}</th>
            <th>{{ $t('list_label_end_time') }}</th>
            <th>{{ $t('list_label_error_count') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="schedule in schedules" :key="schedule.schedule_id">
            <td>
              <div class="device-name">
                <span class="device-glyph"><i class="ph ph-hard-drives"></i></span>
                <span>{{ schedule.device_name }}</span>
              </div>
            </td>
            <td>{{ schedule.sg_name }} · {{ schedule.task_count }} Tasks</td>
            <td>
              <span class="badge" :class="statusBadgeClass(schedule.custom_status)">
                {{ statusLabel(schedule.custom_status) }}
              </span>
            </td>
            <td>{{ formatDateTime(schedule.group_start_time) }}</td>
            <td>{{ formatDateTime(schedule.group_end_time) }}</td>
            <td>{{ schedule.error_summary }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="panel-foot">
      <span>{{ $t('last_schedules_updated', { t: lastUpdated }) }}</span>
    </div>
  </div>
</template>

<script>
import { defineComponent, computed } from "vue";
import { useI18n } from "vue-i18n";
import { parseServerTime } from "@/dashboard/utils/deviceStatus";

export default defineComponent({
  props: {
    schedules: {
      type: Array,
      default: () => [],
    },
  },
  emits: ["refresh"],
  setup() {
    const { t } = useI18n();

    const formatDateTime = (dateString) => {
      const date = parseServerTime(dateString);
      if (!date) return "-";
      const parts = new Intl.DateTimeFormat("en-CA", {
        timeZone: "Asia/Tokyo",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      }).formatToParts(date);
      const get = (type) => parts.find((p) => p.type === type)?.value;
      return `${get("year")}-${get("month")}-${get("day")} ${get("hour")}:${get("minute")}:${get("second")}`;
    };

    const statusBadgeClass = (status) => ({
      online: status === "completed",
      danger: status === "error",
      warning: status === "warning",
    });

    const statusLabels = {
      completed: "exec_status_completed",
      error: "exec_status_error",
      warning: "exec_status_warning",
    };
    const statusLabel = (status) => {
      const key = statusLabels[status];
      return key ? t(key) : status;
    };

    const lastUpdated = computed(() => new Date().toLocaleString());

    return { formatDateTime, statusBadgeClass, statusLabel, lastUpdated };
  },
});
</script>
