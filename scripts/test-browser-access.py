#!/usr/bin/env python3
"""Only invoked by the Go test on an ephemeral loopback listener/database."""
import json
import sys
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect


def main():
    settings = json.load(sys.stdin)
    origin = settings["origin"]
    assert urlparse(origin).hostname == "127.0.0.1"
    base = origin + settings["context"]
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(locale="en-US")
        # The application must not depend on remote fonts/icons for authentication.
        context.route("**/*", lambda route: route.continue_() if route.request.url.startswith(origin + "/") else route.abort())
        page = context.new_page()
        page.goto(origin + "/login")
        page.locator("#auth-id").fill(settings["user"])
        page.locator("#auth-password").fill(settings["password"])
        page.locator("button[type=submit]").click()
        expect(page.locator("#auth-new-password")).to_be_visible()
        assert page.url.endswith("/login")
        assert page.evaluate("localStorage.getItem('access_token')") is None
        page.locator("#auth-password").fill(settings["password"])
        page.locator("#auth-new-password").fill("new-browser-password-123")
        page.locator("#auth-confirm").fill("new-browser-password-123")
        page.locator("button[type=submit]").click()
        expect(page.locator("#auth-id")).to_be_visible()
        expect(page.locator("[role=status]")).to_contain_text("Sign in again")
        page.locator("#auth-id").fill(settings["user"])
        page.locator("#auth-password").fill("new-browser-password-123")
        page.locator("button[type=submit]").click()
        page.wait_for_url("**/dashboard*")
        expect(page.locator(".user-trigger")).to_be_visible()
        me = context.request.get(base + "/auth/me")
        assert me.status == 200 and me.json()["user"]["user_id"] == settings["user"]
        csrf = me.json()["csrf_token"]
        assert csrf not in page.evaluate("JSON.stringify({...localStorage,...sessionStorage})")
        cookies = [c for c in context.cookies() if c["name"] == "tw_session"]
        assert len(cookies) == 1 and cookies[0]["httpOnly"] and cookies[0]["sameSite"] == "Strict"
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
    print("PASS: browser password gate, change, login, reload, cookie/CSRF isolation and real logout")


if __name__ == "__main__":
    main()
