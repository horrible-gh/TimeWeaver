#!/usr/bin/env python3
"""Real embedded device editing and one-time enrollment on disposable loopback."""
import json
import sys
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright, expect


def sign_in(page, origin, user, password):
    page.goto(origin + '/login')
    page.locator('#auth-id').fill(user)
    page.locator('#auth-password').fill(password)
    page.locator('button[type=submit]').click()
    page.wait_for_url('**/dashboard*')


def context(browser, origin):
    ctx = browser.new_context(locale='en-US')
    ctx.route('**/*', lambda route: route.continue_() if route.request.url.startswith(origin + '/') else route.abort())
    return ctx


def main():
    cfg = json.load(sys.stdin)
    origin = cfg['origin']
    assert urlparse(origin).hostname == '127.0.0.1'
    base = origin + cfg['context']
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        admin = context(browser, origin)
        page = admin.new_page()
        page.on('dialog', lambda dialog: dialog.accept())
        sign_in(page, origin, cfg['user'], cfg['password'])
        page.goto(origin + '/dashboard/devices')
        expect(page.get_by_role('button', name='Add Manually', exact=True)).to_be_visible()

        def row(name):
            return page.locator('tr[data-device]').filter(has=page.get_by_text(name, exact=True))

        def save():
            page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
            expect(page.locator('.modal-overlay')).to_have_count(0)

        def issue(name, group='7'):
            page.get_by_role('button', name='Enroll Agent', exact=True).click()
            page.locator('#enrollment-group').select_option(group)
            page.locator('#enrollment-device-name').fill(name)
            with page.expect_response(lambda r: r.request.method == 'POST' and r.url.endswith('/dashboard/agent-enrollment-tokens')) as response:
                page.get_by_role('button', name='Issue Material', exact=True).click()
            assert response.value.status == 201
            return response.value.json()['data']

        def enroll(material, name):
            res = admin.request.post(base + '/api/agent/v1/enroll', data={'enrollment_token': material['token'], 'device_name': name, 'agent_version': 'browser-contract'})
            assert res.status == 200 or res.status == 201
            return res.json()['data']

        page.get_by_role('button', name='Add Manually', exact=True).click()
        page.locator('#device-group').select_option('7')
        page.locator('#device-name').fill('browser-manual')
        save()
        expect(row('browser-manual')).to_be_visible()
        row('browser-manual').get_by_role('button', name='Edit', exact=True).click()
        page.locator('#device-name').fill('browser-renamed')
        save()
        expect(row('browser-renamed')).to_be_visible()
        row('browser-renamed').get_by_role('button', name='Remove', exact=True).click()
        expect(row('browser-renamed')).to_have_count(0)

        # Editors can manage only their group; neither operators nor viewers can edit.
        for role in ['editor', 'operator', 'viewer']:
            ctx = context(browser, origin)
            lp = ctx.new_page()
            sign_in(lp, origin, role, cfg['password'])
            lp.goto(origin + '/dashboard/devices')
            expect(lp.get_by_text('seven', exact=True)).to_be_visible()
            expect(lp.get_by_text('eight', exact=True)).to_have_count(0)
            expect(lp.get_by_role('button', name='Enroll Agent', exact=True)).to_have_count(0)
            expect(lp.get_by_role('button', name='Reissue', exact=True)).to_have_count(0)
            if role == 'editor':
                lp.get_by_role('button', name='Add Manually', exact=True).click()
                assert lp.locator('#device-group option').all_text_contents() == ['Seven']
            else:
                expect(lp.get_by_role('button', name='Add Manually', exact=True)).to_have_count(0)
                expect(lp.get_by_role('button', name='Edit', exact=True)).to_have_count(0)
            me = ctx.request.get(base + '/auth/me').json()
            bad = ctx.request.post(base + '/dashboard/devices/insert_device', headers={'Origin': origin, 'X-CSRF-Token': me['csrf_token']}, data={'group_id': 8, 'device_name': 'forged', 'status': 'active'})
            assert bad.status == 403
            assert ctx.request.get(base + '/dashboard/agent-enrollment-tokens').status == 403
            ctx.close()

        material = issue('browser-agent')
        # Material is one-time display data, never a stored browser credential.
        stored = page.evaluate('JSON.stringify(localStorage) + JSON.stringify(sessionStorage)')
        assert material['token'] not in stored
        data = enroll(material, 'browser-agent')
        page.goto(origin + '/dashboard/devices')
        expect(row('browser-agent')).to_be_visible()
        history = page.locator('[data-enrollment="' + material['enrollment_id'] + '"]')
        expect(history.locator('.status-text.used')).to_be_visible()
        expect(history).to_contain_text(str(data['device_id']))
        listed = admin.request.get(base + '/dashboard/agent-enrollment-tokens').text()
        assert material['token'] not in listed and 'token_hash' not in listed

        # Reissue from a row keeps both the selected name and its group.
        row('browser-agent').get_by_role('button', name='Reissue', exact=True).click()
        expect(page.locator('#enrollment-group')).to_have_value('7')
        expect(page.locator('#enrollment-existing-device')).to_have_value('browser-agent')
        page.locator('#enrollment-group').select_option('8')
        expect(page.locator('#enrollment-existing-device')).to_have_value('')
        assert 'browser-agent' not in page.locator('#enrollment-existing-device option').all_text_contents()
        page.locator('#enrollment-group').select_option('7')
        page.locator('#enrollment-existing-device').select_option('browser-agent')
        with page.expect_response(lambda r: r.request.method == 'POST' and r.url.endswith('/dashboard/agent-enrollment-tokens')) as response:
            page.get_by_role('button', name='Issue Material', exact=True).click()
        assert response.value.status == 201
        replacement = enroll(response.value.json()['data'], 'browser-agent')
        assert replacement['device_id'] == data['device_id']
        assert admin.request.get(base + '/api/agent/v1/snapshot', headers={'Authorization': 'Bearer ' + data['access_token']}).status == 403
        page.goto(origin + '/dashboard/devices')
        row('browser-agent').get_by_role('button', name='Edit', exact=True).click()
        page.locator('#device-status').select_option('inactive')
        save()
        assert admin.request.get(base + '/api/agent/v1/snapshot', headers={'Authorization': 'Bearer ' + replacement['access_token']}).status == 403
        row('browser-agent').get_by_role('button', name='Remove', exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('linked records')
        expect(row('browser-agent')).to_be_visible()
        row('browser-agent').get_by_role('button', name='Edit', exact=True).click()
        page.locator('#device-status').select_option('active')
        save()
        assert admin.request.get(base + '/api/agent/v1/snapshot', headers={'Authorization': 'Bearer ' + replacement['access_token']}).status == 200

        pending = issue('pending-browser-agent')
        page.goto(origin + '/dashboard/devices')
        token_row = page.locator('[data-enrollment="' + pending['enrollment_id'] + '"]')
        token_row.get_by_role('button', name='Revoke', exact=True).click()
        expect(token_row.locator('.status-text.revoked')).to_be_visible()
        assert admin.request.post(base + '/api/agent/v1/enroll', data={'enrollment_token': pending['token'], 'device_name': 'pending-browser-agent'}).status == 403
        browser.close()
    print('PASS: embedded device CRUD, role-scoped controls, token issue/reissue/revoke, credential pause and linked-record preservation')


if __name__ == '__main__':
    main()
