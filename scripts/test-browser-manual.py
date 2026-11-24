#!/usr/bin/env python3
"""Real human manual-run and history UI against a disposable loopback DB."""
import json
import sys
from datetime import datetime, timedelta, timezone
from urllib.parse import urlencode, urlparse
from zoneinfo import ZoneInfo
from playwright.sync_api import sync_playwright, expect


def main():
    cfg = json.load(sys.stdin)
    origin, base = cfg['origin'], cfg['origin'] + cfg['context']
    assert urlparse(origin).hostname == '127.0.0.1'
    tokyo = ZoneInfo('Asia/Tokyo')

    def local_input(value):
        return value.astimezone(tokyo).strftime('%Y-%m-%dT%H:%M:%S')

    def utc(value):
        return value.astimezone(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)

        def login(user):
            ctx = browser.new_context(locale='en-US', timezone_id='Asia/Tokyo')
            ctx.route('**/*', lambda route: route.continue_() if route.request.url.startswith(origin + '/') else route.abort())
            page = ctx.new_page()
            page.on('dialog', lambda dialog: dialog.accept())
            page.goto(origin + '/login')
            page.locator('#auth-id').fill(user)
            page.locator('#auth-password').fill(cfg['password'])
            page.locator('button[type=submit]').click()
            page.wait_for_url('**/dashboard*')
            current = ctx.request.get(base + '/auth/me').json()
            headers = {'Origin': origin, 'X-CSRF-Token': current['csrf_token']}
            return ctx, page, headers

        ctx, page, headers = login('operator')

        def confirm_request(path, title):
            with page.expect_response(lambda response: response.url == base + path and response.request.method in ('POST', 'PUT')) as response:
                page.locator('.modal-foot').get_by_role('button', name=title, exact=True).click()
            assert response.value.status == 200, response.value.text()
            expect(page.locator('.modal-overlay')).to_have_count(0)
            return response.value.json()

        def manual_rows():
            result = ctx.request.get(base + '/dashboard/manual_execution/get_manual_execution_list')
            assert result.status == 200, result.text()
            return result.json()

        def manual_row(mid):
            return page.locator(f'tr[data-manual="{mid}"]')

        # One schedule action creates one request per eligible task atomically.
        page.goto(origin + '/dashboard/schedules')
        schedule = page.locator(f'tr[data-schedule="{cfg["schedule_id"]}"]')
        expect(schedule).to_contain_text('manual-seven')
        expect(page.get_by_text('manual-eight-private', exact=True)).to_have_count(0)
        expect(page.get_by_role('button', name='New Schedule', exact=True)).to_have_count(0)
        schedule.get_by_role('button', name='Run', exact=True).click()
        page.locator('#manual-run-method').select_option('1')
        immediate = confirm_request('/dashboard/schedule/insert_manual_schedule', 'Run')
        assert immediate['count'] == 2 and len(immediate['manual_ids']) == 2
        by_id = {row['manual_id']: row for row in manual_rows()}
        assert all(by_id[mid]['status'] == 'wait' and by_id[mid]['is_immediate'] in (1, True) for mid in immediate['manual_ids'])

        # datetime-local is the browser's local zone. The API persists a UTC instant.
        reservation_time = (datetime.now(timezone.utc) + timedelta(days=2)).replace(second=17, microsecond=0)
        page.goto(origin + '/dashboard/tasks')
        task = page.locator(f'tr[data-task="{cfg["task_id"]}"]')
        task.get_by_role('button', name='Run', exact=True).click()
        page.locator('#manual-run-method').select_option('0')
        page.locator('#manual-run-datetime').fill(local_input(reservation_time))
        reservation = confirm_request('/dashboard/tasks/insert_manual_task', 'Run')
        assert reservation['count'] == 1
        reserved_id = reservation['manual_ids'][0]
        record = next(row for row in manual_rows() if row['manual_id'] == reserved_id)
        assert record['schedule_datetime'] == utc(reservation_time), record
        assert record['status'] == 'wait' and record['is_immediate'] in (0, False)

        page.goto(origin + '/dashboard/manual-execution')
        expect(manual_row(reserved_id)).to_be_visible()
        expect(page.get_by_text('manual-eight-private', exact=True)).to_have_count(0)
        expect(manual_row(cfg['processing_id'])).to_contain_text('processing')
        expect(manual_row(cfg['processing_id']).get_by_role('button')).to_have_count(0)
        manual_row(reserved_id).get_by_role('button', name='Edit', exact=True).click()
        expect(page.locator('#manual-edit-method')).to_have_value('0')
        expect(page.locator('#manual-edit-datetime')).to_have_value(local_input(reservation_time))
        changed_time = reservation_time + timedelta(hours=1)
        page.locator('#manual-edit-datetime').fill(local_input(changed_time))
        replacement = confirm_request('/dashboard/manual_execution/update_manual_execution', 'Save')
        previous_id = reserved_id
        reserved_id = replacement['manual_id']
        assert replacement['previous_manual_id'] == previous_id and reserved_id != previous_id
        by_id = {row['manual_id']: row for row in manual_rows()}
        assert by_id[previous_id]['status'] == 'failed'
        assert by_id[reserved_id]['status'] == 'wait' and by_id[reserved_id]['schedule_datetime'] == utc(changed_time)
        expect(manual_row(previous_id)).to_contain_text('failed')

        # A cancellation affects only a pending request; terminal rows remain visible.
        with page.expect_response(lambda response: response.url == base + '/dashboard/manual_execution/abandon_manual_execution' and response.request.method == 'PUT') as response:
            manual_row(reserved_id).get_by_role('button', name='Cancel request', exact=True).click()
        assert response.value.status == 200, response.value.text()
        expect(manual_row(reserved_id)).to_contain_text('failed')
        expect(manual_row(reserved_id).get_by_role('button')).to_have_count(0)
        processing_denied = ctx.request.put(base + '/dashboard/manual_execution/abandon_manual_execution', headers=headers, data={'manual_id': cfg['processing_id']})
        assert processing_denied.status == 409, processing_denied.text()
        hidden_denied = ctx.request.post(base + '/dashboard/tasks/insert_manual_task', headers=headers, data={'detail_id': cfg['private_task_id'], 'is_immediate': True})
        assert hidden_denied.status == 403, hidden_denied.text()

        viewer, vp, viewer_headers = login('viewer')
        for path in ('/dashboard/schedules', '/dashboard/tasks'):
            vp.goto(origin + path)
            expect(vp.get_by_role('button', name='Run', exact=True)).to_have_count(0)
        vp.goto(origin + '/dashboard/manual-execution')
        expect(vp.locator(f'tr[data-manual="{reserved_id}"]')).to_contain_text('failed')
        expect(vp.locator('tr[data-manual] button')).to_have_count(0)
        for endpoint, payload, method in (
            ('/dashboard/tasks/insert_manual_task', {'detail_id': cfg['task_id'], 'is_immediate': True}, 'post'),
            ('/dashboard/manual_execution/abandon_manual_execution', {'manual_id': immediate['manual_ids'][0]}, 'put'),
        ):
            denied = getattr(viewer.request, method)(base + endpoint, headers=viewer_headers, data=payload)
            assert denied.status == 403, denied.text()

        # Viewer history remains scoped, and server-side ranges use the same UTC instant.
        vp.goto(origin + '/dashboard/schedule-history')
        history_row = vp.locator(f'tr[data-execution="{cfg["history_id"]}"]')
        expect(history_row).to_contain_text('visible-ok')
        expect(vp.get_by_text('private-no', exact=True)).to_have_count(0)
        history_response = viewer.request.get(base + '/dashboard/schedule/execution_history')
        assert history_response.status == 200
        histories = history_response.json()
        assert len(histories) == 1 and histories[0]['start_time'] == cfg['history_time'], histories
        moment = datetime.fromisoformat(cfg['history_time'].replace('Z', '+00:00'))
        for start, end, expected_count in (
            (moment - timedelta(days=2), moment - timedelta(hours=1), 0),
            (moment - timedelta(hours=1), moment + timedelta(hours=1), 1),
        ):
            vp.locator('#history-start').fill(local_input(start))
            vp.locator('#history-end').fill(local_input(end))
            with vp.expect_response(lambda response: response.url.startswith(base + '/dashboard/schedule/execution_history?')) as response:
                vp.locator('#history-apply').click()
            assert response.value.status == 200, response.value.text()
            expect(vp.locator('tr[data-execution]')).to_have_count(expected_count)
        bad_range = viewer.request.get(base + '/dashboard/schedule/execution_history?' + urlencode({'start_date': utc(moment + timedelta(hours=1)), 'end_date': utc(moment)}))
        assert bad_range.status == 422, bad_range.text()
        viewer.close()

        # Admin sees >1000 rows on the initial 30-day load. Server pagination
        # and filters cover the full authorized range, including off-page rows.
        admin, ap, _ = login('timeweaver')
        ap.goto(origin + '/dashboard/schedule-history')
        expect(ap.locator('tr[data-execution]')).to_have_count(7)
        expect(ap.locator('[role=alert]')).to_have_count(0)
        expect(ap.locator('.panel-foot')).to_contain_text('1002')
        first_ids = ap.locator('tr[data-execution]').evaluate_all('(rows) => rows.map(r => r.dataset.execution)')
        ap.locator('.pages').get_by_role('button', name='2', exact=True).click()
        expect(ap.locator('.pages .active')).to_have_text('2')
        second_ids = ap.locator('tr[data-execution]').evaluate_all('(rows) => rows.map(r => r.dataset.execution)')
        assert len(second_ids) == 7 and not set(first_ids).intersection(second_ids)
        ap.locator('#history-group').select_option('manual-seven')
        expect(ap.locator(f'tr[data-execution="{cfg["history_id"]}"]')).to_contain_text('visible-ok')
        expect(ap.locator('tr[data-execution]')).to_have_count(1)
        expect(ap.locator('.pages .active')).to_have_text('1')
        ap.locator('#history-group').select_option('')
        expect(ap.locator('tr[data-execution]')).to_have_count(7)
        ap.locator('#history-code').select_option('1')
        expect(ap.locator('.panel-foot')).to_contain_text('1000')
        ap.locator('#history-code').select_option('')
        expect(ap.locator('.panel-foot')).to_contain_text('1002')
        ap.locator('th').filter(has_text='ID').click()
        expect(ap.locator('tr[data-execution]').first).to_have_attribute('data-execution', str(cfg['history_id']))
        admin.close()
        browser.close()
    print('PASS: operator schedule fan-out, task reservation UTC round trip, pending edit/cancel, processing conflict, viewer scopes and execution-history range filtering')


if __name__ == '__main__':
    main()
