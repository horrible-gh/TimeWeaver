export const TTL_DEFAULT = 24;
export const TTL_OPTIONS = [1, 8, 24, 72, 168];
export const TTL_MIN = 1;
export const TTL_MAX = 168;
export const DEVICE_NAME_MIN_LEN = 1;
export const DEVICE_NAME_MAX_LEN = 255;
export const GROUP_ID_MIN = 0;

export const POLL_INTERVAL_BASE_MS = 5000;
export const POLL_FIRST_DELAY_MS = 0;
export const POLL_BACKOFF_MULTIPLIER = 2;
export const POLL_INTERVAL_MAX_MS = 60000;
export const POLL_FAILURE_LIMIT = 5;
export const POLL_MISSING_LIMIT = 3;
export const POLL_MAX_DURATION_MS = 1800000;
export const POLL_PAUSE_WHEN_HIDDEN = true;

export const TOKEN_REVEAL_AUTO_MASK_MS = 30000;
export const COPY_FEEDBACK_DURATION_MS = 2000;
export const COUNTDOWN_TICK_MS = 1000;
export const EXPIRY_WARNING_THRESHOLD_SEC = 300;
export const TOKEN_PERSIST_ALLOWED = false;

export const HEARTBEAT_INTERVAL_REF_SEC = 30;
export const STALE_THRESHOLD_HEARTBEAT_SEC = 300;
export const STALE_THRESHOLD_LOGIN_SEC = 86400;
export const CLOCK_SKEW_TOLERANCE_SEC = 120;

export const DEVICE_PAGE_SIZE = 7;
export const TOKEN_PAGE_SIZE = 7;
export const SEARCH_DEBOUNCE_MS = 300;
export const NEW_DEVICE_HIGHLIGHT_MS = 10000;
export const LIST_REFRESH_RETRY_DELAY_MS = 3000;
export const LIST_REFRESH_RETRY_MAX = 1;

export const ADMIN_PROBE_RETRY = 0;
export const ADMIN_PROBE_OPTIMISTIC = true;

export const ENROLLMENT_TOKEN_ENV = "TIMEWEAVER_ENROLLMENT_TOKEN";
export const AGENT_TASK_NAME = "TimeWeaver Agent";
export const DEFAULT_INSTALL_DIR = "C:\\TimeWeaver";
