import {
  CLOCK_SKEW_TOLERANCE_SEC,
  DEVICE_PAGE_SIZE,
  STALE_THRESHOLD_HEARTBEAT_SEC,
  STALE_THRESHOLD_LOGIN_SEC,
} from "@/dashboard/constants/enrollment";

const HAS_TIMEZONE = /(?:Z|[+-]\d{2}:?\d{2})$/i;

export function parseServerTime(text) {
  if (text == null || String(text).trim() === "") return null;
  if (text instanceof Date) {
    return Number.isNaN(text.getTime()) ? null : new Date(text.getTime());
  }
  if (typeof text === "number") {
    const parsedNumber = new Date(text);
    return Number.isNaN(parsedNumber.getTime()) ? null : parsedNumber;
  }
  const source = String(text).trim();
  const normalized = HAS_TIMEZONE.test(source) ? source : `${source}Z`;
  const parsed = new Date(normalized);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

export function ageSeconds(ref, now = new Date()) {
  const parsedRef = parseServerTime(ref);
  const parsedNow = parseServerTime(now);
  if (!parsedRef || !parsedNow) return null;
  const age = (parsedNow.getTime() - parsedRef.getTime()) / 1000;
  if (age < -CLOCK_SKEW_TOLERANCE_SEC) {
    console.warn("Device timestamp is ahead of the browser clock.");
  }
  return Math.max(0, Math.floor(age));
}

export function computeLiveness(device, now = new Date()) {
  if (!device || device.status !== "active") return "offline";
  const heartbeatAge = ageSeconds(device.last_heartbeat_at, now);
  if (heartbeatAge != null) {
    return heartbeatAge <= STALE_THRESHOLD_HEARTBEAT_SEC ? "online" : "attention";
  }
  const loginAge = ageSeconds(device.last_login_at, now);
  if (loginAge != null) {
    return loginAge <= STALE_THRESHOLD_LOGIN_SEC ? "online" : "attention";
  }
  return "offline";
}

export function livenessReason(device, now = new Date()) {
  if (!device || device.status !== "active") return "admin_off";
  const heartbeatAge = ageSeconds(device.last_heartbeat_at, now);
  if (heartbeatAge != null) {
    return heartbeatAge <= STALE_THRESHOLD_HEARTBEAT_SEC ? null : "heartbeat_stale";
  }
  const loginAge = ageSeconds(device.last_login_at, now);
  if (loginAge != null) {
    return loginAge <= STALE_THRESHOLD_LOGIN_SEC ? null : "login_stale";
  }
  return "never";
}

export function summarize(devices = [], now = new Date()) {
  const summary = { total: devices.length, online: 0, attention: 0, offline: 0, onlineRatio: null };
  devices.forEach((device) => {
    summary[computeLiveness(device, now)] += 1;
  });
  if (summary.total > 0) {
    summary.onlineRatio = Math.round((summary.online / summary.total) * 100);
  }
  return summary;
}

export function relativeTime(ref, now = new Date()) {
  const age = ageSeconds(ref, now);
  if (age == null) return { key: "time_no_record", n: 0 };
  if (age < 60) return { key: "time_just_now", n: 0 };
  if (age < 3600) return { key: "time_minutes_ago", n: Math.floor(age / 60) };
  if (age < 86400) return { key: "time_hours_ago", n: Math.floor(age / 3600) };
  return { key: "time_days_ago", n: Math.floor(age / 86400) };
}

function sortableValue(row, key, now) {
  if (key === "last_login_at" || key === "last_heartbeat_at" || key === "created_at" || key === "expires_at") {
    const parsed = parseServerTime(row[key]);
    return parsed ? parsed.getTime() : null;
  }
  if (key === "device_id" || key === "enrollment_id") {
    const number = Number(row[key]);
    return Number.isNaN(number) ? String(row[key] || "").toLowerCase() : number;
  }
  if (key === "status") return computeLiveness(row, now);
  const value = row[key];
  return value == null || value === "" ? null : String(value).toLowerCase();
}

export function sortRows(rows = [], key = "device_name", dir = "asc", now = new Date()) {
  const direction = dir === "desc" ? -1 : 1;
  return [...rows].sort((left, right) => {
    const a = sortableValue(left, key, now);
    const b = sortableValue(right, key, now);
    if (a == null && b == null) return 0;
    if (a == null) return 1;
    if (b == null) return -1;
    if (a < b) return -1 * direction;
    if (a > b) return 1 * direction;
    return 0;
  });
}

export function visibleDevices(
  devices = [],
  { search = "", status = "all", sortKey = "device_name", sortDir = "asc", page = 1, pageSize = DEVICE_PAGE_SIZE, now = new Date() } = {}
) {
  const needle = search.trim().toLowerCase();
  const filtered = devices.filter((device) => {
    const matchesName = !needle || String(device.device_name || "").toLowerCase().includes(needle);
    const matchesStatus = status === "all" || computeLiveness(device, now) === status;
    return matchesName && matchesStatus;
  });
  const sorted = sortRows(filtered, sortKey, sortDir, now);
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize));
  const safePage = Math.min(Math.max(1, Number(page) || 1), totalPages);
  const start = (safePage - 1) * pageSize;
  return { rows: sorted.slice(start, start + pageSize), totalCount: sorted.length, page: safePage };
}
