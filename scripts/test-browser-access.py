#!/usr/bin/env python3
"""Only invoked by the Go test on an ephemeral loopback listener/database."""
import json
import sys
import tempfile
import time
from urllib.parse import urlparse
from urllib.request import Request, urlopen
from playwright.sync_api import sync_playwright, expect


def main():
    settings = json.load(sys.stdin)
    origin = settings["origin"]
    assert urlparse(origin).hostname == "127.0.0.1"
    base = origin + settings["context"]

    def control(action, cookie=""):
        assert urlparse(settings["control_origin"]).hostname == "127.0.0.1"
        request = Request(settings["control_origin"], method="POST", data=json.dumps({"action": action, "cookie": cookie}).encode(), headers={"Content-Type": "application/json", "X-Fixture-Key": settings["control_key"]})
        with urlopen(request, timeout=20) as response:
            return json.load(response)

    def only_cookie(context):
        cookies = [cookie for cookie in context.cookies() if cookie["name"] == "tw_session"]
        assert len(cookies) == 1
        cookie = cookies[0]
        assert cookie["httpOnly"] and cookie["sameSite"] == "Strict"
        assert cookie["path"] == settings["context"] + "/"
        return cookie

    def sign_in(page, remember):
        page.goto(origin + "/login")
        page.locator("#auth-id").fill(settings["user"])
        page.locator("#auth-password").fill("new-browser-password-123")
        page.locator("#auth-remember").set_checked(remember)
        page.locator("button[type=submit]").click()
        page.wait_for_url("**/dashboard*")
        expect(page.locator(".user-trigger")).to_be_visible()

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(locale="en-US")
        # The application must not depend on remote fonts/icons for authentication.
        context.route("**/*", lambda route: route.continue_() if route.request.url.startswith(origin + "/") else route.abort())
        page = context.new_page()
        page.goto(origin + "/login")
        page.locator("#auth-id").fill(settings["user"])
        page.locator("#auth-password").fill(settings["password"])
        expect(page.locator("#auth-remember")).not_to_be_checked()
        page.locator("#auth-remember").check()
        page.locator("button[type=submit]").click()
        expect(page.locator("#auth-new-password")).to_be_visible()
        assert page.url.endswith("/login")
        assert context.request.get(base + "/auth/me").json()["session"]["remembered"] is False
        assert only_cookie(context)["expires"] == -1
        assert page.evaluate("localStorage.getItem('access_token')") is None
        restricted = context.new_page()
        restricted.goto(origin + "/login")
        expect(restricted.locator("#auth-new-password")).to_be_visible()
        page.locator("#auth-password").fill(settings["password"])
        page.locator("#auth-new-password").fill("new-browser-password-123")
        page.locator("#auth-confirm").fill("new-browser-password-123")
        page.locator("button[type=submit]").click()
        expect(page.locator("#auth-id")).to_be_visible()
        expect(page.locator("[role=status]")).to_contain_text("Sign in again")
        expect(restricted.locator("#auth-id")).to_be_visible()
        expect(restricted.locator("#auth-new-password")).to_have_count(0)
        restricted.close()
        page.locator("#auth-id").fill(settings["user"])
        page.locator("#auth-password").fill("new-browser-password-123")
        page.locator("#auth-remember").uncheck()
        page.locator("button[type=submit]").click()
        page.wait_for_url("**/dashboard*")
        expect(page.locator(".user-trigger")).to_be_visible()
        me = context.request.get(base + "/auth/me")
        assert me.status == 200 and me.json()["user"]["user_id"] == settings["user"]
        csrf = me.json()["csrf_token"]
        assert me.json()["session"]["remembered"] is False
        assert csrf not in page.evaluate("JSON.stringify({...localStorage,...sessionStorage})")
        cookies = [c for c in context.cookies() if c["name"] == "tw_session"]
        assert len(cookies) == 1 and cookies[0]["httpOnly"] and cookies[0]["sameSite"] == "Strict"
        assert cookies[0]["expires"] == -1
        assert "tw_session" not in page.evaluate("document.cookie")
        page.reload()
        expect(page.locator(".user-trigger")).to_be_visible()
        # A stale legacy JWT is ignored rather than sent alongside the session.
        page.evaluate("localStorage.setItem('access_token','stale-jwt')")
        page.reload()
        expect(page.locator(".user-trigger")).to_be_visible()
        assert page.evaluate("localStorage.getItem('access_token')") is None
        assert context.request.post(base + "/logout", headers={"Origin": origin}).status == 403
        # Exercise the actual profile menu; it must revoke server state, not just
        # remove a browser value as the legacy UI did.
        page.locator(".user-trigger").click()
        page.get_by_role("menuitem", name="Logout", exact=True).click()
        page.wait_for_url("**/login")
        assert context.request.get(base + "/auth/me").status == 401
        assert not [c for c in context.cookies() if c["name"] == "tw_session"]
        browser.close()

        # Real profile restarts exercise Chromium's cookie persistence. The test
        # never restores storage_state, which would also restore session cookies.
        with tempfile.TemporaryDirectory(prefix="timeweaver-browser-session-") as profile:
            def persistent():
                ctx = p.chromium.launch_persistent_context(profile, headless=True, locale="en-US")
                ctx.route("**/*", lambda route: route.continue_() if route.request.url.startswith(origin + "/") else route.abort())
                return ctx, ctx.pages[0] if ctx.pages else ctx.new_page()

            context, page = persistent()
            sign_in(page, False)
            assert only_cookie(context)["expires"] == -1
            context.close()
            context, page = persistent()
            page.goto(origin + "/login")
            expect(page.locator("#auth-id")).to_be_visible()
            assert context.request.get(base + "/auth/me").status == 401

            sign_in(page, True)
            before = only_cookie(context)
            assert before["expires"] > time.time() + 29 * 24 * 3600
            me = context.request.get(base + "/auth/me").json()
            assert me["session"]["remembered"] is True
            absolute_expiry = me["session"]["expires_at"]
            context.close()
            # Two days without activity exceeds the ordinary idle timeout. A
            # fresh listener, service and DB pools must still recover the cookie.
            control("due", before["value"])
            control("restart")
            context, page = persistent()
            page.goto(origin + "/login")
            page.wait_for_url("**/dashboard*")
            expect(page.locator(".user-trigger")).to_be_visible()
            renewed = only_cookie(context)
            assert renewed["value"] != before["value"]
            me = context.request.get(base + "/auth/me").json()
            assert me["session"]["expires_at"] == absolute_expiry
            assert abs(renewed["expires"] - before["expires"]) <= 1
            assert "tw_session" not in page.evaluate("document.cookie")
            storage = page.evaluate("JSON.stringify({...localStorage,...sessionStorage})")
            assert renewed["value"] not in storage and me["csrf_token"] not in storage

            # Both tabs bootstrap against a due credential and remain usable
            # regardless of which refresh response arrives first.
            other = context.new_page()
            other.goto(origin + "/dashboard")
            expect(other.locator(".user-trigger")).to_be_visible()
            control("due", renewed["value"])
            page.reload(wait_until="commit")
            other.reload(wait_until="commit")
            expect(page.locator(".user-trigger")).to_be_visible()
            expect(other.locator(".user-trigger")).to_be_visible()
            concurrent = only_cookie(context)
            assert concurrent["value"] != renewed["value"]
            assert context.request.get(base + "/auth/me").json()["session"]["expires_at"] == absolute_expiry

            # The server commits rotation but withholds its response and cookie.
            # A reload must recover that exact transition using the predecessor,
            # without signing the user out or extending the absolute deadline.
            control("lose-refresh", concurrent["value"])
            with page.expect_response(lambda response: response.url == base + "/auth/refresh" and response.request.method == "POST") as response:
                page.reload()
            assert response.value.status == 503
            # A sibling wake/read may already recover the browser jar. Probe
            # the saved predecessor explicitly so either response ordering is
            # valid while still proving that the lost cookie remains recoverable.
            retry = context.request.get(base + "/auth/me", headers={"Cookie": "tw_session=" + concurrent["value"]})
            assert retry.status == 200 and retry.json()["session"]["refresh_after_seconds"] == 0
            page.reload()
            page.wait_for_url("**/dashboard*")
            expect(page.locator(".user-trigger")).to_be_visible()
            recovered = only_cookie(context)
            assert recovered["value"] != concurrent["value"]
            assert control("status")["lost_refreshes"] == 1
            me = context.request.get(base + "/auth/me").json()
            assert me["session"]["expires_at"] == absolute_expiry

            other.reload()
            expect(other.locator(".user-trigger")).to_be_visible()
            page.locator(".user-trigger").click()
            page.get_by_role("menuitem", name="Logout", exact=True).click()
            page.wait_for_url("**/login")
            other.wait_for_url("**/login")
            assert not [c for c in context.cookies() if c["name"] == "tw_session"]
            assert context.request.post(base + "/auth/refresh", headers={"Origin": origin, "X-CSRF-Token": me["csrf_token"]}).status == 401
            other.close()

            sign_in(page, True)
            expired = only_cookie(context)
            control("expire", expired["value"])
            page.reload()
            page.wait_for_url("**/login")
            expect(page.locator("#auth-id")).to_be_visible()
            assert context.request.get(base + "/auth/me").status == 401
            # An old unauthorized response must never clear a newer cookie.
            assert only_cookie(context)["value"] == expired["value"]

            sign_in(page, False)
            ordinary = only_cookie(context)
            assert ordinary["expires"] == -1
            control("idle", ordinary["value"])
            page.reload()
            page.wait_for_url("**/login")
            expect(page.locator("#auth-id")).to_be_visible()
            assert context.request.get(base + "/auth/me").status == 401
            context.close()
    print("PASS: password gate, ordinary and remembered cookies, real browser/server restart, long-idle renewal, concurrent tabs, lost-response recovery, fixed expiry, cookie/CSRF isolation and cross-tab logout")


if __name__ == "__main__":
    main()
