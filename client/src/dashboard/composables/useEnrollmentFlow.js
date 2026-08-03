import { computed, onMounted, onUnmounted, reactive, ref, unref } from "vue";
import {
  COPY_FEEDBACK_DURATION_MS,
  COUNTDOWN_TICK_MS,
  DEVICE_NAME_MAX_LEN,
  DEVICE_NAME_MIN_LEN,
  EXPIRY_WARNING_THRESHOLD_SEC,
  LIST_REFRESH_RETRY_DELAY_MS,
  LIST_REFRESH_RETRY_MAX,
  POLL_BACKOFF_MULTIPLIER,
  POLL_FAILURE_LIMIT,
  POLL_FIRST_DELAY_MS,
  POLL_INTERVAL_BASE_MS,
  POLL_INTERVAL_MAX_MS,
  POLL_MAX_DURATION_MS,
  POLL_MISSING_LIMIT,
  POLL_PAUSE_WHEN_HIDDEN,
  TOKEN_REVEAL_AUTO_MASK_MS,
  TTL_DEFAULT,
  TTL_MAX,
  TTL_MIN,
  TTL_OPTIONS,
} from "@/dashboard/constants/enrollment";
import { issueEnrollment, listEnrollments, mapError } from "@/dashboard/api/enrollment";
import { parseServerTime } from "@/dashboard/utils/deviceStatus";

export function useEnrollmentFlow(options = {}) {
  const flowState = ref("idle");
  const form = reactive({ groupId: null, deviceName: "", ttlHours: TTL_DEFAULT });
  const formErrors = ref({});
  const vault = ref(null);
  const enrollmentId = ref(null);
  const revealed = ref(false);
  const remainingSeconds = ref(0);
  const watchState = ref("idle");
  const listReflectState = ref(null);
  const notice = ref(null);
  const copyFeedback = ref(null);
  const executionMethod = ref("interactive");

  let revealTimer = null;
  let countdownTimer = null;
  let pollTimer = null;
  let copyTimer = null;
  let refreshTimer = null;
  let watchStartedAt = 0;
  let pollInterval = POLL_INTERVAL_BASE_MS;
  let consecutiveFailures = 0;
  let consecutiveMissing = 0;
  let disposed = false;

  const groups = () => {
    const value = unref(options.groups);
    return Array.isArray(value) ? value : [];
  };
  const currentUserGroupId = () => unref(options.userGroupId);
  const usesImplicitHiddenGroup = () => (
    Number(currentUserGroupId()) === 0
    && groups().length === 0
    && Number(form.groupId) === 0
  );

  function clearTimer(timer) {
    if (timer != null) window.clearTimeout(timer);
  }

  function discardVault() {
    clearTimer(revealTimer);
    clearTimer(countdownTimer);
    clearTimer(pollTimer);
    clearTimer(copyTimer);
    clearTimer(refreshTimer);
    revealTimer = null;
    countdownTimer = null;
    pollTimer = null;
    copyTimer = null;
    refreshTimer = null;
    revealed.value = false;
    vault.value = null;
  }

  function validateForm() {
    const errors = {};
    const name = String(form.deviceName || "").trim();
    if (name.length < DEVICE_NAME_MIN_LEN) errors.deviceName = "msg_device_name_required";
    else if (name.length > DEVICE_NAME_MAX_LEN) errors.deviceName = "msg_device_name_too_long";
    if (
      form.groupId == null
      || (
        !usesImplicitHiddenGroup()
        && !groups().some((group) => Number(group.group_id) === Number(form.groupId))
      )
    ) {
      errors.groupId = "msg_group_required";
    }
    if (!TTL_OPTIONS.includes(Number(form.ttlHours)) || form.ttlHours < TTL_MIN || form.ttlHours > TTL_MAX) {
      errors.ttlHours = "msg_ttl_out_of_range";
    }
    formErrors.value = errors;
    return errors;
  }

  const canIssue = computed(() => {
    if (flowState.value === "issuing") return false;
    return Object.keys(validateForm()).length === 0;
  });

  function resetForm() {
    const userGroupId = Number(currentUserGroupId());
    const availableGroups = groups();
    const preferred = availableGroups.find((group) => Number(group.group_id) === userGroupId);
    form.groupId = userGroupId === 0
      ? (availableGroups.length === 0 ? 0 : null)
      : (preferred ? preferred.group_id : null);
    form.deviceName = "";
    form.ttlHours = TTL_DEFAULT;
    formErrors.value = {};
    notice.value = userGroupId !== 0 && !preferred
      ? { key: "msg_user_group_unavailable", params: {} }
      : null;
    executionMethod.value = "interactive";
  }

  function openFlow() {
    resetForm();
    flowState.value = "form";
  }

  function updateRemaining() {
    if (!vault.value) return;
    const expiry = parseServerTime(vault.value.expiresAt);
    remainingSeconds.value = expiry
      ? Math.max(0, Math.floor((expiry.getTime() - Date.now()) / 1000))
      : 0;
    if (remainingSeconds.value === 0) {
      flowState.value = "expired";
      watchState.value = "expired";
      discardVault();
      return;
    }
    countdownTimer = window.setTimeout(updateRemaining, COUNTDOWN_TICK_MS);
  }

  async function issue() {
    if (flowState.value === "issuing") return;
    if (!canIssue.value) return;
    discardVault();
    enrollmentId.value = null;
    notice.value = null;
    listReflectState.value = null;
    flowState.value = "issuing";
    const deviceName = String(form.deviceName).trim();
    form.deviceName = deviceName;
    try {
      const api = options.issueEnrollment || issueEnrollment;
      const data = await api({ groupId: form.groupId, deviceName, ttlHours: Number(form.ttlHours) });
      if (!data || !data.token) {
        enrollmentId.value = data && data.enrollment_id ? data.enrollment_id : null;
        notice.value = { key: "err_issue_no_token", params: {} };
        flowState.value = "form";
        if (options.onHistoryChange) options.onHistoryChange();
        return;
      }
      enrollmentId.value = data.enrollment_id;
      vault.value = {
        token: data.token,
        enrollmentId: data.enrollment_id,
        expiresAt: data.expires_at,
      };
      remainingSeconds.value = 0;
      revealed.value = false;
      flowState.value = "delivering";
      watchState.value = "idle";
      updateRemaining();
      if (options.onHistoryChange) options.onHistoryChange();
    } catch (error) {
      const mapped = mapError(error);
      notice.value = mapped.messageKey ? { key: mapped.messageKey, params: mapped.params } : null;
      if (mapped.status === 403) {
        flowState.value = "forbidden";
        if (options.onForbidden) options.onForbidden(mapped);
      } else {
        flowState.value = "form";
      }
      if (mapped.action === "reload_groups" && options.reloadGroups) options.reloadGroups();
    }
  }

  function reveal() {
    if (!vault.value) return;
    revealed.value = true;
    clearTimer(revealTimer);
    revealTimer = window.setTimeout(hide, TOKEN_REVEAL_AUTO_MASK_MS);
  }

  function hide() {
    revealed.value = false;
    clearTimer(revealTimer);
    revealTimer = null;
  }

  async function copy(text) {
    clearTimer(copyTimer);
    try {
      await navigator.clipboard.writeText(String(text || ""));
      copyFeedback.value = "success";
    } catch (error) {
      copyFeedback.value = "failed";
    }
    copyTimer = window.setTimeout(() => {
      copyFeedback.value = null;
    }, COPY_FEEDBACK_DURATION_MS);
  }

  function schedulePoll(delay) {
    clearTimer(pollTimer);
    pollTimer = window.setTimeout(pollOnce, delay);
  }

  async function reflectDeviceList() {
    if (
      Number(currentUserGroupId()) !== 0
      && Number(form.groupId) !== Number(currentUserGroupId())
    ) {
      listReflectState.value = "out_of_scope";
      return;
    }
    let attempt = 0;
    while (attempt <= LIST_REFRESH_RETRY_MAX && !disposed) {
      try {
        const result = options.refreshDevices ? await options.refreshDevices(form.deviceName) : null;
        if (result && result.found) {
          listReflectState.value = "done";
          return;
        }
        listReflectState.value = "not_yet";
      } catch (error) {
        listReflectState.value = "failed";
      }
      if (attempt < LIST_REFRESH_RETRY_MAX) {
        await new Promise((resolve) => {
          refreshTimer = window.setTimeout(resolve, LIST_REFRESH_RETRY_DELAY_MS);
        });
      }
      attempt += 1;
    }
  }

  function completeWith(status) {
    watchState.value = status;
    flowState.value = status;
    discardVault();
    if (options.onHistoryChange) options.onHistoryChange();
    if (status === "succeeded") reflectDeviceList();
  }

  async function pollOnce() {
    pollTimer = null;
    if (disposed || flowState.value !== "waiting") return;
    if (POLL_PAUSE_WHEN_HIDDEN && document.hidden) {
      schedulePoll(POLL_INTERVAL_BASE_MS);
      return;
    }
    if (Date.now() - watchStartedAt >= POLL_MAX_DURATION_MS) {
      watchState.value = "watchPaused";
      flowState.value = "watchPaused";
      return;
    }
    try {
      const api = options.listEnrollments || listEnrollments;
      const items = await api(form.groupId);
      consecutiveFailures = 0;
      pollInterval = POLL_INTERVAL_BASE_MS;
      const item = items.find((entry) => String(entry.enrollment_id) === String(enrollmentId.value));
      if (!item) {
        consecutiveMissing += 1;
        if (consecutiveMissing >= POLL_MISSING_LIMIT) {
          watchState.value = "watchFailed";
          flowState.value = "watchFailed";
          return;
        }
        schedulePoll(pollInterval);
        return;
      }
      consecutiveMissing = 0;
      if (item.status === "used") completeWith("succeeded");
      else if (item.status === "expired") completeWith("expired");
      else if (item.status === "revoked") completeWith("revoked");
      else schedulePoll(pollInterval);
    } catch (error) {
      const mapped = mapError(error);
      if (mapped.status === 401) return;
      if (mapped.status === 403) {
        watchState.value = "forbidden";
        flowState.value = "forbidden";
        discardVault();
        if (options.onForbidden) options.onForbidden(mapped);
        return;
      }
      consecutiveFailures += 1;
      if (consecutiveFailures >= POLL_FAILURE_LIMIT) {
        watchState.value = "watchFailed";
        flowState.value = "watchFailed";
        return;
      }
      pollInterval = Math.min(pollInterval * POLL_BACKOFF_MULTIPLIER, POLL_INTERVAL_MAX_MS);
      schedulePoll(pollInterval);
    }
  }

  function startWatch() {
    if (!enrollmentId.value) return;
    clearTimer(pollTimer);
    flowState.value = "waiting";
    watchState.value = "waiting";
    watchStartedAt = Date.now();
    pollInterval = POLL_INTERVAL_BASE_MS;
    consecutiveFailures = 0;
    consecutiveMissing = 0;
    schedulePoll(POLL_FIRST_DELAY_MS);
  }

  function recheck() {
    startWatch();
  }

  function reissue() {
    discardVault();
    enrollmentId.value = null;
    listReflectState.value = null;
    notice.value = null;
    flowState.value = "form";
  }

  function closeFlow() {
    const wasWaiting = flowState.value === "waiting";
    discardVault();
    enrollmentId.value = null;
    flowState.value = "idle";
    watchState.value = "idle";
    if (wasWaiting) notice.value = { key: "enroll_watch_desc", params: {} };
  }

  function setForbidden() {
    flowState.value = "forbidden";
    watchState.value = "forbidden";
    discardVault();
  }

  function markRevoked(id) {
    if (String(id) === String(enrollmentId.value)) completeWith("revoked");
  }

  function onVisibilityChange() {
    if (!document.hidden && flowState.value === "waiting") {
      pollInterval = POLL_INTERVAL_BASE_MS;
      schedulePoll(POLL_FIRST_DELAY_MS);
    }
  }

  onMounted(() => {
    window.addEventListener("beforeunload", discardVault);
    document.addEventListener("visibilitychange", onVisibilityChange);
  });

  onUnmounted(() => {
    disposed = true;
    window.removeEventListener("beforeunload", discardVault);
    document.removeEventListener("visibilitychange", onVisibilityChange);
    discardVault();
  });

  return {
    flowState,
    form,
    formErrors,
    vault,
    enrollmentId,
    revealed,
    remainingSeconds,
    watchState,
    listReflectState,
    notice,
    copyFeedback,
    executionMethod,
    expiryWarning: computed(() => remainingSeconds.value <= EXPIRY_WARNING_THRESHOLD_SEC),
    canIssue,
    openFlow,
    resetForm,
    validateForm,
    issue,
    reveal,
    hide,
    copy,
    startWatch,
    recheck,
    reissue,
    closeFlow,
    discardVault,
    setForbidden,
    markRevoked,
  };
}
