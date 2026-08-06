<template>
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
        <input class="select" type="datetime-local" v-model="selectedStartTime" :aria-label="$t('list_label_start_time')">
        <input class="select" type="datetime-local" v-model="selectedEndTime" :aria-label="$t('list_label_end_time')">
        <select class="select" v-model="selectedResultCode" :aria-label="$t('list_label_exit_code')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="code in uniqueResultCodes" :key="code" :value="code">{{ code }}</option>
        </select>
        <button class="btn" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
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
          <tr v-for="post in paginatedPosts" :key="post.execution_id">
            <td>{{ post.execution_id }}</td>
            <td>{{ post.sg_name }}</td>
            <td>
              <span @click="showSchedule(post.task_type,post.command,post.source_path,post.destination_path)" class="message-short">
                {{ post.schedule_name }}
              </span>
            </td>
            <td>{{ formatDate(post.start_time) }}</td>
            <td>{{ formatDate(post.end_time) }}</td>
            <td>{{ post.result_code }}</td>
            <td>
              <span @click="openModal($t('list_label_message'), post.result_message)" class="message-short">
                {{ truncateMessage(post.result_message) }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty-state">{{ $t('schedules_list_no_datas') }}</div>
    </div>

    <div class="panel-foot" v-if="filteredPosts.length > 0">
      <span>{{ $t('list_footer_range', { total: filteredPosts.length, start: rangeStart, end: rangeEnd }) }}</span>
      <BoardPagination :total="filteredPosts.length" :perPage="perPage" @page-changed="changePage" />
    </div>

    <!-- ✅ Use modal component -->
    <ModalComponent
      :isOpen="isModalOpen"
      :title="$t('sub_groups')"
      :message="modalMessage"
      :confirmText="modalConfirmText"
      @close="isModalOpen = false"
      @confirm="handleConfirm"
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
import { getRequest } from "@api";
import { ref, computed, onMounted } from 'vue';
import BoardPagination from '../../misc/BoardPagination.vue';
import ModalComponent from "../../misc/ModalComponent.vue"; // ✅ Import modal component
import { parseServerTime } from "@/dashboard/utils/deviceStatus";

const posts = ref([]); // ✅ Initial value is an empty array
const isLoading = ref(true);
const currentPage = ref(1);
const perPage = ref(7);
const sortKey = ref('');
const sortOrder = ref('asc');

// ✅ Filter state
const selectedGroup = ref("");
const selectedSchedule = ref("");
const selectedStartTime = ref("");
const selectedEndTime = ref("");
const selectedResultCode = ref("");

const isModalOpen = ref(false);
const modalTitle = ref("");
const modalMessage = ref("");
const modalConfirmText = ref("");

const openModal = (title, message, confirmText = "") => {
  modalTitle.value = title;
  modalMessage.value = message;
  modalConfirmText.value = confirmText;
  isModalOpen.value = true;
};

const handleConfirm = () => {
  alert("Confirm button was clicked.");
  isModalOpen.value = false;
};


// ✅ Exit message truncation function, show up to 11 characters plus '...'
const truncateMessage = (message) => {
  return message && message.length > 11 ? message.substring(0, 11) + "..." : message;
};

// // ✅ View full exit message by opening modal
// const showMessage = (message) => {
//   modalMessage.value = message;
//   isModalOpen.value = true;
// };

// ✅ View schedule by opening modal
const showSchedule = (task_type, command, source_path, destination_path) => {
  modalMessage.value = "Task type : " + task_type + "\n"
  if (task_type === "archive" || task_type === "copy") {
    modalMessage.value += "Source : " + source_path + "\nDestination : " + destination_path;
  } else if (task_type === "housekeep") {
    modalMessage.value += "Destination : " + destination_path;
  } else if (task_type === "command") {
    modalMessage.value += "Command : " + command;
  } else {
    modalMessage.value += "Unknown task type"
  }
  isModalOpen.value = true;
};


// // ✅ Close modal
// const closeModal = () => {
//   isModalOpen.value = false;
// };

// ✅ Date conversion function
const formatDate = (dateString) => {
  const date = parseServerTime(dateString);
  if (!date) return "-";
  return date.toLocaleString("ja-JP", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
};

// ✅ Fetch data from API
const fetchPosts = async () => {
  try {
    const response = await getRequest("/dashboard/schedule/execution_history"); // 🔹 API address changed
    console.log(response)
    posts.value = response || []; // ✅ Use an empty array when undefined
  } catch (error) {
    console.error("Error while fetching data:", error);
  } finally {
    isLoading.value = false;
  }
};

// ✅ Call API when component is mounted
onMounted(fetchPosts);

const resetFilters = () => {
  selectedGroup.value = "";
  selectedSchedule.value = "";
  selectedStartTime.value = "";
  selectedEndTime.value = "";
  selectedResultCode.value = "";
};


// Sorting
const sort = (key) => {
  if (sortKey.value === key) {
    sortOrder.value = sortOrder.value === 'asc' ? 'desc' : 'asc';
  } else {
    sortKey.value = key;
    sortOrder.value = 'asc';
  }
  posts.value.sort((a, b) => {
    const modifier = sortOrder.value === 'asc' ? 1 : -1;
    if (a[key] < b[key]) return -1 * modifier;
    if (a[key] > b[key]) return 1 * modifier;
    return 0;
  });
};


// ✅ Return unique group list
const uniqueGroups = computed(() => {
  return [...new Set(posts.value.map(post => post.sg_name))];
});

// ✅ Return unique schedule names by selected group
const filteredSchedules = computed(() => {
  if (!selectedGroup.value) {
    return [...new Set(posts.value.map(post => post.schedule_name))];
  }
  return [...new Set(posts.value.filter(post => post.sg_name === selectedGroup.value).map(post => post.schedule_name))];
});

// ✅ Return unique exit code list
const uniqueResultCodes = computed(() => {
  return [...new Set(posts.value.map(post => post.result_code))];
});


// ✅ Return filtered data
const filteredPosts = computed(() => {
  return posts.value.filter(post => {
    const matchesGroup = selectedGroup.value ? post.sg_name === selectedGroup.value : true;
    const matchesSchedule = selectedSchedule.value ? post.schedule_name === selectedSchedule.value : true;
    const matchesStartTime = selectedStartTime.value ? new Date(post.start_time) >= new Date(selectedStartTime.value) : true;
    const matchesEndTime = selectedEndTime.value ? new Date(post.end_time) <= new Date(selectedEndTime.value) : true;
    const matchesResultCode = selectedResultCode.value ? post.result_code === selectedResultCode.value : true;

    return matchesGroup && matchesSchedule && matchesStartTime && matchesEndTime && matchesResultCode;
  });
});

// ✅ Pagination
const paginatedPosts = computed(() => {
  const start = (currentPage.value - 1) * perPage.value;
  return filteredPosts.value.slice(start, start + perPage.value);
});

const changePage = (page) => {
  currentPage.value = page;
};

// ✅ Panel foot range text
const rangeStart = computed(() => (filteredPosts.value.length === 0 ? 0 : (currentPage.value - 1) * perPage.value + 1));
const rangeEnd = computed(() => Math.min(currentPage.value * perPage.value, filteredPosts.value.length));

</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7 / TR0017 DeviceList
  convention). `.panel` / `.panel-head` / `.toolbar` / `.select` / `.btn` /
  `.table-scroll` / `.panel-foot` / `.message-short` come from components.css.
  The pre-renewal `.board-container` / `.board-table` / `.filters` /
  `.reset-button` names are gone, so this screen no longer needs list.css.
-->
