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
            <dt><i class="ph ph-play"></i> {{ schedule.group_start_time }}</dt>
            <dd>
              <i class="ph" :class="{'ph-pause': schedule.custom_status === 'completed', 'ph-prohibit': schedule.custom_status === 'error' , 'ph-play-pause': schedule.custom_status === 'warning'}"></i>
              {{ schedule.group_end_time }}
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

export default defineComponent({
  setup() {
    const schedules = ref([]);

    onMounted(async () => {
      try {
        const response = await getRequest("/dashboard/lastest-schedules");
        console.log("API 응답:", response);
        // 만약 response 자체가 배열이면:
        schedules.value = Array.isArray(response) ? response : [];
        console.log("할당된 schedules:", schedules.value);
      } catch (error) {
        // if (error.response && error.response.status === 401) {
        //   alert("오류가 발생 했습니다. 잠시 후 다시 이용하십시요.");
        //   window.location.href = "/login";
        // } else {
        //   alert(`오류가 발생 했습니다. 잠시 후 다시 이용하십시요.`); // ✅ 다른 오류는 알림 표시
        // }
        console.error("오류 발생." + error)
      }
    });


    return { schedules };
  },
});
</script>
