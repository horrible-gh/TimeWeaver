import {
  AGENT_TASK_NAME,
  DEFAULT_INSTALL_DIR,
  ENROLLMENT_TOKEN_ENV,
} from "@/dashboard/constants/enrollment";

function quotePowerShell(value) {
  return String(value).replace(/"/g, '`"');
}

export function buildCommandBundle(method, token, installDir = DEFAULT_INSTALL_DIR) {
  const safeMethod = method === "system_task" ? method : "interactive";
  const safeToken = quotePowerShell(token);
  if (safeMethod === "system_task") {
    return [
      `[Environment]::SetEnvironmentVariable("${ENROLLMENT_TOKEN_ENV}", "${safeToken}", "Machine")`,
      `schtasks /end /tn "${AGENT_TASK_NAME}"`,
      `schtasks /run /tn "${AGENT_TASK_NAME}"`,
    ];
  }
  return [
    `$env:${ENROLLMENT_TOKEN_ENV} = "${safeToken}"`,
    `cd "${quotePowerShell(installDir)}"`,
    ".\\run-agent.cmd",
  ];
}

export function buildCleanupCommand(method) {
  return method === "system_task"
    ? `[Environment]::SetEnvironmentVariable("${ENROLLMENT_TOKEN_ENV}", $null, "Machine")`
    : null;
}

export function bundleToText(lines = []) {
  return lines.join("\n");
}
