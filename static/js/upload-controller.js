/**
 * Owns HTTP uploads, their progress UI, preview lifecycle, and drag-and-drop.
 */

export class UploadController {
    constructor({ ui, getUploadTarget }) {
        this._ui = ui;
        this._getUploadTarget = getUploadTarget;
        this._previewUrl = null;
        this._dragDepth = 0;
    }

    bind() {
        document.getElementById('file-input').addEventListener('change', (event) => {
            const file = event.target.files[0];
            event.target.value = '';
            this._upload(file);
        });

        const roomView = document.getElementById('room-view');
        const dropOverlay = document.getElementById('drop-overlay');

        roomView.addEventListener('dragenter', (event) => {
            if (!this._hasFiles(event)) return;
            event.preventDefault();
            this._dragDepth++;
            if (this._dragDepth === 1) dropOverlay.classList.remove('hidden');
        });

        roomView.addEventListener('dragleave', () => {
            this._dragDepth = Math.max(0, this._dragDepth - 1);
            if (this._dragDepth === 0) dropOverlay.classList.add('hidden');
        });

        roomView.addEventListener('dragover', (event) => {
            if (!this._hasFiles(event)) return;
            event.preventDefault();
            event.dataTransfer.dropEffect = 'copy';
        });

        roomView.addEventListener('drop', (event) => {
            event.preventDefault();
            this._dragDepth = 0;
            dropOverlay.classList.add('hidden');
            if (!this._getUploadTarget()) return;
            [...event.dataTransfer.files].forEach((file) => this._upload(file));
        });
    }

    reset() {
        this._dragDepth = 0;
        document.getElementById('drop-overlay').classList.add('hidden');
        this._clearPreview();
    }

    _hasFiles(event) {
        return event.dataTransfer?.types?.includes('Files');
    }

    _showPreview(file) {
        this._previewUrl = URL.createObjectURL(file);
        const panel = document.getElementById('upload-preview');
        const image = document.getElementById('upload-preview-img');
        const audio = document.getElementById('upload-preview-audio');
        const video = document.getElementById('upload-preview-video');

        image.classList.add('hidden'); image.src = '';
        audio.classList.add('hidden'); audio.src = '';
        video.classList.add('hidden'); video.src = '';

        if (file.type.startsWith('image/')) {
            image.src = this._previewUrl;
            image.alt = file.name;
            image.classList.remove('hidden');
        } else if (file.type.startsWith('audio/')) {
            audio.src = this._previewUrl;
            audio.classList.remove('hidden');
        } else if (file.type.startsWith('video/')) {
            video.src = this._previewUrl;
            video.classList.remove('hidden');
        }

        panel.classList.remove('hidden');
    }

    _clearPreview() {
        const panel = document.getElementById('upload-preview');
        const image = document.getElementById('upload-preview-img');
        const audio = document.getElementById('upload-preview-audio');
        const video = document.getElementById('upload-preview-video');
        panel.classList.add('hidden');
        image.src = '';
        audio.src = '';
        video.src = '';
        if (this._previewUrl) {
            URL.revokeObjectURL(this._previewUrl);
            this._previewUrl = null;
        }
    }

    async _upload(file) {
        const target = this._getUploadTarget();
        if (!file || !target) return;

        if (file.type.startsWith('image/') || file.type.startsWith('audio/') || file.type.startsWith('video/')) {
            this._showPreview(file);
        }

        const url = `/upload?token=${encodeURIComponent(target.token)}&room_id=${encodeURIComponent(target.roomId)}`;
        const formData = new FormData();
        formData.append('file', file);

        this._ui.showUploadProgress(file.name);
        try {
            await this._send(url, formData);
        } catch (err) {
            this._ui.toast(err.message, 'error');
        } finally {
            this._ui.hideUploadProgress();
            this._clearPreview();
        }
    }

    _send(url, formData) {
        return new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest();
            xhr.open('POST', url);
            xhr.upload.addEventListener('progress', (event) => {
                if (event.lengthComputable) {
                    this._ui.updateUploadProgress(event.loaded / event.total);
                }
            });
            xhr.addEventListener('load', () => {
                if (xhr.status >= 200 && xhr.status < 300) {
                    resolve();
                    return;
                }
                let data = {};
                try { data = JSON.parse(xhr.responseText); } catch { /* malformed error response */ }
                reject(new Error(data.error || 'Upload failed'));
            });
            xhr.addEventListener('error', () => reject(new Error('Upload error')));
            xhr.addEventListener('abort', () => reject(new Error('Upload aborted')));
            xhr.send(formData);
        });
    }
}
