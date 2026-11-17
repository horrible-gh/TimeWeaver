<template>
  <p v-if="errorMessage" role="alert">{{ errorMessage }}</p>
  <p v-if="noticeMessage" role="status">{{ noticeMessage }}</p>
  <div class="metric-grid">
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('metric_schedules_total_label') }}</span>
        <span class="metric-icon"><i class="ph ph-calendar"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ totalCount }}</strong>
        <small>{{ $t('metric_schedules_count') }}</small>
      </div>
    </article>
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('label_active') }}</span>
        <span class="metric-icon" style="color:var(--green)"><i class="ph ph-check-circle"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ activeCount }}</strong>
        <small>{{ $t('devices_summary_ratio', { n: activeRatio }) }}</small>
      </div>
    </article>
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('label_manual') }}</span>
        <span class="metric-icon" style="color:var(--amber)"><i class="ph ph-warning"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ manualCount }}</strong>
        <small>{{ $t('metric_manual_desc') }}</small>
      </div>
    </article>
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('label_inactive') }}</span>
        <span class="metric-icon"><i class="ph ph-x-circle"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ inactiveCount }}</strong>
        <small>{{ $t('metric_inactive_desc') }}</small>
      </div>
    </article>
  </div>

  <div class="panel">
    <div class="panel-head">
      <div class="panel-title">
        <h2>{{ $t('schedules_list_title') }}</h2>
        <p>{{ $t('schedules_list_desc') }}</p>
      </div>
      <div class="toolbar">
        <select class="select" v-model="selectedDevice" :aria-label="$t('list_label_device')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="device in uniqueDevices" :key="device" :value="device">{{ device }}</option>
        </select>
        <select class="select" v-model="selectedSchedule" :aria-label="$t('list_label_schedule')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="schedule in filteredSchedules" :key="schedule" :value="schedule">{{ schedule }}</option>
        </select>
        <select class="select" v-model="selectedStatus" :aria-label="$t('list_label_status')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="status in uniqueStatuses" :key="status" :value="status">{{ status }}</option>
        </select>
        <button class="btn" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
        <button v-if="!sessionMode || editableGroups.length" :disabled="saving" class="btn primary" @click="openAddScheduleModal">
          <i class="ph ph-plus"></i> {{ $t('btn_new_schedule') }}
        </button>
      </div>
    </div>

    <div class="table-scroll">
      <table v-if="paginatedPosts.length > 0">
        <thead>
          <tr>
            <SortableHeader field="schedule_id" label="ID" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="device_name" :label="$t('list_label_device')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="name" :label="$t('list_label_schedule')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="status" :label="$t('list_label_status')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader :label="$t('list_label_actions')" :sortable="false" />
          </tr>
        </thead>
        <tbody>
          <tr v-for="post in paginatedPosts" :key="post.schedule_id" :data-schedule="post.schedule_id">
            <td>{{ post.schedule_id }}</td>
            <td>{{ post.device_name }}</td>
            <td>{{ post.name }}</td>
            <td><span class="badge" :class="statusBadgeClass(post.status)">{{ post.status }}</span></td>
            <td>
              <div class="row-actions">
                <button v-if="!sessionMode || post.can_edit" :disabled="saving" class="mini" :aria-label="$t('btn_edit')" @click="openEditModal(post)">
                  <i class="ph ph-pencil-simple"></i>
                </button>
                <button v-if="!sessionMode || post.can_run" :disabled="saving" class="mini" :aria-label="$t('btn_run')" @click="openManualRunEditModal(post)">
                  <i class="ph ph-play"></i>
                </button>
                <button v-if="!sessionMode || post.can_edit" :disabled="saving" class="mini" :aria-label="$t('btn_remove')" @click="deleteRecord(post.schedule_id)">
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
  </div>

  <!-- ✅ 공통 모달 사용 -->
  <ModalComponent
    :isOpen="isModalOpen"
    :busy="saving"
    :title="isEditMode ? $t('list_label_schedule') + ' ' + $t('btn_edit') : $t('list_label_schedule') + ' ' + $t('btn_add')"
    :confirmText="$t('btn_save')"
    @close="closeModal"
    @confirm="saveSchedule"
  >
    <p v-if="errorMessage" role="alert">{{ errorMessage }}</p>
    <div class="form-grid">
      <div v-if="sessionMode" class="field full">
        <label for="schedule-group">{{ $t('schedule_group') }}</label>
        <select id="schedule-group" v-model="formControl.group_id"><option v-for="g in editableGroups" :key="g.group_id" :value="g.group_id">{{ g.group_name }}</option></select>
      </div>
      <div class="field full">
        <label>{{ $t('schedule_name') }}</label>
        <input id="schedule-name" type="text" v-model="formControl.name" :placeholder="$t('schedule_name') + $t('msg_enter')" />
      </div>

      <div class="field">
        <label>{{ $t('schedule_year') }}</label>
        <input type="text" v-model="formControl.year" :placeholder="$t('schedule_year') + $t('msg_enter')" />
      </div>
      <div class="field">
        <label>{{ $t('schedule_month') }}</label>
        <input type="text" v-model="formControl.month" :placeholder="$t('schedule_month') + $t('msg_enter')" />
      </div>
      <div class="field">
        <label>{{ $t('schedule_day') }}</label>
        <input type="text" v-model="formControl.day" :placeholder="$t('schedule_day') + $t('msg_enter')" />
      </div>
      <div class="field">
        <label>{{ $t('schedule_dayofweek') }}</label>
        <select id="schedule-weekday" v-model="formControl.day_of_week">
          <option v-if="sessionMode && !['*','0','1','2','3','4','5','6'].includes(String(formControl.day_of_week))" :value="formControl.day_of_week">{{ formControl.day_of_week }}</option>
          <option value="*">{{ $t('schedule_dayofweek_all') }}</option>
          <option :value="sessionMode ? '6' : '0'">{{ $t('schedule_dayofweek_sun') }}</option>
          <option :value="sessionMode ? '0' : '1'">{{ $t('schedule_dayofweek_mon') }}</option>
          <option :value="sessionMode ? '1' : '2'">{{ $t('schedule_dayofweek_tue') }}</option>
          <option :value="sessionMode ? '2' : '3'">{{ $t('schedule_dayofweek_wed') }}</option>
          <option :value="sessionMode ? '3' : '4'">{{ $t('schedule_dayofweek_thr') }}</option>
          <option :value="sessionMode ? '4' : '5'">{{ $t('schedule_dayofweek_fri') }}</option>
          <option :value="sessionMode ? '5' : '6'">{{ $t('schedule_dayofweek_sat') }}</option>
        </select>
      </div>

      <div class="field">
        <label>{{ $t('schedule_hour') }}</label>
        <input type="text" v-model="formControl.hour" :placeholder="$t('schedule_name') + $t('msg_enter')" />
      </div>
      <div class="field">
        <label>{{ $t('schedule_minute') }}</label>
        <input type="text" v-model="formControl.minute" :placeholder="$t('schedule_minute') + $t('msg_enter')" />
      </div>
      <div class="field">
        <label>{{ $t('schedule_second') }}</label>
        <input id="schedule-second" type="text" v-model="formControl.second" :placeholder="$t('schedule_second') + $t('msg_enter')" />
      </div>
      <div class="field">
        <label>{{ $t('list_label_status') }}</label>
        <select id="schedule-status" v-model="formControl.status">
          <option v-if="formControl.status === 'error'" value="error">{{ $t('label_error') }}</option>
          <option value="active">{{ $t('label_active') }}</option>
          <option value="inactive">{{ $t('label_inactive') }}</option>
          <option value="manual">{{ $t('label_manual') }}</option>
        </select>
      </div>

      <div class="field half">
        <label>{{ $t('schedule_error_stop') }}</label>
        <select v-model="formControl.is_error_stop">
          <option value="1">{{ $t('schedule_error_stop_yes') }}</option>
          <option value="0">{{ $t('schedule_error_stop_no') }}</option>
        </select>
      </div>
      <div class="field half">
        <label>{{ $t('schedule_device') }}</label>
        <select id="schedule-device" v-model="formControl.target_device">
          <option disabled value="">{{ $t('schedule_device') + $t('msg_enter') }}</option>
          <option v-for="device in selectableDevices" :key="device.device_id" :value="device.device_id">
            {{ device.device_name }}
          </option>
        </select>
      </div>

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
        <label>{{ formControlManualRun.name }}</label>
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

      <input type="hidden" v-model="formControlManualRun.schedule_id" />
      <input type="hidden" v-model="formControlManualRun.creator" />
      <input type="hidden" v-model="formControlManualRun.modifier" />
    </div>
  </ModalComponent>

</template>

<script>
import SortableHeader from "@/dashboard/components/misc/SortableHeader.vue";
export default {
    name: 'ScheduleList'
    , components: {
        SortableHeader,
    }
}

</script>


<script setup>
import { useI18n } from "vue-i18n";
import { ref, computed, onMounted, watch } from "vue";
import { getRequest, postRequest, putRequest, deleteRequest, useSort, sessionMode, ensureSession } from "@api";
import ModalComponent from "../../misc/ModalComponent.vue"; // ✅ 공통 모달 컴포넌트
import BoardPagination from "../../misc/BoardPagination.vue";

import { cronDefaults, formBool, schedulePayload, scheduleError } from "@/dashboard/utils/schedulePayload.mjs";
import { canEditGroup, devicesForGroup } from "@/dashboard/utils/devicePermissions.mjs";
import { manualCreatePayload, manualError, localTimeValue } from "@/dashboard/utils/manualPayload.mjs";
const saving = ref(false);
const noticeMessage = ref('');
const errorMessage = ref("");
const principal = ref(null);
const { t } = useI18n(); // ✅ i18n 함수 가져오기

const posts = ref([]); // ✅ 초기값 빈 배열
const { sortKey, sortOrder, sort } = useSort(posts);
const isLoading = ref(true);
const currentPage = ref(1);
const perPage = ref(7);

// const searchschedule = ref("");
// const selectedStatus = ref("");

// ✅ 모달 상태
const schedule = JSON.parse(localStorage.getItem("schedule") || "{}"); // ✅ 안전하게 변환
const group_id = schedule.group_id; // ✅ 이제 정상적으로 사용 가능!
const isModalOpen = ref(false);
const isEditMode = ref(false);
const formControl = ref({ group_id : 0, status: "active", creator: group_id,  modifier:group_id });
const deviceList = ref([]);
const groupList = ref([]);
const originalTarget = ref(null);
const originalGroup = ref(null);
const editableGroups = computed(() => groupList.value.filter(g => (g.status === 'active' || (isEditMode.value && Number(g.group_id) === originalGroup.value)) && canEditGroup(principal.value, g.group_id)));
const selectableDevices = computed(() => {
  if (!sessionMode) return deviceList.value;
  const items = devicesForGroup(deviceList.value, formControl.value.group_id).filter(d => d.can_edit);
  const current = deviceList.value.find(d => d.can_edit && Number(d.device_id) === originalTarget.value && Number(d.group_id) === Number(formControl.value.group_id));
  if (isEditMode.value && current && !items.some(d => d.device_id === current.device_id)) items.push(current);
  return items;
});
watch(() => formControl.value.group_id, () => {
  if (sessionMode && !selectableDevices.value.some(d => Number(d.device_id) === Number(formControl.value.target_device))) formControl.value.target_device = '';
});

// ✅ 장치 목록 가져오기
const fetchDeviceList = async () => {
  try {
    const response = await getRequest("/dashboard/schedule/get_devices", sessionMode ? {} : { group_id: 0 });
    deviceList.value = response || [];
  } catch (error) {
    console.error("디바이스 리스트 가져오기 실패:", error);
  }
};

// ✅ 스케줄 목록 가져오기
const fetchScheduleGroups = async () => {
  try {
    const response = await getRequest("/dashboard/schedule/get_schedule_groups", sessionMode ? {} : { 'group_id': 0 });
    posts.value = response || [];
    if (sessionMode) {
      principal.value = (await ensureSession()).user;
      groupList.value = await getRequest('/dashboard/groups/get_groups');
      if (canEditGroup(principal.value, 0)) groupList.value = [{ group_id: 0, group_name: t('device_default_group'), status: 'active' }, ...groupList.value.filter(g => g.group_id !== 0)];
    }

    await fetchDeviceList(); // ✅ 디바이스 목록도 불러오기

  } catch (error) {
    errorMessage.value = t(scheduleError(error));
  } finally {
    isLoading.value = false;
  }
};

onMounted(fetchScheduleGroups);

// ✅ 유저 ID를 저장할 반응형 변수
const userId = ref("");

// ✅ 마운트될 때 localStorage에서 ID 가져오기
onMounted(() => {
  const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ 문자열 → 객체 변환
  userId.value = user.name || "Guest"; // ✅ "테스트"가 표시됨
});

// ✅ 그룹 추가 모달 열기
const openAddScheduleModal = () => {
  if (saving.value) return;
  errorMessage.value = '';
  isEditMode.value = false;
  originalTarget.value = null; originalGroup.value = null;
  formControl.value = { ...cronDefaults(), group_id: sessionMode ? editableGroups.value[0]?.group_id : 0, name: '', target_device: '', status: 'active', is_error_stop: '0', creator: userId.value, modifier: userId.value };
  isModalOpen.value = true;
};

// ✅ 그룹 수정 모달 열기
const openEditModal = (schedule) => {
  if (saving.value || (sessionMode && !schedule.can_edit)) return;
  errorMessage.value = '';
  formControl.value = {};
  isEditMode.value = true;
  originalTarget.value = Number(schedule.target_device); originalGroup.value = Number(schedule.group_id);
  Object.assign(formControl.value, schedule, {
    is_error_stop: formBool(schedule.is_error_stop) ? "1" : "0",
    modifier: userId.value || "Guest",  // ✅ 수정
    creator: schedule.creator || userId.value  // 기존 creator 유지
  });
  isModalOpen.value = true;
};

// ✅ 그룹 저장 (추가 또는 수정)
const saveSchedule = async () => {
  if (saving.value) return;
  saving.value = true; errorMessage.value = '';
  try {
    const payload = sessionMode ? schedulePayload(formControl.value, isEditMode.value) : formControl.value;
    if (isEditMode.value) await putRequest('/dashboard/schedule/update_schedule', payload, 'json');
    else await postRequest('/dashboard/schedule/insert_schedule', payload, 'json');
    isModalOpen.value = false;
    await fetchScheduleGroups();
  } catch (error) { errorMessage.value = t(scheduleError(error)); }
  finally { saving.value = false; }
};
const deleteRecord = async (id) => {
  if (saving.value || !confirm(t('msg_delete_schedule_name'))) return;
  saving.value = true; errorMessage.value = '';
  try { await deleteRequest(`/dashboard/schedule/remove_schedule/${id}`); await fetchScheduleGroups(); }
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
    ...(sessionMode ? { schedule_id: schedule.schedule_id, name: schedule.name } : schedule),
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
    const payload = sessionMode ? manualCreatePayload(formControlManualRun.value, 'schedule') : formControlManualRun.value;
    await postRequest('/dashboard/schedule/insert_manual_schedule', payload, 'json');
    isManualRunModalOpen.value = false;
    noticeMessage.value = t('manual_request_created');
  } catch (error) { errorMessage.value = t(manualError(error)); }
  finally { saving.value = false; }
};

// ✅ 필터링 상태
const selectedDevice = ref("");
const selectedSchedule = ref("");
const selectedStatus = ref("");

// ✅ 유니크한 그룹 리스트 반환
const uniqueDevices = computed(() => {
  return [...new Set(posts.value.map(post => post.device_name))];
});

// ✅ 선택한 그룹에 따라 유니크한 스케줄 이름 리스트 반환
const filteredSchedules = computed(() => {
  if (!selectedDevice.value) {
    return [...new Set(posts.value.map(post => post.name))];
  }
  return [...new Set(posts.value.filter(post => post.device_name === selectedDevice.value).map(post => post.name))];
});

// ✅ 유니크한 종료 코드 리스트 반환
const uniqueStatuses = computed(() => {
  return [...new Set(posts.value.map(post => post.status))];
});


// ✅ 필터링된 데이터 반환
const filteredPosts = computed(() => {
  return posts.value.filter(post => {
    const matchesDevice = selectedDevice.value ? post.device_name === selectedDevice.value : true;
    const matchesSchedule = selectedSchedule.value ? post.name === selectedSchedule.value : true;
    const matchesStatus = selectedStatus.value ? post.status === selectedStatus.value : true;

    return matchesDevice && matchesSchedule && matchesStatus;
  });
});

const resetFilters = () => {
  selectedDevice.value = "";
  selectedSchedule.value = "";
  selectedStatus.value = "";
  currentPage.value = 1; // 이거 추가!
};

// ✅ 상단 지표 카드 — 필터와 무관하게 전체 스케줄 기준
const totalCount = computed(() => posts.value.length);
const activeCount = computed(() => posts.value.filter(post => post.status === 'active').length);
const manualCount = computed(() => posts.value.filter(post => post.status === 'manual').length);
const inactiveCount = computed(() => posts.value.filter(post => post.status === 'inactive').length);
const activeRatio = computed(() => (totalCount.value ? Math.round((activeCount.value / totalCount.value) * 100) : 0));

// ✅ 상태값 → 배지 색 매핑 (active=online, inactive=offline, manual=warning)
const statusBadgeClass = (status) => ({
  online: status === 'active',
  offline: status === 'inactive',
  warning: status === 'manual',
});

</script>
