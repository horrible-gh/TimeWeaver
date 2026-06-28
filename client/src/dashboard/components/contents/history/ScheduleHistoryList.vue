<template>
  <div class="board-container">
    <h2>{{ $t('sub_history') }}</h2>

    <!-- ✅ Search filter -->
    <div class="filters">
      <div>
        <!-- Select group -->
        <label>{{ $t('list_label_group') }}:</label>
        <select v-model="selectedGroup">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="group in uniqueGroups" :key="group" :value="group">{{ group }}</option>
        </select>
      </div>

      <div>
        <!-- Select schedule name; list changes by selected group -->
        <label>{{ $t('list_label_schedule_name') }}:</label>
        <select v-model="selectedSchedule">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="schedule in filteredSchedules" :key="schedule" :value="schedule">{{ schedule }}</option>
        </select>
      </div>

      <div>
        <!-- Select start time -->
        <label>{{ $t('list_label_start_time') }}:</label>
        <input type="datetime-local" v-model="selectedStartTime">
      </div>

      <div>
        <!-- Select end time -->
        <label>{{ $t('list_label_end_time') }}:</label>
        <input type="datetime-local" v-model="selectedEndTime">
      </div>

      <div>
        <!-- Select exit code -->
        <label>{{ $t('list_label_exit_code') }}:</label>
        <select v-model="selectedResultCode">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="code in uniqueResultCodes" :key="code" :value="code">{{ code }}</option>
        </select>
      </div>

      <!-- ✅ Filter reset button -->
      <button class="reset-button" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
    </div>

    <!-- ✅ Filtered table -->
    <table v-if="filteredPosts.length > 0" class="board-table">
      <thead>
        <tr>
          <th class="title1" @click="sort('execution_id')">ID <span v-if="sortKey === 'execution_id'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title2" @click="sort('sg_name')">{{ $t('list_label_group') }} <span v-if="sortKey === 'sg_name'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title3" @click="sort('schedule_name')">{{ $t('list_label_schedule_name') }} <span v-if="sortKey === 'schedule_name'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title4" @click="sort('start_time')">{{ $t('list_label_start_time') }} <span v-if="sortKey === 'start_time'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title5" @click="sort('end_time')">{{ $t('list_label_end_time') }} <span v-if="sortKey === 'end_time'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title6" @click="sort('result_code')">{{ $t('list_label_exit_code') }} <span v-if="sortKey === 'result_code'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title7" @click="sort('result_message')">{{ $t('list_label_message') }} <span v-if="sortKey === 'result_message'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
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

    <BoardPagination :total="filteredPosts.length" :perPage="perPage" @page-changed="changePage" />
  </div>
</template>

<script setup>
import { getRequest } from "@api";
import { ref, computed, onMounted } from 'vue';
import BoardPagination from '../../misc/BoardPagination.vue';
import ModalComponent from "../../misc/ModalComponent.vue"; // ✅ Import modal component

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
  if (!dateString) return "-";
  const date = new Date(dateString);
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

</script>

<style scoped>

.title1 {
  width:5%
}

.title2 {
  width:17%
}

.title3 {
  width:17%
}

.title4 {
  width:19%
}

.title5 {
  width:19%
}

.title6 {
  width:10%
}

.title7 {
  width:23%
}


</style>
