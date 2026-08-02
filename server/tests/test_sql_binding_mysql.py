"""Live-MariaDB regression for the dashboard device binding failure."""
import asyncio
import os

import pytest
from sqloader.mysql import MySqlWrapper

from schemas.devices import DeviceGetRequest, DeviceInsertRequest
from test_mysql_bootstrap import empty_database, run_server_migrator


MYSQL_HOST = os.getenv("TIMEWEAVER_TEST_MYSQL_HOST")
pytestmark = pytest.mark.skipif(
    not MYSQL_HOST,
    reason="set TIMEWEAVER_TEST_MYSQL_HOST to run live SQL binding tests",
)


def test_devices_group_id_five_and_multi_placeholder_insert_are_valid(
    empty_database, make_router_module
):
    run_server_migrator(empty_database)
    wrapper = MySqlWrapper(
        host=MYSQL_HOST,
        port=int(os.getenv("TIMEWEAVER_TEST_MYSQL_PORT", "3306")),
        user=os.getenv("TIMEWEAVER_TEST_MYSQL_USER", "root"),
        password=os.getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD", ""),
        db=empty_database,
    )
    module, _ = make_router_module("devices")
    module.db_instance = wrapper
    try:
        asyncio.run(module.insert_device(DeviceInsertRequest(
            group_id=5,
            device_name="binding-contract-device",
            status="active",
            creator="binding-test",
        )))

        rows = asyncio.run(module.get_devices(DeviceGetRequest(group_id=5)))

        assert [row["device_name"] for row in rows] == ["binding-contract-device"]
    finally:
        wrapper.close()