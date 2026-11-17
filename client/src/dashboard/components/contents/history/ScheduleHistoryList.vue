<template>
  <p v-if="errorMessage" role="alert">{{ errorMessage }}</p>
  <div class="panel">
    <div class="panel-head">
      <div class="panel-title">
        <h2>{{ $t('history_list_title') }}</h2>
        <p>{{ $t('history_list_desc') }}</p>
      </div>
      <div class="toolbar">
        <select class="select" v-model="selectedGroup" :aria-label="$t('list_label_group')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="group in uniqueGroups" :key="group" :value="group">{{ group }}</option>
        </select>
        <select class="select" v-model="selectedSchedule" :aria-label="$t('list_label_schedule_name')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="schedule in filteredSchedules" :key="schedule" :value="schedule">{{ schedule }}</option>
        </select>
        <input id="history-start" class="select" type="datetime-local" step="1" v-model="selectedStartTime" :disabled="isLoading" :aria-label="$t('list_label_start_time')">
        <input id="history-end" class="select" type="datetime-local" step="1" v-model="selectedEndTime" :disabled="isLoading" :aria-label="$t('list_label_end_time')">
        <select class="select" v-model="selectedResultCode" :aria-label="$t('list_label_exit_code')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="code in uniqueResultCodes" :key="code" :value="code">{{ code }}</option>
        </select>
        <button v-if="sessionMode" id="history-apply" class="btn primary" :disabled="isLoading" @click="fetchPosts">{{ $t('history_apply') }}</button>
        <button class="btn" :disabled="isLoading" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
      </div>
    </div>

    <div class="table-scroll">
      <table v-if="filteredPosts.length > 0">
        <thead>
          <tr>
            <SortableHeader field="execution_id" label="ID" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="sg_name" :label="$t('list_label_group')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="schedule_name" :label="$t('list_label_schedule_name')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="start_time" :label="$t('list_label_start_time')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="end_time" :label="$t('list_label_end_time')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="result_code" :label="$t('list_label_exit_code')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="result_message" :label="$t('list_label_message')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
          </tr>
        </thead>
        <tbody>
          <tr v-for="post in paginatedPosts" :key="post.execution_id" :data-execution="post.execution_id">
            <td>{{ post.execution_id }}</td>
            <td>{{ post.sg_name }}</td>
            <td>
              <button class="text-btn message-short" @click="showSchedule(post)">
                {{ post.schedule_name }}
              </button>
            </td>
            <td>{{ formatDate(post.start_time) }}</td>
            <td>{{ formatDate(post.end_time) }}</td>
            <td>{{ post.result_code }}</td>
            <td>
              <button class="text-btn message-short" @click="openModal($t('list_label_message'), post.result_message)">
                {{ truncateMessage(post.result_message) }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty-state">{{ $t(isLoading ? 'msg_loading' : 'schedules_list_no_datas') }}</div>
    </div>

    <div class="panel-foot" v-if="filteredPosts.length > 0">
      <span>{{ $t('list_footer_range', { total: filteredPosts.length, start: rangeStart, end: rangeEnd }) }}</span>
      <BoardPagination :total="filteredPosts.length" :perPage="perPage" @page-changed="changePage" />
    </div>

    <!-- ✅ Use modal component -->
    <ModalComponent
      :isOpen="isModalOpen"
      :title="modalTitle"
      :message="modalMessage"
      @close="isModalOpen = false"
    >
    </ModalComponent>
  </div>
</template>

<script>
import SortableHeader from "@/dashboard/components/misc/SortableHeader.vue";
export default {
    name: 'ScheduleHistoryList'
    , components: {
        SortableHeader,
    }
}
</script>

<script setup>
import { getRequest, sessionMode, useSort } from "@api";
import { ref, computed, onMounted, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import BoardPagination from '../../misc/BoardPagination.vue';
import ModalComponent from '../../misc/ModalComponent.vue';
import { parseServerTime } from '@/dashboard/utils/deviceStatus';
import { historyQuery, localTimeValue, manualError } from '@/dashboard/utils/manualPayload.mjs';

const { t, locale } = useI18n();
const posts = ref([]);
const { sortKey, sortOrder, sort } = useSort(posts);
const isLoading = ref(false);
const errorMessage = ref('');
const currentPage = ref(1);
const perPage = ref(7);
const selectedGroup = ref('');
const selectedSchedule = ref('');
const selectedStartTime = ref('');
const selectedEndTime = ref('');
const selectedResultCode = ref('');
const isModalOpen = ref(false);
const modalTitle = ref('');
const modalMessage = ref('');

const openModal = (title, message) => {
  modalTitle.value = title;
  modalMessage.value = message || '-';
  isModalOpen.value = true;
};
const truncateMessage = message => message && message.length > 40 ? message.slice(0, 40) + '…' : message || '-';
const showSchedule = post => {
  const lines = [`${t('task_type')}: ${post.task_type || '-'}`];
  if (post.task_type === 'archive' || post.task_type === 'copy') {
    lines.push(`${t('task_source_path')}: ${post.source_path || '-'}`, `${t('task_destination_path')}: ${post.destination_path || '-'}`);
  } else if (post.task_type === 'housekeep') {
    lines.push(`${t('task_destination_path')}: ${post.destination_path || '-'}`);
  } else if (post.task_type === 'command') {
    lines.push(`${t('task_command')}: ${post.command || '-'}`);
  }
  openModal(post.schedule_name || t('list_label_task'), lines.join('\n'));
};
const formatDate = value => {
  const date = parseServerTime(value);
  return date ? date.toLocaleString(locale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '-';
};

const setDefaultDates = () => {
  if (!sessionMode) return;
  const now = new Date();
  selectedEndTime.value = localTimeValue(now);
  selectedStartTime.value = localTimeValue(new Date(now.getTime() - 30 * 86400000));
};
setDefaultDates();

const fetchPosts = async () => {
  if (isLoading.value) return;
  isLoading.value = true; errorMessage.value = '';
  try {
    const params = sessionMode ? historyQuery(selectedStartTime.value, selectedEndTime.value) : {};
    posts.value = await getRequest('/dashboard/schedule/execution_history', params) || [];
    currentPage.value = 1;
  } catch (error) {
    posts.value = [];
    const code = error?.code || error?.response?.data?.detail?.code;
    errorMessage.value = t(code === 'invalid_datetime' ? 'manual_error_invalid_date_range' : manualError(error));
  } finally { isLoading.value = false; }
};
onMounted(fetchPosts);

const resetFilters = () => {
  selectedGroup.value = ''; selectedSchedule.value = ''; selectedResultCode.value = '';
  selectedStartTime.value = ''; selectedEndTime.value = '';
  currentPage.value = 1;
  if (sessionMode) { setDefaultDates(); fetchPosts(); }
};
const uniqueGroups = computed(() => [...new Set(posts.value.map(post => post.sg_name))]);
const filteredSchedules = computed(() => [...new Set(posts.value.filter(post => !selectedGroup.value || post.sg_name === selectedGroup.value).map(post => post.schedule_name))]);
const uniqueResultCodes = computed(() => [...new Set(posts.value.map(post => post.result_code))]);
const filteredPosts = computed(() => posts.value.filter(post => {
  const matchesGroup = !selectedGroup.value || post.sg_name === selectedGroup.value;
  const matchesSchedule = !selectedSchedule.value || post.schedule_name === selectedSchedule.value;
  const matchesResultCode = selectedResultCode.value === '' || String(post.result_code) === String(selectedResultCode.value);
  // Embedded date filtering belongs to the server; both bounds apply to start_time.
  // Standalone keeps its existing client-side filtering of the Python response.
  const matchesStart = sessionMode || !selectedStartTime.value || parseServerTime(post.start_time) >= new Date(selectedStartTime.value);
  const matchesEnd = sessionMode || !selectedEndTime.value || parseServerTime(post.end_time) <= new Date(selectedEndTime.value);
  return matchesGroup && matchesSchedule && matchesResultCode && matchesStart && matchesEnd;
}));
const paginatedPosts = computed(() => filteredPosts.value.slice((currentPage.value - 1) * perPage.value, currentPage.value * perPage.value));
const changePage = page => { currentPage.value = page; };
const rangeStart = computed(() => filteredPosts.value.length === 0 ? 0 : (currentPage.value - 1) * perPage.value + 1);
const rangeEnd = computed(() => Math.min(currentPage.value * perPage.value, filteredPosts.value.length));
watch([selectedGroup, selectedSchedule, selectedResultCode], () => { currentPage.value = 1; });
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7 / TR0017 DeviceList
  convention). `.panel` / `.panel-head` / `.toolbar` / `.select` / `.btn` /
  `.table-scroll` / `.panel-foot` / `.message-short` come from components.css.
  The pre-renewal `.board-container` / `.board-table` / `.filters` /
  `.reset-button` names are gone, so this screen no longer needs list.css.
-->
