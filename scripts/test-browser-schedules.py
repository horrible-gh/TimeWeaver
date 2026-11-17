#!/usr/bin/env python3
"""Real embedded schedule/task CRUD against an explicitly disposable database."""
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

        def login(user):
            ctx = browser.new_context(locale='en-US')
            ctx.route('**/*', lambda route: route.continue_() if route.request.url.startswith(origin + '/') else route.abort())
            page = ctx.new_page()
            page.on('dialog', lambda dialog: dialog.accept())
            page.goto(origin + '/login')
            page.locator('#auth-id').fill(user)
            page.locator('#auth-password').fill(cfg['password'])
            page.locator('button[type=submit]').click()
            page.wait_for_url('**/dashboard*')
            return ctx, page

        ctx, page = login(cfg['user'])

        def save():
            page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
            expect(page.locator('.modal-overlay')).to_have_count(0)

        def schedule_row(name):
            return page.locator('tr[data-schedule]').filter(has=page.get_by_text(name, exact=True))

        page.goto(origin + '/dashboard/schedules')
        page.get_by_role('button', name='New Schedule', exact=True).click()
        page.locator('#schedule-group').select_option('7')
        page.locator('#schedule-name').fill('browser-schedule')
        page.locator('#schedule-device').select_option(label='seven')
        # This UI formerly labelled Sunday as 0; the actual agent uses Monday=0.
        page.locator('#schedule-weekday').select_option(label='Sun')
        expect(page.locator('#schedule-weekday')).to_have_value('6')
        page.locator('#schedule-second').fill('60')
        page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
        expect(page.locator('.modal-body [role=alert]')).to_contain_text('schedule fields')
        page.locator('#schedule-second').fill('0')
        save()
        expect(schedule_row('browser-schedule')).to_be_visible()
        sid = schedule_row('browser-schedule').get_attribute('data-schedule')
        schedule_row('browser-schedule').get_by_role('button', name='Edit', exact=True).click()
        page.locator('#schedule-name').fill('browser-renamed')
        save()
        expect(schedule_row('browser-renamed')).to_be_visible()

        page.goto(origin + '/dashboard/tasks')
        page.get_by_role('button', name='Add', exact=True).click()
        page.locator('#task-schedule').select_option(sid)
        page.locator('#task-task_name').fill('browser-task')
        page.locator('#task-command').fill('echo browser-contract')
        page.locator('#task-sequence').fill('3')
        page.locator('#task-retry').fill('2')
        save()
        row = page.locator('tr[data-task]').filter(has=page.get_by_text('browser-task', exact=True))
        expect(row).to_be_visible()
        tid = row.get_attribute('data-task')
        row.get_by_role('button', name='Edit', exact=True).click()
        expect(page.locator('#task-sequence')).to_have_value('3')
        expect(page.locator('#task-retry')).to_have_value('2')
        page.locator('#task-task_name').fill('browser-task-edited')
        save()
        expect(page.locator(f'tr[data-task="{tid}"]')).to_contain_text('browser-task-edited')
        # Strict write DTOs do not echo server metadata back into mutations.
        me = ctx.request.get(base + '/auth/me').json()
        listed = ctx.request.get(base + '/dashboard/tasks/get_tasks').json()
        task = next(t for t in listed if t['detail_id'] == tid)
        assert task['sequence'] == 3 and task['retry_count'] == 2
        assert not task['house_keep_recursive']
        assert task['creator'] == cfg['user']

        for role in ('viewer', 'operator', 'editor'):
            limited, lp = login(role)
            lp.goto(origin + '/dashboard/schedules')
            expect(lp.locator(f'tr[data-schedule="{sid}"]')).to_contain_text('browser-renamed')
            expect(lp.get_by_text('private-eight', exact=True)).to_have_count(0)
            if role != 'editor':
                expect(lp.get_by_role('button', name='New Schedule', exact=True)).to_have_count(0)
                expect(lp.get_by_role('button', name='Edit', exact=True)).to_have_count(0)
            data = limited.request.get(base + '/dashboard/schedule/get_schedule_groups?group_id=8').json()
            assert all(item['group_id'] == 7 for item in data)
            lp.goto(origin + '/dashboard/tasks')
            expect(lp.locator(f'tr[data-task="{tid}"]')).to_contain_text('browser-task-edited')
            if role != 'editor':
                expect(lp.get_by_role('button', name='Add', exact=True)).to_have_count(0)
                expect(lp.get_by_role('button', name='Edit', exact=True)).to_have_count(0)
                lm = limited.request.get(base + '/auth/me').json()
                refused = limited.request.delete(base + '/dashboard/tasks/remove_task/' + tid, headers={'Origin': origin, 'X-CSRF-Token': lm['csrf_token']})
                assert refused.status == 403
            expect(lp.get_by_role('button', name='Run', exact=True)).to_have_count(0 if role == 'viewer' else 1)
            limited.close()

        page.goto(origin + '/dashboard/schedules')
        schedule_row('browser-renamed').get_by_role('button', name='Remove', exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('linked records')
        page.goto(origin + '/dashboard/tasks')
        page.locator(f'tr[data-task="{tid}"]').get_by_role('button', name='Remove', exact=True).click()
        expect(page.locator(f'tr[data-task="{tid}"]')).to_have_count(0)
        page.goto(origin + '/dashboard/schedules')
        schedule_row('browser-renamed').get_by_role('button', name='Edit', exact=True).click()
        page.locator('#schedule-status').select_option('inactive')
        save()
        expect(schedule_row('browser-renamed')).to_contain_text('inactive')
        # A new form cannot inherit the edited item's ID or status.
        page.get_by_role('button', name='New Schedule', exact=True).click()
        expect(page.locator('#schedule-name')).to_have_value('')
        expect(page.locator('#schedule-status')).to_have_value('active')
        page.locator('#schedule-group').select_option('7')
        page.locator('#schedule-device').select_option(label='seven')
        page.locator('#schedule-name').fill('empty-to-remove')
        save()
        schedule_row('empty-to-remove').get_by_role('button', name='Remove', exact=True).click()
        expect(schedule_row('empty-to-remove')).to_have_count(0)
        browser.close()
    print('PASS: embedded schedule/task CRUD, typed payloads, weekday contract, role scopes, history-safe deletion and fresh forms')


if __name__ == '__main__':
    main()
