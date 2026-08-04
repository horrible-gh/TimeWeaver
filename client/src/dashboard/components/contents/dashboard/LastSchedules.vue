<template>
  <section class="transfer-section">
    <div class="transfer-section-header">
      <h2>Latest schedules</h2>
      <div class="filter-options">
        <p>Top 3 within 24 hours</p>
      </div>
    </div>
    <div v-if="schedules.length === 0">
      {{ $t('schedules_list_no_datas') }}
    </div>
    <div class="transfers" v-else>
      <div class="transfer" v-for="schedule in schedules" :key="schedule.schedule_id">
        <div class="transfer-logo">
          <span :class="{
            'transfer-logo-success': schedule.custom_status === 'completed',
            'transfer-logo-error': schedule.custom_status === 'error',
            'transfer-logo-warn': schedule.custom_status === 'warning'
          }">
            <i class="ph" :class="{
              'ph-check-fat': schedule.custom_status === 'completed',
              'ph-bug': schedule.custom_status === 'error',
              'ph-warning': schedule.custom_status === 'warning'
            }"></i>
          </span>
        </div>
        <dl class="transfer-details">
          <div>
            <dt><i class="ph ph-hard-drives"></i> {{ schedule.device_name }}</dt>
            <dd>{{ schedule.custom_status }}</dd>
          </div>
          <div>
            <dt><i class="ph ph-calendar"></i> {{ schedule.sg_name }}</dt>
            <dd>{{ schedule.task_count }} Tasks</dd>
          </div>
          <div>
            <dt><i class="ph ph-play"></i> {{ formatDateTime(schedule.group_start_time) }}</dt>
            <dd>
              <i class="ph" :class="{'ph-pause': schedule.custom_status === 'completed', 'ph-prohibit': schedule.custom_status === 'error' , 'ph-play-pause': schedule.custom_status === 'warning'}"></i>
              {{ formatDateTime(schedule.group_end_time) }}
            </dd>
          </div>
        </dl>
        <div class="transfer-number">
          {{ schedule.error_summary }}
        </div>
      </div>
    </div>
  </section>
</template>

<script>
import { getRequest } from "@api";
import { defineComponent, onMounted, ref } from "vue";
import { parseServerTime } from "@/dashboard/utils/deviceStatus";

export default defineComponent({
  setup() {
    const schedules = ref([]);

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

    onMounted(async () => {
      try {
        const response = await getRequest("/dashboard/lastest-schedules");
        console.log("API response:", response);
        // If response itself is an array:
        schedules.value = Array.isArray(response) ? response : [];
        console.log("Assigned schedules:", schedules.value);
      } catch (error) {
        // if (error.response && error.response.status === 401) {
        //   alert("An error occurred. Please try again later.");
        //   window.location.href = "/login";
        // } else {
        //   alert(`An error occurred. Please try again later.`); // ✅ Show an alert for other errors
        // }
        console.error("Error occurred." + error)
      }
    });


    return { schedules, formatDateTime };
  },
});
</script>
