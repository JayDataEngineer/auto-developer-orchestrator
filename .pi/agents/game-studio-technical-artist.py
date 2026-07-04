from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-technical-artist',
    "description": 'Tech Noir Technical Artist — pipeline engineer bridging art and code. Converts YAML specs in departments/art/configs/ into Ray cluster jobs (ComfyUI / Forge / TRELLIS / ACE-STEP / Qwen3-TTS), saves to departments/engineering/game/assets/, self-reviews via vision tools.',
    "tools": ['python', 'describe_image'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
