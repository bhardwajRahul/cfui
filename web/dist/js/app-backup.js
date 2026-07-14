/* =========================================================================
   CloudFlared UI — Configuration backup and restore
   ========================================================================= */
(() => {
    'use strict';

    const { state, $, $$, t, toast, setBusy, openDialog, closeDialog, confirm,
            fetchFeatures, fetchConfig, fetchStatus, refreshDDNS } = window.cfui;

    const SECTION_ORDER = ['tunnels', 'remote_management', 'ddns', 's3_webdav', 'application', 'sensitive'];
    const NORMAL_SECTIONS = SECTION_ORDER.filter((section) => section !== 'sensitive');
    const MAX_BACKUP_BYTES = 8 * 1024 * 1024;

    let importFile = null;
    let importInspection = null;
    let inspectedSelection = '';
    let inspectSequence = 0;
    let exportObjectURL = '';

    function checkedValues(selector) {
        return $$(selector).filter((input) => input.checked && !input.disabled).map((input) => input.value);
    }

    function exportSelection() {
        const values = checkedValues('[data-backup-export-section]');
        return {
            sections: values.filter((section) => section !== 'sensitive'),
            includeSensitive: values.includes('sensitive'),
        };
    }

    function importSelection() {
        return checkedValues('[data-backup-import-section]');
    }

    function selectionFingerprint(sections) {
        const selected = new Set(sections);
        return SECTION_ORDER.filter((section) => selected.has(section)).join('\n');
    }

    function resetExportDialog() {
        $$('[data-backup-export-section]').forEach((input) => {
            input.checked = input.value !== 'sensitive';
        });
        if ($('config-backup-export-password')) $('config-backup-export-password').value = '';
        if ($('config-backup-export-warning')) $('config-backup-export-warning').hidden = true;
        if (exportObjectURL) {
            URL.revokeObjectURL(exportObjectURL);
            exportObjectURL = '';
        }
    }

    function resetImportDialog() {
        inspectSequence++;
        importFile = null;
        importInspection = null;
        inspectedSelection = '';
        if ($('config-backup-file')) $('config-backup-file').value = '';
        if ($('config-backup-import-password')) $('config-backup-import-password').value = '';
        if ($('config-backup-import-password-field')) $('config-backup-import-password-field').hidden = true;
        if ($('config-backup-import-details')) $('config-backup-import-details').hidden = true;
        if ($('config-backup-replace')) $('config-backup-replace').disabled = true;
        if ($('config-backup-file-name')) $('config-backup-file-name').textContent = t('config_backup_choose_file');
        $$('[data-backup-import-option]').forEach((label) => { label.hidden = true; });
        $$('[data-backup-import-section]').forEach((input) => { input.checked = false; });
        renderPreview({ removed_tunnels: [], restart_required: [], warnings: [] });
    }

    function updateExportWarning() {
        const { includeSensitive } = exportSelection();
        const hasPassword = !!$('config-backup-export-password')?.value;
        if ($('config-backup-export-warning')) $('config-backup-export-warning').hidden = !includeSensitive || hasPassword;
    }

    function openExportDialog() {
        resetExportDialog();
        openDialog($('config-backup-export-dialog'));
    }

    function chooseImportFile() {
        $('config-backup-file')?.click();
    }

    async function readBackupError(response) {
        let body = {};
        try { body = await response.json(); } catch { /* use status fallback */ }
        const error = new Error(body.error || response.statusText || t('config_backup_invalid_file'));
        error.code = body.code || 'invalid_backup';
        error.status = response.status;
        return error;
    }

    function backupErrorKey(code) {
        return ({
            password_required: 'config_backup_password_required',
            invalid_password_or_tampered: 'config_backup_invalid_password',
            invalid_backup: 'config_backup_invalid_file',
            unsupported_version: 'config_backup_unsupported_version',
            too_large: 'config_backup_too_large',
            invalid_selection: 'config_backup_select_section',
            save_failed: 'config_backup_save_failed',
        })[code] || 'config_backup_invalid_file';
    }

    async function downloadBackup() {
        const button = $('config-backup-download');
        const selection = exportSelection();
        const password = $('config-backup-export-password')?.value || '';
        if (!selection.sections.length) {
            toast.err(t('config_backup_select_section'));
            return;
        }

        let confirmPlaintextSensitive = false;
        if (selection.includeSensitive && !password) {
            const approved = await confirm({
                title: t('config_backup_plaintext_sensitive_title'),
                message: t('config_backup_plaintext_sensitive_message'),
                okText: t('config_backup_download'),
                okClass: 'btn--primary',
            });
            if (!approved) return;
            confirmPlaintextSensitive = true;
        }

        setBusy(button, true, t('config_backup_exporting'));
        try {
            const response = await fetch('/api/config-backup/export', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    sections: selection.sections,
                    include_sensitive: selection.includeSensitive,
                    password,
                    confirm_plaintext_sensitive: confirmPlaintextSensitive,
                }),
            });
            if (!response.ok) throw await readBackupError(response);
            const blob = await response.blob();
            const filename = backupFilename(response.headers.get('Content-Disposition'));
            if (exportObjectURL) URL.revokeObjectURL(exportObjectURL);
            exportObjectURL = URL.createObjectURL(blob);
            const link = document.createElement('a');
            link.href = exportObjectURL;
            link.download = filename;
            document.body.appendChild(link);
            link.click();
            link.remove();
            const completedURL = exportObjectURL;
            exportObjectURL = '';
            setTimeout(() => URL.revokeObjectURL(completedURL), 1000);
            toast.ok(t('config_backup_exported'));
            closeDialog($('config-backup-export-dialog'));
            resetExportDialog();
        } catch (error) {
            toast.err(t(backupErrorKey(error.code)));
        } finally {
            setBusy(button, false);
        }
    }

    function backupFilename(contentDisposition) {
        const fallback = 'cfui-backup.json';
        if (!contentDisposition) return fallback;
        const match = contentDisposition.match(/filename\*?=(?:UTF-8''|"?)([^";]+)/i);
        if (!match) return fallback;
        let value = match[1].trim();
        try { value = decodeURIComponent(value); } catch { /* keep header value */ }
        value = value.split(/[\\/]/).pop();
        return value || fallback;
    }

    async function onImportFileSelected(event) {
        const file = event.target.files?.[0];
        if (!file) return;
        if (file.size > MAX_BACKUP_BYTES) {
            event.target.value = '';
            toast.err(t('config_backup_too_large'));
            return;
        }
        resetImportDialog();
        importFile = file;
        $('config-backup-file-name').textContent = file.name;
        openDialog($('config-backup-import-dialog'));
        await inspectBackup(true);
    }

    async function inspectBackup(initialize) {
        if (!importFile) return;
        const selected = initialize ? null : importSelection();
        const fingerprint = selected ? selectionFingerprint(selected) : '';
        $('config-backup-replace').disabled = true;
        if (!initialize && !selected.length) {
            inspectedSelection = '';
            renderPreview({ removed_tunnels: [], restart_required: [], warnings: [] });
            return;
        }

        const sequence = ++inspectSequence;
        const button = $('config-backup-inspect');
        setBusy(button, true, t('config_backup_inspecting'));
        try {
            const form = new FormData();
            form.append('file', importFile, importFile.name);
            const password = $('config-backup-import-password')?.value || '';
            if (password) form.append('password', password);
            if (selected) form.append('sections', JSON.stringify(selected));
            const response = await fetch('/api/config-backup/inspect', { method: 'POST', body: form });
            if (!response.ok) throw await readBackupError(response);
            const inspection = await response.json();
            if (sequence !== inspectSequence) return;
            importInspection = inspection;
            $('config-backup-import-password-field').hidden = true;
            $('config-backup-import-details').hidden = false;
            if (initialize) {
                configureImportOptions(inspection.sections || []);
                renderInspection(inspection);
                await inspectBackup(false);
                return;
            }
            inspectedSelection = fingerprint;
            renderInspection(inspection);
            $('config-backup-replace').disabled = fingerprint !== selectionFingerprint(importSelection());
        } catch (error) {
            if (sequence !== inspectSequence) return;
            inspectedSelection = '';
            if (error.code === 'password_required' || error.code === 'invalid_password_or_tampered') {
                $('config-backup-import-password-field').hidden = false;
                $('config-backup-import-details').hidden = true;
                $('config-backup-replace').disabled = true;
                if (error.code === 'invalid_password_or_tampered') toast.err(t('config_backup_invalid_password'));
                setTimeout(() => $('config-backup-import-password')?.focus(), 0);
            } else {
                toast.err(t(backupErrorKey(error.code)));
            }
        } finally {
            setBusy(button, false);
        }
    }

    function configureImportOptions(availableSections) {
        const available = new Set(availableSections);
        for (const section of SECTION_ORDER) {
            const label = document.querySelector(`[data-backup-import-option="${section}"]`);
            const input = label?.querySelector('[data-backup-import-section]');
            if (!label || !input) continue;
            label.hidden = !available.has(section);
            input.disabled = !available.has(section);
            input.checked = available.has(section) && section !== 'sensitive';
        }
    }

    function renderInspection(inspection) {
        if (!inspection) return;
        const created = new Date(inspection.created_at);
        $('config-backup-created-at').textContent = Number.isNaN(created.getTime()) ? '-' : created.toLocaleString();
        $('config-backup-source-version').textContent = inspection.app_version || '-';
        $('config-backup-encryption').textContent = t(inspection.encrypted ? 'config_backup_encrypted' : 'config_backup_plaintext');
        const counts = [
            `${t('config_backup_tunnel_count')}: ${inspection.tunnel_profiles || 0}`,
            `${t('config_backup_ddns_source_count')}: ${inspection.ddns_sources || 0}`,
            `${t('config_backup_ddns_record_count')}: ${inspection.ddns_records || 0}`,
            `${t('config_backup_s3_mount_count')}: ${inspection.s3_mounts || 0}`,
        ];
        if (inspection.contains_sensitive) counts.push(t('config_backup_contains_sensitive'));
        $('config-backup-counts').textContent = counts.join(' · ');
        renderPreview(inspection);
    }

    function renderPreview(inspection) {
        renderSummary('config-backup-removed', 'config-backup-removed-wrap', inspection.removed_tunnels || []);
        renderSummary('config-backup-restart', 'config-backup-restart-wrap', inspection.restart_required || []);
        renderSummary('config-backup-warnings', 'config-backup-warnings-wrap', inspection.warnings || []);
    }

    function renderSummary(containerID, wrapperID, values) {
        const container = $(containerID);
        const wrapper = $(wrapperID);
        if (!container || !wrapper) return;
        container.replaceChildren();
        wrapper.hidden = !values.length;
        for (const value of values) {
            const item = document.createElement('span');
            item.textContent = String(value);
            container.appendChild(item);
        }
    }

    async function importBackup() {
        if (!importFile) return;
        const selected = importSelection();
        if (!selected.length) {
            toast.err(t('config_backup_select_section'));
            return;
        }
        if (selectionFingerprint(selected) !== inspectedSelection) {
            await inspectBackup(false);
            return;
        }
        const details = [t('config_backup_replace_warning')];
        if (importInspection?.removed_tunnels?.length) details.push(`${t('config_backup_removed_tunnels')}: ${importInspection.removed_tunnels.join(', ')}`);
        if (importInspection?.restart_required?.length) details.push(`${t('config_backup_restart_required')}: ${importInspection.restart_required.join(', ')}`);
        const approved = await confirm({
            title: t('config_backup_import_title'),
            message: details.join(' '),
            okText: t('config_backup_replace'),
        });
        if (!approved) return;

        const button = $('config-backup-replace');
        setBusy(button, true, t('config_backup_importing'));
        try {
            const form = new FormData();
            form.append('file', importFile, importFile.name);
            const password = $('config-backup-import-password')?.value || '';
            if (password) form.append('password', password);
            form.append('sections', JSON.stringify(selected));
            const response = await fetch('/api/config-backup/import', { method: 'POST', body: form });
            if (!response.ok) throw await readBackupError(response);
            const result = await response.json();
            closeDialog($('config-backup-import-dialog'));
            resetImportDialog();
            await refreshAfterImport();
            toast.ok(t('config_backup_imported'));
            const notices = [];
            if (result.stop_requested?.length) notices.push(`${t('config_backup_stop_requested')}: ${result.stop_requested.join(', ')}`);
            if (result.restart_required?.length) notices.push(`${t('config_backup_restart_required')}: ${result.restart_required.join(', ')}`);
            if (result.warnings?.length) notices.push(`${t('config_backup_warnings')}: ${result.warnings.join(', ')}`);
            if (notices.length) toast.warn(notices.join(' · '));
        } catch (error) {
            toast.err(t(backupErrorKey(error.code)));
        } finally {
            setBusy(button, false);
        }
    }

    async function refreshAfterImport() {
        await fetchFeatures?.();
        await fetchConfig?.();
        await fetchStatus?.();
        if (state.features?.ddns && !$('panel-ddns')?.hidden) await refreshDDNS?.();
        if (state.features?.s3_webdav && !$('panel-s3')?.hidden) await window.cfui.fetchS3Settings?.();
    }

    function rerenderDynamicCopy() {
        if (importFile && $('config-backup-file-name')) $('config-backup-file-name').textContent = importFile.name;
        if (importInspection) renderInspection(importInspection);
        updateExportWarning();
    }

    function wireBackup() {
        $('config-backup-export')?.addEventListener('click', openExportDialog);
        $('config-backup-import')?.addEventListener('click', chooseImportFile);
        $('config-backup-file')?.addEventListener('change', onImportFileSelected);
        $('config-backup-download')?.addEventListener('click', downloadBackup);
        $('config-backup-inspect')?.addEventListener('click', () => inspectBackup(true));
        $('config-backup-replace')?.addEventListener('click', importBackup);
        $('config-backup-import-password')?.addEventListener('keydown', (event) => {
            if (event.key === 'Enter') { event.preventDefault(); inspectBackup(true); }
        });
        $$('[data-backup-export-section], #config-backup-export-password').forEach((input) => input.addEventListener('change', updateExportWarning));
        $('config-backup-export-password')?.addEventListener('input', updateExportWarning);
        $$('[data-backup-import-section]').forEach((input) => input.addEventListener('change', () => inspectBackup(false)));

        const exportDialog = $('config-backup-export-dialog');
        const importDialog = $('config-backup-import-dialog');
        exportDialog?.addEventListener('click', (event) => {
            if (event.target === exportDialog || event.target.closest('[data-close-dialog]')) queueMicrotask(() => { if (exportDialog.hidden) resetExportDialog(); });
        });
        importDialog?.addEventListener('click', (event) => {
            if (event.target === importDialog || event.target.closest('[data-close-dialog]')) queueMicrotask(() => { if (importDialog.hidden) resetImportDialog(); });
        });
        document.addEventListener('keydown', (event) => {
            if (event.key !== 'Escape') return;
            queueMicrotask(() => {
                if (exportDialog?.hidden) resetExportDialog();
                if (importDialog?.hidden && importFile) resetImportDialog();
            });
        });
        document.addEventListener('localechange', rerenderDynamicCopy);
    }

    window.cfui.wireBackup = wireBackup;
})();
