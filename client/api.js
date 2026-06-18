import axios from "axios";
import config from "@config"; // ✅ Webpack alias 사용
import { ref } from "vue";

const API_BASE_URL = `${config.API_SERVER_URL}`;
axios.defaults.withCredentials = true;

// Axios 인스턴스 생성
const apiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    "Content-Type": "application/x-www-form-urlencoded", // ✅ 기본값 유지
  },
});

// ✅ 중복 리디렉트를 방지하기 위한 상태 변수
let isRedirecting = false;

/**
 *  ✅ 401 에러 감지 인터셉터 추가
 */
apiClient.interceptors.response.use(
  (response) => response, // 응답이 정상적이면 그대로 반환
  (error) => {
    if (error.response && (error.response.status === 401)) {
      console.warn("⚠️ 401 인증 오류 발생! 로그인 페이지로 이동합니다.");

      if (!isRedirecting) {
        isRedirecting = true; // ✅ 중복 리디렉트 방지
        localStorage.removeItem("access_token"); // ✅ 토큰 삭제
        sessionStorage.clear(); // ✅ 세션도 정리
        window.location.href = "/login"; // ✅ 로그인 페이지로 강제 이동
      }
    }
    return Promise.reject(error);
  }
);

/**
 * ✅ 토큰을 가져와서 헤더 설정
 */
const getAuthHeaders = (type = "default") => {
  const token = localStorage.getItem("access_token");
  if (!token) {
    console.warn("⚠️ 인증 토큰이 없습니다.");
    return {}; // 토큰이 없으면 빈 헤더 반환
  }

  return {
    "Authorization": `Bearer ${token}`,
    ...(type === "json"
      ? { "Content-Type": "application/json" }
      : { "Content-Type": "application/x-www-form-urlencoded" }),
  };
};

/**
 * ✅ API 요청 함수 (POST)
 */
export const postRequest = async (url, data, type = "default") => {
  try {
    const headers = getAuthHeaders(type);
    const response = await apiClient.post(url, data, { headers });
    return response.data;
  } catch (error) {
    console.error("POST 요청 오류:", error);
    throw error;
  }
};

/**
 * ✅ API 요청 함수 (GET)
 */
export const getRequest = async (url, params = {}, type = "default") => {
  try {
    const headers = getAuthHeaders(type);
    const response = await apiClient.get(url, { params, headers });
    return response.data;
  } catch (error) {
    console.error("GET 요청 오류:", error);
    throw error;
  }
};

/**
 * ✅ API 요청 함수 (PUT)
 */
export const putRequest = async (url, data, type = "default") => {
  try {
    const headers = getAuthHeaders(type);
    const response = await apiClient.put(url, data, { headers });
    return response.data;
  } catch (error) {
    console.error("PUT 요청 오류:", error);
    throw error;
  }
};

/**
 * ✅ API 요청 함수 (DELETE)
 */
export const deleteRequest = async (url, params = {}) => {
  try {
    const headers = getAuthHeaders();
    const response = await apiClient.delete(url, { params, headers });
    return response.data;
  } catch (error) {
    console.error("DELETE 요청 오류:", error);
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
      const valA = a[key] || ""; // ✅ undefined 방지
      const valB = b[key] || "";

      if (valA < valB) return -1 * modifier;
      if (valA > valB) return 1 * modifier;
      return 0;
    });
  };

  return { sortKey, sortOrder, sort };
}
