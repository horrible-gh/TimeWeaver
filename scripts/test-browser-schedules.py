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
            writes = {
                base + '/dashboard/schedule/insert_schedule', base + '/dashboard/schedule/update_schedule',
                base + '/dashboard/tasks/insert_task', base + '/dashboard/tasks/update_task',
            }
            with page.expect_response(lambda response: response.url in writes and response.request.method in ('POST', 'PUT')) as response:
                page.locator('.modal-foot').get_by_role('button', name='Save', exact=True).click()
            assert response.value.ok, f'{response.value.request.method} {urlparse(response.value.url).path}: {response.value.status} {response.value.text()}'
            expect(page.locator('.modal-overlay')).to_have_count(0)

        def schedule_row(name):
            return page.locator('tr[data-schedule]').filter(has=page.get_by_text(name, exact=True))

        page.goto(origin + '/dashboard/schedules')
        page.get_by_role('button', name='New Schedule', exact=True).click()
        page.locator('#schedule-group').select_option('7')
        page.locator('#schedule-name').fill('browser-schedule')
        page.locator('#schedule-device').select_option(label='seven')
        target_device = page.locator('#schedule-device').input_value()
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
        # Verify all four type forms before saving the original command task.
        def assert_fields(visible):
            for field in ('command', 'source_path', 'destination_path', 'archive-type',
                          'missing-source', 'house_keep_days', 'recursive'):
                expect(page.locator('#task-' + field)).to_have_count(int(field in visible))
            for field in ('schedule', 'task_name', 'type', 'sequence', 'status', 'retry'):
                expect(page.locator('#task-' + field)).to_be_visible()

        assert_fields({'command'})
        page.locator('#task-command').fill('echo stale')
        page.locator('#task-type').select_option('copy')
        assert_fields({'source_path','destination_path','missing-source'})
        page.locator('#task-source_path').fill('/tmp/source')
        page.locator('#task-destination_path').fill('/tmp/old-target')
        page.locator('#task-type').select_option('housekeep')
        assert_fields({'destination_path','house_keep_days','recursive'})
        expect(page.locator('#task-destination_path')).to_have_value('')
        expect(page.locator('#task-house_keep_days')).to_have_value('')
        expect(page.locator('#task-recursive')).not_to_be_checked()
        # The checkbox is a full-size theme control aligned with retention.
        recursive = page.locator('#task-recursive')
        option = page.locator('label.housekeep-recursive')
        retention = page.locator('#task-house_keep_days')
        expect(option).to_be_visible()
        rect = option.bounding_box()
        days_rect = retention.bounding_box()
        assert abs(rect['height'] - days_rect['height']) <= 1
        if page.viewport_size['width'] > 1100:
            assert abs(rect['y'] - days_rect['y']) <= 1
            assert abs(rect['width'] - days_rect['width']) <= 1
        expect(option).to_have_css('border-top-width', '1px')
        expect(option).to_have_css('background-color', 'rgb(12, 16, 22)')
        expect(recursive).to_have_css('accent-color', 'rgb(56, 215, 199)')
        # Clicking the label text and using the keyboard both toggle the input.
        option.locator('span').click()
        expect(recursive).to_be_checked()
        recursive.press('Space')
        expect(recursive).not_to_be_checked()
        page.locator('#task-type').select_option('archive')
        assert_fields({'source_path','destination_path','archive-type','missing-source'})
        expect(page.locator('#task-archive-type')).to_have_value('zip')
        page.locator('#task-type').select_option('command')
        assert_fields({'command'})
        expect(page.locator('#task-command')).to_have_value('')
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
        # Production Python schedules may belong to Unknown (group zero) while
        # their agent belongs to a named group. Task creation must inherit that
        # assignment, not ask the user to select a different execution device.
        legacy = next(s for s in ctx.request.get(base + '/dashboard/tasks/get_schedule_groups').json()
                      if s['name'] == 'legacy-cross-group')
        assert legacy['group_id'] == 0 and legacy['can_edit']
        page.get_by_role('button', name='Add', exact=True).click()
        page.locator('#task-schedule').select_option(str(legacy['schedule_id']))
        page.locator('#task-task_name').fill('legacy-housekeep')
        page.locator('#task-type').select_option('housekeep')
        assert_fields({'destination_path','house_keep_days','recursive'})
        page.locator('#task-destination_path').fill('/tmp/timeweaver-browser-fixture')
        page.locator('#task-house_keep_days').fill('1')
        page.locator('#task-recursive').check()
        page.locator('#task-sequence').fill('20')
        save()
        hkrow = page.locator('tr[data-task]').filter(has=page.get_by_text('legacy-housekeep', exact=True))
        expect(hkrow).to_be_visible()
        hkid = hkrow.get_attribute('data-task')
        hkrow.get_by_role('button', name='Edit', exact=True).click()
        expect(page.locator('#task-schedule')).to_have_value(str(legacy['schedule_id']))
        expect(page.locator('#task-destination_path')).to_have_value('/tmp/timeweaver-browser-fixture')
        expect(page.locator('#task-recursive')).to_be_checked()
        page.locator('#task-task_name').fill('legacy-housekeep-edited')
        save()
        hk = next(t for t in ctx.request.get(base + '/dashboard/tasks/get_tasks').json() if t['detail_id'] == hkid)
        assert hk['task_name'] == 'legacy-housekeep-edited' and hk['house_keep_recursive']
        assert hk['schedule_id'] == legacy['schedule_id'] and hk['sequence'] == 20
        assert not hk['command'] and not hk['source_path'] and not hk['archive_type']
        assert not hk['error_on_missing_source']
        # Save and edit actual file tasks as well, against the same API.
        for kind in ('copy', 'archive'):
            page.get_by_role('button', name='Add', exact=True).click()
            page.locator('#task-schedule').select_option(str(legacy['schedule_id']))
            page.locator('#task-task_name').fill('browser-' + kind)
            page.locator('#task-type').select_option(kind)
            assert_fields({'source_path','destination_path','missing-source'} |
                          ({'archive-type'} if kind == 'archive' else set()))
            page.locator('#task-source_path').fill('/tmp/source-fixture')
            page.locator('#task-destination_path').fill('/tmp/destination-fixture')
            page.locator('#task-missing-source').select_option('0')
            save()
            row = page.locator('tr[data-task]').filter(has=page.get_by_text('browser-' + kind, exact=True))
            expect(row).to_be_visible()
            fid = row.get_attribute('data-task')
            row.get_by_role('button', name='Edit', exact=True).click()
            expect(page.locator('#task-source_path')).to_have_value('/tmp/source-fixture')
            expect(page.locator('#task-destination_path')).to_have_value('/tmp/destination-fixture')
            expect(page.locator('#task-missing-source')).to_have_value('0')
            if kind == 'archive':
                expect(page.locator('#task-archive-type')).to_have_value('zip')
            page.locator('#task-task_name').fill('browser-' + kind + '-edited')
            save()
            saved = next(t for t in ctx.request.get(base + '/dashboard/tasks/get_tasks').json() if t['detail_id'] == fid)
            assert not saved['command'] and not saved['house_keep_recursive'] and not saved['error_on_missing_source']
            assert saved['house_keep_days'] is None
            assert saved['archive_type'] == ('zip' if kind == 'archive' else None)
        # The schedule edit form must also preserve the original target option.
        page.goto(origin + '/dashboard/schedules')
        expect(schedule_row('legacy-cross-group')).to_be_visible()
        schedule_row('legacy-cross-group').get_by_role('button', name='Edit', exact=True).click()
        expect(page.locator('#schedule-group')).to_have_value('0')
        expect(page.locator('#schedule-device')).to_have_value(str(legacy['target_device']))
        page.locator('#schedule-status').select_option('inactive')
        save()
        expect(schedule_row('legacy-cross-group')).to_contain_text('inactive')
        current = next(s for s in ctx.request.get(base + '/dashboard/tasks/get_schedule_groups').json()
                       if s['schedule_id'] == legacy['schedule_id'])
        assert current['target_device'] == legacy['target_device'] and current['group_id'] == 0

        # Rows must not offer Edit before its reference options are ready.
        # Previously the group watcher cleared the saved target in this window,
        # so a status-only save incorrectly sent target_device:null.
        pending_devices = []
        devices_url = base + '/dashboard/schedule/get_devices'
        def hold_devices(route):
            pending_devices.append(route)
        page.route(devices_url, hold_devices)
        with page.expect_request(lambda request: request.url == devices_url):
            page.goto(origin + '/dashboard/schedules')
        expect(schedule_row('browser-renamed')).to_have_count(0)
        assert len(pending_devices) == 1
        pending_devices.pop().continue_()
        page.unroute(devices_url, hold_devices)
        expect(schedule_row('browser-renamed')).to_be_visible()
        schedule_row('browser-renamed').get_by_role('button', name='Edit', exact=True).click()
        expect(page.locator('#schedule-device')).to_have_value(target_device)
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
    print('PASS: embedded schedule/task CRUD, typed payloads, weekday contract, role scopes, history-safe deletion, deferred device options and fresh forms')


if __name__ == '__main__':
    main()
