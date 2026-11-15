// Commands intentionally contain no token. The Go setup wizard requests it
// through hidden input; no machine-wide environment or scheduled task is needed.
export function buildCommandBundle(method) {
  return method === "linux" ? ["bash setup.sh"] : [".\\setup.ps1"];
}

export function buildRunCommand(method) {
  return method === "linux" ? "bash run.sh" : ".\\run.ps1";
}

// Kept for EnrollmentWatchStep: interactive setup leaves no token to clean up.
export function buildCleanupCommand() {
  return null;
}

export function bundleToText(lines = []) {
  return lines.join("\n");
}
