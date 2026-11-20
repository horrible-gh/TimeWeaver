import { createRouter, createWebHistory } from "vue-router";
import { watch } from "vue";
import DashboardForm from "@/dashboard/components/contents/dashboard/DashboardOverview.vue";  // Main page
import ScheduleHistoryForm from "@/dashboard/components/contents/history/ScheduleHistory.vue";  // Main page
import GroupsOverviewForm from "@/dashboard/components/contents/groups/GroupsOverview.vue";  // Main page
import DevicesOverviewForm from "@/dashboard/components/contents/devices/DevicesOverview.vue";  // Main page
import SchedulesOverviewForm from "@/dashboard/components/contents/schedules/SchedulesOverview.vue";  // Main page
import TasksOverviewForm from "@/dashboard/components/contents/tasks/TasksOverview.vue";  // Main page
import ManualExecutionOverviewForm from "@/dashboard/components/contents/manual_execution/ManualExecutionHistory.vue";  // Main page
import { ensureSession, logout, sessionMode, sessionUnavailable, sessionUser } from "@api";
import { isUnauthenticated, isSessionChanged } from "../../../session.mjs";

import UsersOverview from "@/dashboard/components/contents/users/UsersOverview.vue";

const routes = [
  ...(sessionMode ? [{ path: "/users", component: UsersOverview, meta: { admin: true } }] : []),
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

let failedSessionPath = null;
router.beforeEach(async (to) => {
  // A new navigation replaces any earlier failed destination.
  failedSessionPath = null;
  if (to.path === "/logout") {
    await logout(); window.location.href = "/login"; return false;
  }
  if (sessionMode) {
    try {
      const current = await ensureSession();
      if (current.user.must_change_password) {
        window.location.href = "/login?password=required"; return false;
      }
      if (to.meta.admin && current.user.role !== "admin") return { path: "/groups" };
      return true;
    } catch (error) {
      if (!isUnauthenticated(error) && !isSessionChanged(error)) failedSessionPath = to.fullPath;
      return false;
    }
  }
  if (!localStorage.getItem("access_token")) {
    window.location.href = "/login"; return false;
  }
  return true;
});

if (sessionMode) watch(sessionUnavailable, unavailable => {
  if (!unavailable && sessionUser.value && failedSessionPath) {
    // A wake check can recover after the first route was aborted offline.
    // Resume only that requested route; existing mounted forms stay in place.
    const path = failedSessionPath;
    failedSessionPath = null;
    router.replace(path).catch(() => {});
  }
});

export default router;

