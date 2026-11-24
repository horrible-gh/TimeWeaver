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
const hasWarnings = computed(() => (props.counts.warning || 0) > 0);
const labels = computed(() => [...[1, 2, 3].map(n => t(`chart_tasks_content_title${n}`)), ...(hasWarnings.value ? [t('exec_status_warning')] : []), t('chart_tasks_content_title4')]);
const values = computed(() => [props.counts.in_progress, props.counts.pending, props.counts.completed, ...(hasWarnings.value ? [props.counts.warning] : []), props.counts.error]);
const description = computed(() => labels.value.map((label, index) => `${label}: ${values.value[index]}`).join(', '));
const chartData = computed(() => ({
  labels: labels.value,
  datasets: [{ data: values.value, backgroundColor: [palette.running, hasWarnings.value ? palette.inactive : palette.wait, palette.completed, ...(hasWarnings.value ? [palette.wait] : []), palette.error], borderColor: palette.surface, borderWidth: 2 }],
}));
const chartStyles = { width: '100%', height: '100%' };
const chartOptions = {
  responsive: true, maintainAspectRatio: false, cutout: '62%',
  plugins: { legend: { position: 'right', labels: { boxWidth: 10, boxHeight: 10, padding: 10, font: { size: 10 } } } },
};
</script>
