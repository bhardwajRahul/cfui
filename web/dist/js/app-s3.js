/* =========================================================================
   CloudFlared UI - S3 WebDAV overview and file browser
   ========================================================================= */
(() => {
    'use strict';
    const { state, $, t, API_BASE, apiGet, apiSend, toast, setBusy } = window.cfui;

    const PROVIDER_R2 = 'cloudflare_r2';
    const DEFAULT_MOUNT = '/webdav/s3/';

    const availabilityKeys = {
        READY: 's3_ready',
        S3_ENDPOINT_REQUIRED: 's3_endpoint_required',
        S3_CREDENTIALS_REQUIRED: 's3_credentials_required',
        S3_MOUNT_PATH_INVALID: 's3_mount_path_invalid',
        BUCKET_REQUIRED: 's3_bucket_required',
        WEBDAV_CREDENTIALS_REQUIRED: 's3_webdav_credentials_required',
        S3_CONFIGURATION_INCOMPLETE: 's3_configuration_incomplete',
        S3_FILESYSTEM_UNAVAILABLE: 's3_filesystem_unavailable',
    };

    function s3AvailabilityText(availability) {
        if (!availability) return t('s3_configure_first');
        const key = availabilityKeys[availability.status];
        return key ? t(key) : (availability.message || t('s3_configure_first'));
    }

    async function fetchS3Settings() {
        try {
            const data = await apiGet('/s3/settings');
            state.s3.settings = data;
            state.s3.activeKey = data.active_key || data.mounts?.[0]?.key || 'default';
            renderS3Settings(data);
            const mount = activeMount();
            if (data.enabled && mount?.availability?.can_enable) await loadS3Files(state.s3.path || '/');
        } catch (err) {
            setS3Status('error', err.message);
        }
    }

    function renderS3Settings(settings) {
        if (!settings) return;
        renderMountList(settings);
        const mount = activeMount();
        const featureOn = !!settings.enabled || !!state.features?.s3_webdav;
        const ready = !!mount?.availability?.can_enable;
        setS3Status(ready ? 'ok' : 'warn', ready && featureOn ? t('s3_status_enabled') : ready ? t('s3_status_ready_to_enable') : t('s3_status_setup'));
        const notice = $('s3-status-message');
        if (notice) {
            notice.textContent = mount ? s3AvailabilityText(mount.availability) : t('s3_configure_first');
            notice.dataset.state = ready ? 'ok' : 'warn';
        }
        const filesReady = featureOn && ready;
        $('s3-file-disabled').hidden = filesReady;
        $('s3-file-manager').hidden = !filesReady;
    }

    function activeMount() {
        const settings = state.s3.settings;
        if (!settings?.mounts?.length) return null;
        const key = state.s3.activeKey || settings.active_key;
        return settings.mounts.find((m) => m.key === key) || settings.mounts[0];
    }

    function renderMountList(settings) {
        const list = $('s3-mount-list');
        if (!list) return;
        const mounts = settings.mounts || [];
        const activeKey = state.s3.activeKey || settings.active_key;
        list.innerHTML = '';
        if (!mounts.length) {
            const empty = document.createElement('div');
            empty.className = 'empty';
            empty.textContent = t('s3_empty_mounts');
            list.appendChild(empty);
            return;
        }
        for (const mount of mounts) list.appendChild(mountItem(mount, mount.key === activeKey));
    }

    function mountItem(mount, active) {
        const item = document.createElement('article');
        item.className = 's3-mount-item';
        item.dataset.active = String(active);
        item.setAttribute('role', 'listitem');

        const main = document.createElement('div');
        main.className = 's3-mount-main';

        const head = document.createElement('div');
        head.className = 's3-mount-item__head';
        const titleWrap = document.createElement('div');
        titleWrap.className = 's3-mount-title';
        const name = document.createElement('div');
        name.className = 's3-mount-item__name';
        name.textContent = mount.name || mount.key;
        const detail = document.createElement('div');
        detail.className = 's3-mount-item__meta';
        detail.textContent = `${providerLabel(mount.provider)} · ${mount.bucket_name || t('s3_bucket_required')}${mount.root_prefix ? '/' + mount.root_prefix : ''}`;
        titleWrap.append(name, detail);
        head.append(titleWrap, statusPill(mount));

        const endpoint = document.createElement('div');
        endpoint.className = 's3-mount-endpoint';
        const endpointLabel = document.createElement('div');
        endpointLabel.className = 's3-mount-label';
        endpointLabel.textContent = t('s3_webdav_endpoint');
        const endpointBox = document.createElement('div');
        endpointBox.className = 's3-copy-line';
        const endpointInput = document.createElement('input');
        endpointInput.type = 'text';
        endpointInput.className = 'input';
        endpointInput.readOnly = true;
        endpointInput.value = webDAVEndpointFor(mount.mount_path || DEFAULT_MOUNT);
        endpointInput.setAttribute('aria-label', t('s3_webdav_endpoint'));
        const copy = iconButton('copy_webdav_endpoint', copyIcon());
        copy.addEventListener('click', () => copyMountEndpoint(mount));
        endpointBox.append(endpointInput, copy);
        endpoint.append(endpointLabel, endpointBox);

        const meta = document.createElement('div');
        meta.className = 's3-mount-badges';
        meta.append(
            badge(mount.access_key_id && mount.secret_access_key_set ? t('s3_s3_keys_ready') : t('s3_s3_keys_missing'), mount.access_key_id && mount.secret_access_key_set ? 'ok' : 'warn'),
            badge(mount.webdav_username && mount.password_set ? t('s3_webdav_login_ready') : t('s3_webdav_login_missing'), mount.webdav_username && mount.password_set ? 'ok' : 'warn'),
            badge(mount.mount_path || DEFAULT_MOUNT, 'neutral')
        );

        main.append(head, endpoint, meta);

        const actions = document.createElement('div');
        actions.className = 's3-mount-actions';
        const files = textButton(t('s3_mount_files'), 'btn--sm');
        files.addEventListener('click', () => selectMount(mount.key));
        const edit = textButton(t('edit'), 'btn--sm');
        edit.addEventListener('click', () => window.cfui.openS3Wizard?.({ mode: 'edit', mount }));
        const del = textButton(t('delete'), 'btn--sm btn--danger');
        del.addEventListener('click', () => deleteS3Mount(mount.key, del));
        actions.append(files, edit, del);

        item.append(main, actions);
        return item;
    }

    function statusPill(mount) {
        const pill = document.createElement('span');
        pill.className = 'pill';
        pill.dataset.state = mount.availability?.can_enable ? 'ok' : 'warn';
        pill.innerHTML = '<span class="dot" aria-hidden="true"></span><span class="text"></span>';
        pill.querySelector('.text').textContent = mount.enabled ? (mount.availability?.can_enable ? t('ready') : t('s3_status_setup')) : t('disabled');
        return pill;
    }

    function badge(text, stateName) {
        const el = document.createElement('span');
        el.className = 's3-badge';
        el.dataset.state = stateName;
        el.textContent = text;
        return el;
    }

    function providerLabel(provider) {
        return provider === PROVIDER_R2 ? t('s3_provider_r2') : t('s3_provider_generic');
    }

    function setS3Status(stateName, text) {
        const el = $('s3-status');
        if (!el) return;
        el.dataset.state = stateName;
        el.querySelector('.text').textContent = text;
    }

    async function selectMount(key) {
        state.s3.activeKey = key;
        state.s3.path = '/';
        renderS3Settings(state.s3.settings);
        try {
            const data = await apiSend('/s3/settings', 'POST', { enabled: !!state.features?.s3_webdav, active_key: key });
            state.s3.settings = data;
            state.s3.activeKey = data.active_key || key;
            renderS3Settings(data);
            if (data.enabled && activeMount()?.availability?.can_enable) await loadS3Files('/');
        } catch (err) {
            toast.err(err.message);
        }
    }

    async function deleteS3Mount(key, btn) {
        const mount = (state.s3.settings?.mounts || []).find((m) => m.key === key);
        if (!mount) return;
        const ok = await window.cfui.confirm({
            title: t('s3_delete_mount_title'),
            message: t('s3_delete_mount_message', { name: mount.name || mount.key }),
            okText: t('delete'),
        });
        if (!ok) return;
        setBusy(btn, true);
        try {
            const data = await apiSend(`/s3/mounts/${encodeURIComponent(mount.key)}`, 'DELETE');
            state.s3.settings = data;
            state.s3.activeKey = data.active_key;
            state.s3.path = '/';
            renderS3Settings(data);
            toast.ok(t('s3_mount_deleted'));
        } catch (err) {
            toast.err(t('s3_mount_delete_failed') + ': ' + err.message);
        } finally {
            setBusy(btn, false);
        }
    }

    function nextMountPath() {
        const used = new Set((state.s3.settings?.mounts || []).map((m) => m.mount_path));
        if (!used.has(DEFAULT_MOUNT)) return DEFAULT_MOUNT;
        for (let i = 2; i < 100; i += 1) {
            const p = `/webdav/s3-${i}/`;
            if (!used.has(p)) return p;
        }
        return `/webdav/s3-${Date.now()}/`;
    }

    function webDAVEndpointFor(path) {
        const normalized = (path || DEFAULT_MOUNT).trim() || DEFAULT_MOUNT;
        try {
            return new URL(normalized, window.location.origin).toString();
        } catch {
            return normalized;
        }
    }

    function copyMountEndpoint(mount) {
        const value = webDAVEndpointFor(mount.mount_path || DEFAULT_MOUNT);
        navigator.clipboard?.writeText(value).then(() => toast.ok(t('copied_to_clipboard')), () => toast.err(t('copy_failed')));
    }

    async function loadS3Files(path = state.s3.path || '/') {
        const mount = activeMount();
        if (!mount || !state.s3.settings?.enabled || !mount.availability?.can_enable) {
            renderS3Settings(state.s3.settings);
            return;
        }
        state.s3.loading = true;
        try {
            const data = await apiGet('/s3/files?mount_key=' + encodeURIComponent(mount.key) + '&path=' + encodeURIComponent(path));
            state.s3.path = data.path || '/';
            state.s3.files = data.entries || [];
            renderS3Files(data);
        } catch (err) {
            toast.err(t('s3_files_load_failed') + ': ' + err.message);
        } finally {
            state.s3.loading = false;
        }
    }

    function renderS3Files(data = { path: '/', parent: '', entries: [] }) {
        renderBreadcrumb(data.path || '/');
        const list = $('s3-file-list');
        if (!list) return;
        list.innerHTML = '';
        const entries = data.entries || [];
        if (!entries.length) {
            const empty = document.createElement('div');
            empty.className = 'empty';
            empty.textContent = t('s3_files_empty');
            list.appendChild(empty);
            return;
        }
        for (const entry of entries) {
            const row = document.createElement('div');
            row.className = 's3-file-row';
            const main = document.createElement('div');
            main.className = 's3-file-main';
            const name = document.createElement('div');
            name.className = 's3-file-name';
            name.textContent = (entry.is_dir ? '/ ' : '') + entry.name;
            const meta = document.createElement('div');
            meta.className = 's3-file-meta';
            meta.textContent = entry.is_dir ? t('s3_folder') : `${formatBytes(entry.size)} · ${formatDate(entry.mod_time)}`;
            main.append(name, meta);

            const actions = document.createElement('div');
            actions.className = 's3-file-actions';
            if (entry.is_dir) actions.append(actionButton(t('open'), () => loadS3Files(entry.path)));
            else actions.append(actionLink(t('download'), `${API_BASE}/s3/files/download?${fileQuery(entry.path)}`));
            actions.append(actionButton(t('rename'), () => renameS3Path(entry)));
            actions.append(actionButton(t('delete'), () => deleteS3Path(entry), 'btn--ghost'));
            row.append(main, actions);
            list.appendChild(row);
        }
    }

    function renderBreadcrumb(path) {
        const el = $('s3-breadcrumb');
        if (!el) return;
        el.innerHTML = '';
        const root = document.createElement('button');
        root.type = 'button';
        root.textContent = '/';
        root.addEventListener('click', () => loadS3Files('/'));
        el.appendChild(root);
        const parts = path.split('/').filter(Boolean);
        let acc = '';
        for (const part of parts) {
            acc += '/' + part;
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.textContent = part;
            const target = acc;
            btn.addEventListener('click', () => loadS3Files(target));
            el.appendChild(btn);
        }
    }

    function actionButton(label, fn, extra = '') {
        const btn = textButton(label, `btn--sm ${extra}`.trim());
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

    function textButton(label, extra = '') {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = `btn ${extra}`.trim();
        btn.textContent = label;
        return btn;
    }

    function iconButton(labelKey, svg) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'icon-btn';
        btn.setAttribute('aria-label', t(labelKey));
        btn.setAttribute('title', t(labelKey));
        btn.innerHTML = svg;
        return btn;
    }

    function copyIcon() {
        return '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';
    }

    async function uploadS3File(file) {
        const mount = activeMount();
        if (!mount || !file) return;
        const target = joinPath(state.s3.path || '/', file.name);
        try {
            const res = await fetch(`${API_BASE}/s3/files/${encodeObjectPath(target)}?mount_key=${encodeURIComponent(mount.key)}`, { method: 'PUT', body: file });
            if (!res.ok) throw new Error(await responseError(res));
            toast.ok(t('s3_upload_done'));
            await loadS3Files(state.s3.path);
        } catch (err) {
            toast.err(t('s3_upload_failed') + ': ' + err.message);
        }
    }

    async function createS3Folder() {
        const mount = activeMount();
        const name = window.prompt(t('s3_new_folder_prompt'));
        if (!mount || !name) return;
        try {
            await apiSend('/s3/files/mkdir', 'POST', { mount_key: mount.key, path: joinPath(state.s3.path || '/', name.trim()) });
            toast.ok(t('s3_folder_created'));
            await loadS3Files(state.s3.path);
        } catch (err) {
            toast.err(t('s3_folder_create_failed') + ': ' + err.message);
        }
    }

    async function renameS3Path(entry) {
        const mount = activeMount();
        const nextName = window.prompt(t('s3_rename_prompt'), entry.name);
        if (!mount || !nextName || nextName === entry.name) return;
        try {
            await apiSend('/s3/files/rename', 'POST', { mount_key: mount.key, from: entry.path, to: joinPath(parentPath(entry.path), nextName.trim()) });
            toast.ok(t('s3_renamed'));
            await loadS3Files(state.s3.path);
        } catch (err) {
            toast.err(t('s3_rename_failed') + ': ' + err.message);
        }
    }

    async function deleteS3Path(entry) {
        const mount = activeMount();
        const ok = await window.cfui.confirm({ title: t('s3_delete_title'), message: t('s3_delete_message', { name: entry.name }), okText: t('delete') });
        if (!mount || !ok) return;
        try {
            const res = await fetch(`${API_BASE}/s3/files/${encodeObjectPath(entry.path)}?mount_key=${encodeURIComponent(mount.key)}`, { method: 'DELETE' });
            if (!res.ok) throw new Error(await responseError(res));
            toast.ok(t('s3_deleted'));
            await loadS3Files(state.s3.path);
        } catch (err) {
            toast.err(t('s3_delete_failed') + ': ' + err.message);
        }
    }

    function fileQuery(path) {
        const mount = activeMount();
        return `mount_key=${encodeURIComponent(mount?.key || '')}&path=${encodeURIComponent(path)}`;
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

    function wireS3() {
        $('s3-new-mount')?.addEventListener('click', () => window.cfui.openS3Wizard?.({ mode: 'create' }));
        $('s3-refresh-files')?.addEventListener('click', () => loadS3Files(state.s3.path || '/'));
        $('s3-new-folder')?.addEventListener('click', createS3Folder);
        $('s3-upload-input')?.addEventListener('change', (e) => {
            const file = e.target.files?.[0];
            e.target.value = '';
            uploadS3File(file);
        });
        window.cfui.wireS3Wizard?.();
    }

    const ns = window.cfui;
    ns.s3AvailabilityText = s3AvailabilityText;
    ns.s3ProviderLabel = providerLabel;
    ns.s3WebDAVEndpointFor = webDAVEndpointFor;
    ns.s3NextMountPath = nextMountPath;
    ns.s3ActiveMount = activeMount;
    ns.renderS3Settings = renderS3Settings;
    ns.fetchS3Settings = fetchS3Settings;
    ns.loadS3Files = loadS3Files;
    ns.wireS3 = wireS3;
})();
