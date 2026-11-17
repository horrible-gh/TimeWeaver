// Display hints only. Every API authorizes the current session and DB resource.
export function canEditGroup(user, groupId) {
  if (!user || user.must_change_password || groupId == null || groupId === "") return false;
  const id = Number(groupId);
  if (!Number.isSafeInteger(id) || id < 0) return false;
  return user.role === "admin" || (user.role === "user" && Array.isArray(user.groups) &&
    user.groups.some(g => g.role === "editor" && (Number(g.group_id) === 0 || Number(g.group_id) === id)));
}
export function devicesForGroup(devices, groupId) {
  if (groupId == null || groupId === "") return [];
  return devices.filter(d => d && d.group_id != null && Number(d.group_id) === Number(groupId) &&
    d.device_name && d.status === "active")
    .slice().sort((a, b) => String(a.device_name).localeCompare(String(b.device_name)));
}
