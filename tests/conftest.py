"""Shared fixtures for the dcode-workspace suite.

The legacy lane fixtures (fake_orgs_tree, add_org/add_agent, provider test
keys) died with ``pux_harness.agent`` — the kept suites build their own trees
against ``tmp_path`` and parameterise the compiler with ``project_root``.
"""
