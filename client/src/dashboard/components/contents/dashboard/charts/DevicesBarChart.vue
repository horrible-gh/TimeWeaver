<template>
    <div>
      <BarChart :chart-data="chartData" :chart-options="chartOptions" />
    </div>
</template>

<script>
import { getRequest } from "@api";
import { defineComponent, ref, onMounted  } from "vue";
import { BarChart } from "vue-chart-3"; // ✅ Use BarChart
import {
  Chart as ChartJS,
  Title,
  Tooltip,
  Legend,
  BarElement,
  CategoryScale,
  LinearScale,
  BarController, // ✅ Add BarController
} from "chart.js";

// ✅ Chart.js controller registration (Add BarController)
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
          label: "Devices",
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
    // Load data in onMounted
    onMounted(async () => {
      try {
        const response = await getRequest("/dashboard/charts/devices");
        activeCount.value = response.active_count;
        errorCount.value = response.error_count;
        inactiveCount.value = response.inactive_count;

        // Update chart data
        chartData.value.datasets[0].data = [
          activeCount.value,
          errorCount.value,
          inactiveCount.value
        ];
      } catch (error) {
        console.error("Failed to query data:", error);
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

