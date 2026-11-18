#!/usr/bin/env python3
"""Embedded dashboard charts, scoped reads and truthful loading/error states."""
import json
import sys
from collections import Counter
from datetime import datetime
from urllib.parse import urlparse
from zoneinfo import ZoneInfo
from playwright.sync_api import sync_playwright, expect


def main():
    cfg = json.load(sys.stdin)
    origin, base = cfg['origin'], cfg['origin'] + cfg['context']
    assert urlparse(origin).hostname == '127.0.0.1'
    endpoints = ('/dashboard/charts/devices', '/dashboard/charts/schedules', '/dashboard/charts/tasks', '/dashboard/lastest-schedules')
    addresses = {base + endpoint for endpoint in endpoints}
    calls = Counter()

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        ctx = browser.new_context(locale='en-US', timezone_id='Asia/Tokyo')
        ctx.route('**/*', lambda route: route.continue_() if route.request.url.startswith(origin + '/') else route.abort())
        page = ctx.new_page()
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.on('request', lambda request: calls.update([request.url]) if request.url in addresses else None)
        page.goto(origin + '/login')
        page.locator('#auth-id').fill('viewer')
        page.locator('#auth-password').fill(cfg['password'])
        page.locator('button[type=submit]').click()
        page.wait_for_url('**/dashboard*')

        def metric(name):
            return page.locator(f'[data-metric="{name}"] strong')

        def ready():
            expect(page.locator('#dashboard-refresh')).to_be_enabled()
            expect(page.locator('[data-dashboard-error]')).to_have_count(0)
            for name in ('devices', 'schedules', 'tasks', 'errors'):
                expect(metric(name)).to_have_text('1')
            expect(page.locator('[data-chart] canvas')).to_have_count(3)
            expect(page.locator('#dashboard-updated')).to_be_visible()
            expect(page.locator('#latest-updated')).to_be_visible()

        ready()
        assert all(calls[address] == 1 for address in addresses), calls
        row = page.locator(f'tr[data-latest-schedule="{cfg["schedule_id"]}"]')
        expect(row).to_contain_text('Overview seven')
        local_time = datetime.fromisoformat(cfg['history_time'].replace('Z', '+00:00')).astimezone(ZoneInfo('Asia/Tokyo')).strftime('%Y-%m-%d %H:%M:%S')
        expect(row).to_contain_text(local_time)
        expect(page.get_by_text('Overview private eight', exact=True)).to_have_count(0)
        expect(page.locator('tr[data-latest-schedule]')).to_have_count(1)
        for kind in ('devices', 'schedules', 'tasks'):
            canvas = page.locator(f'[data-chart="{kind}"] canvas')
            expect(canvas).to_be_visible()
            assert canvas.evaluate('(canvas) => canvas.width > 0 && canvas.height > 0')
            expect(page.locator(f'[data-chart="{kind}"] [role="img"]')).to_be_visible()
        expect(page.locator('[data-chart="tasks"] [role="img"]')).to_have_attribute('aria-label', 'Running: 0, Wait: 1, Completed: 1, Error: 0')

        # All four summaries share a refresh, without duplicate chart fetches.
        previous = calls.copy()
        with page.expect_request(base + endpoints[2]):
            page.locator('#dashboard-refresh').click()
        ready()
        assert all(calls[address] == previous[address] + 1 for address in addresses), calls
        previous = calls.copy()
        with page.expect_response(lambda response: response.url == base + endpoints[3]):
            page.locator('#latest-refresh').click()
        expect(row).to_be_visible()
        expect(page.locator('#latest-refresh')).to_be_enabled()
        assert calls[base + endpoints[3]] == previous[base + endpoints[3]] + 1, calls
        assert all(calls[base + endpoint] == previous[base + endpoint] for endpoint in endpoints[:3]), calls

        # Keep one request pending to inspect the actual loading state. The
        # other three requests still reach the disposable Go server normally.
        pending = []
        task_url = base + '/dashboard/charts/tasks'
        page.route(task_url, lambda route: pending.append(route))
        with page.expect_request(task_url):
            page.locator('#dashboard-refresh').click()
        expect(page.locator('[data-metric="tasks"]')).to_have_attribute('aria-busy', 'true')
        expect(metric('tasks')).to_have_text('—')
        expect(page.locator('#dashboard-refresh')).to_be_disabled()
        expect(page.locator('[data-chart="tasks"] canvas')).to_have_count(0)
        expect(page.locator('#dashboard-updated')).to_have_count(0)
        assert len(pending) == 1
        pending[0].continue_()
        page.unroute(task_url)
        ready()

        # A real HTTP failure cannot masquerade as a successful zero count or
        # leave a stale chart marked as freshly updated.
        page.route(task_url, lambda route: route.fulfill(status=503, content_type='application/json', body='{"detail":{"code":"unavailable"}}'))
        with page.expect_request(task_url):
            page.locator('#dashboard-refresh').click()
        expect(page.locator('[data-dashboard-error]')).to_be_visible()
        expect(metric('tasks')).to_have_text('—')
        expect(page.locator('[data-chart="tasks"] canvas')).to_have_count(0)
        expect(page.locator('#dashboard-updated')).to_have_count(0)
        expect(page.locator('#dashboard-refresh')).to_be_enabled()
        expect(metric('devices')).to_have_text('1')
        expect(metric('schedules')).to_have_text('1')
        page.unroute(task_url)
        with page.expect_request(task_url):
            page.locator('#dashboard-refresh').click()
        ready()

        # The successful empty-response state is distinct from both failure
        # and loading; numeric zero is valid only after a successful response.
        zero = {'active_count': 0, 'error_count': 0, 'inactive_count': 0}
        empty_responses = {endpoints[0]: zero, endpoints[1]: zero, endpoints[2]: {'pending_count': 0, 'in_progress_count': 0, 'completed_count': 0, 'error_count': 0}, endpoints[3]: []}

        def empty_handler(payload):
            return lambda route: route.fulfill(status=200, content_type='application/json', body=json.dumps(payload))

        for endpoint, payload in empty_responses.items():
            page.route(base + endpoint, empty_handler(payload))
        with page.expect_request(task_url):
            page.locator('#dashboard-refresh').click()
        expect(page.locator('#dashboard-refresh')).to_be_enabled()
        expect(page.locator('[data-dashboard-error]')).to_have_count(0)
        for name in ('devices', 'schedules', 'tasks', 'errors'):
            expect(metric(name)).to_have_text('0')
        expect(page.locator('[data-chart] canvas')).to_have_count(0)
        expect(page.locator('[data-chart] .chart-state')).to_have_count(3)
        expect(page.locator('tr[data-latest-schedule]')).to_have_count(0)
        expect(page.locator('#dashboard-updated')).to_be_visible()
        for endpoint in endpoints:
            page.unroute(base + endpoint)
        previous = calls.copy()
        page.reload()
        ready()
        assert all(calls[address] == previous[address] + 1 for address in addresses), calls
        assert not errors, errors
        browser.close()
    print('PASS: scoped real dashboard charts, recent UTC display, one request per summary, refresh/reload, loading/error recovery and explicit empty states')


if __name__ == '__main__':
    main()
