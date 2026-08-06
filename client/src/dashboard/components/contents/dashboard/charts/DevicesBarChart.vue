<template>
    <div>
      <!-- vue-chart-3 quirks, both of which silently broke these charts:
             - the options prop is `options`; `chart-options` is ignored.
             - it wraps the <canvas> in a div of its own with no height, and
               that div is what chart.js measures. Without `styles` the chart
               measures itself, settles on a square, and overflows the tile. -->
      <BarChart :chart-data="chartData" :options="chartOptions" :styles="chartStyles" />
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
import { applyChartDefaults, chartPalette } from "@/dashboard/utils/chartTheme";

// ✅ Chart.js controller registration (Add BarController)
ChartJS.register(Title, Tooltip, Legend, BarElement, CategoryScale, LinearScale, BarController);
applyChartDefaults(ChartJS);


export default defineComponent({
  components: { BarChart },
  setup() {
    const activeCount = ref(0);
    const errorCount = ref(0);
    const inactiveCount = ref(0);
    const palette = chartPalette();

    const chartData = ref({
      labels: ["Active", "Error", "Inactive"],
      datasets: [
        {
          label: "Devices",
          data: [activeCount.value, errorCount.value, inactiveCount.value],
          backgroundColor: [palette.active, palette.error, palette.inactive],
          borderWidth: 0,
          borderRadius: 4,
          maxBarThickness: 46,
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
      // Gives vue-chart-3's own wrapper a definite box to measure.
      chartStyles: { width: "100%", height: "100%" },
      chartOptions: {
        responsive: true,
        maintainAspectRatio: false,
        // The tile title and .tile-foot already name the series; a one-dataset
        // legend would only repeat them inside the 96px chart well.
        plugins: { legend: { display: false } },
        scales: {
          x: { grid: { display: false } },
          y: { beginAtZero: true, ticks: { precision: 0 } },
        },
      },
    };
  },
});
</script>
