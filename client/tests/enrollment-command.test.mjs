import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function loadSource(relative) {
  const source = await readFile(new URL(relative, import.meta.url), "utf8");
  return import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);
}
const { buildCommandBundle, buildRunCommand, buildCleanupCommand, bundleToText } =
  await loadSource("../src/dashboard/utils/enrollmentCommand.js");
const { default: copy } = await loadSource("../src/dashboard/i18n/agentSetup.js");

test("Linux and Windows both use the Go setup and run wrappers", () => {
  assert.equal(bundleToText(buildCommandBundle("linux")), "bash setup.sh");
  assert.equal(buildRunCommand("linux"), "bash run.sh");
  assert.equal(bundleToText(buildCommandBundle("interactive")), ".\\setup.ps1");
  assert.equal(buildRunCommand("interactive"), ".\\run.ps1");
});
test("commands never embed tokens or machine-wide environment/task changes", () => {
  for (const method of ["linux", "interactive", "system_task", "unknown"]) {
    const text = bundleToText(buildCommandBundle(method, "enr-private-token", "D:\\old"));
    assert.doesNotMatch(text, /enr-private-token|SetEnvironmentVariable|schtasks|run-agent\.cmd|TIMEWEAVER_ENROLLMENT_TOKEN/);
    assert.equal(buildCleanupCommand(method), null);
  }
});
test("all supported languages contain the same new onboarding keys", () => {
  const keys = Object.keys(copy.en).sort();
  for (const locale of ["en", "ko", "ja"]) {
    assert.deepEqual(Object.keys(copy[locale]).sort(), keys);
    for (const value of Object.values(copy[locale])) assert.ok(value.length > 0);
  }
});
test("delivery view exposes both platforms and an explicit start command", async () => {
  const source = await readFile(new URL("../src/dashboard/components/contents/devices/EnrollmentDeliverStep.vue", import.meta.url), "utf8");
  assert.match(source, /const METHODS = \["interactive", "linux"\]/);
  assert.match(source, /buildRunCommand\(props\.method\)/);
  assert.doesNotMatch(source, /system_task|DEFAULT_INSTALL_DIR/);
});
