# ARTIFACT_STRUCTURE

**The artifacts/ directory spec + entity dossier rules.** Read this when
building entity dossiers or writing pipeline output. Covers the directory
tree, the subject-based dossier organization, symlink discipline, and
provenance format.

## Directory tree

```
<project-root>/
├── sandbox/               ← backbone scripts (run as python3 sandbox/X.py)
├── data/                  ← raw source data (the --data folder)
└── artifacts/             ← PIPELINE OUTPUTS — organized for HUMAN consumption
    │
    │  ┌─────────────────────────────────────────────────────────────┐
    │  │ ROOT: only the FINAL deliverables a human opens directly.   │
    │  │ NO working files, NO raw dumps, NO staging junk.            │
    │  └─────────────────────────────────────────────────────────────┘
    │
    ├── brief.md            ← THE intelligence report (synthesizer output)
    ├── audit_report.md     ← quality audit (auditor output)
    ├── index.md            ← master index linking to all subfolders
    │
    ├── entities/           ← PRIMARY HUMAN BROWSING SURFACE
    │   │                     One folder per entity, sorted by evidence.
    │   │                     A human opens entities/<Name>/ and finds
    │   │                     EVERYTHING about that person.
    │   │
    │   ├── index.md        ← master entity table (name, kind, evidence, links)
    │   ├── <Name>/
    │   │   ├── <Name>.md   ← full dossier (summary, aliases, claims)
    │   │   ├── images/     ← symlinked photos (face cluster)
    │   │   ├── audio/      ← symlinked audio (speaks / mentioned)
    │   │   ├── videos/     ← symlinked videos (appears in)
    │   │   └── text/
    │   │       ├── mentions.md       ← text excerpts about them
    │   │       └── audio_mentions.md ← transcript excerpts
    │   └── other/
    │       └── minor_entities.md      ← minor entities (table, not folders)
    │
    ├── sources/            ← RAW SOURCE DATA — organized by modality
    │   │                     (when a human needs the original evidence)
    │   ├── transcripts/    ← audio transcripts
    │   ├── ocr/            ← OCR'd text from screenshots
    │   ├── text/           ← chat messages
    │   ├── video_frames/   ← VLM frame descriptions
    │   └── audio/          ← audio summaries
    │
    └── staging/            ← LLM WORKING FILES — NOT for humans
        │                     Intermediate consolidations the orchestrator
        │                     builds before delegating to synthesizer.
        │                     DELETE after the brief is written.
        └── synthesis_input.md
```

## Entity Dossier Spec

The `entities/` folder MUST be organized by **SUBJECT**, not by pipeline
modality. An analyst opens `entities/<Name>/` and finds everything about that
person in one place — not scattered across `face_clusters/`,
`voice_clusters/`, `text_and_scenes/`.

**Directory structure (what the DRE MUST produce):**

```
entities/
  index.md                              # master index, one table per kind
  <Name>/                               # MAJOR entity (top ~25)
    <Name>.md                           # full dossier: summary, aliases,
                                        #   attributes, evidence excerpts,
                                        #   confidence assessment
    photos/                             # photos OF this entity (face cluster
                                        #   linked via same_as — ONLY path)
    audio/                              # audio OF this entity (voice cluster
                                        #   linked via same_as — ONLY path)
    videos/                             # symlinked videos mentioning them
    text/
      mentions.md                       # plaintext excerpts mentioning them
      audio_mentions.md                 # transcript excerpts mentioning them
  clusters/                             # unresolved clusters live here so the
    face_cluster_N/                     # top-level surface shows ONLY named
      face_cluster_N.md                 # entities. PROVEN same-face photos
      photos/                           # whose name isn't resolved yet.
    voice_cluster_N/                    # PROVEN same-voice audio. Same idea.
      voice_cluster_N.md
      audio/
  ...
  raw/                                  # original modality output preserved
    face_clusters/                      # for provenance (NOT the primary
    voice_clusters/                     # browsing surface)
    text_and_scenes/
    video_frames/
```

### Rules

1. **The root of `artifacts/` is NOT a dump.** Only `brief.md`,
   `audit_report.md`, and `index.md` belong there. Everything else goes into
   a subfolder.
2. **The LLM operates off SurrealDB.** The filesystem is for HUMANS. Do not
   dump raw data, consolidated inputs, or working files into the root.
3. **Entity folders are the primary surface.** Build them via
   `sandbox/build_entity_dossiers.py`. Media in entity folders are SYMLINKS
   (not copies) into `data/`.
4. **Raw source data goes in `sources/<modality>/`.** If a human needs the
   original transcript, OCR text, or video frame description, they find it
   under `sources/`.
5. **Working files go in `staging/` and are deleted after use.**
   `synthesis_input.md`, consolidated dumps, scratch files — these are
   intermediate artifacts. They are NOT deliverables.

### Entity rules

- **Major entities** (top ~25 by evidence score): full dossier folder with
  `.md` + `photos/` + `audio/` + `videos/` + `text/` subfolders. Curated
  aliases ensure all name variants are found (e.g. "Grady" = "Primary
  Speaker" = "City Councilor" = "(Grady)The Pagan of Montana").
- **Cluster pseudo-entities** (under `clusters/face_cluster_N/`,
  `clusters/voice_cluster_N/`): proven-same-face/voice sets whose names
  aren't resolved. These get their own folders with `photos/` (or `audio/`)
  populated. The agent or a human investigator proposes labels via LLM
  Identity Resolution (PIPELINE_RUNBOOK.md Step 2).
- **Media = symlinks**, not copies. A 1.2 GB dataset must not be duplicated
  per entity. Symlink to the source file under `data/` using ABSOLUTE targets
  (`os.path.abspath(src)`) so the entity tree is relocatable.
- **Per-item metadata**, not giant dumps. Each photo can have a companion
  `.json` sidecar if metadata is needed. NEVER write a single 50-page
  `info.md`.
- **Evidence-grounded dossiers.** Every claim in the `.md` links to a source:
  `[Audio: New Recording 7.wav]`, `[Item: 2026_03_13T...]`, `[Video:
  IMG_5795]`. No unsourced assertions.
- **Identity confidence.** When a voice cluster resolves to a sender, the
  dossier MUST note the method (sender co-occurrence ≠ voice biometrics) and
  flag any third-person references that reduce confidence.
- **Generic.** Reads `RUN_DIR` from env. Works on ANY dataset.
- **Idempotent.** Re-running wipes `entities/` (preserving `raw/`) and
  rebuilds cleanly.

## Provenance (REQUIRED on every artifact)

Every file you write under `artifacts/` starts with this HTML-comment block
(invisible in rendered markdown, machine-parseable):

```markdown
<!--
pux:agent=<your-agent-name>
pux:saved=<UTC ISO 8601 timestamp, from `date -u +%Y-%m-%dT%H:%M:%SZ`>
pux:task=<first 8 chars of sha256 of the original user task string>
pux:stage=<research | brief | article | posts | audit | pdf>
-->
```

Then a blank line, then the file's actual content. Why: the bundle command
links files back to the thread that produced them by mtime + this header.

```bash
TASK_HASH=$(printf '%s' "$TASK_STRING" | sha256sum | cut -c1-8)
```
