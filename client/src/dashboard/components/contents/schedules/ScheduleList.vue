<template>
  <div class="board-container">
    <h2>{{ $t('sub_schedules') }}</h2>

    <!-- ✅ 스케줄 추가 버튼 -->
    <button class="add-button" @click="openAddScheduleModal">
      <i class="ph ph-plus"></i> {{ $t('btn_add') }}
    </button>

    <!-- ✅ 검색 필터 -->
    <div class="filters">
      <div>
        <label>{{ $t('list_label_device') }}:</label>
        <select v-model="selectedDevice">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="device in uniqueDevices" :key="device" :value="device">{{ device }}</option>
        </select>
      </div>

      <!-- 이거 추가 -->
      <div>
        <label>{{ $t('list_label_schedule') }}:</label>
        <select v-model="selectedSchedule">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="schedule in filteredSchedules" :key="schedule" :value="schedule">{{ schedule }}</option>
        </select>
      </div>

      <!-- 이거 추가 -->
      <div>
        <label>{{ $t('list_label_status') }}:</label>
        <select v-model="selectedStatus">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="status in uniqueStatuses" :key="status" :value="status">{{ status }}</option>
        </select>
      </div>

      <button class="reset-button" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
    </div>


    <!-- ✅ 스케줄 목록 -->
    <table v-if="paginatedPosts.length > 0" class="board-table">
      <thead>
        <tr>
          <SortableHeader cssClass="title1" field="schedule_id" label="ID" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" width="12%" />
          <SortableHeader cssClass="title21" field="device_name" :label="$t('list_label_device')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" width="18%" />
          <SortableHeader cssClass="title22" field="name" :label="$t('list_label_schedule')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" width="20%" />
          <SortableHeader cssClass="title3" field="status" :label="$t('list_label_status')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" width="13%" />
          <SortableHeader cssClass="title4" :label="$t('list_label_actions')"  width="20%" :sortable="false" />
        </tr>
      </thead>
      <tbody>
        <tr v-for="post in paginatedPosts" :key="post.schedule_id">
          <td>{{ post.schedule_id }}</td>
          <td>{{ post.device_name }}</td>
          <td>{{ post.name }}</td>
          <td>{{ post.status }}</td>
          <td>
            <div class="button-group">
              <button class="edit-button" @click="openEditModal(post)">
                <i class="ph ph-pencil-simple"></i> {{ $t('btn_edit') }}
              </button>
              <button class="delete-button" @click="deleteRecord(post.schedule_id)">
                <i class="ph ph-trash"></i> {{ $t('btn_remove') }}
              </button>
              <button class="run-button" @click="openManualRunEditModal(post);">
                <i class="ph ph-play"></i> {{ $t('btn_run') }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <BoardPagination v-if="paginatedPosts.length > 0" :total="filteredPosts.length" :perPage="perPage" @page-changed="changePage" />

    <!-- ✅ 공통 모달 사용 -->
    <ModalComponent
      :isOpen="isModalOpen"
      :title="isEditMode ? $t('list_label_schedule') + ' ' + $t('btn_edit') : $t('list_label_schedule') + ' ' + $t('btn_add')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveSchedule"
    >
      <div class="modal-form grid-form">
        <div class="form-field">
          <label>{{ $t('schedule_name') }}</label>
          <input type="text" v-model="formControl.name" :placeholder="$t('schedule_name') + $t('msg_enter')" />
        </div>

        <div class="form-group-inline">
          <div class="form-field">
            <label>{{ $t('schedule_year') }}</label>
            <input type="text" v-model="formControl.year" :placeholder="$t('schedule_year') + $t('msg_enter')" />
          </div>

          <div class="form-field">
            <label>{{ $t('schedule_month') }}</label>
            <input type="text" v-model="formControl.month" :placeholder="$t('schedule_month') + $t('msg_enter')" />
          </div>
        </div>

        <div class="form-group-inline">
          <div class="form-field">
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

          <div class="form-field">
            <label>{{ $t('schedule_day') }}</label>
            <input type="text" v-model="formControl.day" :placeholder="$t('schedule_day') + $t('msg_enter')" />
          </div>
        </div>

        <div class="form-group-inline">
          <div class="form-field">
            <label>{{ $t('schedule_hour') }}</label>
            <input type="text" v-model="formControl.hour" :placeholder="$t('schedule_name') + $t('msg_enter')" />
          </div>

          <div class="form-field">
            <label>{{ $t('schedule_minute') }}</label>
            <input type="text" v-model="formControl.minute" :placeholder="$t('schedule_minute') + $t('msg_enter')" />
          </div>
        </div>

        <div class="form-group-inline">
          <div class="form-field">
            <label>{{ $t('schedule_second') }}</label>
            <input type="text" v-model="formControl.second" :placeholder="$t('schedule_second') + $t('msg_enter')" />
          </div>

          <div class="form-field">
          </div>
        </div>

        <div class="form-field">
          <label>{{ $t('list_label_status') }}</label>
          <select v-model="formControl.status">
            <option value="active">{{ $t('label_active') }}</option>
            <option value="inactive">{{ $t('label_inactive') }}</option>
            <option value="manual">{{ $t('label_manual') }}</option>
          </select>
        </div>

        <div class="form-field">
          <label>{{ $t('schedule_error_stop') }}</label>
          <select v-model="formControl.is_error_stop">
            <option value="1">{{ $t('schedule_error_stop_yes') }}</option>
            <option value="0">{{ $t('schedule_error_stop_no') }}</option>
          </select>
        </div>

        <div class="form-field">
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
      <div class="modal-form grid-form">
        <div class="form-field">
          <label>{{ $t('schedule_name') }}</label>
          <label>{{ formControlManualRun.name }}</label>
        </div>

        <div class="form-field">
          <label>{{ $t('manual_run_method') }}</label>
          <select v-model="formControlManualRun.is_immediate">
            <option value="0">{{ $t('manual_run_immediate_no') }}</option>
            <option value="1">{{ $t('manual_run_immediate_yes') }}</option>
          </select>
        </div>

        <div class="form-field">
          <label>{{ $t('manual_run_set_time') }}</label>
          <input type="datetime-local" v-model="formControlManualRun.schedule_datetime" :disabled="formControlManualRun.is_immediate === '1'">
        </div>

        <div class="form-field">
          <label>{{ $t('manual_run_status') }}</label>
          <select v-model="formControlManualRun.status">
            <option value="active">{{ $t('manual_run_status_active') }}</option>
            <option value="wait">{{ $t('manual_run_status_wait') }}</option>
          </select>
        </div>

        <input type="hidden" v-model="formControlManualRun.schedule_id" />
        <input type="hidden" v-model="formControlManualRun.creator" />
        <input type="hidden" v-model="formControlManualRun.modifier" />
      </div>
    </ModalComponent>

  </div>
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

</script>
