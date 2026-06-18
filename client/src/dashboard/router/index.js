import { createRouter, createWebHistory } from "vue-router";
import DashboardForm from "@/dashboard/components/contents/dashboard/DashboardOverview.vue";  // 메인 페이지
import ScheduleHistoryForm from "@/dashboard/components/contents/history/ScheduleHistory.vue";  // 메인 페이지
import GroupsOverviewForm from "@/dashboard/components/contents/groups/GroupsOverview.vue";  // 메인 페이지
import DevicesOverviewForm from "@/dashboard/components/contents/devices/DevicesOverview.vue";  // 메인 페이지
import SchedulesOverviewForm from "@/dashboard/components/contents/schedules/SchedulesOverview.vue";  // 메인 페이지
import { postRequest } from "@api";

const routes = [
  { path: "/", component: DashboardForm },
  { path: "/schedule-history", component: ScheduleHistoryForm },
  { path: "/groups", component: GroupsOverviewForm },
  { path: "/devices", component: DevicesOverviewForm },
  { path: "/schedules", component: SchedulesOverviewForm },
];

const router = createRouter({
  history: createWebHistory("/dashboard/"),
  routes,
});

router.beforeEach((to, from, next) => {
  const isAuthenticated = !!localStorage.getItem("access_token");
  console.log('[Router] 이동:', from.fullPath, '→', to.fullPath, 'Auth:', isAuthenticated);

  if (to.path === "/logout") {
    postRequest("/logout");
    localStorage.clear();
    sessionStorage.clear();
    window.location.href = "/login";
  } else if (!isAuthenticated && to.path !== "/login") {
    alert("로그인이 필요합니다.");
    window.location.href = "/login";
  } else {
    next();
  }
});



export default router;

