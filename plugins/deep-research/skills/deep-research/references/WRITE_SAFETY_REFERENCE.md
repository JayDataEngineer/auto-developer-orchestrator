# WRITE_SAFETY_REFERENCE

**How to write a comprehensive safety reference / protective intelligence dossier on an extremist network dataset.** Use when the deliverable is a defensive identification document — the kind of thing a security team, journalist, or community-safety desk uses to **recognize and avoid** specific people.

This skill captures the format proven on the Montana extremist-network dataset (ChatExport_2026-03-13). It combines **Michael Bazzell's OSINT Target Profile worksheet** (per-person structured fields) with a **network/crossover mapping layer** Bazzell doesn't cover, because real-world extremist networks are not isolated target dossiers — they are **two or more adversarial networks that share members.**

## When to read this reference

| Task | Read |
|------|------|
| Compiling a defensive identification dossier on an extremist dataset | This file, end to end |
| Documenting a single target (Bazzell-style) | §3 per-person template |
| Documenting a network with left/right crossover | §4 network map + §5 crossover table |
| Producing a Draw.io / network-graph visualization | §6 entities-and-edges format |
| Photo-confirmation workflow (face clustering + vision) | §7 + INGEST_FACE_CLUSTERING + INGEST_MULTIMODAL_PERSONS |
| Source reliability after adversarial-compiled dataset | §8 |

## 1. Core principles

1. **Defensive purpose, always.** The document exists so people can recognize and avoid the individuals documented. State this on the cover. Every person documented is entitled to due process.
2. **List everyone.** If the dataset has evidence tying a person to the network, document them — even if they're on the "adversary" side. **"These people are practically all in one boat with cross over"** — if you only document one side, you miss the structure.
3. **Embed images inline.** Use `![alt](photos/filename.jpg)` so reviewers see the face next to the assessment. Don't make people open a separate gallery.
4. **The clustering system is the determiner of what's in the folder.** Sender attribution is banned. If face clustering says two photos are the same person, they're the same person; if it doesn't, you need explicit user confirmation before claiming a match.
5. **Adversarial source ≠ neutral truth.** If the dataset was compiled by an adversary (e.g. WLM Montana compiling a dossier on a communist cell), every characterization of mental health, intent, or morality carries adversarial bias. State this on the cover.
6. **Correction notes are first-class content.** When you catch an error (you will catch errors), add a dated CORRECTION NOTE inline at the point of the error. Don't silently fix — show the correction so reviewers can audit your reasoning.

## 2. Document skeleton (cover page + TOC)

```markdown
# COMPREHENSIVE SAFETY REFERENCE
## [Region] Extremist Network — Identification & Protective Intelligence dossier
## ([Network A] + [Network B] + Crossover Figures)

**Date compiled:** YYYY-MM-DD
**Source dataset:** [export identifier]
**Purpose:** Defensive identification of individuals engaged in doxxing, surveillance,
and political violence planning in [region]. This document exists to keep people safe.

**CRITICAL SOURCE CAVEAT:** [Who compiled this dataset? What's their bias?]

**THE TWO-NETWORK STRUCTURE (read this first):** [1-2 paragraphs explaining that the
dataset shows N adversarial networks that share members. Name them.]

---

## TABLE OF CONTENTS

1. Network Overview
2. Threat Tier Classifications
3. Tier 1 Profiles — [Network A] Primary Threats
4. **[Network B] Profiles** ← the side the dataset was built to document
5. Tier 2 Profiles — Associates & Enablers
6. Surveillance & Doxxing Infrastructure
7. Network Connection Map (master diagram + crossover analysis)
8. Group Chat Directory
9. Protective Recommendations
10. Photo Identification Sheet
11. Source Reliability Assessment
```

## 3. Per-person profile template (Bazzell-adapted)

**Every named individual gets one of these.** Use this exact field set so profiles are comparable across people and the document reads as a structured dossier rather than narrative prose.

```markdown
### N.X [NAME] ([primary alias])

**THREAT LEVEL: ★★★★★ HIGHEST / ★★★★☆ HIGH / ★★★☆☆ ELEVATED**
**Reason:** [one-sentence justification tying to weapons / access / stated intent / org role]

| Field | Detail | Source |
|-------|--------|--------|
| **Full name** | [name] | [source: doxxing profile / audio / OCR] |
| **Aliases / Handles** | [all known handles] | [source] |
| **Age / DOB** | [age or DOB] | [source] |
| **Sex/Gender** | [AFAB/AMAB, trans status if confirmed, pronouns] | [source] |
| **Height/Weight** | [physical descriptors] | [source] |
| **Origin** | [place of birth / prior location] | [source] |
| **Location** | [current address / city] | [source] |
| **Address** | [street address if known] | [source] |
| **Cohabitant(s)** | [who lives there] | [source] |
| **Phone** | [number with area code] | [source] |
| **Email** | [if known] | [source] |
| **Vehicle(s)** | [year/make/model + plate] | [source] |
| **Weapons** | [firearms / other] | [source] |
| **Affiliation** | [org + role] | [source] |
| **Employment** | [job / unemployment] | [source] |

**PHYSICAL IDENTIFICATION (photo + cluster data):**

![Name photo A](photos/[filename].jpg)
*[photo_N] — [brief description] ([face_cluster_N], [singleton/N members])*

[If no confirmed photo: state NO CONFIRMED FACE PHOTO and what the investigator
should look for based on the doxxing profile descriptors.]

**ROLE / OPERATIONAL POSTURE:**
[2-4 paragraphs. What does this person do in the network? Quote directly from
audio/OCR where possible. Always cite [Audio: New Recording N] or [Photo: photo_N].]

**IDEOLOGICAL TRAJECTORY (if crossover figure):**
[Timeline showing movement between groups. The Montana pattern: WLM → PF-marriage
→ AFP orbit → CPUSA. The extremism is constant; the label is the variable.]

**MENTAL HEALTH (self-reported or single-source adversarial — label which):**
[PTSD, alcoholism, depression, etc. — but mark clearly whether this is the subject's
own self-report or an adversarial characterization.]

**ALLEGED (single-source, adversarial — UNVERIFIED):**
[Any claim from a doxxing profile that hasn't been independently confirmed.
Always label with the caveat. Examples: "FBI informant", "father makes bombs",
"federal connections".]

**WEAPONS / VIOLENT INTENT:**
[Direct quotes. If the subject named a target ("Cletus"), document the named
target and the specific plan.]

**ASSESSMENT:**
[2-4 paragraphs synthesizing threat level. Why does this person matter? What's
the distinguishing risk vector? What's the gap in our knowledge?]
```

### 3.1 Bazzell's full 11-section structure (alternative: deeper individual worksheet)

For a single-target deep dive, Bazzell's full worksheet is the standard. Use this when you're producing a standalone target profile rather than a network dossier:

1. **Case Metadata** — Case File ID, Date Opened, Investigator, Case Status
2. **Primary Identity Details** — Full Legal Name, Aliases, DOB, SSN/National ID, Place of Birth, Physical Descriptors, Distinguishing Marks
3. **Residential History** — Current Address (ownership type, associated utilities), Previous Addresses (with dates)
4. **Telephone Records** — Primary/Secondary (carrier, line type, connected accounts, leak mentions)
5. **Email Addresses** — Primary/Secondary (domain host, breach mentions, connected services)
6. **Username & Alias Inventory** — per platform
7. **Social Media Footprint** — per platform: URL, numeric user ID, stated location, employer
8. **Vehicles & Assets** — vehicles (plate, VIN), real estate, domains
9. **Associates, Family, & Relatives** — relationship, name, contact details, profile links
10. **Investigation Log & Time-Stamped Evidence** — table: timestamp, source/URL, finding
11. **Future Pivot Tasks** — open checkboxes

A self-contained HTML webform version of this worksheet exists that exports to Markdown locally. Use it for offline intake.

## 4. Network-level structure: the two-network map

Real extremist datasets are not one group. They are **two or more adversarial networks that share members, geography, and personal ties.** Document the structure explicitly.

```markdown
## N. NETWORK CONNECTION MAP

### N.1 THE TWO-NETWORK STRUCTURE + CROSSOVER (MASTER MAP)

[ASCII diagram showing both networks as separate boxes connected by labeled
crossover figures. Each network gets its own panel. Crossover figures get their
own panel between them.]

### N.2 THE NEXUS CHAT (if applicable)

[If the dataset contains a group chat that physically contains the crossover —
e.g. a 3-person Nazi chat with members from "both" networks — diagram that chat
explicitly. This is structural evidence that the "two networks" are actually one
overlapping network.]

### N.3 [Specific org] — familial/institutional embedding

[For each major institutional tie (Patriot Front Network 7, American Freedom
Party, etc.), diagram the relationship chain. Show how a "communist cell" member
is connected by marriage / co-parenting / social orbit to WN institutional
leadership.]

### N.4 STRADDLING THE LEFT/RIGHT DIVIDE — table

| Individual | WN / Right-Wing tie | Communist / Left tie | Status |
|-----------|---------------------|---------------------|--------|
| ... | ... | ... | ... |

### N.5 Trans members crossover (if applicable)

[Some accelerationist-Nazi milieus have documented "genderbending" / trans
membership despite anti-LGBTQ ideology. Document with correction-note discipline
— misidentification of trans status is a frequent error.]

### N.6 Active-duty military / institutional nexus

[Flag any tie to active-duty military, law enforcement, or government. The
combination of (a) elected office + (b) weapons + (c) stated violent intent +
(d) military contact is a pattern defense protective intelligence evaluates.]

### N.7 Dataset compiler ↔ target channel (flash-risk)

[If the dataset was compiled by an adversary (Adversary A) but Adversary A also
maintained personal contact with the primary target (B) — and B contributed
their own material to A's dossier — this is a flash-point. Document the channel.]
```

## 5. Crossover analysis (the analytical core)

The crossover table is the single most important analytical artifact in this kind of dossier. It's the answer to "is this messy data, or is the messiness the pattern?"

```markdown
**Key analytical point:** These individuals are NOT "former [X] who became [Y]."
They are **continuous extremists whose label changed.** The deradicalization
(where it happened at all) is partial. [Subject A] still harbors [residual
belief]; [Subject B] never left [extremist orbit]; [Subject C] carried [org]
rhetoric into [new space]. **The extremism is the constant; the ideological
branding is the variable.**

**Why this matters for protective work:** Do not assume someone is "safe" because
they currently identify as [progressive/communist/antifa]. The same person who is
at a [left-wing event] today may be at a [right-wing event] tomorrow. Watch
behavior and association patterns, not labels.
```

## 6. Draw.io / network-graph export format

To produce a Draw.io (or Gephi / yEd / Cytoscape) diagram from the dossier, extract two CSVs:

### 6.1 Nodes CSV (`nodes.csv`)

```csv
id,label,type,network,threat_level,has_photo,notes
grady_kirk,Grady Kirk,person,CROSSOVER,5,false,"Elected official; AR-15; target 'Cletus'"
semok,Christopher Semok,person,COMMUNIST,4,false,"CPUSA MT leader; Life360 admin"
scott_ernest,Scott Ernest,person,CROSSOVER,3,true,"Lot 60; Blood & Soil tagline"
tyler_dipeppe,Tyler Dipeppe,person,CROSSOVER,4,true,"Former AWD; MTF"
will_axolotl,Will (Axolotl),person,WN,4,false,"WLM-MT; dataset compiler"
mml,Montana Mountain Lion,person,CROSSOVER,4,false,"Folkish; Active Club"
alex_pf,Alex,person,WN,3,false,"PF Net 7 member; Grady's ex"
mark_hayden,Mark Hayden,person,WN,4,false,"PF Net 7 ND 'Bill Mass.'"
john_fassbinder,John Fassbinder,person,WN,3,false,"AFP ED"
brandon_russell,Brandon Clint Russell,person,WN,5,false,"AWD founder name-match"
karl_gharst,Karl Gharst,person,WN,3,false,"Referenced 'most notorious local WN'"
lot_60,Lot 60 (Columbia Falls RV Park),location,BOTH,0,false,"Physical hub"
pagan_village_people,"The Pagan Village People",chat,BOTH,0,false,"3-member Nazi chat"
patriot_front_n7,PF Network 7,org,WN,0,false,"Regional cell"
cpusa_mt,CPUSA Montana,org,COMMUNIST,0,false,"Local chapter"
wlm_mt,WLM Montana,org,WN,0,false,"Will's org"
```

### 6.2 Edges CSV (`edges.csv`)

```csv
source,target,relationship,evidence_type,note
carl_wood,semok,handler, AUDIO,"CPUSA national handler"
semok,scott_ernest,co_resident,DOXXING,"Lot 60 co-residents"
semok,grady_kirk,recruited, AUDIO,"Recruited into CPUSA"
grady_kirk,tyler_dipeppe,monitors,DOXXING,"Monitoring chat"
grady_kirk,alex_pf,married_divorced, AUDIO,"Ex-husband; father of Claire"
alex_pf,mark_hayden,introduced_by, AUDIO,"Hayden brought Alex to bars"
grady_kirk,john_fassbinder,social_orbit, AUDIO,"AFP gatherings"
will_axolotl,grady_kirk,adversary_contact, AUDIO,"Grady sent Will recordings"
scott_ernest,pagan_village_people,member,OCR,"Blood & Soil tagline"
mml,pagan_village_people,member,OCR,"Active in chat"
grady_kirk,pagan_village_people,member,OCR,"Active in chat"
mml,wlm_mt,member,OCR,"WLM chats"
mml,big_sky_active_club,member,OCR,"Active Club network"
brandon_russell,vorkuta_chat,posted,OCR,"2020-01-14 O9A request"
```

### 6.3 Draw.io import

In Draw.io: **Arrange → Insert → Advanced → CSV** (or use the `mxgraph` import). Use `nodes.csv` for the entity diagram, then `edges.csv` for the relationship overlay. Color by `network` column:
- `COMMUNIST` → red
- `WN` → blue
- `CROSSOVER` → purple (border) / yellow (fill) — these are the high-priority nodes
- `BOTH` (chats, locations) → gray

The crossover nodes are the most important — they're the structural evidence that the "two networks" are actually one overlapping network.

## 7. Photo-confirmation workflow (CRITICAL — read this before claiming any photo match)

This is where most errors happen. The Montana dataset had **four material photo-identification errors** caught by direct user review. Document your method honestly.

### 7.1 The determiner rule

**The clustering system is the determiner of what's in the folder.** Sender attribution is banned. If face clustering says two photos are the same person, they're the same person. If clustering doesn't link them, **you need explicit user confirmation before claiming a biometric match.**

### 7.2 Honest confidence levels

Use these exact labels:
- **HIGH (user-confirmed)** — a human reviewer has directly confirmed the match
- **MEDIUM** — vision-model cross-reference + doxxing-profile match, but no biometric cluster link
- **LOW** — single-source inference, possibly fabricated

### 7.3 Known failure modes

| Failure mode | What happens | Mitigation |
|--------------|--------------|------------|
| **Vision model mis-race** | Vision describes a non-white Facebook avatar as "olive/light complexion" | Don't trust vision for race; ask user |
| **Vision model mis-trans** | Vision describes trans person without flagging transition | Doxxing profile flags trans status; vision doesn't |
| **Vision model ambiguous match** | "Beard + glasses + RV setting" matches doxxing profiles of two different people | Pick wrong one analytically → misidentification |
| **Hormone therapy breaks clustering** | MTF/FTM pre/post-transition faces don't cluster | Expected; doesn't rule out ID |
| **Fabricated cluster claims** | "Confirmed via face_cluster_N" with no actual cluster JSON backing | Always cite the cluster ID; verify the cluster exists |
| **Forwarded-photo avatar extraction** | Tiny Facebook avatars in screenshots get clustered as full subjects | Exclude sub-50px faces from clustering input |

### 7.4 The correction-note discipline

When you catch an error (you will), **do not silently fix it.** Add a dated CORRECTION NOTE inline at the point of the error:

```markdown
**CORRECTION NOTE (YYYY-MM-DD):** An earlier version of this section identified
[person] as [wrong label] based on [evidence]. **That identification was wrong.**
[Evidence] actually shows [correct label]. See §X for the corrected entry.

[Then rename any mislabeled photo files with `_was_mislabeled_` suffix:]
01_grady_kirk_a.jpg → thomas_howard_a_misidentified_as_grady.jpg
```

Renaming files (rather than deleting them) preserves the audit trail. Reviewers can see what was wrong and how it was corrected.

## 8. Source reliability assessment (always include)

End the document with explicit per-source reliability ratings. The Montana pattern:

| Source | Reliability | Notes |
|--------|-------------|-------|
| **Doxxing profiles** (adversary's text compilations) | **MEDIUM** | Factual data (addresses, phones, vehicles) appears accurate. Characterizations (mental health, intent) carry adversarial bias. |
| **Audio recordings** (subjects' own statements) | **HIGH** | Subjects' own words. Statements about violence, ideology, and plans are self-incriminating. **But the subject curated which recordings to share** — the audio is selected by the subject. |
| **OCR screenshots** (chat captures) | **HIGH** | Direct captures of subjects' communications. **But screenshots taken from one party's device capture their framing; counterparty context is partial.** |
| **Face clustering** (biometric) | **MEDIUM** (downgrade if you've caught fabricated claims) | Computer-vision clustering is reliable for what it does, but **multiple biometric claims in earlier passes were not actually backed by cluster JSON.** Trust the cluster JSON, not claims about it. |
| **Vision model (mimo-v2.5 or similar)** | **MEDIUM** | Useful for description; prone to age overestimation; **cannot reliably determine race or trans status.** A tool, not an oracle. |
| **Single-source adversarial allegations** (fed informant, bomb-making, etc.) | **UNVERIFIABLE** | Always label as single-source adversarial. Never repeat as fact. |

### 8.1 Dataset provenance — multi-party OSINT swap (common pattern)

Most adversarial datasets are **not clean leaks.** They are compiled dossiers assembled by [Adversary A] and delivered to [Recipient B], with material from multiple parties with partial, overlapping consent:

1. **Adversary A contributed** their own side's OSINT
2. **Subject C contributed their own material** to Recipient B (recordings, screenshots)
3. **Recipient B (third-party researcher)** acted as intermediary
4. **Other parties (D, E, ...) did NOT necessarily consent** — their words appear because C chose to share

**Practical implication:** Material sourced from a subject's own recordings is **self-reported** and curated. Material sourced from the adversary is **adversarial OSINT**. Both bias the portrait. The dataset is a **multi-perspective composite**, not an objective record. State this on the cover.

## 9. Group chat directory (always include)

For datasets with Telegram/chat exports, the group-chat directory is structural evidence — not just context. Each chat is a **node in the network graph**. Document:

| Group | Platform | Members | Purpose |
|-------|----------|---------|---------|
| [name] | [platform] | [count + named members] | [purpose, with direct-quote evidence] |

**If a chat's name is a joke cover** (e.g. "Pagan Village People" referencing the disco group), state what the chat actually is. Scott Ernest's tagline *"Blood and soil.. oh hey.. squirrel!"* in that chat is the evidence that it's a Nazi chat, despite the joke name. **Taglines, bios, and chat names are evidence.**

## 10. Production checklist (run before publishing)

- [ ] Cover page states defensive purpose, source caveat, and two-network structure
- [ ] TOC includes Section 4 = the "other side" of the network (not just the cell the dataset was built to document)
- [ ] Every named individual has a per-person profile (§3 template)
- [ ] Every profile with an available photo has embedded inline image
- [ ] Crossover table (§5) is present and complete
- [ ] Network map (§4) shows both networks + crossover figures
- [ ] Draw.io nodes.csv + edges.csv (§6) are exported
- [ ] Photo-confirmation confidence levels use HIGH/MEDIUM/LOW labels honestly (§7)
- [ ] Every error caught has a dated inline CORRECTION NOTE (§7.4)
- [ ] Source reliability table (§8) rates every source type
- [ ] Dataset provenance (§8.1) explains who compiled it and how
- [ ] Group chat directory (§9) lists every chat as a network node
- [ ] Appendices: Key Locations, Key Phone Numbers, Key Vehicles (for quick reference)

## 11. Anti-patterns (do not do these)

- ❌ **Documenting only one side.** If the dataset has evidence tying someone to the network, document them — even if they're the "adversary." Listing only the communist cell and not the WN network misses the structure.
- ❌ **Silent corrections.** When you catch an error, add a dated CORRECTION NOTE inline. Don't quietly fix.
- ❌ **Vision-model-only race/trans claims.** The vision model cannot reliably determine race or trans status. Always ask the user.
- ❌ **"Biometric cluster N" claims without verification.** Always cite the cluster ID and verify the cluster exists in the JSON before claiming a biometric match.
- ❌ **Treating trans status as visual.** MTF/FTM status comes from doxxing profiles, audio self-reference, or third-party pronouns — NOT from vision. Pre/post-transition photos of the same person typically do NOT cluster.
- ❌ **Adversarial allegations repeated as fact.** "FBI informant", "bomb-making father", "federal connections" — always label as single-source adversarial and unverified.
- ❌ **Flip-flopping on contested calls without new evidence.** Once you've made an evidence-based call (e.g. "Grady is cisgender"), hold to it unless new evidence emerges. Flipping back and forth in the document creates the "10 versions" problem.
- ❌ **Multiple stale artifact directories.** One canonical version. Delete stale run directories before publishing.

---

**Source:** This skill was compiled from the ChatExport_2026-03-13 (Montana extremist network) safety-reference production run. The dataset had four material photo-identification errors caught by direct user review, multiple fabricated biometric claims in earlier passes, and a two-network crossover structure (communist cell + WN network) that the initial single-side framing missed. The lessons are baked into the workflow above.
