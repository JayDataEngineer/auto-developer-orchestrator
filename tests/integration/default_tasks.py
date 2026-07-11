# Per-org default forcing tasks. Each is UN-answerable from the system prompt
# alone, so the agent must delegate to its specialist — the seam we're proving.
# Tasks name the NATIVE `execute` tool; `pux_sandbox_bash` is gone.
DEFAULT_TASKS: dict[str, str] = {
    "general": (
        "How many Python modules ship under /sandbox/workspace/pux-harness/pux_harness/, "
        "and what are their names? Delegate to the `researcher` subagent — do NOT "
        "inspect the codebase yourself. Have it run, via the native `execute` tool: "
        "`find /sandbox/workspace/pux-harness/pux_harness -name '*.py'`. "
        "Report the researcher's findings verbatim."
    ),
    "_demo": (
        "List the top-level entries of the project root inside the sandbox. Delegate "
        "to the `researcher` subagent — do NOT run tools yourself. Have it use the "
        "native `execute` tool: `ls -1 /sandbox/workspace`. Report its findings verbatim."
    ),
    # The coder sample Go package was removed in the pi-pivot; coder now
    # ships only its 3 specialist agents. Target THOSE — the explorer's real
    # read-only-investigator job (grep the frontmatter `name:` field, cite files).
    "coder": (
        "What specialist agents does the coder org ship? Delegate to the "
        "`coder-explorer` subagent — do NOT inspect the code yourself. The agent "
        "definitions live under /sandbox/workspace/orgs/specialists/coder/agents/. "
        "Have the explorer find every agent's declared `name:` field (in each `.md` "
        "frontmatter) and report each name with its file citation. "
        "Report its findings verbatim."
    ),
    "fs-explorer": (
        "What top-level directories exist under /sandbox/workspace/? "
        "Use the native `execute` tool: `ls -1 /sandbox/workspace`. "
        "Report the entries verbatim."
    ),
    "web-search": (
        "Use the web_research `search` tool to look up 'pux sandbox orchestrator'. "
        "Report the top result title and URL verbatim."
    ),
    # --- The remaining 7 orgs. Each forces delegation to a named
    # specialist and drives a NATIVE tool against the org's OWN bundled content
    # (no external keys/images needed). Answers are verifiable against the FS.
    "invest": (
        "How many Python modules are under /sandbox/workspace/orgs/specialists/invest/sandbox/? "
        "Delegate to the `invest-researcher` subagent — do NOT inspect the code "
        "yourself. Have it run, via the native `execute` tool: "
        "`find /sandbox/workspace/orgs/specialists/invest/sandbox -name '*.py'`. "
        "Report the count and the module filenames verbatim."
    ),
    "game-studio": (
        "What playbook markdown docs ship under /sandbox/workspace/orgs/specialists/game-studio/skills/? "
        "Delegate to the `game-studio-docs-writer` subagent — do NOT look yourself. "
        "Have it use the native `glob` tool for "
        "`/sandbox/workspace/orgs/specialists/game-studio/skills/*.md` and list each filename. "
        "Report verbatim."
    ),
    "deep-research-engine": (
        "How many Python modules are under /sandbox/workspace/orgs/specialists/deep-research-engine/sandbox/? "
        "Delegate to the `dre-auditor` subagent — do NOT inspect yourself. Have it "
        "run, via the native `execute` tool: "
        "`find /sandbox/workspace/orgs/specialists/deep-research-engine/sandbox -name '*.py'`. "
        "Report the count and module filenames verbatim."
    ),
    "social-media-pipeline": (
        "Read the campaign-angles file at "
        "/sandbox/workspace/orgs/specialists/social-media-pipeline/data/options.json. Delegate to "
        "the `smp-writer` subagent — do NOT read it yourself. Have it use the native "
        "`read_file` tool, then report how many angles there are and the id + angle "
        "of each. Report verbatim."
    ),
    "twitter-agent": (
        "What helper docs ship under /sandbox/workspace/orgs/specialists/twitter-agent/skills/? "
        "Delegate to the `twitter-drafter` subagent — do NOT look yourself. Have it "
        "use the native `glob` tool for `/sandbox/workspace/orgs/specialists/twitter-agent/skills/**`. "
        "Report the filenames found."
    ),
    "telegram-agent": (
        "Read the campaign file at /sandbox/workspace/orgs/specialists/telegram-agent/data/campaign.json. "
        "Delegate to the `telegram-drafter` subagent — do NOT read it yourself. Have it "
        "use the native `read_file` tool, then report how many messages the campaign "
        "contains. Report the count verbatim."
    ),
    "video-production": (
        "What ships under /sandbox/workspace/orgs/specialists/video-production/skills/? Delegate to "
        "the `video-scriptwriter` subagent — do NOT look yourself. Have it use the "
        "native `execute` tool: `ls -1 /sandbox/workspace/orgs/specialists/video-production/skills`. "
        "Report the entries verbatim."
    ),
    "orchestrator": (
        "What specialist agents does the orchestrator org ship, and what agents "
        "does it inherit from general? Delegate to the `task-planner` subagent — "
        "do NOT inspect the code yourself. Have it use the native `execute` tool: "
        "`cat /sandbox/workspace/orgs/specialists/orchestrator/org.yaml`. "
        "Report the extends chain and the agent roster verbatim."
    ),
}
