import axios from "axios";
import config from "@config";
import { ref } from "vue";
import { createCookieSession } from "./session.mjs";
import { availableLoginMethods, loginPayload } from "./authentication.mjs";

export const sessionMode = config.AUTH_MODE === "session";
const apiClient = axios.create({ baseURL: config.API_SERVER_URL, withCredentials: true });
const cookieSession = sessionMode ? createCookieSession(async (url, options) =>
  (await apiClient.request({ url, ...options })).data, localStorage) : null;
let redirecting = false;

apiClient.interceptors.response.use(response => response, error => {
  const url = error.config?.url || "";
  if (error.response?.status === 401 && !["/login", "/auth/password"].includes(url)) {
    cookieSession?.clear();
    localStorage.removeItem("access_token"); localStorage.removeItem("user");
    if (!redirecting && window.location.pathname !== "/login" && window.location.pathname !== "/") {
      redirecting = true; window.location.href = "/login";
    }
  }
  return Promise.reject(error);
});

async function request(method, url, data, type = "default", params) {
  const headers = { "Content-Type": type === "json" ? "application/json" : "application/x-www-form-urlencoded" };
  if (sessionMode) {
    if (method !== "get" && url !== "/login") Object.assign(headers, await cookieSession.headers());
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
export async function login(username, password, method = 'local') {
  cookieSession?.clear();
  const data = await postRequest("/login", loginPayload(username, password, method, sessionMode));
  if (sessionMode) cookieSession.accept(data);
  else {
    localStorage.setItem("access_token", data.access_token);
    localStorage.setItem("user", JSON.stringify(data.user));
  }
  return data;
}
export async function getLoginMethods() {
  return sessionMode ? availableLoginMethods(await getRequest('/auth/methods')) : ['local'];
}
export const ensureSession = () => sessionMode ? cookieSession.read() : Promise.resolve(null);
export async function logout() {
  try { await postRequest("/logout"); }
  catch (error) { if (error.response?.status !== 401) throw error; }
  cookieSession?.clear(); localStorage.removeItem("access_token"); localStorage.removeItem("user");
}
export async function changePassword(current, next) {
  await postRequest("/auth/password", { current_password: current, new_password: next }, "json");
  cookieSession.clear();
}

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
