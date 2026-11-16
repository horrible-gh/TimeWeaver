// DB values may be booleans or 0/1; never use Boolean("0").
export function recursiveEnabled(value) {
  return value === true || value === 1 ||
    (typeof value === "string" && ["1", "true"].includes(value.trim().toLowerCase()));
}

export function housekeepOptions(task) {
  return { house_keep_recursive: task.task_type === "housekeep" && recursiveEnabled(task.house_keep_recursive) };
}
