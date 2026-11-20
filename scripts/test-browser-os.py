#!/usr/bin/env python3
"""OS account linking and human sessions through the real embedded Vue UI.

The Go test injects a deterministic provider; no native accounts are created by
this browser test. Native authentication has separate disposable CI coverage.
"""
import json
import sys
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect


def main():
    cfg = json.load(sys.stdin)
    origin, base = cfg['origin'], cfg['origin'] + cfg['context']
    assert urlparse(origin).hostname == '127.0.0.1'
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)

        def context():
            ctx = browser.new_context(locale='en-US')
            ctx.route('**/*', lambda route: route.continue_() if route.request.url.startswith(origin + '/') else route.abort())
            return ctx, ctx.new_page()

        def sign_in(page, method, name, password, expected=200):
            page.goto(origin + '/login')
            expect(page.locator('#auth-method')).to_be_visible()
            page.locator('#auth-method').select_option(method)
            page.locator('#auth-id').fill(name)
            page.locator('#auth-password').fill(password)
            page.locator('#auth-remember').check()
            with page.expect_response(lambda response: response.url == base + '/login' and response.request.method == 'POST') as response:
                page.locator('button[type=submit]').click()
            assert response.value.status == expected, response.value.text()
            if expected == 200:
                page.wait_for_url('**/dashboard*')
            else:
                expect(page.locator('#auth-id')).to_be_visible()

        admin, page = context()
        sign_in(page, 'local', cfg['user'], cfg['password'])
        page.goto(origin + '/dashboard/users')
        page.get_by_role('button', name='Add user', exact=True).click()
        page.locator('#user-auth-method').select_option('os')
        expect(page.locator('#user-id')).to_have_count(0)
        expect(page.locator('#user-password')).to_have_count(0)
        page.locator('#user-os-name').fill('browser-os-one')
        page.locator('#user-name').fill('OS Browser User')
        page.locator('#grant-7').select_option('viewer')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.locator('.modal-overlay')).to_have_count(0)
        users = admin.request.get(base + '/dashboard/users').json()
        user = next(user for user in users if user['auth_method'] == 'os' and user['os_name'] == 'browser-os-one')
        uid = user['user_id']
        assert uid and user['role'] == 'user' and not user['must_change_password']
        row = page.locator(f'tr[data-user="{uid}"]')
        expect(row).to_contain_text('OS Browser User')
        expect(row).to_contain_text('browser-os-one')
        expect(row.get_by_role('button', name='Reset password', exact=True)).to_have_count(0)

        limited, lp = context()
        sign_in(lp, 'os', 'browser-os-alias', 'wrong-os-password', 401)
        sign_in(lp, 'os', 'browser-os-alias', cfg['os_password'])
        expect(lp.locator('#auth-new-password')).to_have_count(0)
        principal = limited.request.get(base + '/auth/me').json()
        assert principal['user']['user_id'] == uid
        assert principal['user']['auth_method'] == 'os' and principal['user']['role'] == 'user'
        assert not principal['user']['must_change_password']
        assert principal['session']['remembered'] is True
        assert limited.request.get(base + '/dashboard/users').status == 403
        devices = limited.request.get(base + '/dashboard/devices/get_devices?group_id=8').json()
        assert [device['device_name'] for device in devices] == ['seven'], devices
        lp.goto(origin + '/dashboard/devices')
        expect(lp.get_by_text('seven', exact=True)).to_be_visible()
        expect(lp.get_by_text('eight', exact=True)).to_have_count(0)
        assert cfg['os_password'] not in lp.evaluate('JSON.stringify(localStorage)')
        assert lp.evaluate('localStorage.getItem("access_token")') is None
        assert any(cookie['name'] == 'tw_session' and cookie['httpOnly'] for cookie in limited.cookies())
        assert next(cookie for cookie in limited.cookies() if cookie['name'] == 'tw_session')['expires'] > 0
        protected = limited.request.post(base + '/auth/password', headers={'Origin': origin, 'X-CSRF-Token': principal['csrf_token']}, data={'current_password': cfg['os_password'], 'new_password': 'must-not-become-local-password'})
        assert protected.status == 409 and protected.json()['detail']['code'] == 'os_password_managed'

        # An identity change preserves the application user ID and revokes the
        # previous session; the old OS name cannot regain the connection.
        row.get_by_role('button', name='Edit', exact=True).click()
        expect(page.locator('#user-id')).to_be_disabled()
        expect(page.locator('#user-auth-method')).to_be_disabled()
        page.locator('#user-os-name').fill('browser-os-two')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.locator('.modal-overlay')).to_have_count(0)
        expect(row).to_contain_text('browser-os-two')
        assert limited.request.get(base + '/auth/me').status == 401
        assert limited.request.post(base + '/auth/refresh', headers={'Origin': origin, 'X-CSRF-Token': principal['csrf_token']}).status == 401
        sign_in(lp, 'os', 'browser-os-one', cfg['os_password'], 401)
        sign_in(lp, 'os', 'browser-os-two', cfg['os_password'])
        principal = limited.request.get(base + '/auth/me').json()
        assert principal['user']['user_id'] == uid
        assert principal['session']['remembered'] is True

        row.get_by_role('button', name='Edit', exact=True).click()
        page.locator('#user-status').select_option('disabled')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.locator('.modal-overlay')).to_have_count(0)
        expect(row.locator('.badge')).to_have_text('Disabled')
        assert limited.request.get(base + '/auth/me').status == 401
        assert limited.request.post(base + '/auth/refresh', headers={'Origin': origin, 'X-CSRF-Token': principal['csrf_token']}).status == 401
        sign_in(lp, 'os', 'browser-os-two', cfg['os_password'], 401)
        row.get_by_role('button', name='Edit', exact=True).click()
        page.locator('#user-status').select_option('active')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.locator('.modal-overlay')).to_have_count(0)
        sign_in(lp, 'os', 'browser-os-two', cfg['os_password'])
        expect(row.get_by_role('button', name='Reset password', exact=True)).to_have_count(0)
        browser.close()
    print('PASS: explicit OS link with generated app identity, remembered alias login, no local password/bearer storage, group scope, remembered-session rebind/disable revocation and reactivation')


if __name__ == '__main__':
    main()
