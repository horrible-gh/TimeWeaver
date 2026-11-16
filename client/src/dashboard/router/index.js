import { createRouter, createWebHistory } from "vue-router";
import DashboardForm from "@/dashboard/components/contents/dashboard/DashboardOverview.vue";  // Main page
import ScheduleHistoryForm from "@/dashboard/components/contents/history/ScheduleHistory.vue";  // Main page
import GroupsOverviewForm from "@/dashboard/components/contents/groups/GroupsOverview.vue";  // Main page
import DevicesOverviewForm from "@/dashboard/components/contents/devices/DevicesOverview.vue";  // Main page
import SchedulesOverviewForm from "@/dashboard/components/contents/schedules/SchedulesOverview.vue";  // Main page
import TasksOverviewForm from "@/dashboard/components/contents/tasks/TasksOverview.vue";  // Main page
import ManualExecutionOverviewForm from "@/dashboard/components/contents/manual_execution/ManualExecutionHistory.vue";  // Main page
import { ensureSession, logout, sessionMode } from "@api";

const routes = [
  { path: "/", component: DashboardForm },
  { path: "/schedule-history", component: ScheduleHistoryForm },
  { path: "/groups", component: GroupsOverviewForm },
  { path: "/devices", component: DevicesOverviewForm },
  { path: "/schedules", component: SchedulesOverviewForm },
  { path: "/tasks", component: TasksOverviewForm },
  { path: "/manual-execution", component: ManualExecutionOverviewForm },
];

const router = createRouter({
  history: createWebHistory("/dashboard/"),
  routes,
});

router.beforeEach(async (to) => {
  if (to.path === "/logout") {
    await logout(); window.location.href = "/login"; return false;
  }
  if (sessionMode) {
    try {
      const current = await ensureSession();
      if (current.user.must_change_password) {
        window.location.href = "/login?password=required"; return false;
      }
      return true;
    } catch { return false; }
  }
  if (!localStorage.getItem("access_token")) {
    window.location.href = "/login"; return false;
  }
  return true;
});

export default router;

