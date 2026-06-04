/* =========================================================================
   CloudFlared UI — R2 WebDAV
   ========================================================================= */
(() => {
    'use strict';
    const { state, $, t, API_BASE, apiGet, apiSend, toast, setBusy, flashField, setTokenVisible } = window.cfui;

    const availabilityKeys = {
        READY: 'r2_ready',
        API_TOKEN_REQUIRED: 'r2_api_token_required',
        ACCOUNT_ID_REQUIRED: 'r2_account_required',
        R2_PERMISSION_DENIED: 'r2_permission_denied',
        BUCKET_REQUIRED: 'r2_bucket_required',
        WEBDAV_CREDENTIALS_REQUIRED: 'r2_webdav_credentials_required',
        R2_BUCKET_NOT_FOUND: 'r2_bucket_not_found',
    };

    function r2AvailabilityText(availability) {
        if (!availability) return t('r2_configure_first');
        const key = availabilityKeys[availability.status];
        return key ? t(key) : (availability.message || t('r2_configure_first'));
    }

    async function fetchR2Settings() {
        try {
            const data = await apiGet('/r2/settings');
            state.r2.settings = data;
            renderR2Settings(data);
            if (data.enabled) await loadR2Files(state.r2.path || '/');
        } catch (err) {
            setR2Status('error', err.message);
        }
    }

    function renderR2Settings(s) {
        if (!s) return;
        $('r2-account-id').value = s.account_id || '';
        $('r2-jurisdiction').value = s.jurisdiction || 'default';
        $('r2-webdav-username').value = s.webdav_username || '';
        $('r2-webdav-password').value = '';
        $('r2-password-state').textContent = t(s.password_set ? 'r2_password_set' : 'r2_password_not_set');
        $('r2-endpoint').value = s.endpoint || '/webdav/r2/';

        const availability = s.availability || state.features?.availability?.r2_webdav;
        const ready = availability?.can_enable || s.enabled;
        setR2Status(s.enabled ? 'ok' : ready ? 'warn' : 'error', s.enabled ? t('r2_status_enabled') : ready ? t('r2_status_ready_to_enable') : t('r2_status_setup'));
        const notice = $('r2-permission-message');
        if (notice) {
            notice.textContent = r2AvailabilityText(availability);
            notice.dataset.state = availability?.can_enable ? 'ok' : 'warn';
        }
        renderBucketSelect(state.r2.buckets, s.bucket_name);
        $('r2-file-disabled').hidden = !!s.enabled;
        $('r2-file-manager').hidden = !s.enabled;
    }

    function setR2Status(stateName, text) {
        const el = $('r2-status');
        if (!el) return;
        el.dataset.state = stateName;
        el.querySelector('.text').textContent = text;
    }

    async function loadR2Buckets() {
        const btn = $('r2-refresh-buckets');
        setBusy(btn, true);
        try {
            const data = await apiGet('/r2/buckets');
            state.r2.buckets = data.buckets || [];
            renderBucketSelect(state.r2.buckets, $('r2-bucket-select')?.value || state.r2.settings?.bucket_name);
            toast.ok(t('r2_buckets_loaded'));
        } catch (err) {
            toast.err(t('r2_bucket_load_failed') + ': ' + err.message);
        } finally {
            setBusy(btn, false);
        }
    }

    function renderBucketSelect(buckets = [], selected = '') {
        const sel = $('r2-bucket-select');
        if (!sel) return;
        const current = selected || sel.value || '';
        sel.innerHTML = '';
        const empty = document.createElement('option');
        empty.value = '';
        empty.textContent = t('r2_bucket_choose');
        sel.appendChild(empty);
        const names = new Set();
        for (const bucket of buckets) {
            names.add(bucket.name);
            const opt = document.createElement('option');
            opt.value = bucket.name;
            opt.textContent = bucket.location ? `${bucket.name} (${bucket.location})` : bucket.name;
            sel.appendChild(opt);
        }
        if (current && !names.has(current)) {
            const opt = document.createElement('option');
            opt.value = current;
            opt.textContent = current;
            sel.appendChild(opt);
        }
        sel.value = current;
    }

    async function createR2Bucket() {
        const btn = $('r2-create-bucket');
        const input = $('r2-create-bucket-name');
        const name = input.value.trim();
        if (!name) {
            toast.err(t('r2_bucket_name_required'));
            return;
        }
        setBusy(btn, true, t('creating'));
        try {
            const bucket = await apiSend('/r2/buckets', 'POST', { name });
            state.r2.buckets = [...state.r2.buckets.filter((b) => b.name !== bucket.name), bucket];
            renderBucketSelect(state.r2.buckets, bucket.name);
            input.value = '';
            toast.ok(t('r2_bucket_created'));
        } catch (err) {
            toast.err(t('r2_bucket_create_failed') + ': ' + err.message);
        } finally {
            setBusy(btn, false);
        }
    }

    async function saveR2Settings() {
        const btn = $('r2-save-settings');
        setBusy(btn, true, t('saving'));
        const payload = {
            enabled: !!state.features?.r2_webdav,
            account_id: $('r2-account-id').value.trim(),
            bucket_name: $('r2-bucket-select').value.trim(),
            jurisdiction: $('r2-jurisdiction').value,
            webdav_username: $('r2-webdav-username').value.trim(),
            webdav_password: $('r2-webdav-password').value,
        };
        try {
            const data = await apiSend('/r2/settings', 'POST', payload);
            state.r2.settings = data;
            renderR2Settings(data);
            await window.cfui.fetchFeatures();
            flashField('r2-save-settings');
            toast.ok(t('r2_settings_saved'));
        } catch (err) {
            toast.err(t('r2_settings_save_failed') + ': ' + err.message);
        } finally {
            setBusy(btn, false);
        }
    }

    async function loadR2Files(path = state.r2.path || '/') {
        if (!state.r2.settings?.enabled) {
            renderR2Settings(state.r2.settings);
            return;
        }
        state.r2.loading = true;
        try {
            const data = await apiGet('/r2/files?path=' + encodeURIComponent(path));
            state.r2.path = data.path || '/';
            state.r2.files = data.entries || [];
            renderR2Files(data);
        } catch (err) {
            toast.err(t('r2_files_load_failed') + ': ' + err.message);
        } finally {
            state.r2.loading = false;
        }
    }

    function renderR2Files(data = { path: '/', parent: '', entries: [] }) {
        renderBreadcrumb(data.path || '/');
        const list = $('r2-file-list');
        if (!list) return;
        list.innerHTML = '';
        const entries = data.entries || [];
        if (!entries.length) {
            const empty = document.createElement('div');
            empty.className = 'empty';
            empty.textContent = t('r2_files_empty');
            list.appendChild(empty);
            return;
        }
        for (const entry of entries) {
            const row = document.createElement('div');
            row.className = 'r2-file-row';
            const main = document.createElement('div');
            main.className = 'r2-file-main';
            const name = document.createElement('div');
            name.className = 'r2-file-name';
            name.textContent = (entry.is_dir ? '/ ' : '') + entry.name;
            const meta = document.createElement('div');
            meta.className = 'r2-file-meta';
            meta.textContent = entry.is_dir ? t('r2_folder') : `${formatBytes(entry.size)} · ${formatDate(entry.mod_time)}`;
            main.append(name, meta);

            const actions = document.createElement('div');
            actions.className = 'r2-file-actions';
            if (entry.is_dir) actions.append(actionButton(t('open'), () => loadR2Files(entry.path)));
            else actions.append(actionLink(t('download'), `${API_BASE}/r2/files/download?path=${encodeURIComponent(entry.path)}`));
            actions.append(actionButton(t('rename'), () => renameR2Path(entry)));
            actions.append(actionButton(t('delete'), () => deleteR2Path(entry), 'btn--ghost'));
            row.append(main, actions);
            list.appendChild(row);
        }
    }

    function renderBreadcrumb(path) {
        const el = $('r2-breadcrumb');
        if (!el) return;
        el.innerHTML = '';
        const root = document.createElement('button');
        root.type = 'button';
        root.textContent = '/';
        root.addEventListener('click', () => loadR2Files('/'));
        el.appendChild(root);
        const parts = path.split('/').filter(Boolean);
        let acc = '';
        for (const part of parts) {
            acc += '/' + part;
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.textContent = part;
            const target = acc;
            btn.addEventListener('click', () => loadR2Files(target));
            el.appendChild(btn);
        }
    }

    function actionButton(label, fn, extra = '') {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = `btn btn--sm ${extra}`.trim();
        btn.textContent = label;
        btn.addEventListener('click', fn);
        return btn;
    }

    function actionLink(label, href) {
        const a = document.createElement('a');
        a.className = 'btn btn--sm';
        a.href = href;
        a.textContent = label;
        return a;
    }

    async function uploadR2File(file) {
        if (!file) return;
        const target = joinPath(state.r2.path || '/', file.name);
        try {
            const res = await fetch(`${API_BASE}/r2/files/${encodeObjectPath(target)}`, { method: 'PUT', body: file });
            if (!res.ok) throw new Error(await responseError(res));
            toast.ok(t('r2_upload_done'));
            await loadR2Files(state.r2.path);
        } catch (err) {
            toast.err(t('r2_upload_failed') + ': ' + err.message);
        }
    }

    async function createR2Folder() {
        const name = window.prompt(t('r2_new_folder_prompt'));
        if (!name) return;
        try {
            await apiSend('/r2/files/mkdir', 'POST', { path: joinPath(state.r2.path || '/', name.trim()) });
            toast.ok(t('r2_folder_created'));
            await loadR2Files(state.r2.path);
        } catch (err) {
            toast.err(t('r2_folder_create_failed') + ': ' + err.message);
        }
    }

    async function renameR2Path(entry) {
        const nextName = window.prompt(t('r2_rename_prompt'), entry.name);
        if (!nextName || nextName === entry.name) return;
        try {
            await apiSend('/r2/files/rename', 'POST', { from: entry.path, to: joinPath(parentPath(entry.path), nextName.trim()) });
            toast.ok(t('r2_renamed'));
            await loadR2Files(state.r2.path);
        } catch (err) {
            toast.err(t('r2_rename_failed') + ': ' + err.message);
        }
    }

    async function deleteR2Path(entry) {
        const ok = await window.cfui.confirm({ title: t('r2_delete_title'), message: t('r2_delete_message', { name: entry.name }), okText: t('delete') });
        if (!ok) return;
        try {
            const res = await fetch(`${API_BASE}/r2/files/${encodeObjectPath(entry.path)}`, { method: 'DELETE' });
            if (!res.ok) throw new Error(await responseError(res));
            toast.ok(t('r2_deleted'));
            await loadR2Files(state.r2.path);
        } catch (err) {
            toast.err(t('r2_delete_failed') + ': ' + err.message);
        }
    }

    function encodeObjectPath(path) {
        return path.split('/').filter(Boolean).map(encodeURIComponent).join('/');
    }

    function joinPath(base, name) {
        base = base || '/';
        name = (name || '').replace(/^\/+|\/+$/g, '');
        return base === '/' ? '/' + name : base.replace(/\/+$/g, '') + '/' + name;
    }

    function parentPath(path) {
        const parts = path.split('/').filter(Boolean);
        parts.pop();
        return parts.length ? '/' + parts.join('/') : '/';
    }

    async function responseError(res) {
        try { const data = await res.json(); return data.error || res.statusText; }
        catch { return res.statusText; }
    }

    function formatBytes(size) {
        if (!Number.isFinite(size) || size < 0) return '0 B';
        const units = ['B', 'KB', 'MB', 'GB', 'TB'];
        let value = size, unit = 0;
        while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit += 1; }
        return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
    }

    function formatDate(value) {
        const d = new Date(value);
        if (Number.isNaN(d.getTime())) return '';
        return d.toLocaleString();
    }

    function wireR2() {
        $('r2-refresh-buckets')?.addEventListener('click', loadR2Buckets);
        $('r2-create-bucket')?.addEventListener('click', createR2Bucket);
        $('r2-save-settings')?.addEventListener('click', saveR2Settings);
        $('r2-refresh-files')?.addEventListener('click', () => loadR2Files(state.r2.path || '/'));
        $('r2-new-folder')?.addEventListener('click', createR2Folder);
        $('r2-upload-input')?.addEventListener('change', (e) => {
            const file = e.target.files?.[0];
            e.target.value = '';
            uploadR2File(file);
        });
        $('r2-copy-endpoint')?.addEventListener('click', () => {
            const v = $('r2-endpoint')?.value || '/webdav/r2/';
            navigator.clipboard?.writeText(v).then(() => toast.ok(t('copied_to_clipboard')), () => toast.err(t('copy_failed')));
        });
        $('r2-bucket-select')?.addEventListener('change', () => {
            if (state.r2.settings) state.r2.settings.bucket_name = $('r2-bucket-select').value;
        });
        window.cfui.setTokenVisible && $('r2-password-toggle')?.addEventListener('click', (e) => {
            e.preventDefault();
            const input = $('r2-webdav-password');
            setTokenVisible(input, $('r2-password-toggle'), input.type === 'password');
        });
    }

    const ns = window.cfui;
    ns.r2AvailabilityText = r2AvailabilityText;
    ns.fetchR2Settings = fetchR2Settings;
    ns.loadR2Buckets = loadR2Buckets;
    ns.loadR2Files = loadR2Files;
    ns.wireR2 = wireR2;
})();
