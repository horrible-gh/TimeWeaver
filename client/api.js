import axios from "axios";
import config from "@config"; // ✅ Use Webpack alias
import { ref } from "vue";

const API_BASE_URL = `${config.API_SERVER_URL}`;
axios.defaults.withCredentials = true;

// Create Axios instance
const apiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    "Content-Type": "application/x-www-form-urlencoded", // ✅ Keep default value
  },
});

// ✅ State flag to prevent duplicate redirects
let isRedirecting = false;

/**
 *  ✅ Add interceptor for 401 errors
 */
apiClient.interceptors.response.use(
  (response) => response, // Return successful responses as-is
  (error) => {
    if (error.response && (error.response.status === 401)) {
      console.warn("⚠️ 401 authentication error. Redirecting to the login page.");

      if (!isRedirecting) {
        isRedirecting = true; // ✅ Prevent duplicate redirects
        localStorage.removeItem("access_token"); // ✅ Remove token
        sessionStorage.clear(); // ✅ Clear session data
        window.location.href = "/login"; // ✅ Force redirect to login page
      }
    }
    return Promise.reject(error);
  }
);

/**
 * ✅ Get token and set headers
 */
const getAuthHeaders = (type = "default") => {
  const token = localStorage.getItem("access_token");
  if (!token) {
    console.warn("⚠️ Authentication token is missing.");
    return {}; // Return empty headers when token is missing
  }

  return {
    "Authorization": `Bearer ${token}`,
    ...(type === "json"
      ? { "Content-Type": "application/json" }
      : { "Content-Type": "application/x-www-form-urlencoded" }),
  };
};

/**
 * ✅ API request function (POST)
 */
export const postRequest = async (url, data, type = "default") => {
  try {
    const headers = getAuthHeaders(type);
    const response = await apiClient.post(url, data, { headers });
    return response.data;
  } catch (error) {
    console.error("POST request error:", error);
    throw error;
  }
};

/**
 * ✅ API request function (GET)
 */
export const getRequest = async (url, params = {}, type = "default") => {
  try {
    const headers = getAuthHeaders(type);
    const response = await apiClient.get(url, { params, headers });
    return response.data;
  } catch (error) {
    console.error("GET request error:", error);
    throw error;
  }
};

/**
 * ✅ API request function (PUT)
 */
export const putRequest = async (url, data, type = "default") => {
  try {
    const headers = getAuthHeaders(type);
    const response = await apiClient.put(url, data, { headers });
    return response.data;
  } catch (error) {
    console.error("PUT request error:", error);
    throw error;
  }
};

/**
 * ✅ API request function (DELETE)
 */
export const deleteRequest = async (url, params = {}) => {
  try {
    const headers = getAuthHeaders();
    const response = await apiClient.delete(url, { params, headers });
    return response.data;
  } catch (error) {
    console.error("DELETE request error:", error);
    throw error;
  }
};


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
