<template>
    <div>
      <!-- vue-chart-3 quirks, both of which silently broke these charts:
             - the options prop is `options`; `chart-options` is ignored.
             - it wraps the <canvas> in a div of its own with no height, and
               that div is what chart.js measures. Without `styles` the chart
               measures itself, settles on a square, and overflows the tile. -->
      <DoughnutChart :chart-data="chartData" :options="chartOptions" :styles="chartStyles" />
    </div>
  </template>

  <script>
  import { defineComponent, ref, onMounted } from "vue";
  import { DoughnutChart } from 'vue-chart-3';
  import { Chart, registerables } from 'chart.js';
  import { getRequest } from "@api";
  import { applyChartDefaults, chartPalette } from "@/dashboard/utils/chartTheme";

  Chart.register(...registerables);
  applyChartDefaults(Chart);

export default defineComponent({
  components: { DoughnutChart },
  setup() {
    const inProgressCount = ref(0);
    const pendingCount = ref(0);
    const completedCount = ref(0);
    const errorCount = ref(0);
    const palette = chartPalette();

    const chartData = ref({
      labels: ['Running', 'Wait', 'Complate', 'Error'],
      datasets: [
        {
          data: [inProgressCount.value, pendingCount.value, completedCount.value, errorCount.value],
          backgroundColor: [palette.running, palette.wait, palette.completed, palette.error],
          borderColor: palette.surface,
          borderWidth: 2,
        },
      ],
    });

    // Load data in onMounted
    onMounted(async () => {
      try {
        const response = await getRequest("/dashboard/charts/tasks");
        inProgressCount.value = response.in_progress_count;
        pendingCount.value = response.pending_count;
        completedCount.value = response.completed_count;
        errorCount.value = response.error_count;

        // Update chart data
        chartData.value.datasets[0].data = [
        inProgressCount.value,
        pendingCount.value,
          completedCount.value,
          errorCount.value,
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
        cutout: "62%",
        // Four states need naming, so this legend stays -- but on the side, so
        // the ring keeps the height of the 96px chart well.
        plugins: {
          legend: {
            position: "right",
            labels: { boxWidth: 10, boxHeight: 10, padding: 10, font: { size: 10 } },
          },
        },
      },
    };
  },
});
</script>
