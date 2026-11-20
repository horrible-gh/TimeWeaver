#!/usr/bin/env python3
"""Real Vue administration + malicious scope requests on a disposable local DB."""
import json
import sys
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect


def sign_in(page, origin, user, password):
    page.goto(origin + '/login')
    page.locator('#auth-id').fill(user)
    page.locator('#auth-password').fill(password)
    page.locator('#auth-remember').check()
    page.locator('button[type=submit]').click()


def new_context(browser, origin):
    context = browser.new_context(locale='en-US')
    context.route('**/*', lambda route: route.continue_() if route.request.url.startswith(origin + '/') else route.abort())
    return context


def main():
    settings = json.load(sys.stdin)
    origin = settings['origin']
    assert urlparse(origin).hostname == '127.0.0.1'
    base = origin + settings['context']
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        admin = new_context(browser, origin)
        page = admin.new_page()
        sign_in(page, origin, settings['user'], settings['password'])
        page.wait_for_url('**/dashboard*')
        page.goto(origin + '/dashboard/groups')
        page.get_by_role('button', name='Add', exact=True).click()
        page.locator('.modal-body input[type=text]').fill('Browser Group')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.get_by_role('cell', name='Browser Group', exact=True)).to_be_visible()
        group = next(g for g in admin.request.get(base + '/dashboard/groups/get_groups').json() if g['group_name'] == 'Browser Group')
        page.goto(origin + '/dashboard/users')
        page.get_by_role('button', name='Add user', exact=True).click()
        page.locator('#user-id').fill('browser-limited')
        page.locator('#user-name').fill('Limited User')
        page.locator('#user-password').fill('limited-first-password')
        page.locator('#grant-7').select_option('viewer')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        row = page.locator('[data-user="browser-limited"]')
        expect(row).to_contain_text('Limited User')
        limited = new_context(browser, origin)
        lp = limited.new_page()
        sign_in(lp, origin, 'browser-limited', 'limited-first-password')
        expect(lp.locator('#auth-new-password')).to_be_visible()
        lp.locator('#auth-password').fill('limited-first-password')
        lp.locator('#auth-new-password').fill('limited-changed-password')
        lp.locator('#auth-confirm').fill('limited-changed-password')
        lp.locator('button[type=submit]').click()
        expect(lp.locator('#auth-id')).to_be_visible()
        sign_in(lp, origin, 'browser-limited', 'limited-changed-password')
        lp.wait_for_url('**/dashboard*')
        me = limited.request.get(base + '/auth/me').json()
        assert me['session']['remembered'] is True
        assert limited.request.get(base + '/dashboard/users').status == 403
        assert limited.request.get(base + '/dashboard/devices/get_devices?group_id=0').json()[0]['device_name'] == 'device-seven'
        assert len(limited.request.get(base + '/dashboard/devices/get_devices?group_id=8').json()) == 1
        assert [g['group_id'] for g in limited.request.get(base + '/dashboard/groups/get_groups').json()] == [7]
        headers = {'Origin': origin, 'X-CSRF-Token': me['csrf_token']}
        assert limited.request.post(base + '/dashboard/groups/insert_group', headers=headers, data={'group_name': 'Forbidden', 'status': 'active'}).status == 403
        assert limited.request.get(base + '/dashboard/devices/get_devices', headers={'Authorization': 'Bearer forged-device-jwt'}).status == 401
        lp.goto(origin + '/dashboard/devices')
        expect(lp.get_by_text('device-seven', exact=True)).to_be_visible()
        expect(lp.get_by_text('device-eight', exact=True)).to_have_count(0)
        # Password change advanced the user version; reload before an intentional edit.
        page.reload()
        expect(row).to_contain_text('Limited User')
        # UI edits modify actual server memberships and revoke the old session.
        row.get_by_role('button', name='Edit', exact=True).click()
        page.locator('#user-name').fill('Updated User')
        page.locator('#grant-7').select_option('')
        page.locator('#grant-8').select_option('operator')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(row).to_contain_text('Updated User')
        assert limited.request.get(base + '/auth/me').status == 401
        assert limited.request.post(base + '/auth/refresh', headers={'Origin': origin, 'X-CSRF-Token': me['csrf_token']}).status == 401
        sign_in(lp, origin, 'browser-limited', 'limited-changed-password')
        lp.wait_for_url('**/dashboard*')
        me = limited.request.get(base + '/auth/me').json()
        assert me['session']['remembered'] is True
        devices = limited.request.get(base + '/dashboard/devices/get_devices?group_id=7').json()
        assert [d['device_name'] for d in devices] == ['device-eight']
        row.get_by_role('button', name='Reset password', exact=True).click()
        page.locator('#user-password').fill('limited-reset-password')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.locator('.modal-overlay')).to_have_count(0)
        assert limited.request.get(base + '/auth/me').status == 401
        assert limited.request.post(base + '/auth/refresh', headers={'Origin': origin, 'X-CSRF-Token': me['csrf_token']}).status == 401
        sign_in(lp, origin, 'browser-limited', 'limited-reset-password')
        expect(lp.locator('#auth-new-password')).to_be_visible()
        assert limited.request.get(base + '/dashboard/devices/get_devices').status == 403
        # Last ready administrator cannot demote themselves, even though a
        # seeded (password-change-restricted) administrator also exists.
        page.locator('[data-user="browser-admin"]').get_by_role('button', name='Edit', exact=True).click()
        page.locator('#user-role').select_option('user')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.locator('.modal-body [role=alert]')).to_contain_text('Keep at least one administrator')
        page.locator('.modal-foot').get_by_role('button', name='Close', exact=True).click()
        page.goto(origin + '/dashboard/groups')
        gr = page.get_by_role('row').filter(has=page.get_by_role('cell', name='Browser Group', exact=True))
        gr.get_by_role('button', name='Edit', exact=True).click()
        page.locator('.modal-body input[type=text]').fill('Renamed Group')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.get_by_role('cell', name='Renamed Group', exact=True)).to_be_visible()
        page.on('dialog', lambda dialog: dialog.accept())
        gr = page.get_by_role('row').filter(has=page.get_by_role('cell', name='Renamed Group', exact=True))
        gr.get_by_role('button', name='Remove', exact=True).click()
        expect(page.get_by_role('cell', name='Renamed Group', exact=True)).to_have_count(0)
        assert group['group_id'] not in [g['group_id'] for g in admin.request.get(base + '/dashboard/groups/get_groups').json()]
        browser.close()
    print('PASS: real user/group administration, password reset, last-admin protection, group-scoped device browsing and remembered-session revocation after membership/password changes')


if __name__ == '__main__':
    main()
