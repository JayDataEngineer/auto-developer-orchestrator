---
name: game-studio-workflows
description: Tech Noir game-studio production workflows — the autonomous dev loop, ComfyUI + Forge art pipelines, Godot 4.6 via MCP, docs/GDD authoring, and media QC. Use when building or shipping the 2.5D anime survival-horror game — generating art, routing models on the Ray cluster, driving the Godot editor, authoring design docs, or QC-ing media.
---

# Game Studio Workflows

Tech Noir ships 2.5D anime survival-horror in Godot 4.6 with the art pipeline
driven by AI models on the Tech Noir Ray cluster. This skill indexes the
studio's production playbooks. Read the `references/` file for the work you
were delegated.

## When to read which reference

| Task | Read |
|------|------|
| How the studio autonomously loops plan → build → review | `references/AUTONOMOUS_LOOP.md` |
| ComfyUI art pipeline (sprites, textures) on the Ray cluster | `references/COMFYUI_WORKFLOW.md` |
| Forge model routing / generation dispatch | `references/FORGE_WORKFLOW.md` |
| Driving the Godot 4.6 editor over MCP | `references/GODOT_VIA_MCP.md` |
| Writing/updating the GDD, design notes, READMEs | `references/DOCS_AUTHORING.md` |
| QC of generated media (art, music, SFX, voice) | `references/MEDIA_QA.md` |

## Operating rule

The studio director coordinates; specialists execute. If a step needs the Godot
editor, a ComfyUI workflow, or a Ray-cluster model, open the matching reference
before delegating or running it — the contract details (ports, formats, params)
live there, not in this index.
