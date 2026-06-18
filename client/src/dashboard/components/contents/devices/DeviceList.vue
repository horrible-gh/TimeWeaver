<template>
  <div class="board-container">
    <h2>{{ $t('sub_devices') }}</h2>

    <!-- ✅ 장치 추가 버튼 -->
    <button class="add-button" @click="openAddDeviceModal">
      <i class="ph ph-plus"></i> {{ $t('btn_add') }}
    </button>

    <!-- ✅ 장치치 목록 -->
    <table v-if="paginatedPosts.length > 0" class="board-table">
      <thead>
        <tr>
          <th class="title1" @click="sort('device_id')">ID <span v-if="sortKey === 'device_id'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title2" @click="sort('device_name')">{{ $t('list_label_device') }} <span v-if="sortKey === 'device_name'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title3" @click="sort('status')">{{ $t('list_label_status') }} <span v-if="sortKey === 'status'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title4" @click="sort('version')">{{ $t('list_label_version') }} <span v-if="sortKey === 'version'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title5" @click="sort('last_login_at')">{{ $t('list_label_last_login_at') }} <span v-if="sortKey === 'last_login_at'">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th>
          <th class="title6">{{ $t('list_label_actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="post in paginatedPosts" :key="post.device_id">
          <td>{{ post.device_id }}</td>
          <td>{{ post.device_name }}</td>
          <td>{{ post.status }}</td>
          <td>{{ post.version }}</td>
          <td>{{ post.last_login_at }}</td>
          <td>
            <div class="button-group">
              <button class="edit-button" @click="openEditDeviceModal(post)">
                <i class="ph ph-pencil-simple"></i> {{ $t('btn_edit') }}
              </button>
              <button class="delete-button" @click="deleteDevice(post.device_id)">
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
      :title="isEditMode ? $t('list_label_device') + ' ' + $t('btn_edit') : $t('list_label_device') + ' ' + $t('btn_add')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveDevice"
    >
      <div class="modal-form">
        <label>{{ $t('list_label_device') }}</label>
        <input type="text" v-model="formdevice.device_name" :placeholder="$t('msg_enter_device_name')" />

        <label>{{ $t('list_label_status') }}</label>
        <select v-model="formdevice.status">
          <option value="active">{{ $t('label_active') }}</option>
          <option value="inactive">{{ $t('label_inactive') }}</option>
        </select>

        <input type="hidden" v-model="formdevice.creator" />
        <input type="hidden" v-model="formdevice.modifier" />
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

// const searchdevice = ref("");
// const selectedStatus = ref("");

// ✅ 모달 상태
const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ 안전하게 변환
const user_id = user.user_id; // ✅ 이제 정상적으로 사용 가능!
const isModalOpen = ref(false);
const isEditMode = ref(false);
const formdevice = ref({ group_id : 0, device_name: "", status: "active",  creator: user_id });

// ✅ 그룹 목록 가져오기
const fetchDevices = async () => {
  try {
    const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ 안전하게 변환
    const group_id = user.group_id;

    console.log("📌 group_id 값:", group_id); // ✅ group_id 값 확인 (디버깅)

    if (!group_id && group_id != 0) {
      console.error("🚨 group_id가 undefined 또는 null입니다.");
      return;
    }

    const response = await getRequest("/dashboard/devices/get_devices", { group_id }); // ✅ key-value 형식 전달
    posts.value = response || [];
  } catch (error) {
    console.error("데이터 가져오기 실패:", error);
  } finally {
    isLoading.value = false;
  }
};


onMounted(fetchDevices);

// ✅ 그룹 추가 모달 열기
const openAddDeviceModal = () => {
  isEditMode.value = false;
  Object.assign(formdevice.value, {
    device_name: "",
    status: "active",
    creator: user_id,
    modifier: user_id
  });
  isModalOpen.value = true;
};

// ✅ 그룹 수정 모달 열기
const openEditDeviceModal = (device) => {
  isEditMode.value = true;
  Object.assign(formdevice.value, device, {
    modifier: user_id,
    creator: device.creator || user_id // 기존 creator 유지
  });
  isModalOpen.value = true;
};

// ✅ 그룹 저장 (추가 또는 수정)
const saveDevice = async () => {
  try {
    // ✅ 요청 데이터 확인 (디버깅)
    console.log("전송 데이터:", JSON.stringify(formdevice.value, null, 2));
    if (isEditMode.value) {
      await putRequest(`/dashboard/devices/update_device`, formdevice.value, "json");
    } else {
      await postRequest(`/dashboard/devices/insert_device`, formdevice.value, "json");
    }
    await fetchDevices();
    closeModal();
  } catch (error) {
    console.error("그룹 저장 실패:", error);
  }
};


// ✅ 그룹 삭제
const deleteDevice = async (deviceId) => {
  if (confirm(t('msg_delete_device_name'))) {
    try {
      await deleteRequest(`/dashboard/devices/remove_device/${deviceId}`);
      await fetchDevices();
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
  width:5%
}

.title2 {
  width:20%
}

.title3 {
  width:12%
}

.title4 {
  width:20%
}

.title5 {
  width:20%
}

.title6 {
  width:10%
}

</style>
