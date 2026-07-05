from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-renderer',
    "description": 'Game Studio pipeline engineer — executes the YAML asset manifest against the Ray cluster (ComfyUI / Forge / TRELLIS / ACE-STEP / Qwen3-TTS). Saves outputs to art/output/.',
    "tools": ['python'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
