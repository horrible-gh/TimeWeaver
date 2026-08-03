import { deleteRequest, getRequest, postRequest } from "@api";

function unwrap(response) {
  return response && Object.prototype.hasOwnProperty.call(response, "data") ? response.data : response;
}

export async function issueEnrollment({ groupId, deviceName, ttlHours }) {
  const response = await postRequest(
    "/dashboard/agent-enrollment-tokens",
    { group_id: groupId, device_name: deviceName, ttl_hours: ttlHours },
    "json"
  );
  return unwrap(response);
}

export async function listEnrollments(groupId = null) {
  const response = await getRequest(
    "/dashboard/agent-enrollment-tokens",
    groupId == null ? {} : { group_id: groupId }
  );
  const data = unwrap(response);
  return Array.isArray(data && data.items) ? data.items : [];
}

export async function revokeEnrollment(enrollmentId) {
  const response = await deleteRequest(`/dashboard/agent-enrollment-tokens/${encodeURIComponent(enrollmentId)}`);
  return unwrap(response);
}

export function mapError(error) {
  const response = error && error.response;
  if (!response) return { status: 0, code: null, messageKey: "err_network", params: {}, action: "retry" };
  const status = response.status;
  const detail = response.data && response.data.detail;
  const code = !Array.isArray(detail) && detail ? detail.code || null : null;
  if (status === 401) return { status, code, messageKey: null, params: {}, action: "none" };
  if (status === 403 && code === "admin_required") {
    return { status, code, messageKey: "err_admin_required", params: {}, action: "relogin_as_admin" };
  }
  if (status === 422 && code === "group_inactive") {
    return { status, code, messageKey: "err_group_inactive", params: {}, action: "reload_groups" };
  }
  if (status === 422) {
    return { status, code, messageKey: "err_invalid_request", params: {}, action: "fix_form" };
  }
  if (status === 409 && code === "already_used") {
    return { status, code, messageKey: "err_already_used", params: {}, action: "reload_history" };
  }
  if (status === 404 && code === "not_found") {
    return { status, code, messageKey: "err_not_found", params: {}, action: "reload_history" };
  }
  if (status === 503) {
    const retryAfter = Number(response.headers && response.headers["retry-after"]);
    return Number.isFinite(retryAfter) && retryAfter >= 0
      ? { status, code, messageKey: "err_unavailable_retry_after", params: { n: retryAfter }, action: "retry" }
      : { status, code, messageKey: "err_unavailable", params: {}, action: "retry" };
  }
  if (status >= 400 && status < 500) {
    return { status, code, messageKey: "err_generic_4xx", params: {}, action: "retry" };
  }
  return { status, code, messageKey: "err_network", params: {}, action: "retry" };
}
