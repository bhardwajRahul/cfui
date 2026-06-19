# OAuth Validation Workbench Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the OAuth Overview validation area into a repeatable workbench for diagnosing OAuth app allowlist, requested/granted scopes, Cloudflare API availability, and saved validation snapshots.

**Architecture:** Reuse the existing backend validation endpoints and SQLite archive storage. Keep the change mostly in `web/dist/js/app-oauth.js` plus localized copy and the requirements document; do not add new Cloudflare product APIs or persist sensitive values.

**Tech Stack:** Go backend, SQLite/ent archive storage, native JavaScript frontend in `web/dist/js`, TOML i18n files, existing CSS utility classes.

---

### Task 1: Improve Validation Data Presentation

**Files:**
- Modify: `web/dist/js/app-oauth.js`

- [x] **Step 1: Add validation group helpers**

Add helpers near the existing validation helpers:

```js
function validationWorkbenchGroups(report) {
    const scopeChecks = new Map();
    for (const check of report?.scope_checks || []) {
        if (!check?.feature || scopeChecks.has(check.feature)) continue;
        scopeChecks.set(check.feature, check);
    }
    const apiChecks = new Map();
    for (const check of report?.api_checks || []) {
        if (!check?.id || apiChecks.has(check.id)) continue;
        apiChecks.set(check.id, check);
    }
    return validationGroupDefinitions().map((group) => ({
        ...group,
        scopeChecks: group.features.map((feature) => scopeChecks.get(feature)).filter(Boolean),
        apiChecks: group.metrics.map((metric) => apiChecks.get(metric)).filter(Boolean),
    })).filter((group) => group.publicOnly || group.scopeChecks.length || group.apiChecks.length);
}

function validationGroupDefinitions() {
    return [
        { id: 'identity', label: t('oauth_validation_group_identity'), features: ['account'], metrics: ['accounts'] },
        { id: 'dns', label: t('oauth_validation_group_dns'), features: ['zones', 'dns', 'zone_settings', 'cache_purge'], metrics: ['zones', 'active_zones', 'dns_records'] },
        { id: 'tunnel', label: t('oauth_validation_group_tunnel'), features: ['tunnels'], metrics: ['tunnels'] },
        { id: 'workers', label: t('oauth_validation_group_workers'), features: ['workers', 'workers_tail'], metrics: ['workers'] },
        { id: 'r2', label: t('oauth_validation_group_r2'), features: ['r2'], metrics: ['r2_buckets'] },
        { id: 'd1', label: t('oauth_validation_group_d1'), features: ['d1'], metrics: ['d1_databases'] },
        { id: 'kv', label: t('oauth_validation_group_kv'), features: ['kv'], metrics: ['kv_namespaces'] },
        { id: 'snippets', label: t('oauth_validation_group_snippets'), features: ['snippets'], metrics: ['snippets'] },
        { id: 'waf', label: t('oauth_validation_group_waf'), features: ['waf'], metrics: ['waf_rules'] },
        { id: 'analytics', label: t('oauth_validation_group_analytics'), features: ['analytics'], metrics: [] },
        { id: 'status', label: t('oauth_validation_group_status'), features: [], metrics: [], publicOnly: true },
    ];
}
```

- [x] **Step 2: Replace the flat validation panels**

Update `overviewValidationNode()` so the summary metrics remain, then append `validationWorkbenchNode(report)` instead of separately appending action items, missing scopes, and API issues.

- [x] **Step 3: Render grouped modules**

Add `validationWorkbenchNode(report)`, `validationGroupNode(group)`, `validationGroupLineNode(...)`, `validationScopeCheckText(check)`, and `validationAPIActionText(check)` to show one card per module with scope/API rows and actionable guidance.

- [x] **Step 4: Run frontend syntax check**

Run:

```bash
node --check web/dist/js/app-oauth.js
```

Expected: command exits 0.

### Task 2: Improve Archive List Summaries

**Files:**
- Modify: `web/dist/js/app-oauth.js`

- [x] **Step 1: Add archive score helpers**

Add helpers:

```js
function validationArchiveIssueCount(report) {
    return Number(report?.scope_missing || 0) + Number(report?.api_unavailable || 0) + Number(report?.api_missing_scope || 0);
}

function validationArchiveFingerprint(report) {
    return [report?.requested_scope_hash || '-', report?.granted_scope_hash || '-'].join(' / ');
}
```

- [x] **Step 2: Update `validationArchiveMeta(report)`**

Include saved time, requested/granted scope counts, issue count, action count, and fingerprint. Keep all data no-token and already available in archive summaries.

- [x] **Step 3: Preserve existing open/delete behavior**

Keep row click opening and delete confirmation unchanged. Buttons must still call `openOAuthValidationArchive` and `deleteOAuthValidationArchive`.

### Task 3: Localize Workbench Copy

**Files:**
- Modify: `locales/zh/oauth.toml`
- Modify: `locales/en/oauth.toml`
- Modify: `locales/ja/oauth.toml`

- [x] **Step 1: Add group labels**

Add keys for identity, DNS, tunnel, workers, R2, D1, KV, snippets, WAF, analytics, and status.

- [x] **Step 2: Add status/action copy**

Add labels for grouped status, requested/granted scope counts, fingerprint, API limited, API missing scope, API unavailable, and no issues.

- [x] **Step 3: Verify i18n endpoint**

After restarting the local server, run:

```bash
curl -fsS http://127.0.0.1:14333/api/i18n/zh | jq -r '.oauth_validation_workbench_title'
```

Expected: localized Chinese title.

### Task 4: Update Requirements

**Files:**
- Modify: `docs/oauth-cloudflare-console-requirements.md`

- [x] **Step 1: Update the OAuth overview bullet**

Document that validation history is now a workbench with module grouping, issue/action summaries, and no-token archive comparisons.

- [x] **Step 2: Keep the next-gap wording honest**

Do not claim real-account coverage is complete. Keep remaining gaps framed as real-account validation and tuning.

### Task 5: Verify, Run, Commit

**Files:**
- No new source files beyond the plan.

- [x] **Step 1: Run checks**

Run:

```bash
node --check web/dist/js/app-oauth.js
node --check web/dist/js/app-oauth-setup.js
node --check web/dist/js/app-core.js
node --check web/dist/js/app-ui.js
git diff --check
go test -count=1 ./internal/cfoauth ./internal/cfaccount ./internal/server
go test -count=1 ./...
go build -o /tmp/cfui-validation-workbench-verify .
```

Expected: all commands exit 0.

- [x] **Step 2: Update LAN instance**

Build `/tmp/cfui-lan-current`, restart port `14333` with OAuth env, and verify static resources include `validationWorkbenchNode`.

- [ ] **Step 3: Commit and push**

Commit message:

```bash
git commit -m "feat: add OAuth validation workbench"
git push
```

---

## Self-Review

- Spec coverage: covers grouped validation details, archive summaries, i18n, docs, verification, LAN update, commit, and push.
- Placeholder scan: no TODO/TBD placeholders.
- Type consistency: helper names are defined before use in this plan and match the existing frontend naming style.
