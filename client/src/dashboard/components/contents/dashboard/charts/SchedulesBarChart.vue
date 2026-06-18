<template>
  <div>
    <BarChart :chart-data="chartData" :chart-options="chartOptions" />
  </div>
</template>

<script>
import { getRequest } from "@api";
import { defineComponent, ref, onMounted } from "vue";
import { BarChart } from "vue-chart-3"; // ✅ BarChart 사용
import {
  Chart as ChartJS,
  Title,
  Tooltip,
  Legend,
  BarElement,
  CategoryScale,
  LinearScale,
  BarController, // ✅ BarController 추가
} from "chart.js";

// ✅ Chart.js 컨트롤러 등록 (BarController 추가)
ChartJS.register(Title, Tooltip, Legend, BarElement, CategoryScale, LinearScale, BarController);
ChartJS.defaults.color = "#ffffff";

export default defineComponent({
components: { BarChart },
setup() {
  const activeCount = ref(0);
  const errorCount = ref(0);
  const inactiveCount = ref(0);

  const chartData = ref({
    labels: ["Active", "Error", "Inactive"],
    datasets: [
      {
        label: "Schedules",
        data: [activeCount.value, errorCount.value, inactiveCount.value],
        backgroundColor: [
          "rgba(54, 162, 235, 0.4)",
          "rgba(255, 99, 132, 0.4)",
          "rgba(255, 206, 86, 0.4)",
        ],
        borderColor: [
          "rgba(54, 162, 235, 1)",
          "rgba(255, 99, 132, 1)",
          "rgba(255, 206, 86, 1)",
        ],
        borderWidth: 1,
      },
    ],
  });

  // 데이터 불러오기 (onMounted에서 실행)
  onMounted(async () => {
    try {
      const response = await getRequest("/dashboard/charts/schedules");
      activeCount.value = response.active_count;
      errorCount.value = response.error_count;
      inactiveCount.value = response.inactive_count;

      // 차트 데이터 업데이트
      chartData.value.datasets[0].data = [
        activeCount.value,
        errorCount.value,
        inactiveCount.value
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
  width: 95%;
  height: 250px;
}
</style>

