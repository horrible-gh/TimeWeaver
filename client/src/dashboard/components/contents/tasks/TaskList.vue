<template>
  <p v-if="errorMessage" role="alert">{{ errorMessage }}</p>
  <p v-if="noticeMessage" role="status">{{ noticeMessage }}</p>
  <div class="panel">
    <div class="panel-head">
      <div class="panel-title">
        <h2>{{ $t('tasks_list_title') }}</h2>
        <p>{{ $t('tasks_list_desc') }}</p>
      </div>
      <div class="toolbar">
        <select id="task-schedule-filter" class="select" v-model="selectedSchedule" :aria-label="$t('list_label_schedule')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="schedule in scheduleFilters" :key="schedule.id" :value="schedule.id">{{ schedule.label }}</option>
        </select>
        <select id="task-name-filter" class="select" v-model="selectedTask" :aria-label="$t('list_label_task')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="task in filteredTasks" :key="task" :value="task">{{ task }}</option>
        </select>
        <select class="select" v-model="selectedStatus" :aria-label="$t('list_label_status')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="status in uniqueStatuses" :key="status" :value="status">{{ status }}</option>
        </select>
        <button class="btn" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
        <button v-if="!sessionMode || editableSchedules.length" :disabled="saving || isLoading" class="btn primary" @click="openAddScheduleModal">
          <i class="ph ph-plus"></i> {{ $t('btn_add') }}
        </button>
      </div>
    </div>

    <div class="table-scroll">
      <table v-if="paginatedPosts.length > 0">
        <thead>
          <tr>
            <SortableHeader field="schedule_id" label="ID" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="schedule_group_name" :label="$t('list_label_schedule')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="task_name" :label="$t('list_label_task')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="status" :label="$t('list_label_status')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="lastest_start_time" :label="$t('list_lastest_execution_time')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader :label="$t('list_label_actions')" :sortable="false" />
          </tr>
        </thead>
        <tbody>
          <tr v-for="post in paginatedPosts" :key="post.detail_id" :data-task="post.detail_id">
            <td>{{ post.schedule_id }}</td>
            <td>{{ post.schedule_group_name }}</td>
            <td>{{ post.task_name }}</td>
            <td><span class="badge" :class="statusBadgeClass(post.status)">{{ post.status }}</span></td>
            <td>{{ formatDate(post.lastest_start_time) }}</td>
            <td>
              <div class="row-actions">
                <button v-if="!sessionMode || post.can_edit" :disabled="saving || isLoading" class="mini" :aria-label="$t('btn_edit')" @click="openEditModal(post)">
                  <i class="ph ph-pencil-simple"></i>
                </button>
                <button v-if="!sessionMode || post.can_run" :disabled="saving" class="mini" :aria-label="$t('btn_run')" @click="openManualRunEditModal(post);">
                  <i class="ph ph-play"></i>
                </button>
                <button v-if="!sessionMode || post.can_edit" :disabled="saving" class="mini" :aria-label="$t('btn_remove')" @click="deleteRecord(post.detail_id)">
                  <i class="ph ph-trash"></i>
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty-state">{{ $t('schedules_list_no_datas') }}</div>
    </div>

    <div class="panel-foot" v-if="paginatedPosts.length > 0">
      <span>{{ $t('list_footer_range', { total: filteredPosts.length, start: rangeStart, end: rangeEnd }) }}</span>
      <BoardPagination :total="filteredPosts.length" :perPage="perPage" @page-changed="changePage" />
    </div>

    <!-- ✅ 공통 모달 사용 -->
    <ModalComponent
      :isOpen="isModalOpen"
    :busy="saving"
      :title="isEditMode ? $t('list_label_task') + ' ' + $t('btn_edit') : $t('list_label_task') + ' ' + $t('btn_add')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveTasks"
    >
      <p v-if="errorMessage" role="alert">{{ errorMessage }}</p>
      <div class="form-grid task-form">
        <div class="field full">
          <label for="task-schedule">{{ $t('schedule_name') }}</label>
          <select v-if="sessionMode" id="task-schedule" v-model="formControl.schedule_id" aria-required="true">
            <option value="" disabled>{{ $t('task_select_schedule') }}</option>
            <option v-for="item in editableSchedules" :key="item.schedule_id" :value="item.schedule_id">{{ item.group_name }} / {{ item.name }}{{ duplicateScheduleIds.has(String(item.schedule_id)) ? ' (#' + item.schedule_id + ')' : '' }}</option>
          </select>
          <SearchableSelect v-else v-model="formControl.schedule_id" :items="legacyScheduleOptions" label-key="label" value-key="schedule_id" :placeholder="$t('task_select_schedule')"/>
        </div>

        <div class="field half">
          <label for="task-task_name">{{ $t('task_name') }}</label>
          <input id="task-task_name" type="text" v-model="formControl.task_name" :placeholder="$t('task_name') + $t('msg_enter')" aria-required="true" />
        </div>
        <div class="field half">
          <label for="task-type">{{ $t('task_type') }}</label>
          <select id="task-type" :value="formControl.task_type" @change="changeTaskType($event.target.value)">
            <option value="command">{{ $t('task_type_command') }}</option>
            <option value="copy">{{ $t('task_type_copy') }}</option>
            <option value="archive">{{ $t('task_type_archive') }}</option>
            <option value="housekeep">{{ $t('task_type_housekeep') }}</option>
          </select>
        </div>

        <div v-if="formControl.task_type === 'command'" class="field full">
          <label for="task-command">{{ $t('task_command') }}</label>
          <input id="task-command" type="text" v-model="formControl.command" :placeholder="$t('task_command') + $t('msg_enter')" aria-required="true" />
        </div>

        <div v-if="fileTask" class="field full">
          <label for="task-source_path">{{ $t('task_source_path') }}</label>
          <input id="task-source_path" type="text" v-model="formControl.source_path" :placeholder="$t('task_source_path') + $t('msg_enter')" aria-required="true" />
        </div>
        <div v-if="fileTask || formControl.task_type === 'housekeep'" class="field full">
          <label for="task-destination_path">{{ $t(formControl.task_type === 'housekeep' ? 'task_cleanup_path' : 'task_destination_path') }}</label>
          <input id="task-destination_path" type="text" v-model="formControl.destination_path" :placeholder="$t(formControl.task_type === 'housekeep' ? 'task_cleanup_path' : 'task_destination_path') + $t('msg_enter')" aria-required="true" />
        </div>
        <div v-if="formControl.task_type === 'archive'" class="field half">
          <label for="task-archive-type">{{ $t('task_archive_type') }}</label>
          <select id="task-archive-type" v-model="formControl.archive_type" aria-required="true">
            <option value="zip">{{ $t('task_archive_type_zip') }}</option>
          </select>
        </div>
        <div v-if="fileTask" class="field" :class="formControl.task_type === 'archive' ? 'half' : 'full'">
          <label for="task-missing-source">{{ $t('task_error_on_missing_source') }}</label>
          <select id="task-missing-source" v-model="formControl.error_on_missing_source">
            <option value="1">{{ $t('list_yes') }}</option>
            <option value="0">{{ $t('list_no') }}</option>
          </select>
        </div>

        <div v-if="formControl.task_type === 'housekeep'" class="task-housekeep-options" :class="{ 'task-housekeep-legacy': !sessionMode }">
          <div class="field">
            <label for="task-house_keep_days">{{ $t('task_retention_days') }}</label>
            <input id="task-house_keep_days" type="number" v-model="formControl.house_keep_days" :placeholder="$t('task_retention_days') + $t('msg_enter')" min="0" :max="sessionMode ? 2147483647 : 999" aria-required="true" @input="!sessionMode && (formControl.house_keep_days = $event.target.value.slice(0, 3))" />
          </div>
          <div class="field task-recursive-field">
            <label for="task-recursive" class="housekeep-recursive">
              <input id="task-recursive" type="checkbox" v-model="formControl.house_keep_recursive" />
              <span>{{ $t('task_house_keep_recursive') }}</span>
            </label>
          </div>
          <div v-if="sessionMode" class="field task-recursive-field">
            <label for="task-delete-empty" class="housekeep-recursive">
              <input id="task-delete-empty" type="checkbox" v-model="formControl.house_keep_delete_empty_dirs" />
              <span>{{ $t('task_house_keep_delete_empty_dirs') }}</span>
            </label>
          </div>
        </div>

        <div class="task-runtime" :class="{ 'task-runtime-legacy': !sessionMode }">
          <div class="field">
            <label for="task-sequence">{{ $t('task_sequence') }}</label>
            <input id="task-sequence" type="number" v-model="formControl.sequence" min="0" :max="sessionMode ? 2147483647 : 999" @input="!sessionMode && (formControl.sequence = $event.target.value.slice(0, 3))" />
          </div>
          <div class="field">
            <label for="task-status">{{ $t('task_status') }}</label>
            <select id="task-status" v-model="formControl.status">
              <option v-if="formControl.status === 'error'" value="error">{{ $t('label_error') }}</option>
              <option value="active">{{ $t('label_active') }}</option>
              <option value="inactive">{{ $t('label_inactive') }}</option>
              <option value="manual">{{ $t('label_manual') }}</option>
            </select>
          </div>
          <div v-if="sessionMode" class="field">
            <label for="task-retry">{{ $t('schedule_retry') }}</label>
            <input id="task-retry" type="number" min="0" max="100" v-model="formControl.retry_count" />
          </div>
        </div>
        <input type="hidden" v-model="formControl.detail_id" />
        <input type="hidden" v-model="formControl.creator" />
        <input type="hidden" v-model="formControl.modifier" />
      </div>
    </ModalComponent>


    <!-- ✅ 수동실행 모달 사용 -->
    <ModalComponent
      :isOpen="isManualRunModalOpen"
      :busy="saving"
      :title="$t('manual_run_title')"
      :confirmText="$t('btn_run')"
      @close="closeManualRunModal"
      @confirm="manualRun"
    >
      <p v-if="errorMessage" role="alert">{{ errorMessage }}</p>
      <div class="form-grid">
        <div class="field full">
          <label>{{ $t('schedule_name') }}</label>
          <label>{{ formControlManualRun.schedule_group_name }}</label>
        </div>

        <div class="field full">
          <label>{{ $t('task_name') }}</label>
          <label>{{ formControlManualRun.task_name }}</label>
        </div>

        <div class="field half">
          <label for="manual-run-method">{{ $t('manual_run_method') }}</label>
          <select id="manual-run-method" v-model="formControlManualRun.is_immediate" :disabled="saving">
            <option value="0">{{ $t('manual_run_immediate_no') }}</option>
            <option value="1">{{ $t('manual_run_immediate_yes') }}</option>
          </select>
        </div>

        <div v-if="!sessionMode" class="field half">
          <label>{{ $t('manual_run_status') }}</label>
          <select v-model="formControlManualRun.status">
            <option value="active">{{ $t('manual_run_status_active') }}</option>
            <option value="wait">{{ $t('manual_run_status_wait') }}</option>
          </select>
        </div>

        <div class="field full">
          <label for="manual-run-datetime">{{ $t('manual_run_set_time') }}</label>
          <input id="manual-run-datetime" type="datetime-local" step="1" v-model="formControlManualRun.schedule_datetime" :disabled="saving || formControlManualRun.is_immediate === '1'">
        </div>

        <input type="hidden" v-model="formControlManualRun.detail_id" />
        <input type="hidden" v-model="formControlManualRun.creator" />
        <input type="hidden" v-model="formControlManualRun.modifier" />
      </div>
    </ModalComponent>

  </div>
</template>

<script>
import SortableHeader from "@/dashboard/components/misc/SortableHeader.vue";
import SearchableSelect from "@/dashboard/components/misc/SearchableSelect.vue";
export default {
    name: 'TaskList'
    , components: {
        SortableHeader,
        SearchableSelect,
    }
}

</script>


<script setup>
import { ref, computed, onMounted, watch} from "vue";
import { getRequest, postRequest, putRequest, deleteRequest, useSort, sessionMode } from "@api";
import ModalComponent from "../../misc/ModalComponent.vue"; // ✅ 공통 모달 컴포넌트
import BoardPagination from "../../misc/BoardPagination.vue";
import { useI18n } from "vue-i18n";
import { parseServerTime } from "@/dashboard/utils/deviceStatus";
import { housekeepOptions } from "@/dashboard/utils/housekeeping";
import { cronDefaults, formBool, taskPayload, taskTypePayload, resetTaskType, isFileTask, scheduleError, taskScheduleOptions, selectedTaskScheduleId } from "@/dashboard/utils/schedulePayload.mjs";
import { manualCreatePayload, manualError, localTimeValue } from "@/dashboard/utils/manualPayload.mjs";
const saving = ref(false);
const noticeMessage = ref('');
const errorMessage = ref("");
const { t } = useI18n(); // ✅ i18n 함수 가져오기

const posts = ref([]); // ✅ 초기값 빈 배열
const { sortKey, sortOrder, sort } = useSort(posts);
const isLoading = ref(true);
const currentPage = ref(1);
const perPage = ref(7);

// const searchschedule = ref("");
// const selectedStatus = ref("");

// ✅ 모달 상태
const tasks = JSON.parse(localStorage.getItem("tasks") || "{}"); // ✅ 안전하게 변환
const group_id = tasks.group_id; // ✅ 이제 정상적으로 사용 가능!
const schedule_id = tasks.schedule_id; // ✅ 이제 정상적으로 사용 가능!
const isModalOpen = ref(false);
const isEditMode = ref(false);
const formControl = ref({
  schedule_id : 0,
  is_immediate: "1",
  status: "active",
  creator: schedule_id,
  modifier:schedule_id,
  house_keep_recursive: false,
  house_keep_delete_empty_dirs: false,
});
const scheduleList = ref([]);
const editableSchedules = computed(() => scheduleList.value.filter(s => s.can_edit));
const availableSchedules = computed(() => sessionMode ? editableSchedules.value : scheduleList.value);
const scheduleFilters = computed(() => taskScheduleOptions(scheduleList.value, posts.value));
const duplicateScheduleIds = computed(() => new Set(scheduleFilters.value.filter(s => s.duplicate).map(s => s.id)));
const legacyScheduleOptions = computed(() => scheduleList.value.map(s => ({
  ...s, label: scheduleFilters.value.find(option => option.id === String(s.schedule_id))?.label || s.name,
})));
const fileTask = computed(() => isFileTask(formControl.value.task_type));
const changeTaskType = type => {
  if (type !== formControl.value.task_type) {
    Object.assign(formControl.value, resetTaskType(type));
    errorMessage.value = '';
  }
};

// ✅ 스케줄 목록 가져오기
const fetchScheduleList = async () => {
  try {
    const response = await getRequest("/dashboard/tasks/get_schedule_groups", sessionMode ? {} : { group_id: '%' });
    scheduleList.value = response || [];
  } catch (error) {
    scheduleList.value = [];
    throw error;
  }
};

// ✅ 태스크 목록 가져오기
const fetchTasks = async () => {
  try {
    const response = await getRequest("/dashboard/tasks/get_tasks", { 'schedule_id': 0 });
    posts.value = response || [];

    await fetchScheduleList(); // ✅ 디바이스 목록도 불러오기

  } catch (error) {
    errorMessage.value = t(scheduleError(error));
  } finally {
    isLoading.value = false;
  }
};

onMounted(fetchTasks);

// ✅ 날짜 변환 함수
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

// ✅ 유저 ID를 저장할 반응형 변수
const userId = ref("");

// ✅ 마운트될 때 localStorage에서 ID 가져오기
onMounted(() => {
  const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ 문자열 → 객체 변환
  userId.value = user.name || "Guest"; // ✅ "테스트"가 표시됨
});

// ✅ 그룹 추가 모달 열기
const openAddScheduleModal = () => {
  if (saving.value || isLoading.value) return;
  errorMessage.value = '';
  isEditMode.value = false;
  formControl.value = { ...cronDefaults(), schedule_id: selectedTaskScheduleId(selectedSchedule.value, availableSchedules.value), task_name: '', command: '', status: 'active', task_type: "command",
    house_keep_recursive: false, house_keep_delete_empty_dirs: false, archive_type: null, error_on_missing_source: '1', is_error_stop: '0', sequence: 0, retry_count: 0, creator: userId.value, modifier: userId.value };
  isModalOpen.value = true;
};

// ✅ 그룹 수정 모달 열기
const openEditModal = (schedule) => {
  if (saving.value || isLoading.value || (sessionMode && !schedule.can_edit)) return;
  errorMessage.value = '';
  formControl.value = {};
  isEditMode.value = true;
  Object.assign(formControl.value, schedule, housekeepOptions(schedule), {
    schedule_id: selectedTaskScheduleId(String(schedule.schedule_id), availableSchedules.value),
    is_immediate: "1",
    is_error_stop: formBool(schedule.is_error_stop) ? "1" : "0",
    error_on_missing_source: formBool(schedule.error_on_missing_source) ? "1" : "0",
    modifier: userId.value || "Guest",  // ✅ 수정
    creator: schedule.creator || userId.value  // 기존 creator 유지
  });
  isModalOpen.value = true;
};

// ✅ 그룹 저장 (추가 또는 수정)
const saveTasks = async () => {
  if (saving.value) return;
  if (selectedTaskScheduleId(String(formControl.value.schedule_id ?? ''), availableSchedules.value) === '') {
    errorMessage.value = t('task_select_schedule');
    return;
  }
  saving.value = true; errorMessage.value = '';
  try {
    Object.assign(formControl.value, housekeepOptions(formControl.value));
    const payload = sessionMode ? taskPayload(formControl.value, isEditMode.value) : { ...formControl.value, ...taskTypePayload(formControl.value) };
    if (!sessionMode) delete payload.house_keep_delete_empty_dirs;
    if (isEditMode.value) await putRequest('/dashboard/tasks/update_task', payload, 'json');
    else {
      await postRequest('/dashboard/tasks/insert_task', payload, 'json');
      // Clear filters that would exclude the newly created task.
      if (selectedSchedule.value) selectedSchedule.value = String(payload.schedule_id);
      if (selectedTask.value && selectedTask.value !== payload.task_name) selectedTask.value = '';
      if (selectedStatus.value && selectedStatus.value !== payload.status) selectedStatus.value = '';
      currentPage.value = 1;
    }
    isModalOpen.value = false;
    await fetchTasks();
  } catch (error) { errorMessage.value = t(scheduleError(error)); }
  finally { saving.value = false; }
};
const deleteRecord = async (id) => {
  if (saving.value || !confirm(t('msg_delete_task_name'))) return;
  saving.value = true; errorMessage.value = '';
  try { await deleteRequest(`/dashboard/tasks/remove_task/${id}`); await fetchTasks(); }
  catch (error) { errorMessage.value = t(scheduleError(error)); }
  finally { saving.value = false; }
};

// ✅ 모달 닫기
const closeModal = () => {
  if (saving.value) return;
  isModalOpen.value = false;
};

// ✅ 페이징 처리
const paginatedPosts = computed(() => {
  const start = (currentPage.value - 1) * perPage.value;
  return filteredPosts.value.slice(start, start + perPage.value);
});

const changePage = (page) => {
  currentPage.value = page;
};

// ✅ 패널 바닥의 "총 N건 중 X–Y 표시" 범위
const rangeStart = computed(() => (filteredPosts.value.length === 0 ? 0 : (currentPage.value - 1) * perPage.value + 1));
const rangeEnd = computed(() => Math.min(currentPage.value * perPage.value, filteredPosts.value.length));

const isManualRunModalOpen = ref(false);
const formControlManualRun = ref({});

const openManualRunEditModal = (schedule) => {
  if (saving.value || (sessionMode && !schedule.can_run)) return;
  errorMessage.value = ''; noticeMessage.value = '';
  formControlManualRun.value = {
    ...(sessionMode ? { detail_id: schedule.detail_id, schedule_group_name: schedule.schedule_group_name, task_name: schedule.task_name } : schedule),
    schedule_datetime: localTimeValue(new Date(Date.now() + 5 * 60000)),
    is_immediate: '1', status: 'wait', creator: userId.value || group_id, modifier: userId.value || group_id,
  };
  isManualRunModalOpen.value = true;
};

const closeManualRunModal = () => {
  if (!saving.value) isManualRunModalOpen.value = false;
};

const manualRun = async () => {
  if (saving.value) return;
  saving.value = true; errorMessage.value = ''; noticeMessage.value = '';
  try {
    const payload = sessionMode ? manualCreatePayload(formControlManualRun.value, 'task') : formControlManualRun.value;
    await postRequest('/dashboard/tasks/insert_manual_task', payload, 'json');
    isManualRunModalOpen.value = false;
    noticeMessage.value = t('manual_request_created');
  } catch (error) { errorMessage.value = t(manualError(error)); }
  finally { saving.value = false; }
};

// 필터링 상태 추가
const selectedSchedule = ref("");
const selectedTask = ref("");
const selectedStatus = ref("");

// 선택한 스케줄에 따라 필터링된 태스크 리스트
const filteredTasks = computed(() => {
  if (!selectedSchedule.value) {
    return [...new Set(posts.value.map(post => post.task_name))];
  }
  return [...new Set(posts.value.filter(post => String(post.schedule_id) === selectedSchedule.value).map(post => post.task_name))];
});

// 유니크한 상태 리스트
const uniqueStatuses = computed(() => {
  return [...new Set(posts.value.map(post => post.status))];
});

// 필터링된 데이터
const filteredPosts = computed(() => {
  return posts.value.filter(post => {
    const matchesSchedule = selectedSchedule.value ? String(post.schedule_id) === selectedSchedule.value : true;
    const matchesTask = selectedTask.value ? post.task_name === selectedTask.value : true;
    const matchesStatus = selectedStatus.value ? post.status === selectedStatus.value : true;

    return matchesSchedule && matchesTask && matchesStatus;
  });
});

const resetFilters = () => {
  selectedSchedule.value = "";
  selectedTask.value = "";
  selectedStatus.value = "";
  currentPage.value = 1;
};

watch(selectedSchedule, () => { selectedTask.value = ''; });
watch([selectedSchedule, selectedTask, selectedStatus], () => {
  currentPage.value = 1;
});

// ✅ 상태값 → 배지 색 매핑 (active=online, inactive=offline, manual=warning)
const statusBadgeClass = (status) => ({
  online: status === 'active',
  offline: status === 'inactive',
  warning: status === 'manual',
});

</script>

<!--
  Shared layout stays in components.css; local styles only size the checkbox.
  `.panel` / `.panel-head` / `.toolbar` / `.select` / `.btn` /
  `.table-scroll` / `.badge` / `.row-actions` / `.mini` / `.panel-foot` /
  `.form-grid` / `.field` come from components.css. The pre-renewal
  `.board-container` / `.board-table` / `.filters` / `.add-button` /
  `.edit-button` / `.delete-button` / `.run-button` / `.reset-button` /
  `.button-group` / `.modal-form` / `.grid-form` / `.form-field` /
  `.form-group-inline` names are gone, so this screen no longer needs list.css.
-->

<style scoped>
.task-runtime {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  border-top: 1px solid var(--line-soft);
  padding-top: 14px;
}
.task-runtime-legacy {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.task-housekeep-options {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}
.task-housekeep-legacy {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.task-recursive-field {
  display: flex;
  align-items: flex-end;
}
.task-recursive-field .housekeep-recursive {
  display: flex;
  align-items: center;
  gap: 10px;
  box-sizing: border-box;
  width: 100%;
  height: var(--field-h);
  min-width: 0;
  margin: 0;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
  color: var(--text);
  font-size: 11px;
  font-weight: 400;
  cursor: pointer;
}
.task-recursive-field .housekeep-recursive:hover {
  border-color: var(--line-control-hover);
}
.task-recursive-field .housekeep-recursive:focus-within {
  border-color: var(--focus-line);
  box-shadow: 0 0 0 3px var(--focus-halo);
}
.task-recursive-field .housekeep-recursive input[type="checkbox"] {
  appearance: auto;
  accent-color: var(--cyan);
  color-scheme: dark;
  flex: 0 0 16px;
  width: 16px;
  height: 16px;
  min-height: 0;
  margin: 0;
  padding: 0;
  box-shadow: none;
  cursor: pointer;
}
.task-recursive-field .housekeep-recursive span {
  color: inherit;
  line-height: 1.4;
}
@media (max-width: 480px) {
  .task-runtime, .task-runtime-legacy, .task-housekeep-options, .task-housekeep-legacy {
    grid-template-columns: 1fr;
  }
}
</style>
