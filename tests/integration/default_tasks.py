# Per-org default forcing tasks. Each is UN-answerable from the system prompt
# alone, so the agent must delegate to its specialist — the seam we're proving.
# Tasks name the NATIVE `execute` tool (Phase 3); `pux_sandbox_bash` is gone.
DEFAULT_TASKS: dict[str, str] = {
    "general": (
        "How many Python modules ship under /sandbox/workspace/harness/pux_harness/, "
        "and what are their names? Delegate to the `researcher` subagent — do NOT "
        "inspect the codebase yourself. Have it run, via the native `execute` tool: "
        "`find /sandbox/workspace/harness/pux_harness -name '*.py'`. "
        "Report the researcher's findings verbatim."
    ),
    "_demo": (
        "List the top-level entries of the project root inside the sandbox. Delegate "
        "to the `researcher` subagent — do NOT run tools yourself. Have it use the "
        "native `execute` tool: `ls -1 /sandbox/workspace`. Report its findings verbatim."
    ),
    # `go` is not installed in the pux-sandbox image, so we exercise the
    # read-only explorer specialist rather than the tester's run-tests path.
    "dev-bot": (
        "What does the dev-bot sample Go package export? Delegate to the "
        "`dev-bot-explorer` subagent — do NOT inspect the code yourself. The package "
        "is under /sandbox/workspace/orgs/specialists/dev-bot/. Have the explorer find every "
        "exported identifier (names starting with an uppercase letter) and report "
        "each with a file:line citation. Report its findings verbatim."
    ),
    # --- Phase 5: the remaining 7 orgs. Each forces delegation to a named
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
}
