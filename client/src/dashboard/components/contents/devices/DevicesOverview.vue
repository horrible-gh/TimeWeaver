<template>
  <div class="app-body-main-content">
    <DeviceList />
  </div>
</template>

<script>
import DeviceList from "@/dashboard/components/contents/devices/DeviceList.vue";

export default {
    name: 'DevicesOverview'
    , components: {
      DeviceList,
    }
}
</script>
