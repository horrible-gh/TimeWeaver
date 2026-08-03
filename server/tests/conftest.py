"""Test bootstrap for the server package.

``routers.dashboard.tasks`` does ``from config import settings, db`` at import
time, and importing the real ``config`` calls ``database_init()``, which opens a
live MariaDB connection. These tests are about router logic, not about the
driver, so we install a stub ``config`` module before anything imports it.
"""
import importlib
import json
import sys
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace

import pytest

SERVER_ROOT = Path(__file__).resolve().parents[1]
PROJECT_ROOT = SERVER_ROOT.parent

if str(SERVER_ROOT) not in sys.path:
    sys.path.insert(0, str(SERVER_ROOT))

SQLOADER_JSON = SERVER_ROOT / "res" / "sql" / "sqloader" / "mysql" / "time_weaver.json"
MIGRATION_DIR = SERVER_ROOT / "res" / "sql" / "migration" / "mysql"


class StubSqloader:
    """Resolves dotted keys against the real time_weaver.json.

    Reading the actual file rather than hardcoding SQL means these tests still
    fail if a statement is renamed or its placeholder count drifts from the
    tuple the router builds.
    """

    def __init__(self, path=SQLOADER_JSON):
        path = Path(path)
        self._root = path.parent
        self._data = json.loads(path.read_text(encoding="utf-8"))

    def load_sql(self, _file, key):
        node = self._data
        for part in key.split("."):
            node = node[part]
        if not isinstance(node, str):
            raise KeyError(key)
        if node.endswith(".sql"):
            return (self._root / node).read_text(encoding="utf-8")
        return node


_NO_PARAMS = object()


def _validate_positional_binding(query, params=_NO_PARAMS):
    """Mirror PyMySQL's unnamed-%s contract before recording a fake call."""
    placeholders = query.count("%s")
    if placeholders == 0:
        if params is not _NO_PARAMS:
            raise AssertionError("SQL without placeholders must not receive params")
        return
    if params is _NO_PARAMS:
        raise AssertionError(f"SQL expects {placeholders} positional params")
    if not isinstance(params, (tuple, list)):
        raise AssertionError("unnamed %s placeholders require tuple/list params")
    if len(params) != placeholders:
        raise AssertionError(
            f"SQL expects {placeholders} positional params, got {len(params)}"
        )


class RecordingTransaction:
    def __init__(self, db, fail_on):
        self.db = db
        self.fail_on = fail_on
        self.pending = []

    def execute(self, query, params):
        _validate_positional_binding(query, params)
        self.pending.append((query, params))
        if self.fail_on and self.fail_on in query:
            raise RuntimeError(f"simulated DB failure: {self.fail_on}")
        return {"rowcount": 1}


class FakeDbInstance:
    """Mimics the sqloader surface the router uses, with commit semantics.

    ``execute_query`` opens its own connection and commits on its own - that is
    the pre-fix path that produced orphan schedule_detail rows. ``committed``
    only receives statements from a transaction that exited cleanly, so a test
    can assert that a failed task insert leaves nothing behind.
    """

    def __init__(self, fail_on=None):
        self.fail_on = fail_on
        self.committed = []
        self.rolled_back = []
        self.execute_query_calls = []
        self.fetch_all_calls = []
        self.fetch_one_calls = []
        self.user_group_id = 5
        self.transactions = []

    def execute_query(self, query, params):
        _validate_positional_binding(query, params)
        self.execute_query_calls.append((query, params))
        self.committed.append((query, params))
        return {"rowcount": 1}

    def fetch_one(self, query, params=_NO_PARAMS):
        _validate_positional_binding(query, params)
        call = (query,) if params is _NO_PARAMS else (query, params)
        self.fetch_one_calls.append(call)
        if " from users " in " ".join(query.lower().split()):
            return {"group_id": self.user_group_id}
        return {}

    def fetch_all(self, query, params=_NO_PARAMS):
        _validate_positional_binding(query, params)
        call = (query,) if params is _NO_PARAMS else (query, params)
        self.fetch_all_calls.append(call)
        return []

    @contextmanager
    def begin_transaction(self):
        txn = RecordingTransaction(self, self.fail_on)
        self.transactions.append(txn)
        try:
            yield txn
        except Exception:
            self.rolled_back.extend(txn.pending)
            raise
        else:
            self.committed.extend(txn.pending)


def _install_config_stub(db_instance):
    stub = SimpleNamespace(
        settings=SimpleNamespace(
            SECRET_KEY="test-secret-key",
            ACCESS_TOKEN_EXPIRE_MINUTES=30,
            CONTEXT="/timeweaver",
            ALLOWED_ORIGIN="*",
        ),
        db=SimpleNamespace(db_instance=db_instance, sqloader=StubSqloader()),
        get_db_instance=lambda: db_instance,
        get_sqloader_instance=lambda: StubSqloader(),
    )
    sys.modules["config"] = stub
    return stub


# Installed at collection time: routers.dashboard.tasks binds db_instance and
# sqloader to module-level names on import, so the stub has to exist first.
_install_config_stub(FakeDbInstance())


@pytest.fixture
def make_router_module():
    """Reimport one dashboard router bound to a fresh validating fake DB."""

    def _make(module_name, fail_on=None):
        db_instance = FakeDbInstance(fail_on=fail_on)
        _install_config_stub(db_instance)
        for name in list(sys.modules):
            if name.startswith("routers.") or name == "routers":
                del sys.modules[name]
        module = importlib.import_module(f"routers.dashboard.{module_name}")
        module.db_instance = db_instance
        module.sqloader = StubSqloader()
        return module, db_instance

    return _make


@pytest.fixture
def make_tasks_module(make_router_module):
    """Backward-compatible convenience fixture for existing task tests."""

    def _make(fail_on=None):
        return make_router_module("tasks", fail_on=fail_on)

    return _make
