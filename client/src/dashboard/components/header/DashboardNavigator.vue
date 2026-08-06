<template>
  <div class="crumb">
    {{ $t('shell_crumb_root') }}&nbsp; / &nbsp;<b>{{ $t(currentLabel) }}</b>
  </div>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute } from 'vue-router';

const route = useRoute();

// ✅ Route path -> existing navigation i18n key. Kept in one place so the
//    breadcrumb and the sidebar always name a screen the same way.
const LABELS = {
  '/': 'sub_dashboard',
  '/groups': 'sub_groups',
  '/devices': 'sub_devices',
  '/schedules': 'sub_schedules',
  '/tasks': 'sub_tasks',
  '/schedule-history': 'sub_history',
  '/manual-execution': 'sub_manual_execution',
};

const currentLabel = computed(() => LABELS[route.path] || 'sub_dashboard');
</script>
