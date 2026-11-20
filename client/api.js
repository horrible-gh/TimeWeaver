import axios from "axios";
import config from "@config";
import { ref } from "vue";
import { createCookieSession, isUnauthenticated, isSessionChanged, signalSessionChange, watchCookieSession } from "./session.mjs";
import { availableLoginMethods, loginPayload } from "./authentication.mjs";

export const sessionMode = config.AUTH_MODE === "session";
export const sessionUnavailable = ref(false);
export const sessionUser = ref(null);
const apiClient = axios.create({ baseURL: config.API_SERVER_URL, withCredentials: true });
const rawRequest = async (url, options) => (await apiClient.request({ url, ...options })).data;
let redirecting = false;
const loginPage = () => ["/", "/login", "/login/"].includes(window.location.pathname);
function redirectToLogin() {
  sessionUser.value = null;
  sessionUnavailable.value = false;
  if (!redirecting && !loginPage()) {
    redirecting = true; window.location.href = "/login";
  }
}
const cookieSession = sessionMode ? createCookieSession(rawRequest, localStorage, {
  onUnauthenticated: redirectToLogin,
}) : null;
const sessionSignal = sessionMode ? `timeweaver.session:${new URL(config.API_SERVER_URL, window.location.origin).pathname}` : '';

apiClient.interceptors.response.use(response => response, error => {
  const url = error.config?.url || "";
  if (!sessionMode && error.response?.status === 401 && !["/login", "/auth/password"].includes(url)) {
    localStorage.removeItem("access_token"); localStorage.removeItem("user");
    redirectToLogin();
  }
  return Promise.reject(error);
});

async function request(method, url, data, type = "default", params) {
  const headers = { "Content-Type": type === "json" ? "application/json" : "application/x-www-form-urlencoded" };
  if (sessionMode) {
    const send = ["/login", "/auth/methods"].includes(url) ? rawRequest : cookieSession.request;
    return send(url, { method, data, params, headers });
  } else {
    const token = localStorage.getItem("access_token");
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  return (await apiClient.request({ method, url, data, params, headers })).data;
}
export const getRequest = (url, params = {}, type = "default") => request("get", url, undefined, type, params);
export const postRequest = (url, data, type = "default") => request("post", url, data, type);
export const putRequest = (url, data, type = "default") => request("put", url, data, type);
export const deleteRequest = (url, params = {}) => request("delete", url, undefined, "default", params);
export async function login(username, password, method = 'local', remember = false) {
  const epoch = cookieSession?.clear();
  if (sessionMode) sessionUser.value = null;
  const data = await postRequest("/login", loginPayload(username, password, method, sessionMode, remember));
  if (sessionMode) {
    cookieSession.accept(data, epoch);
    signalSessionChange(localStorage, sessionSignal, 'login');
    sessionUser.value = data.user;
    sessionUnavailable.value = false;
  }
  else {
    localStorage.setItem("access_token", data.access_token);
    localStorage.setItem("user", JSON.stringify(data.user));
  }
  redirecting = false;
  return data;
}
export async function getLoginMethods() {
  return sessionMode ? availableLoginMethods(await getRequest('/auth/methods')) : ['local'];
}
export async function ensureSession() {
  if (!sessionMode) return null;
  try {
    const data = await cookieSession.read();
    sessionUser.value = data.user;
    sessionUnavailable.value = false;
    return data;
  } catch (error) {
    sessionUnavailable.value = !isUnauthenticated(error) && !isSessionChanged(error);
    throw error;
  }
}
export const retrySession = () => window.location.reload();
export async function logout() {
  try { await postRequest("/logout"); }
  catch (error) { if (sessionMode ? !isUnauthenticated(error) : error.response?.status !== 401) throw error; }
  cookieSession?.clear(); localStorage.removeItem("access_token"); localStorage.removeItem("user");
  sessionUser.value = null;
  if (sessionMode) signalSessionChange(localStorage, sessionSignal, 'logout');
}
export async function changePassword(current, next) {
  await postRequest("/auth/password", { current_password: current, new_password: next }, "json");
  cookieSession.clear();
  sessionUser.value = null;
  signalSessionChange(localStorage, sessionSignal, 'logout');
}

if (sessionMode) watchCookieSession({
  session: cookieSession, storage: localStorage, key: sessionSignal, window, document,
  shouldResume: () => window.location.pathname.startsWith('/dashboard'),
  onResume: async () => {
    const current = await ensureSession();
    if (current.user.must_change_password) window.location.href = '/login?password=required';
  },
  onLogout: () => {
    sessionUser.value = null;
    sessionUnavailable.value = false;
    redirecting = true;
    // Also reset a sibling tab that is already showing the password-change
    // form at /login; clearing memory alone cannot reset that mounted form.
    window.location.replace('/login');
  },
  // A different account must not inherit another tab's mounted dashboard data.
  onLogin: () => { sessionUser.value = null; window.location.reload(); },
});

export function useSort(data) {
  const sortKey = ref("");
  const sortOrder = ref("asc");

  const sort = (key) => {
    if (sortKey.value === key) {
      sortOrder.value = sortOrder.value === "asc" ? "desc" : "asc";
    } else {
      sortKey.value = key;
      sortOrder.value = "asc";
    }

    const modifier = sortOrder.value === "asc" ? 1 : -1;

    data.value.sort((a, b) => {
      const valA = a[key] || ""; // ✅ Avoid undefined
      const valB = b[key] || "";

      if (valA < valB) return -1 * modifier;
      if (valA > valB) return 1 * modifier;
      return 0;
    });
  };

  return { sortKey, sortOrder, sort };
}
