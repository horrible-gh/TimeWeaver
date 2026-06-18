<template>
  <div class="board-container">
    <h2>{{ $t('sub_schedules') }}</h2>

    <!-- ✅ 스케줄 추가 버튼 -->
    <button class="add-button" @click="openAddScheduleModal">
      <i class="ph ph-plus"></i> {{ $t('btn_add') }}
    </button>

    <!-- ✅ 스케줄 목록 -->
    <table v-if="paginatedPosts.length > 0" class="board-table">
      <thead>
        <tr>
          <th class="title1" @click="sort('group_id')">ID <span v-if="sortKey === 'group_id'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title2" @click="sort('group_name')">{{ $t('list_label_group') }} <span v-if="sortKey === 'group_name'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title3" @click="sort('status')">{{ $t('list_label_status') }} <span v-if="sortKey === 'status'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title4">{{ $t('list_label_actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="post in paginatedPosts" :key="post.group_id">
          <td>{{ post.group_id }}</td>
          <td>{{ post.group_name }}</td>
          <td>{{ post.status }}</td>
          <td>
            <div class="button-group">
              <button class="edit-button" @click="openEditGroupModal(post)">
                <i class="ph ph-pencil-simple"></i> {{ $t('btn_edit') }}
              </button>
              <button class="delete-button" @click="deleteGroup(post.group_id)">
                <i class="ph ph-trash"></i> {{ $t('btn_remove') }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <BoardPagination v-if="paginatedPosts.length > 0" :total="posts.length" :perPage="perPage" @page-changed="changePage" />

    <!-- ✅ 공통 모달 사용 -->
    <ModalComponent
      :isOpen="isModalOpen"
      :title="isEditMode ? $t('list_label_group') + ' ' + $t('btn_edit') : $t('list_label_group') + ' ' + $t('btn_add')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveGroup"
    >
      <div class="modal-form">
        <label>{{ $t('list_label_group') }}</label>
        <input type="text" v-model="formGroup.group_name" :placeholder="$t('msg_enter_group_name')" />

        <label>{{ $t('list_label_status') }}</label>
        <select v-model="formGroup.status">
          <option value="active">{{ $t('label_active') }}</option>
          <option value="inactive">{{ $t('label_inactive') }}</option>
        </select>

        <input type="hidden" v-model="formGroup.creator" />
        <input type="hidden" v-model="formGroup.modifier" />
      </div>
    </ModalComponent>

  </div>
</template>



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

// const searchGroup = ref("");
// const selectedStatus = ref("");

// ✅ 모달 상태
const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ 안전하게 변환
const user_id = user.user_id; // ✅ 이제 정상적으로 사용 가능!
const isModalOpen = ref(false);
const isEditMode = ref(false);
const formGroup = ref({ group_id : "", group_name: "", status: "active", creator: user_id,  modifier:user_id });

// ✅ 그룹 목록 가져오기
const fetchGroups = async () => {
  try {
    const response = await getRequest("/dashboard/groups/get_groups");
    posts.value = response || [];
  } catch (error) {
    console.error("데이터 가져오기 실패:", error);
  } finally {
    isLoading.value = false;
  }
};

onMounted(fetchGroups);

// ✅ 그룹 추가 모달 열기
const openAddScheduleModal = () => {
  isEditMode.value = false;
  Object.assign(formGroup.value, {
    group_name: "",
    status: "active",
    creator: user_id,
    modifier: user_id
  });
  isModalOpen.value = true;
};

// ✅ 그룹 수정 모달 열기
const openEditGroupModal = (group) => {
  isEditMode.value = true;
  Object.assign(formGroup.value, group, {
    modifier: user_id,
    creator: group.creator || user_id // 기존 creator 유지
  });
  isModalOpen.value = true;
};

// ✅ 그룹 저장 (추가 또는 수정)
const saveGroup = async () => {
  try {
    // ✅ 요청 데이터 확인 (디버깅)
    console.log("전송 데이터:", JSON.stringify(formGroup.value, null, 2));
    if (isEditMode.value) {
      await putRequest(`/dashboard/groups/update_group`, formGroup.value, "json");
    } else {
      await postRequest(`/dashboard/groups/insert_group`, formGroup.value, "json");
    }
    await fetchGroups();
    closeModal();
  } catch (error) {
    console.error("그룹 저장 실패:", error);
  }
};


// ✅ 그룹 삭제
const deleteGroup = async (groupId) => {
  if (confirm(t('msg_delete_group_name'))) {
    try {
      await deleteRequest(`/dashboard/groups/remove_group/${groupId}`);
      await fetchGroups();
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
  return posts.value.slice(start, start + perPage.value);
});
const changePage = (page) => {
  currentPage.value = page;
};

</script>


<style scoped>

.title1 {
  width:25%
}

.title2 {
  width:25%
}

.title3 {
  width:25%
}

.title4 {
  width:25%
}

</style>
