# Instruction: How to Build an Interactive Test Checklist for the PiGate Project

> Standard guide for **humans and AI** on building an interactive HTML test
> checklist report (with JSON export) to verify work after implementation,
> especially the parts that **cannot be tested automatically** and must run on
> real hardware. The goal is for every task to be verified the same way, with
> results that are auditable and that the AI can read back and re-check.
>
> Companion to `work-planning-instruction-eng.md`: the plan defines the
> Definition of Done; this document defines how to prove that DoD is true on a
> real device.
>
> Example following this standard: the cookie-only session auth verification
> (issue #29) — the tester pasted the real `Set-Cookie` value from DevTools,
> then exported JSON so the AI could confirm `Secure` actually works over HTTPS.

---

## 0. Three Core Principles (read before starting, every time)

1. **The checklist must derive from the plan's DoD + Cautions — never invented
   on the spot during testing.** Every checklist item should trace back to §6
   (Definition of Done) and §5 (Cautions) of the work plan. Each caution = at
   least one test item, because a caution is "a spot already known to be able
   to break." If the checklist doesn't cover a caution, you haven't proven the
   risk you yourself wrote down is actually mitigated.

2. **Critical items must capture the "observed value", not just pass/fail.**
   A checkbox alone can't be trusted — someone can mark it pass without really
   looking. The core items (DevTools values, response body, headers, localStorage
   contents) must have a field where the tester **pastes the real value they
   saw**, so the reviewer/AI can read that value and judge for themselves rather
   than trust the checkbox. E.g. instead of trusting "cookie has Secure", capture
   the real header `set-cookie: ...; HttpOnly; Secure; SameSite=Strict` so the
   AI can confirm it.

3. **Cleanly separate "automatable" from "requires real hardware."** Whatever
   can be automated, the AI must do first — never offload it to the human:
   `go build/test`, `yarn build/lint`, and **driving the real UI with the
   `verify` skill** (embedded single-binary + Playwright headless in mock mode).
   The HTML checklist exists for **the remaining gap that mock/CI can't cover**
   — real HTTPS, certs, real browser behavior, real hardware, end-to-end
   role/permissions. Don't pad the human's manual list with things already
   covered by automation.

---

## 1. When This Kind of Checklist Is Required

Build one when the work has a gap that mock/CI can't prove and the outcome
affects real users:

- Work whose real behavior depends on the **real environment**: HTTPS/TLS,
  certs, cookie `Secure`, D-Bus/Netlink/kernel behavior, Wi-Fi, downstream
  DHCP/DNS, power
- Work that must be **confirmed by eye in a real browser**: DevTools
  (Network/Application/Console), redirect behavior, SSE, page bounces, dark/light
- Work touching **auth/role/permissions** — must be tested with multiple roles
  on real hardware
- Work that **migrates already-installed devices** (re-running install.sh,
  stale legacy keys)

Small work / one-spot style tweaks fully covered by the `verify` skill or unit
tests **do not** need an HTML checklist — use judgment.

**Where to put the report file:** place the `.html` at the **repo root** (or a
temp folder) so the user can open it easily — it is a **test artifact, not
committed** to the repo (unless the user asks to keep it). The JSON the user
exports back is likewise an artifact.

---

## 2. Where the Test Items Come From (map plan → checklist)

Walk the task's work plan and convert it into test items:

| Source in the plan | How to convert into a test item |
|---|---|
| §6 Definition of Done checkboxes | Items already automated (build/test/lint/`verify`) → AI does them, keep out of the HTML; items needing a human's eyes on real hardware → checklist items |
| §5 Cautions, each one | One caution = one "prove it's actually mitigated" item (e.g. caution that the cookie is dropped without Secure → item "cookie has Secure on real HTTPS") |
| §0 Goal (user-facing behavior) | The main happy-path items (login → use → logout, etc.) |
| §4 API + role | Permission items (super_admin can / read-only blocked with 403) |
| Traps found during the code survey | Edge-case items or items in the "top risks" section |

**Mark the values to capture:** any item the AI can re-check from a real value
should be a `value`-type item with a placeholder telling the tester what to
paste (e.g. "paste the Set-Cookie header value").

---

## 3. Checklist Section Structure (ordered by test flow)

Group items in the order a person actually tests. At minimum:

0. **Pre-test setup** — build/deploy/setcap/permissions ready on real hardware
1. **Main happy path** — the flow a real user runs from start to finish
2. **Decisive observations (the heart of the work)** — items with an observed
   field for pasting real values (DevTools, headers, storage) — this is what
   makes the result auditable
3. **End-to-end / multi-page / multi-role**
4. **Edge cases** — from cautions and discovered traps
5. **Top risks — confirm first** — the 2-3 items that, if wrong, break the whole
   thing (make them stand out, e.g. a colored warning strip) so the tester
   focuses on them first

Each item has: **a clear label** (says where to look and what to expect) +
status pass/fail/na + a notes field + (critical items only) an observed field.

---

## 4. HTML Report Requirements

- **Single self-contained file** — all CSS/JS inline; **no external deps
  (CDN/font/script)** since it must open on a machine that may be offline or
  origin-locked
- **A local file, NOT a claude.ai Artifact** — the download/export-JSON button
  needs Blob + `URL.createObjectURL`, which the Artifact CSP blocks; a local
  file opened directly supports every feature
- **Interactive:** per-item status (pass/fail/na), a notes field, and an
  observed field on critical items
- **Metadata header:** tester, date, device, URL/IP, build/commit, browser
- **Autosave to localStorage** — survives close/reopen/refresh so testing can
  span days
- **A progress bar** counting pass/fail/na in real time
- **Export JSON** (download) **+ Copy JSON** (clipboard) **+ Reset** buttons
- Support dark/light via `prefers-color-scheme`

**Exported JSON schema (fixed — so the AI can read and re-check it):**

```json
{
  "report": "<topic-slug>",
  "branch": "<branch>",
  "generatedAt": "<ISO timestamp>",
  "meta": { "tester": "", "date": "", "device": "", "url": "", "build": "", "browser": "", "overall": "" },
  "summary": { "total": 0, "pass": 0, "fail": 0, "na": 0, "untested": 0 },
  "items": [
    { "id": "<stable-id>", "section": "<n> <title>", "label": "<plain text>",
      "status": "pass|fail|na|untested", "observed": "<value the tester pasted>", "notes": "" }
  ]
}
```

Each item's `id` must be **stable and meaningful** (e.g. `set_cookie`,
`ls_no_session`) so it can be referenced during review.

---

## 5. Ready-to-Use Template

Start from **`docs/ref/instruction/test-checklist-template.html`** — edit only
the top two blocks of the `<script>`:

1. `META` (report name, branch, displayed title)
2. `SECTIONS` (array of sections + test items) — each item looks like:
   ```js
   { id: "set_cookie", label: "Set-Cookie has HttpOnly + Secure + SameSite=Strict",
     type: "value",            // set "value" if it must capture an observed value; omit for pass/fail only
     hint: "must have Secure on real HTTPS",
     vLabel: "paste the Set-Cookie header", vPlaceholder: "pigate_session=...; HttpOnly; Secure" }
   ```
   A "top risks" section takes `risk: true` at the section level to get the
   warning strip.

The render / autosave / export machinery in the template is complete — don't
touch it. Key mechanics: config-driven (DOM built from `SECTIONS`), state stored
in `localStorage` under a report-specific key, and `buildReport()` assembles the
JSON per the schema above.

> **Do NOT in the template:** add external `<script src>`/`<link href>`, change
> export to hit the network, or remove autosave (testers lose mid-session data).

---

## 6. The Workflow (human + AI)

1. **AI builds the report** from the plan (§2-§3) → copy the template, edit
   `META`/`SECTIONS`, place the file at repo root → send it to the user with
   `SendUserFile` (display render)
2. **AI runs the automatable parts first** — `go test`, `yarn lint`, the
   `verify` skill (mock) → tell the user only the real-hardware items remain
3. **User deploys + tests on real hardware**, fills in status + pastes observed
   values → Export JSON
4. **User pastes the JSON back** → **the AI reads each `items[].observed` and
   re-checks** that the real value is what it should be (not just the `status`)
   — e.g. confirm the pasted header actually contains `Secure`, the response
   body truly has no `token`, localStorage has no forbidden key
5. **AI reports the verdict** — all passed? which need fixing? are the N/A items
   acceptably justified? → once the DoD is fully met, proceed to commit/PR

The highest-value part of this approach is the **observed field** — it turns
"trust that they tested it" into "check the values they saw," letting the AI
help verify the far end even without being at the machine.

---

## 7. Writing Style

- **Language:** checklist labels/hints in Thai mixed with English technical
  terms (matching the project UI); file names/ids in English
- **A label must state "where to look + what to expect"**, not just a feature
  name — "Network → auth/login → Response: no token field" beats "check login"
- **A top-risks section** is mandatory whenever the work can fail silently —
  spell out the symptom the tester would see if it breaks, so they can catch it
- **Item count:** matched to DoD + cautions; don't pad with already-automated
  items — typically ~15-25 items is right
- **Stable ids:** set once, never change — they anchor the JSON review
- **After testing:** the report file + JSON are artifacts — ask the user whether
  to keep them in the repo, `.gitignore` them, or discard; update the plan /
  security artifact to note which items are now verified
