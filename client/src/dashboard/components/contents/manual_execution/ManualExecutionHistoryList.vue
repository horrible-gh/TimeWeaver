<template>
  <div class="board-container">
    <h2>{{ $t('sub_manual_execution') }}</h2>

    <!-- ✅ 검색 필터 -->
    <div class="filters">
      <div>
        <label>{{ $t('list_label_group') }}:</label>
        <select v-model="selectedGroup">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="group in uniqueGroups" :key="group" :value="group">{{ group }}</option>
        </select>
      </div>
    
      <div>
        <label>{{ $t('list_label_schedule_name') }}:</label>
        <select v-model="selectedSchedule">
          <option value="">{{ $t('select_box_all') }}</option>
          <option v-for="schedule in filteredSchedules" :key="schedule" :value="schedule">{{ schedule }}</option>
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

    <!-- ✅ 필터링된 테이블 -->
    <table v-if="filteredPosts.length > 0" class="board-table">
      <thead>
        <tr>
          <th class="title1" @click="sort('manual_id')">ID <span v-if="sortKey === 'manual_id'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title2" @click="sort('name')">{{ $t('list_label_group') }} <span v-if="sortKey === 'name'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title3" @click="sort('schedule_name')">{{ $t('list_label_schedule_name') }} <span v-if="sortKey === 'schedule_name'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title4" @click="sort('is_immediate')">{{ $t('manual_run_method') }} <span v-if="sortKey === 'is_immediate'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title5" @click="sort('schedule_datetime')">{{ $t('manual_run_set_time') }} <span v-if="sortKey === 'schedule_datetime'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title6" @click="sort('status')">{{ $t('list_label_status') }} <span v-if="sortKey === 'status'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title7">{{ $t('list_label_actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="post in paginatedPosts" :key="post.manual_id">
          <td>{{ post.manual_id }}</td>
          <td>{{ post.name }}</td>
          <td>{{ post.schedule_name }}</td>
          <td>{{ post.is_immediate === 1 ? $t('manual_run_immediate_yes') : $t('manual_run_immediate_no') }}</td>
          <td>{{ formatDate(post.schedule_datetime) }}</td>
          <td>{{ post.status }}</td>
          <td>
            <button v-if="post.status === 'processing'" class="abandon-button" @click="abandonManualExecution(post)">
              <i class="ph ph-x-circle"></i> {{ $t('btn_abandon') }}
            </button>
            <button v-else-if="post.edit_enable === '1'" class="edit-button" @click="openEditModal(post)">
              <i class="ph ph-pencil-simple"></i> {{ $t('btn_edit') }}
            </button>
            <span v-else>-</span>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- ✅ 모달 컴포넌트 사용 -->
    <ModalComponent
      :isOpen="isModalOpen"
      :title="$t('manual_run_title') + ' ' + $t('btn_edit')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveManualExecution"
    >
      <div class="modal-form grid-form">
        <div class="form-field">
          <label>{{ $t('list_label_group') }}</label>
          <input type="text" v-model="formControl.name" disabled />
        </div>
      
        <div class="form-field">
          <label>{{ $t('list_label_schedule_name') }}</label>
          <input type="text" v-model="formControl.schedule_name" disabled />
        </div>
      
        <div class="form-field">
          <label>{{ $t('manual_run_method') }}</label>
          <select v-model="formControl.is_immediate">
            <option value="0">{{ $t('manual_run_immediate_no') }}</option>
            <option value="1">{{ $t('manual_run_immediate_yes') }}</option>
          </select>
        </div>
      
        <div class="form-field">
          <label>{{ $t('manual_run_set_time') }}</label>
          <input type="datetime-local" v-model="formControl.schedule_datetime" :disabled="formControl.is_immediate === '1'">
        </div>
      
        <div class="form-field">
          <label>{{ $t('list_label_status') }}</label>
          <select v-model="formControl.status">
            <option value="active">{{ $t('manual_run_status_active') }}</option>
            <option value="wait">{{ $t('manual_run_status_wait') }}</option>
            <option value="inactive">{{ $t('label_inactive') }}</option>
          </select>
        </div>
      
        <input type="hidden" v-model="formControl.manual_id" />
        <input type="hidden" v-model="formControl.modifier" />
      </div>
    </ModalComponent>

    <BoardPagination :total="filteredPosts.length" :perPage="perPage" @page-changed="changePage" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue';
import { getRequest, putRequest } from "@api";
import BoardPagination from '../../misc/BoardPagination.vue';
import ModalComponent from "../../misc/ModalComponent.vue";
import { useI18n } from "vue-i18n";
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

// 수정 모달 열기
const openEditModal = (post) => {
  Object.assign(formControl.value, post, {
    modifier: userId.value
  });
  
  // datetime-local 형식으로 변환
  if (post.schedule_datetime) {
    const date = new Date(post.schedule_datetime);
    formControl.value.schedule_datetime = new Date(date.getTime() - date.getTimezoneOffset() * 60000)
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

</script>

<style scoped>
.title1 { width: 8%; }
.title2 { width: 18%; }
.title3 { width: 18%; }
.title4 { width: 12%; }
.title5 { width: 18%; }
.title6 { width: 12%; }
.title7 { width: 14%; }
</style>
