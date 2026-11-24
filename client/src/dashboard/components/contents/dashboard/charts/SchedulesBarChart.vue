<template>
  <div role="img" :aria-label="description">
    <BarChart :chart-data="chartData" :options="chartOptions" :styles="chartStyles" />
  </div>
</template>

<script setup>
import { computed, defineProps } from 'vue';
import { useI18n } from 'vue-i18n';
import { BarChart } from 'vue-chart-3';
import { Chart, Title, Tooltip, Legend, BarElement, CategoryScale, LinearScale, BarController } from 'chart.js';
import { applyChartDefaults, chartPalette } from '@/dashboard/utils/chartTheme';

Chart.register(Title, Tooltip, Legend, BarElement, CategoryScale, LinearScale, BarController);
applyChartDefaults(Chart);
const props = defineProps({ counts: { type: Object, required: true } });
const { t } = useI18n();
const palette = chartPalette();
const hasWarnings = computed(() => (props.counts.warning || 0) > 0);
const labels = computed(() => [t('chart_schedules_content1'), ...(hasWarnings.value ? [t('exec_status_warning')] : []), t('chart_schedules_content2'), t('chart_schedules_content3')]);
const values = computed(() => [props.counts.active, ...(hasWarnings.value ? [props.counts.warning] : []), props.counts.error, props.counts.inactive]);
const description = computed(() => labels.value.map((label, index) => `${label}: ${values.value[index]}`).join(', '));
const chartData = computed(() => ({
  labels: labels.value,
  datasets: [{ label: t('chart_schedules_content_title'), data: values.value, backgroundColor: [palette.active, ...(hasWarnings.value ? [palette.wait] : []), palette.error, palette.inactive], borderWidth: 0, borderRadius: 4, maxBarThickness: 46 }],
}));
// vue-chart-3's wrapper needs a fixed box to respect the existing tile height.
const chartStyles = { width: '100%', height: '100%' };
const chartOptions = {
  responsive: true, maintainAspectRatio: false,
  plugins: { legend: { display: false } },
  scales: { x: { grid: { display: false } }, y: { beginAtZero: true, ticks: { precision: 0 } } },
};
</script>
