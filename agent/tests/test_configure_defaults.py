"""T0004 6.1 T1/T2: effective default configuration and its diagnostics."""

from agent import configure


def test_sample_fallback_adopts_server_default_base_url():
    # In a clean checkout no real conf/time_weaver.json exists, so the module
    # import adopted the packaged sample (the third candidate).
    assert configure.sample_adopted.get("time_weaver.json") is True
    assert configure.config_sources["time_weaver.json"].endswith(
        "time_weaver.sample.json"
    )
    assert configure.agent_api_config["base_url"] == "http://127.0.0.1:8000/time_weaver"


def test_missing_api_section_applies_context_default_and_warns_once(monkeypatch):
    warns = []
    monkeypatch.setattr(
        configure.Logger, "warn", lambda tag="", msg=None: warns.append(str(tag))
    )

    result = configure.build_agent_api_config({"device": "batch-01"})

    assert result["base_url"] == "http://127.0.0.1:8000/time_weaver"
    assert ":8000" in result["base_url"]
    assert result["base_url"].endswith("/time_weaver")
    assert len(warns) == 1


def test_missing_base_url_key_applies_default_and_warns_once(monkeypatch):
    warns = []
    monkeypatch.setattr(
        configure.Logger, "warn", lambda tag="", msg=None: warns.append(str(tag))
    )

    result = configure.build_agent_api_config(
        {"api": {"credential_path": "conf/agent_credential.json"}}
    )

    assert result["base_url"] == "http://127.0.0.1:8000/time_weaver"
    assert len(warns) == 1
