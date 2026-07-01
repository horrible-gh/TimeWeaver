<template>
  <div class="board-container">
    <h2>{{ $t('sub_tasks') }}</h2>

    <!-- ✅ 스케줄 추가 버튼 -->
    <button class="add-button" @click="openAddScheduleModal">
      <i class="ph ph-plus"></i> {{ $t('btn_add') }}
    </button>

    <!-- ✅ 필터 -->
    <div class="filters">
      <div>
        <label>{{ $t('list_label_schedule') }}:</label>
        <select v-model="selectedSchedule">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="schedule in uniqueSchedules" :key="schedule" :value="schedule">{{ schedule }}</option>
        </select>
      </div>

      <div>
        <label>{{ $t('list_label_task') }}:</label>
        <select v-model="selectedTask">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="task in filteredTasks" :key="task" :value="task">{{ task }}</option>
        </select>
      </div>

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
          <SortableHeader cssClass="title1" field="schedule_id" label="ID" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" width="7%" />
          <SortableHeader cssClass="title2" field="schedule_group_name" :label="$t('list_label_schedule')"  :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort"   width="16%" />
          <SortableHeader cssClass="title3" field="task_name" :label="$t('list_label_task')"  :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort"   width="16%" />
          <SortableHeader cssClass="title4" field="status" :label="$t('list_label_status')"  :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort"  width="9%" />
          <SortableHeader cssClass="title5" field="lastest_start_time" :label="$t('list_lastest_execution_time')"  :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort"  width="17%" />
          <SortableHeader cssClass="title6" :label="$t('list_label_actions')"  width="22%" :sortable="false" />
        </tr>
      </thead>
      <tbody>
        <tr v-for="post in paginatedPosts" :key="post.detail_id">
          <td>{{ post.schedule_id }}</td>
          <td>{{ post.schedule_group_name }}</td>
          <td>{{ post.task_name }}</td>
          <td>{{ post.status }}</td>
          <td>{{ post.lastest_start_time }}</td>
          <td>
            <div class="button-group">
              <button class="edit-button" @click="openEditModal(post)">
                <i class="ph ph-pencil-simple"></i> {{ $t('btn_edit') }}
              </button>
              <button class="delete-button" @click="deleteRecord(post.detail_id)">
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
      :title="isEditMode ? $t('list_label_task') + ' ' + $t('btn_edit') : $t('list_label_task') + ' ' + $t('btn_add')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveTasks"
    >
      <div class="modal-form grid-form">

        <div class="form-field">
          <label>{{ $t('schedule_name') }}</label>
          <SearchableSelect v-model="formControl.schedule_id" :items="scheduleList" label-key="name" value-key="schedule_id" :placeholder="$t('schedule_name') + $t('msg_enter')"/>
        </div>

        <div class="form-field">
          <label>{{ $t('task_name') }}</label>
          <input type="text" v-model="formControl.task_name" :placeholder="$t('task_command') + $t('msg_enter')" />
        </div>

        <div class="form-field">
          <label>{{ $t('task_command') }}</label>
          <input type="text" v-model="formControl.command" :placeholder="$t('task_command') + $t('msg_enter')" />
        </div>

        <div class="form-group-inline">
          <div class="form-field">
            <label>{{ $t('task_type') }}</label>
            <select v-model="formControl.task_type">
              <option value="command">{{ $t('task_type_command') }}</option>
              <option value="archive">{{ $t('task_type_archive') }}</option>
              <option value="copy">{{ $t('task_type_copy') }}</option>
              <option value="housekeep">{{ $t('task_type_housekeep') }}</option>
            </select>
          </div>

          <div class="form-field">
            <label>{{ $t('task_archive_type') }}</label>
            <select v-model="formControl.archive_type">
              <option value="null">{{ $t('task_archive_type_null') }}</option>
              <option value="zip">{{ $t('task_archive_type_zip') }}</option>
            </select>
          </div>
        </div>

        <div class="form-group-inline">
          <div class="form-field">
            <label>{{ $t('task_error_on_missing_source') }}</label>
            <select v-model="formControl.error_on_missing_source">
              <option value="1">{{ $t('list_yes') }}</option>
              <option value="0">{{ $t('list_no') }}</option>
            </select>
          </div>

          <div class="form-field">
            <label>{{ $t('task_sequence') }}</label>
            <input type="number" v-model="formControl.sequence" :placeholder="$t('task_sequence') + $t('msg_enter')" min="0" max="999" @input="formControl.sequence = $event.target.value.slice(0, 3)" />
          </div>
        </div>

        <div class="form-field">
          <label>{{ $t('task_source_path') }}</label>
          <input type="text" v-model="formControl.source_path" :placeholder="$t('task_source_path') + $t('msg_enter')" />
        </div>

        <div class="form-field">
          <label>{{ $t('task_destination_path') }}</label>
          <input type="text" v-model="formControl.destination_path" :placeholder="$t('task_destination_path') + $t('msg_enter')" />
        </div>


        <div class="form-field">
          <label>{{ $t('task_house_keep_days') }}</label>
          <input type="number" v-model="formControl.house_keep_days" :placeholder="$t('task_house_keep_days') + $t('msg_enter')" min="0" max="999" @input="formControl.house_keep_days = $event.target.value.slice(0, 3)" />
        </div>


          <div class="form-field">
            <label>{{ $t('task_status') }}</label>
            <select v-model="formControl.status">
              <option value="active">{{ $t('label_active') }}</option>
              <option value="inactive">{{ $t('label_inactive') }}</option>
              <option value="manual">{{ $t('label_manual') }}</option>
            </select>
          </div>

        <input type="hidden" v-model="formControl.detail_id" />
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
          <label>{{ formControlManualRun.schedule_group_name }}</label>
        </div>

        <div class="form-field">
          <label>{{ $t('task_name') }}</label>
          <label>{{ formControlManualRun.task_name }}</label>
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
import { getRequest, postRequest, putRequest, deleteRequest, useSort } from "@api";
import ModalComponent from "../../misc/ModalComponent.vue"; // ✅ 공통 모달 컴포넌트
import BoardPagination from "../../misc/BoardPagination.vue";
import { useI18n } from "vue-i18n";
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
});
const scheduleList = ref([]);

// ✅ 스케줄 목록 가져오기
const fetchScheduleList = async () => {
  try {
    const response = await getRequest("/dashboard/tasks/get_schedule_groups", { group_id: '%' });
    scheduleList.value = response || [];
  } catch (error) {
    console.error("스케줄 리스트 가져오기 실패:", error);
  }
};

// ✅ 태스크 목록 가져오기
const fetchTasks = async () => {
  try {
    const response = await getRequest("/dashboard/tasks/get_tasks", { 'schedule_id': 0 });
    posts.value = response || [];

    await fetchScheduleList(); // ✅ 디바이스 목록도 불러오기

  } catch (error) {
    console.error("데이터 가져오기 실패:", error);
  } finally {
    isLoading.value = false;
  }
};

onMounted(fetchTasks);

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
    archive_type: "null",
    task_type: "command",
    error_on_missing_source: "1",
    creator: userId.value || "Guest",  // ✅ 수정
    modifier: userId.value || "Guest"  // ✅ 수정
  });
  isModalOpen.value = true;
};

// ✅ 그룹 수정 모달 열기
const openEditModal = (schedule) => {
  isEditMode.value = true;
  Object.assign(formControl.value, schedule, {
    is_immediate: "1",
    modifier: userId.value || "Guest",  // ✅ 수정
    creator: schedule.creator || userId.value  // 기존 creator 유지
  });
  isModalOpen.value = true;
};

// ✅ 그룹 저장 (추가 또는 수정)
const saveTasks = async () => {
  try {
    // ✅ 요청 데이터 확인 (디버깅)
    console.log("전송 데이터:", JSON.stringify(formControl.value, null, 2));
    if (isEditMode.value) {
      await putRequest(`/dashboard/tasks/update_task`, formControl.value, "json");
    } else {
      await postRequest(`/dashboard/tasks/insert_task`, formControl.value, "json");
    }
    await fetchTasks();
    closeModal();
  } catch (error) {
    console.error("그룹 저장 실패:", error);
  }
};


// ✅ 그룹 삭제
const deleteRecord = async (detail_id) => {
  if (confirm(t('msg_delete_task_name'))) {
    try {
      await deleteRequest(`/dashboard/tasks/remove_task/${detail_id}`);
      await fetchTasks();
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
    await postRequest(`/dashboard/tasks/insert_manual_task`, formControlManualRun.value, "json");
    closeManualRunModal();
  } catch (error) {
    console.error("그룹 저장 실패:", error);
  }
};

// 필터링 상태 추가
const selectedSchedule = ref("");
const selectedTask = ref("");
const selectedStatus = ref("");

// 유니크한 스케줄 리스트
const uniqueSchedules = computed(() => {
  return [...new Set(posts.value.map(post => post.schedule_group_name))];
});

// 선택한 스케줄에 따라 필터링된 태스크 리스트
const filteredTasks = computed(() => {
  if (!selectedSchedule.value) {
    return [...new Set(posts.value.map(post => post.task_name))];
  }
  return [...new Set(posts.value.filter(post => post.schedule_group_name === selectedSchedule.value).map(post => post.task_name))];
});

// 유니크한 상태 리스트
const uniqueStatuses = computed(() => {
  return [...new Set(posts.value.map(post => post.status))];
});

// 필터링된 데이터
const filteredPosts = computed(() => {
  return posts.value.filter(post => {
    const matchesSchedule = selectedSchedule.value ? post.schedule_group_name === selectedSchedule.value : true;
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

watch([selectedSchedule, selectedTask, selectedStatus], () => {
  currentPage.value = 1;
});

</script>
