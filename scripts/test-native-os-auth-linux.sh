#!/usr/bin/env bash
# Run only on the disposable GitHub Linux runner. No PAM or sudoers changes.
set -euo pipefail

if [[ "${GITHUB_ACTIONS:-}" != true || "${RUNNER_OS:-}" != Linux || "$(id -u)" == 0 ]]; then
  echo "Native OS authentication fixtures require a non-root disposable Linux CI runner." >&2
  exit 1
fi

native_tmp="$(mktemp -d)"
native_users=()
cleanup() {
  for native_user in "${native_users[@]}"; do
    sudo userdel "$native_user" >/dev/null 2>&1 || true
  done
  rm -rf -- "$native_tmp"
}
trap cleanup EXIT

native_suffix="$(openssl rand -hex 3)"
native_password="Tw!$(openssl rand -hex 24)aA9"
echo "::add-mask::$native_password"
native_active="twok$native_suffix"
native_disabled="twlock$native_suffix"
native_expired="twexpire$native_suffix"
native_password_expired="twpw$native_suffix"
native_restricted="twshell$native_suffix"

for native_user in "$native_active" "$native_disabled" "$native_expired" "$native_password_expired" "$native_restricted"; do
  sudo useradd --no-create-home --shell /bin/sh "$native_user"
  native_users+=("$native_user")
  printf '%s:%s\n' "$native_user" "$native_password" | sudo chpasswd
done
sudo usermod --lock "$native_disabled"
sudo chage --expiredate 1970-01-02 "$native_expired"
sudo chage --lastday 0 "$native_password_expired"
sudo usermod --shell /usr/sbin/nologin "$native_restricted"

export TIMEWEAVER_OS_AUTH_TEST_USER="$native_active"
export TIMEWEAVER_OS_AUTH_TEST_PASSWORD="$native_password"
export TIMEWEAVER_OS_AUTH_TEST_DISABLED="$native_disabled"
export TIMEWEAVER_OS_AUTH_TEST_EXPIRED="$native_expired"
export TIMEWEAVER_OS_AUTH_TEST_PASSWORD_EXPIRED="$native_password_expired"
export TIMEWEAVER_OS_AUTH_TEST_RESTRICTED="$native_restricted"

# Exercise the same cgo-free account lookup used by distributed binaries.
CGO_ENABLED=0 go test -c -o "$native_tmp/access-native.test" ./internal/access
"$native_tmp/access-native.test" -test.run '^(TestNativeOS|TestLiveNativeOS)' -test.v -test.timeout 2m

# Credential-free root check proves the provider cannot use PAM's root trust.
sudo "$native_tmp/access-native.test" -test.run '^TestNativeOSLinuxRootDisabled$' -test.v -test.timeout 15s
