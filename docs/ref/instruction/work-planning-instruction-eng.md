# Instruction: How to Write a Work Plan for the PiGate Project

> Standard guide for **humans and AI** on writing work-plan documents into
> `docs/ref/todo/` before starting any feature/change. The goal is for every
> work plan to **cover system-wide impact** and share the same structure,
> so every document reads consistently.
>
> Example plans written to this standard:
> `docs/ref/todo/power-control-device-plan.md`,
> `docs/ref/todo/sidebar-dynamic-hostname-plan.md`

---

## 0. Three Core Principles (read before starting, every time)

1. **Always survey the real code before writing the plan — never write from
   memory or from documentation.** The Feature Status table in the README and
   the documents in `docs/` can drift from the actual code (CLAUDE.md warns
   about this too). Every status claimed in the plan must come from opening
   the files as of the writing date. For example, in the Power Control case:
   the README said "Mock", but the actual survey found the frontend was fully
   done and the routes already existed — only the backend handler was missing.
   The real scope was much smaller than the docs suggested. Without surveying
   first, you would plan work that duplicates what already exists.

2. **The plan must state "where" in a way that can actually be pointed to** —
   every step must give the full file path and an approximate line number
   (e.g. `backend/internal/api/handlers.go:1656`), plus a short code sample
   when it helps. A reader following the plan must be able to open the file
   and land on the exact spot being discussed, without searching on their own.

3. **A good plan states both "what to do" and "what to avoid/watch out for"** —
   the cautions section (side effects, project constraints, traps discovered
   during the survey) is worth as much as the steps themselves, because it is
   what actually prevents damage.

---

## 1. When a Work Plan Is Required

- Any new feature, or any Mock → Real conversion, in every case
- Work that touches more than one layer (frontend + backend, or backend
  across layers)
- Work that touches sensitive areas: auth/role, firewall rule generation,
  D-Bus/Netlink, `install.sh`/Polkit, migration of already-installed devices
- Very small work (typo fix, one-spot style tweak) does not need a plan —
  use judgment, but if in doubt, write one

**Location:** `docs/ref/todo/<feature-name-in-kebab-case>-plan.md`
(one file per task; file name in English, content in Thai mixed with English
technical terms)

---

## 2. Planning Procedure (follow in order)

### Phase A — Frame the Task and Draw the Scope

Answer these questions before touching any code:

- What does "done" look like for this task (what does the user press, and
  what actually happens)?
- What is **out of scope** — always write it down explicitly (it matters as
  much as the scope itself). For example, the Power Control plan explicitly
  cut "remote power-on" and the "System Services panel" to keep the plan
  from bloating.
- Which of the project's core constraints does this task collide with
  (see the checklist in §4)?

### Phase B — Survey the Current State of the Code (the most important phase)

Grep/search the feature's keywords across **every layer**, then actually open
and read the files found. The paths to walk for PiGate (ordered by request
flow):

| Order | Layer | What to check |
|---|---|---|
| 1 | Frontend UI | `frontend/src/pages/`, `frontend/src/components/`, `frontend/src/hooks/` — does UI already exist? Called from how many places? Shared via a hook? |
| 2 | Frontend API client | `frontend/src/services/*.ts` — which endpoint and method is called |
| 3 | Route + middleware | `backend/internal/api/router.go` — does the route exist? `authRoute` or `superAdminRoute`? (Check the effect of `RoleReadOnlyMiddleware` in `middleware.go` alongside) |
| 4 | Handler | `backend/internal/api/handlers.go` — real implementation, or a stub that just returns 200 |
| 5 | Service layer | `backend/internal/service/` — is there a supporting service yet |
| 6 | Kernel layer | `backend/internal/kernel/interfaces.go` — does an interface exist? Are both `real_*.go` and `mock.go` implemented? |
| 7 | DB | `backend/internal/db/` — is a new table/migration needed (remember: runtime state is not persisted to SQLite — reduces SD card wear) |
| 8 | Wiring | `backend/cmd/pigate/main.go` — where is the manager selected real/mock, where is the service constructed and passed into `api.NewServer`, does config need applying at boot |
| 9 | Install/permissions | `install.sh` — Polkit rules, sudoers, systemd unit, the `pigate` user — what extra privileges does this task need |
| 10 | Docs/contract | `docs/openapi.yaml` **and** `frontend/public/openapi.yaml` (must stay in sync), README Feature Status, related `docs/ref/*` |

The output of this phase is the **"Current State" table** in the plan: each
part marked done / stub / missing, with file:line references — this table is
the evidence that a real survey happened, and it determines which steps the
plan actually needs.

### Phase C — Choose the Technical Approach, with Reasons and Rejected Alternatives

- Specify the mechanism precisely (e.g. which D-Bus destination + method,
  which Netlink call, which library that already exists in `go.sum`)
- Write down **why this approach was chosen** and **which alternatives were
  considered and rejected, and why** (e.g. choosing `login1.PowerOff` over
  `systemd1.StartUnit("poweroff.target")` because the Polkit action is
  narrower and the intent is clearer) — this section prevents whoever comes
  later from re-asking or switching approaches without knowing it was
  already considered
- The approach must pass every item in §4. If it does not, go back and
  rethink — do not record it as an exception
- Use an existing pattern in the codebase as the template, and name the
  template file in the plan (e.g. "follow the style of `real_hostname.go`")
  so the new code looks like the existing code

### Phase D — Write the Steps (Step-by-step)

- Order the steps along the **dependency direction**: innermost layer first,
  moving outward. For the PiGate backend that is: `kernel/interfaces.go` →
  `real_*.go` → `mock.go` → `service/` → `main.go` wiring →
  `api/handlers.go` → `router.go` → `install.sh` → docs → frontend (if any)
- Every step must include: **the file (new or modified), the approximate
  location, what to do, and a code sample if it makes things clearer**
- Mark optional/polish steps clearly as optional and place them last
- Also state what this task does **not** need that the usual pattern would
  normally include, with the reason (e.g. "no `InitApplyConfig()` needed
  because there is no state to apply at boot") — this stops people from
  over-building unnecessarily

### Phase E — Analyze Impact and Write the Cautions

Walk the impact checklist in §5 item by item. For each relevant item, write
it into the plan as its own point stating: **what breaks / how it breaks /
how to prevent it** — not just a bare "watch out for X". For example, do not
write "be careful about the response"; write "if D-Bus is called directly in
the handler, logind may stop the service before the response reaches the
browser → the frontend enters the error branch even though the command
succeeded → fix with `time.AfterFunc`".

Traps **discovered during the survey** (Phase B) must always be recorded in
this section — such as discovering that the existing Polkit rule ends with a
catch-all `return polkit.Result.NO`, which makes adding a rule in the wrong
position fail silently. Things like this cannot be found in documentation;
they are only found by reading the code, and they are the most valuable part
of the plan.

### Phase F — Summarize as a Checklist (Definition of Done)

- Convert every step into `- [ ]` checkboxes, one line per file/task
- Always include **testing** items: how to test in mock mode, how to test on
  the real device (and the safety conditions for testing), and how to test
  roles/permissions
- Always include **documentation update** items: openapi.yaml (both files),
  README Feature Status, and related docs

---

## 3. Work-Plan Document Structure (Template)

Every plan uses this skeleton. Irrelevant sections may be cut, but the order
must not be shuffled:

```markdown
# <Feature name> — <one-line description>

> Work plan for feature: <1-3 lines expanding what changes from what to what>
>
> Written: YYYY-MM-DD · Reference branch: `<branch>`
> Status in README Feature Status: <current value> → target is <new value>   (if any)

## 0. Goal and Scope
   - Goal (user-visible behavior + technical conditions that must hold)
   - **Out of scope:** always stated explicitly

## 1. Current State (code surveyed as of the writing date)
   - Table: part | status (done / stub / missing) with file:line
   - Close with a one-line summary of where the real work is concentrated

## 2. Technical Approach
   - Chosen mechanism + short code sample
   - Why it was chosen, and which alternatives were rejected and why
   - The pattern/template file in the existing code to follow

## 3. Steps (ordered + files to modify)
   - Step 1..N: heading + **File:** path (mark "new file" when creating one)
   - Things that do NOT need doing, with reasons, as a blockquote in the
     relevant step

## 4. Related API
   - Table: Method | Path | who may call (role) | behavior
   - State whether the route is new or existing, and the effect of
     -disable-edit mode

## 5. Cautions
   - One issue per point: what breaks / how it breaks / how to prevent it
   - Include test requirements that carry safety conditions

## 6. Summary Checklist (Definition of Done)
   - Checkboxes covering every file + tests + docs
```

---

## 4. Project Constraints Every Plan Must Check

The technical approach (Phase C) must not violate any of the following
(full details in `docs/tech_stack_design.md` and `CLAUDE.md`):

- [ ] **No shell execution** (`exec.Command`) for anything that has a
      Netlink/D-Bus path — this is the project's primary command-injection
      defense
- [ ] **Runs as the `pigate` user + capabilities** (`cap_net_admin,cap_net_raw`),
      not root — if the task needs privileges beyond this, the answer is a
      targeted Polkit rule or sudoers entry in `install.sh`, never assuming
      root
- [ ] **Only the kernel layer touches the OS** — new capabilities must go
      through an interface in `interfaces.go` and be implemented in
      **both real and mock, always**
- [ ] **Mock mode is 100% safe** — devs run `-mock=true` on their actual
      workstation; mock code must have zero side effects on the operating
      system
- [ ] **Preserve the SD card** — runtime/ephemeral state lives in RAM
      (ring buffer, read live from the kernel); no frequent writes to SQLite
- [ ] **Firewall input chain keeps its 4 sections** — if firewall rule
      generation is touched, preserve the order: sanity/drop → audit log →
      dynamic accept (+Docker compat) → final drop-and-log
- [ ] **Wi-Fi goes through wpa_supplicant directly** (config file + control
      socket), not NetworkManager — read
      `docs/wifi_wpa_working_instruction.md` first
- [ ] **Frontend:** shadcn/ui only; semantic color variables (no hardcoded
      palette classes); flat design (no `shadow-*`/`backdrop-blur-*`);
      dark/light both supported; Dialogs containing portal components use
      `modal={false}` **only when the Dialog contains a Combobox input
      field** — see `docs/rules_of_work.md`
- [ ] **New dependencies** — avoid; if truly needed, prefer stdlib /
      `golang.org/x` / modules already present, in that order

---

## 5. Impact Analysis Checklist

Answer every item during Phase E — any item that is "relevant" must appear in
the plan (in the steps or in the cautions); items that are "not relevant" do
not need to be written into the plan:

**Permissions and security**
- [ ] Which role does the new/existing endpoint use — `authRoute` (POST is
      already blocked for non-super_admin by `RoleReadOnlyMiddleware`) or
      should it be an explicit `superAdminRoute`? Can sensitive data leak
      through GET?
- [ ] Should `-disable-edit=true` mode block this task
      (DisableEditMiddleware already blocks mutations system-wide — confirm
      that behavior is correct for this task)
- [ ] Where is user input validated — is there any path where a value ends
      up composing something dangerous (config file, D-Bus argument,
      nft rule)?
- [ ] Does `install.sh` need Polkit/sudoers changes → if yes:
      **how do already-installed devices migrate** (re-run install.sh /
      manual edit) — must be noted in the release notes

**Backend architecture**
- [ ] Does it touch routing/interfaces → does `netlink_monitor.go` need to
      be aware of or reconcile the new thing?
- [ ] Does state need applying at boot → per the startup order in `main.go`
      (interfaces → routes → monitor → DHCP → DNS → firewall → QoS),
      where should the new step slot in / or state that it is not needed
- [ ] Any new schema/migration in `db/` → what happens to existing users'
      data
- [ ] Does Backup/Restore (`service/backup.go`, schema v2) need to include
      this new config?
- [ ] Timing between the HTTP response and the side effect — is there a case
      where the side effect kills/blocks the process before the response is
      sent (e.g. reboot, restarting a service pigate depends on)?

**Frontend**
- [ ] Is the same thing invoked from one UI spot or several — logic should
      live in a shared hook/service so it is fixed in one place (e.g.
      `usePowerControl` is used by both Settings and nav-user)
- [ ] Does frontend mock mode (`services/config.ts`, `mockSync.ts`) need
      support?
- [ ] Is there a special state where the backend disappears temporarily
      (reboot, service restart) — must the frontend handle the dropped
      connection / poll until it returns?

**Docs and contract**
- [ ] `docs/openapi.yaml` and `frontend/public/openapi.yaml` — sync both
- [ ] Does the README Feature Status need updating?
- [ ] Do the `docs/ref/*` design docs of the touched subsystem need
      updating?

**Testing**
- [ ] Which flows can mock mode cover / what can only be tested on the real
      board
- [ ] Does testing on the real board risk locking yourself out of the device
      (network, firewall, power) → set conditions such as "test only when
      you have physical access" and a safe order (e.g. reboot before
      shutdown)
- [ ] `go build ./...` + `go test ./...` and `yarn build` + `yarn lint`
      pass after the changes

---

## 6. Writing Requirements (Style)

- **Language:** content in Thai; technical terms/file names/code in English.
  File header is a single `#` + a summary blockquote + writing date +
  reference branch
- **Location references:** repo-root-relative paths + approximate line
  numbers, with a `~` note when not exact (line numbers drift — the path and
  function name matter more)
- **Code samples:** only where they make a step clearer, as short as
  possible while still communicating. Do not write the full
  implementation — a plan is not a PR
- **Length:** a plan should fit in ~150-250 lines. If it is longer, the task
  is too big and should be split into multiple plans/phases
- **Data honesty:** every "current state" claim must come from opening the
  file as of the writing date. If some part was not checked, write "not yet
  checked" — never guess
- **Updating the plan:** if, once work starts, the plan turns out to be
  wrong or the situation changes, edit the plan file to match reality (a
  plan in `todo/` is a living document until the work is done; once done,
  design content worth keeping long-term should be moved/summarized into
  `docs/ref/<subsystem>-design.md` as appropriate)
