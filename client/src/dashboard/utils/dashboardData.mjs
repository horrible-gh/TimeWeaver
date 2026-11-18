const countKeys = {
  devices: ['active', 'error', 'inactive'],
  schedules: ['active', 'error', 'inactive'],
  tasks: ['in_progress', 'pending', 'completed', 'error'],
};

function count(value) {
  if ((typeof value !== 'number' && typeof value !== 'string') || String(value).trim() === '') throw new Error('invalid_dashboard_data');
  const result = Number(value);
  if (!Number.isSafeInteger(result) || result < 0) throw new Error('invalid_dashboard_data');
  return result;
}

// Python SQL drivers may return numeric strings; Go returns JSON numbers.
// A failed/malformed response must never look like a successful zero count.
export function dashboardCounts(kind, response) {
  if (!countKeys[kind] || !response || typeof response !== 'object' || Array.isArray(response)) throw new Error('invalid_dashboard_data');
  return Object.fromEntries(countKeys[kind].map(key => [key, count(response[`${key}_count`])]));
}

export function latestSchedules(response) {
  if (!Array.isArray(response)) throw new Error('invalid_dashboard_data');
  return response.map(row => {
    if (!row || typeof row !== 'object' || Array.isArray(row)) throw new Error('invalid_dashboard_data');
    const id = count(row.schedule_id);
    if (id === 0) throw new Error('invalid_dashboard_data');
    return { ...row, schedule_id: id, task_count: count(row.task_count) };
  });
}

export const dashboardSection = () => ({ data: null, loading: false, error: false, updatedAt: null });

export async function loadDashboardSection(state, request, normalize, now = Date.now) {
  if (state.loading) return false;
  state.loading = true;
  state.data = null;
  state.error = false;
  state.updatedAt = null;
  try {
    state.data = normalize(await request());
    state.updatedAt = new Date(now()).toISOString();
    return true;
  } catch {
    state.data = null;
    state.error = true;
    return false;
  } finally {
    state.loading = false;
  }
}
