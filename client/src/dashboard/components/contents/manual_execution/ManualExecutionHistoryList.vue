<template>
  <div class="panel">
    <div class="panel-head">
      <div class="panel-title">
        <h2>{{ $t('manual_execution_list_title') }}</h2>
        <p>{{ $t('manual_execution_list_desc') }}</p>
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
        <select class="select" v-model="selectedStatus" :aria-label="$t('list_label_status')">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="status in uniqueStatuses" :key="status" :value="status">{{ status }}</option>
        </select>
        <button class="btn" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
      </div>
    </div>

    <div class="table-scroll">
      <table v-if="paginatedPosts.length > 0">
        <thead>
          <tr>
            <SortableHeader field="manual_id" label="ID" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="name" :label="$t('list_label_group')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="schedule_name" :label="$t('list_label_schedule_name')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="is_immediate" :label="$t('manual_run_method')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="schedule_datetime" :label="$t('manual_run_set_time')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="status" :label="$t('list_label_status')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader :label="$t('list_label_actions')" :sortable="false" />
          </tr>
        </thead>
        <tbody>
          <tr v-for="post in paginatedPosts" :key="post.manual_id">
            <td>{{ post.manual_id }}</td>
            <td>{{ post.name }}</td>
            <td>{{ post.schedule_name }}</td>
            <td>{{ post.is_immediate === 1 ? $t('manual_run_immediate_yes') : $t('manual_run_immediate_no') }}</td>
            <td>{{ formatDate(post.schedule_datetime) }}</td>
            <td><span class="badge" :class="statusBadgeClass(post.status)">{{ post.status }}</span></td>
            <td>
              <div class="row-actions">
                <button v-if="post.status === 'processing'" class="mini" :aria-label="$t('btn_abandon')" @click="abandonManualExecution(post)">
                  <i class="ph ph-x-circle"></i>
                </button>
                <button v-else-if="post.edit_enable === '1'" class="mini" :aria-label="$t('btn_edit')" @click="openEditModal(post)">
                  <i class="ph ph-pencil-simple"></i>
                </button>
                <span v-else>-</span>
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

    <!-- ✅ 모달 컴포넌트 사용 -->
    <ModalComponent
      :isOpen="isModalOpen"
      :title="$t('manual_run_title') + ' ' + $t('btn_edit')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveManualExecution"
    >
      <div class="form-grid">
        <div class="field full">
          <label>{{ $t('list_label_group') }}</label>
          <input type="text" v-model="formControl.name" disabled />
        </div>

        <div class="field full">
          <label>{{ $t('list_label_schedule_name') }}</label>
          <input type="text" v-model="formControl.schedule_name" disabled />
        </div>

        <div class="field half">
          <label>{{ $t('manual_run_method') }}</label>
          <select v-model="formControl.is_immediate">
            <option value="0">{{ $t('manual_run_immediate_no') }}</option>
            <option value="1">{{ $t('manual_run_immediate_yes') }}</option>
          </select>
        </div>

        <div class="field half">
          <label>{{ $t('list_label_status') }}</label>
          <select v-model="formControl.status">
            <option value="active">{{ $t('manual_run_status_active') }}</option>
            <option value="wait">{{ $t('manual_run_status_wait') }}</option>
            <option value="inactive">{{ $t('label_inactive') }}</option>
          </select>
        </div>

        <div class="field full">
          <label>{{ $t('manual_run_set_time') }}</label>
          <input type="datetime-local" v-model="formControl.schedule_datetime" :disabled="formControl.is_immediate === '1'">
        </div>

        <input type="hidden" v-model="formControl.manual_id" />
        <input type="hidden" v-model="formControl.modifier" />
      </div>
    </ModalComponent>

  </div>
</template>

<script>
import SortableHeader from "@/dashboard/components/misc/SortableHeader.vue";
export default {
    name: 'ManualExecutionHistoryList'
    , components: {
        SortableHeader,
    }
}
</script>

<script setup>
import { ref, computed, onMounted, watch } from 'vue';
import { getRequest, putRequest } from "@api";
import BoardPagination from '../../misc/BoardPagination.vue';
import ModalComponent from "../../misc/ModalComponent.vue";
import { useI18n } from "vue-i18n";
import { parseServerTime } from "@/dashboard/utils/deviceStatus";
const { t } = useI18n();

const posts = ref([]);
const isLoading = ref(true);
const currentPage = ref(1);
const perPage = ref(7);
const sortKey = ref('');
const sortOrder = ref('asc');

// 필터링 상태
const selectedGroup = ref("");
const selectedSchedule = ref("");
const selectedStatus = ref("");

// 모달 상태
const isModalOpen = ref(false);
const formControl = ref({});

// 유저 정보
const userId = ref("");

onMounted(() => {
  const user = JSON.parse(localStorage.getItem("user") || "{}");
  userId.value = user.name || "Guest";
});

// API에서 데이터 가져오기
const fetchPosts = async () => {
  try {
    const response = await getRequest("/dashboard/manual_execution/get_manual_execution_list");
    posts.value = response || [];
  } catch (error) {
    console.error("데이터를 가져오는 중 오류 발생:", error);
  } finally {
    isLoading.value = false;
  }
};

onMounted(fetchPosts);

// 날짜 변환 함수
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

// 수정 모달 열기
const openEditModal = (post) => {
  Object.assign(formControl.value, post, {
    modifier: userId.value
  });

  // datetime-local 형식으로 변환
  const parsedDatetime = parseServerTime(post.schedule_datetime);
  if (parsedDatetime) {
    formControl.value.schedule_datetime = new Date(parsedDatetime.getTime() - parsedDatetime.getTimezoneOffset() * 60000)
      .toISOString()
      .slice(0, 16);
  }

  isModalOpen.value = true;
};

// 저장
const saveManualExecution = async () => {
  try {
    await putRequest("/dashboard/manual_execution/update_manual_execution", formControl.value, "json");
    await fetchPosts();
    closeModal();
  } catch (error) {
    console.error("저장 실패:", error);
  }
};

// 모달 닫기
const closeModal = () => {
  isModalOpen.value = false;
};

// 필터 초기화
const resetFilters = () => {
  selectedGroup.value = "";
  selectedSchedule.value = "";
  selectedStatus.value = "";
  currentPage.value = 1;
};

// 정렬
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

// 유니크한 그룹 리스트
const uniqueGroups = computed(() => {
  return [...new Set(posts.value.map(post => post.name))];
});

// 필터링된 스케줄 리스트
const filteredSchedules = computed(() => {
  if (!selectedGroup.value) {
    return [...new Set(posts.value.map(post => post.schedule_name))];
  }
  return [...new Set(posts.value.filter(post => post.name === selectedGroup.value).map(post => post.schedule_name))];
});

// 유니크한 상태 리스트
const uniqueStatuses = computed(() => {
  return [...new Set(posts.value.map(post => post.status))];
});

// 필터링된 데이터
const filteredPosts = computed(() => {
  return posts.value.filter(post => {
    const matchesGroup = selectedGroup.value ? post.name === selectedGroup.value : true;
    const matchesSchedule = selectedSchedule.value ? post.schedule_name === selectedSchedule.value : true;
    const matchesStatus = selectedStatus.value ? post.status === selectedStatus.value : true;

    return matchesGroup && matchesSchedule && matchesStatus;
  });
});

// 페이징 처리
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

// 필터 변경시 페이지 리셋
watch([selectedGroup, selectedSchedule, selectedStatus], () => {
  currentPage.value = 1;
});


// ✅ 수동실행 포기
const abandonManualExecution = async (post) => {
  if (confirm(t('msg_abandon_manual_execution'))) {
    try {
      post.modifier = userId
      await putRequest("/dashboard/manual_execution/abandon_manual_execution", post, "json");
      await fetchPosts();
    } catch (error) {
      console.error("수동실행 업데이트 실패:", error);
    }
  }
};

// ✅ 상태값 → 배지 색 매핑 (active=online, wait=warning, inactive=offline, processing=info)
const statusBadgeClass = (status) => ({
  online: status === 'active',
  warning: status === 'wait',
  offline: status === 'inactive',
  info: status === 'processing',
});

</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7 / TR0017 DeviceList
  convention). `.panel` / `.panel-head` / `.toolbar` / `.select` / `.btn` /
  `.table-scroll` / `.badge` / `.row-actions` / `.mini` / `.panel-foot` /
  `.form-grid` / `.field` come from components.css. The pre-renewal
  `.board-container` / `.board-table` / `.filters` / `.reset-button` /
  `.edit-button` / `.abandon-button` / `.modal-form` / `.grid-form` /
  `.form-field` names are gone, so this screen no longer needs list.css.
-->
