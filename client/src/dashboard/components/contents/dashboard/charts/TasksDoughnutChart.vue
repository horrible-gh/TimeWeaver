<template>
    <div>
      <DoughnutChart :chart-data="chartData" :chart-options="chartOptions" />
    </div>
  </template>

  <script>
  import { defineComponent, ref, onMounted } from "vue";
  import { DoughnutChart } from 'vue-chart-3';
  import { Chart, registerables } from 'chart.js';
  import { getRequest } from "@api";

  Chart.register(...registerables);
  Chart.defaults.color = "#ffffff";

export default defineComponent({
  components: { DoughnutChart },
  setup() {
    const inProgressCount = ref(0);
    const pendingCount = ref(0);
    const completedCount = ref(0);
    const errorCount = ref(0);

    const chartData = ref({
      labels: ['Running', 'Wait', 'Complate', 'Error'],
      datasets: [
        {
          data: [inProgressCount.value, pendingCount.value, completedCount.value, errorCount.value],
          backgroundColor: [
            "rgba(67, 220, 45, 0.5)",
            "rgba(255, 206, 86, 0.5)",
            "rgba(54, 162, 235, 0.5)",
            "rgba(255, 84, 127, 0.5)",
          ],
        },
      ],
    });

    // 데이터 불러오기 (onMounted에서 실행)
    onMounted(async () => {
      try {
        const response = await getRequest("/dashboard/charts/tasks");
        inProgressCount.value = response.in_progress_count;
        pendingCount.value = response.pending_count;
        completedCount.value = response.completed_count;
        errorCount.value = response.error_count;

        // 차트 데이터 업데이트
        chartData.value.datasets[0].data = [
        inProgressCount.value,
        pendingCount.value,
          completedCount.value,
          errorCount.value,
        ];
      } catch (error) {
        console.error("데이터 조회 실패:", error);
      }
    });

    return {
      chartData,
      chartOptions: {
        responsive: true,
        maintainAspectRatio: false,
      },
    };
  },
});
</script>

<style scoped>
div {
  width: 100%;
  height: 250px;
}
</style>
