<template>
  <div role="img" :aria-label="description">
    <DoughnutChart :chart-data="chartData" :options="chartOptions" :styles="chartStyles" />
  </div>
</template>

<script setup>
import { computed, defineProps } from 'vue';
import { useI18n } from 'vue-i18n';
import { DoughnutChart } from 'vue-chart-3';
import { Chart, registerables } from 'chart.js';
import { applyChartDefaults, chartPalette } from '@/dashboard/utils/chartTheme';

Chart.register(...registerables);
applyChartDefaults(Chart);
const props = defineProps({ counts: { type: Object, required: true } });
const { t } = useI18n();
const palette = chartPalette();
const labels = computed(() => [1, 2, 3, 4].map(n => t(`chart_tasks_content_title${n}`)));
const values = computed(() => [props.counts.in_progress, props.counts.pending, props.counts.completed, props.counts.error]);
const description = computed(() => labels.value.map((label, index) => `${label}: ${values.value[index]}`).join(', '));
const chartData = computed(() => ({
  labels: labels.value,
  datasets: [{ data: values.value, backgroundColor: [palette.running, palette.wait, palette.completed, palette.error], borderColor: palette.surface, borderWidth: 2 }],
}));
const chartStyles = { width: '100%', height: '100%' };
const chartOptions = {
  responsive: true, maintainAspectRatio: false, cutout: '62%',
  plugins: { legend: { position: 'right', labels: { boxWidth: 10, boxHeight: 10, padding: 10, font: { size: 10 } } } },
};
</script>
