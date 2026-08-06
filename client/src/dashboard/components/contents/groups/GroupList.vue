<template>
  <div class="panel">
    <div class="panel-head">
      <div class="panel-title">
        <h2>{{ $t('groups_list_title') }}</h2>
        <p>{{ $t('groups_list_desc') }}</p>
      </div>
      <div class="toolbar">
        <button class="btn primary" @click="openAddGroupModal">
          <i class="ph ph-plus"></i> {{ $t('btn_add') }}
        </button>
      </div>
    </div>

    <div class="table-scroll">
      <table v-if="paginatedPosts.length > 0">
        <thead>
          <tr>
            <SortableHeader field="group_id" label="ID" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="group_name" :label="$t('list_label_group')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader field="status" :label="$t('list_label_status')" :currentSortKey="sortKey" :sortOrder="sortOrder" :sort="sort" />
            <SortableHeader :label="$t('list_label_actions')" :sortable="false" />
          </tr>
        </thead>
        <tbody>
          <tr v-for="post in paginatedPosts" :key="post.group_id">
            <td>{{ post.group_id }}</td>
            <td>{{ post.group_name }}</td>
            <td><span class="badge" :class="statusBadgeClass(post.status)">{{ post.status }}</span></td>
            <td>
              <div class="row-actions">
                <button class="mini" :aria-label="$t('btn_edit')" @click="openEditGroupModal(post)">
                  <i class="ph ph-pencil-simple"></i>
                </button>
                <button class="mini" :aria-label="$t('btn_remove')" @click="deleteGroup(post.group_id)">
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
      <span>{{ $t('list_footer_range', { total: posts.length, start: rangeStart, end: rangeEnd }) }}</span>
      <BoardPagination :total="posts.length" :perPage="perPage" @page-changed="changePage" />
    </div>

    <!-- ✅ Use shared modal -->
    <ModalComponent
      :isOpen="isModalOpen"
      :title="isEditMode ? $t('list_label_group') + ' ' + $t('btn_edit') : $t('list_label_group') + ' ' + $t('btn_add')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveGroup"
    >
      <div class="form-grid">
        <div class="field full">
          <label>{{ $t('list_label_group') }}</label>
          <input type="text" v-model="formGroup.group_name" :placeholder="$t('msg_enter_group_name')" />
        </div>

        <div class="field full">
          <label>{{ $t('list_label_status') }}</label>
          <select v-model="formGroup.status">
            <option value="active">{{ $t('label_active') }}</option>
            <option value="inactive">{{ $t('label_inactive') }}</option>
          </select>
        </div>

        <input type="hidden" v-model="formGroup.creator" />
        <input type="hidden" v-model="formGroup.modifier" />
      </div>
    </ModalComponent>

  </div>
</template>



<script>
import SortableHeader from "@/dashboard/components/misc/SortableHeader.vue";
export default {
    name: 'GroupList'
    , components: {
        SortableHeader,
    }
}

</script>


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

// ✅ Modal state
const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ Convert safely
const user_id = user.user_id; // ✅ Now usable
const isModalOpen = ref(false);
const isEditMode = ref(false);
const formGroup = ref({ group_id : "", group_name: "", status: "active", creator: user_id,  modifier:user_id });

// ✅ Fetch group list
const fetchGroups = async () => {
  try {
    const response = await getRequest("/dashboard/groups/get_groups");
    posts.value = response || [];
  } catch (error) {
    console.error("Failed to fetch data:", error);
  } finally {
    isLoading.value = false;
  }
};

onMounted(fetchGroups);

// ✅ Open add group modal
const openAddGroupModal = () => {
  isEditMode.value = false;
  Object.assign(formGroup.value, {
    group_name: "",
    status: "active",
    creator: user_id,
    modifier: user_id
  });
  isModalOpen.value = true;
};

// ✅ Open edit group modal
const openEditGroupModal = (group) => {
  isEditMode.value = true;
  Object.assign(formGroup.value, group, {
    modifier: user_id,
    creator: group.creator || user_id // Keep existing creator
  });
  isModalOpen.value = true;
};

// ✅ Save group, add or update
const saveGroup = async () => {
  try {
    // ✅ Check request data for debugging
    console.log("Payload:", JSON.stringify(formGroup.value, null, 2));
    if (isEditMode.value) {
      await putRequest(`/dashboard/groups/update_group`, formGroup.value, "json");
    } else {
      await postRequest(`/dashboard/groups/insert_group`, formGroup.value, "json");
    }
    await fetchGroups();
    closeModal();
  } catch (error) {
    console.error("Failed to save group:", error);
  }
};


// ✅ Delete group
const deleteGroup = async (groupId) => {
  if (confirm(t('msg_delete_group_name'))) {
    try {
      await deleteRequest(`/dashboard/groups/remove_group/${groupId}`);
      await fetchGroups();
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

// ✅ Panel foot의 "총 N건 중 X–Y 표시" 범위
const rangeStart = computed(() => (posts.value.length === 0 ? 0 : (currentPage.value - 1) * perPage.value + 1));
const rangeEnd = computed(() => Math.min(currentPage.value * perPage.value, posts.value.length));

// ✅ 상태값 → 배지 색 매핑 (active=online, inactive=offline)
const statusBadgeClass = (status) => ({
  online: status === 'active',
  offline: status === 'inactive',
});

</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7 / TR0017 DeviceList
  convention). `.panel` / `.panel-head` / `.toolbar` / `.btn` / `.table-scroll`
  / `.badge` / `.row-actions` / `.mini` / `.panel-foot` / `.form-grid` / `.field`
  come from components.css. The pre-renewal `.board-container` / `.board-table`
  / `.add-button` / `.edit-button` / `.delete-button` / `.button-group` /
  `.modal-form` names are gone, so this screen no longer needs list.css.
-->
