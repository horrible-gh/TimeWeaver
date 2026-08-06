<template>
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
        <button class="btn primary" @click="openAddScheduleModal">
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
          <tr v-for="post in paginatedPosts" :key="post.schedule_id">
            <td>{{ post.schedule_id }}</td>
            <td>{{ post.device_name }}</td>
            <td>{{ post.name }}</td>
            <td><span class="badge" :class="statusBadgeClass(post.status)">{{ post.status }}</span></td>
            <td>
              <div class="row-actions">
                <button class="mini" :aria-label="$t('btn_edit')" @click="openEditModal(post)">
                  <i class="ph ph-pencil-simple"></i>
                </button>
                <button class="mini" :aria-label="$t('btn_run')" @click="openManualRunEditModal(post)">
                  <i class="ph ph-play"></i>
                </button>
                <button class="mini" :aria-label="$t('btn_remove')" @click="deleteRecord(post.schedule_id)">
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
    :title="isEditMode ? $t('list_label_schedule') + ' ' + $t('btn_edit') : $t('list_label_schedule') + ' ' + $t('btn_add')"
    :confirmText="$t('btn_save')"
    @close="closeModal"
    @confirm="saveSchedule"
  >
    <div class="form-grid">
      <div class="field full">
        <label>{{ $t('schedule_name') }}</label>
        <input type="text" v-model="formControl.name" :placeholder="$t('schedule_name') + $t('msg_enter')" />
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
        <select v-model="formControl.day_of_week">
          <option value="*">{{ $t('schedule_dayofweek_all') }}</option>
          <option value="0">{{ $t('schedule_dayofweek_sun') }}</option>
          <option value="1">{{ $t('schedule_dayofweek_mon') }}</option>
          <option value="2">{{ $t('schedule_dayofweek_tue') }}</option>
          <option value="3">{{ $t('schedule_dayofweek_wed') }}</option>
          <option value="4">{{ $t('schedule_dayofweek_thr') }}</option>
          <option value="5">{{ $t('schedule_dayofweek_fri') }}</option>
          <option value="6">{{ $t('schedule_dayofweek_sat') }}</option>
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
        <input type="text" v-model="formControl.second" :placeholder="$t('schedule_second') + $t('msg_enter')" />
      </div>
      <div class="field">
        <label>{{ $t('list_label_status') }}</label>
        <select v-model="formControl.status">
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
        <select v-model="formControl.target_device">
          <option disabled value="">{{ $t('schedule_device') + $t('msg_enter') }}</option>
          <option v-for="device in deviceList" :key="device.device_id" :value="device.device_id">
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
    :title="$t('manual_run_title')"
    :confirmText="$t('btn_run')"
    @close="closeManualRunModal"
    @confirm="manualRun"
  >
    <div class="form-grid">
      <div class="field full">
        <label>{{ $t('schedule_name') }}</label>
        <label>{{ formControlManualRun.name }}</label>
      </div>

      <div class="field half">
        <label>{{ $t('manual_run_method') }}</label>
        <select v-model="formControlManualRun.is_immediate">
          <option value="0">{{ $t('manual_run_immediate_no') }}</option>
          <option value="1">{{ $t('manual_run_immediate_yes') }}</option>
        </select>
      </div>
      <div class="field half">
        <label>{{ $t('manual_run_status') }}</label>
        <select v-model="formControlManualRun.status">
          <option value="active">{{ $t('manual_run_status_active') }}</option>
          <option value="wait">{{ $t('manual_run_status_wait') }}</option>
        </select>
      </div>

      <div class="field full">
        <label>{{ $t('manual_run_set_time') }}</label>
        <input type="datetime-local" v-model="formControlManualRun.schedule_datetime" :disabled="formControlManualRun.is_immediate === '1'">
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
import { ref, computed, onMounted } from "vue";
import { getRequest, postRequest, putRequest, deleteRequest, useSort } from "@api";
import ModalComponent from "../../misc/ModalComponent.vue"; // ✅ 공통 모달 컴포넌트
import BoardPagination from "../../misc/BoardPagination.vue";

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

// ✅ 장치 목록 가져오기
const fetchDeviceList = async () => {
  try {
    const response = await getRequest("/dashboard/schedule/get_devices", { group_id: 0 });
    deviceList.value = response || [];
  } catch (error) {
    console.error("디바이스 리스트 가져오기 실패:", error);
  }
};

// ✅ 스케줄 목록 가져오기
const fetchScheduleGroups = async () => {
  try {
    const response = await getRequest("/dashboard/schedule/get_schedule_groups", { 'group_id': 0 });
    posts.value = response || [];

    await fetchDeviceList(); // ✅ 디바이스 목록도 불러오기

  } catch (error) {
    console.error("데이터 가져오기 실패:", error);
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
  isEditMode.value = false;
  Object.assign(formControl.value, {
    schedule_name: "",
    status: "active",
    creator: userId.value || "Guest",  // ✅ 수정
    modifier: userId.value || "Guest"  // ✅ 수정
  });
  isModalOpen.value = true;
};

// ✅ 그룹 수정 모달 열기
const openEditModal = (schedule) => {
  isEditMode.value = true;
  Object.assign(formControl.value, schedule, {
    modifier: userId.value || "Guest",  // ✅ 수정
    creator: schedule.creator || userId.value  // 기존 creator 유지
  });
  isModalOpen.value = true;
};

// ✅ 그룹 저장 (추가 또는 수정)
const saveSchedule = async () => {
  try {
    // ✅ 요청 데이터 확인 (디버깅)
    console.log("전송 데이터:", JSON.stringify(formControl.value, null, 2));
    if (isEditMode.value) {
      await putRequest(`/dashboard/schedule/update_schedule`, formControl.value, "json");
    } else {
      await postRequest(`/dashboard/schedule/insert_schedule`, formControl.value, "json");
    }
    await fetchScheduleGroups();
    closeModal();
  } catch (error) {
    console.error("그룹 저장 실패:", error);
  }
};


// ✅ 그룹 삭제
const deleteRecord = async (scheduleId) => {
  if (confirm(t('msg_delete_schedule_name'))) {
    try {
      await deleteRequest(`/dashboard/schedule/remove_schedule/${scheduleId}`);
      await fetchScheduleGroups();
    } catch (error) {
      console.error("그룹 삭제 실패:", error);
    }
  }
};

// ✅ 모달 닫기
const closeModal = () => {
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
const isManualRunEditMode = ref(false);
const formControlManualRun = ref({ group_id : 0, status: "active", creator: group_id,  modifier:group_id });

// ✅ 수동실행 모달 열기
const openManualRunEditModal = (schedule) => {
  isManualRunEditMode.value = true;
  Object.assign(formControlManualRun.value, schedule);
  formControlManualRun.value.schedule_datetime =
    new Date(Date.now() - new Date().getTimezoneOffset() * 60000)
      .toISOString()
      .slice(0, 16);
  formControlManualRun.value.is_immediate = "1"
  formControlManualRun.value.status = "wait"
  formControlManualRun.value.creator = userId.value || group_id // 기존 creator 유지
  formControlManualRun.value.modifier = userId.value || group_id // 기존 creator 유지

  isManualRunModalOpen.value = true;
};


// ✅ 모달 닫기
const closeManualRunModal = () => {
  isManualRunModalOpen.value = false;
};

// ✅ 수동실행
const manualRun = async () => {
  try {
    // ✅ 요청 데이터 확인 (디버깅)
    console.log("전송 데이터:", JSON.stringify(formControlManualRun.value, null, 2));
    await postRequest(`/dashboard/schedule/insert_manual_schedule`, formControlManualRun.value, "json");
    closeManualRunModal();
  } catch (error) {
    console.error("그룹 저장 실패:", error);
  }
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
