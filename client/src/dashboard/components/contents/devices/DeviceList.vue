<template>
  <div class="board-container">
    <h2>{{ $t('sub_devices') }}</h2>

    <!-- ✅ Add device button -->
    <button class="add-button" @click="openAddDeviceModal">
      <i class="ph ph-plus"></i> {{ $t('btn_add') }}
    </button>

    <!-- ✅ Device list -->
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

    <!-- ✅ Use shared modal -->
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
import ModalComponent from "../../misc/ModalComponent.vue"; // ✅ Shared modal component
import BoardPagination from "../../misc/BoardPagination.vue";

const { t } = useI18n(); // ✅ Get i18n function

const posts = ref([]); // ✅ Initial value is an empty array
const { sortKey, sortOrder, sort } = useSort(posts);
const isLoading = ref(true);
const currentPage = ref(1);
const perPage = ref(7);

// const searchdevice = ref("");
// const selectedStatus = ref("");

// ✅ Modal state
const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ Convert safely
const user_id = user.user_id; // ✅ Now usable
const isModalOpen = ref(false);
const isEditMode = ref(false);
const formdevice = ref({ group_id : 0, device_name: "", status: "active",  creator: user_id });

// ✅ Fetch group list
const fetchDevices = async () => {
  try {
    const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ Convert safely
    const group_id = user.group_id;

    console.log("📌 group_id value:", group_id); // ✅ Check group_id value for debugging

    if (!group_id && group_id != 0) {
      console.error("🚨 group_id is undefined or null.");
      return;
    }

    const response = await getRequest("/dashboard/devices/get_devices", { group_id }); // ✅ Pass key-value format
    posts.value = response || [];
  } catch (error) {
    console.error("Failed to fetch data:", error);
  } finally {
    isLoading.value = false;
  }
};


onMounted(fetchDevices);

// ✅ Open add group modal
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

// ✅ Open edit group modal
const openEditDeviceModal = (device) => {
  isEditMode.value = true;
  Object.assign(formdevice.value, device, {
    modifier: user_id,
    creator: device.creator || user_id // Keep existing creator
  });
  isModalOpen.value = true;
};

// ✅ Save group, add or update
const saveDevice = async () => {
  try {
    // ✅ Check request data for debugging
    console.log("Payload:", JSON.stringify(formdevice.value, null, 2));
    if (isEditMode.value) {
      await putRequest(`/dashboard/devices/update_device`, formdevice.value, "json");
    } else {
      await postRequest(`/dashboard/devices/insert_device`, formdevice.value, "json");
    }
    await fetchDevices();
    closeModal();
  } catch (error) {
    console.error("Failed to save group:", error);
  }
};


// ✅ Delete group
const deleteDevice = async (deviceId) => {
  if (confirm(t('msg_delete_device_name'))) {
    try {
      await deleteRequest(`/dashboard/devices/remove_device/${deviceId}`);
      await fetchDevices();
    } catch (error) {
      console.error("Delete group failure:", error);
    }
  }
};

// ✅ Close modal
const closeModal = () => {
  isModalOpen.value = false;
};

// ✅ Pagination
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
