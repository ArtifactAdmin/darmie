/**
 * ui.js — All DOM manipulation lives here.
 * ALL user-generated strings are set via textContent (never innerHTML)
 * to prevent XSS.
 */

import { ICONS } from './icons.js?v=10';

export const UI = {
    // 
    // View switching
    // 

    showAuth() {
        document.getElementById('auth-view').classList.remove('hidden');
        document.getElementById('app-view').classList.add('hidden');
    },

    showApp(username) {
        document.getElementById('auth-view').classList.add('hidden');
        document.getElementById('app-view').classList.remove('hidden');
        const nameEl = document.getElementById('user-name');
        nameEl.textContent = username;
        const av = document.getElementById('user-avatar');
        av.textContent = username[0].toUpperCase();
        av.style.background = _strColor(username);
    },

    clearAuthError() {
        const el = document.getElementById('auth-error');
        el.textContent = '';
        el.classList.add('hidden');
    },

    setAuthError(msg) {
        const el = document.getElementById('auth-error');
        el.textContent = msg;
        el.classList.remove('hidden');
    },

    // 
    // Sidebar room list
    // 

    updateRoomList(rooms, currentRoomId, onJoin) {
        const list = document.getElementById('room-list');
        list.innerHTML = '';
        for (const room of rooms) {
            const li = document.createElement('li');
            li.className = 'room-item';

            const nameSpan = document.createElement('span');
            nameSpan.className = 'room-item-name';
            nameSpan.textContent = '# ' + room.name;

            const badge = document.createElement('span');
            badge.className = 'room-item-count';
            badge.textContent = room.user_count;

            li.append(nameSpan, badge);
            li.addEventListener('click', () => onJoin(room.id));
            list.appendChild(li);
        }
    },

    // 
    // Main area — room view
    // 

    showRoom(name) {
        const messages = document.getElementById('messages');
        _revokeBlobUrls(messages);
        document.getElementById('no-room').classList.add('hidden');
        document.getElementById('room-view').classList.remove('hidden');
        document.getElementById('room-name-display').textContent = name;
        messages.innerHTML = '';
    },

    hideRoom() {
        const messages = document.getElementById('messages');
        _revokeBlobUrls(messages);
        document.getElementById('no-room').classList.remove('hidden');
        document.getElementById('room-view').classList.add('hidden');
        document.getElementById('user-list').innerHTML = '';
        const grid = document.getElementById('video-grid');
        grid.innerHTML = '';
        grid.classList.add('hidden');
        grid.classList.remove('has-focus');
        messages.innerHTML = '';
    },

    // 
    // User list (right panel)
    // 

    updateUserList(users) {
        const list = document.getElementById('user-list');
        list.innerHTML = '';
        for (const u of users) _appendUserItem(list, u);
    },

    addUser(user) {
        const list = document.getElementById('user-list');
        if (!list.querySelector(`[data-uid="${user.id}"]`)) {
            _appendUserItem(list, user);
        }
    },

    removeUser(userId) {
        document.querySelector(`#user-list [data-uid="${userId}"]`)?.remove();
    },

    // 
    // Chat messages
    // 

    addMessage({ fromUsername, content, timestamp, isSelf }) {
        const messages = document.getElementById('messages');
        const div = document.createElement('div');
        div.className = 'message' + (isSelf ? ' self' : '');

        const av = _makeAvatar(fromUsername);

        const body = document.createElement('div');
        body.className = 'msg-body';

        const hdr = document.createElement('div');
        hdr.className = 'msg-header';

        const un = document.createElement('span');
        un.className = 'msg-username';
        un.textContent = fromUsername;

        const ts = document.createElement('span');
        ts.className = 'msg-time';
        ts.textContent = _fmtTime(timestamp);

        hdr.append(un, ts);

        const txt = document.createElement('div');
        txt.className = 'msg-text';
        txt.textContent = content; // textContent — no XSS risk

        body.append(hdr, txt);

        // YouTube embeds — rendered for every video ID found in the message text
        for (const videoId of _extractYouTubeIds(content)) {
            const wrap = document.createElement('div');
            wrap.className = 'yt-embed-wrap';

            const iframe = document.createElement('iframe');
            iframe.className  = 'yt-embed';
            iframe.src        = `https://www.youtube.com/embed/${videoId}`;
            iframe.title      = 'YouTube video';
            iframe.loading    = 'lazy';
            iframe.allow      = 'accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share';
            iframe.allowFullscreen = true;

            wrap.appendChild(iframe);
            body.appendChild(wrap);
        }

        div.append(av, body);
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
    },

    addFileMessage({ fromUsername, filename, url, size, mimeType = '', isSelf = false }) {
        const messages = document.getElementById('messages');
        const div = document.createElement('div');
        div.className = 'message file-msg' + (isSelf ? ' self' : '');

        const av = _makeAvatar(fromUsername);

        const body = document.createElement('div');
        body.className = 'msg-body';

        const hdr = document.createElement('div');
        hdr.className = 'msg-header';

        const un = document.createElement('span');
        un.className = 'msg-username';
        un.textContent = fromUsername;

        const ts = document.createElement('span');
        ts.className = 'msg-time';
        ts.textContent = _fmtTime(Date.now());

        hdr.append(un, ts);

        const fileLine = document.createElement('div');
        fileLine.className = 'msg-file';

        const icon = document.createElement('span');
        icon.textContent = '📎 ';

        const link = document.createElement('a');
        link.href     = url;
        link.download = filename;
        link.textContent = filename; // filename is user-provided but we use textContent
        link.className = 'file-link';
        link.dataset.blobUrl = url; // tracked for revocation when the room is cleared

        const szSpan = document.createElement('span');
        szSpan.className = 'file-size';
        szSpan.textContent = ' (' + _fmtBytes(size) + ')';

        fileLine.append(icon, link, szSpan);
        body.append(hdr, fileLine);

        // Inline preview for images: a clickable thumbnail above the file line.
        if (_isImage(mimeType, filename)) {
            const thumbLink = document.createElement('a');
            thumbLink.href = url;
            thumbLink.target = '_blank';
            thumbLink.rel = 'noopener';
            thumbLink.dataset.blobUrl = url; // revoked with the rest on room clear

            const img = document.createElement('img');
            img.className = 'file-thumb';
            img.loading = 'lazy';
            img.alt = filename;
            img.src = url;

            thumbLink.appendChild(img);
            body.appendChild(thumbLink);
        }

        // Inline audio player
        if (_isAudio(mimeType, filename)) {
            const audio = document.createElement('audio');
            audio.controls  = true;
            audio.preload   = 'metadata';
            audio.src       = url;
            audio.className = 'file-audio';
            body.appendChild(audio);
        }

        // Inline video player
        if (_isVideo(mimeType, filename)) {
            const video = document.createElement('video');
            video.controls   = true;
            video.preload    = 'metadata';
            video.playsInline = true;
            video.src        = url;
            video.className  = 'file-video';
            body.appendChild(video);
        }

        div.append(av, body);
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
    },

    // Empty the video grid and hide it. Used when leaving or switching rooms so
    // local tiles (camera/screen) never linger across channels.
    clearVideoGrid() {
        const grid = document.getElementById('video-grid');
        grid.innerHTML = '';
        grid.classList.add('hidden');
        grid.classList.remove('has-focus');
    },

    // 
    // Video grid
    // 

    addVideoTile(userId, username, stream) {
        const grid = document.getElementById('video-grid');
        grid.classList.remove('hidden');

        // 'local'        → our own camera (mirror it, like a selfie view)
        // 'local-screen' → our own screen/window share (must NOT be mirrored,
        //                  otherwise shared text/UI reads backwards)
        const isLocal  = userId.startsWith('local');
        const isCamera = userId === 'local';
        const displayName = userId === 'local'        ? 'You'
                          : userId === 'local-screen'  ? 'You (screen)'
                          :                              username;

        let tile = document.getElementById('vtile-' + userId);
        if (!tile) {
            tile = document.createElement('div');
            tile.id        = 'vtile-' + userId;
            tile.className = 'video-tile';

            const video = document.createElement('video');
            video.autoplay   = true;
            video.playsInline = true;
            if (isLocal)  video.muted = true;        // never echo our own audio
            if (isCamera) video.classList.add('mirrored'); // mirror camera only

            // Shown in place of the black video frame while the stream carries
            // audio only (e.g. a voice-only participant sharing just a mic).
            const avatar = _makeAvatar(displayName, 72);
            avatar.classList.add('tile-avatar');

            const label = document.createElement('div');
            label.className = 'video-label';
            label.textContent = displayName;

            const fsBtn = document.createElement('button');
            fsBtn.className = 'video-fs-btn';
            fsBtn.title = 'Fullscreen';
            fsBtn.innerHTML = ICONS.fullscreen;

            fsBtn.addEventListener('click', () => {
                if (document.fullscreenElement === tile) {
                    document.exitFullscreen();
                } else {
                    tile.requestFullscreen();
                }
            });

            tile.addEventListener('fullscreenchange', () => {
                const isFs = document.fullscreenElement === tile;
                fsBtn.innerHTML = isFs ? ICONS.minimize : ICONS.fullscreen;
                fsBtn.title     = isFs ? 'Exit fullscreen' : 'Fullscreen';
            });

            // The <video> element fires `resize` whenever a video track starts
            // or stops producing frames, so the tile flips between live video
            // and the audio-only avatar on its own — no extra signalling needed.
            video.addEventListener('resize', () => _syncTileMode(tile, stream));

            // Click a stream to spotlight it in-app; click again to restore the grid.
            video.title = 'Click to focus';
            video.addEventListener('click', () => _focusTile(tile));

            // Per-user volume slider (remote tiles only — you never hear yourself).
            const extras = [];
            if (!userId.startsWith('local')) {
                const volCtrl = document.createElement('div');
                volCtrl.className = 'video-vol-ctrl';

                const volIcon = document.createElement('span');
                volIcon.className = 'video-vol-icon';
                volIcon.textContent = '🔊';

                const vol = document.createElement('input');
                vol.type      = 'range';
                vol.min       = '0';
                vol.max       = '1';
                vol.step      = '0.05';
                vol.value     = '1';
                vol.className = 'video-vol';
                vol.title     = 'Volume';
                vol.addEventListener('input', () => {
                    video.volume = Number(vol.value);
                    volIcon.textContent = vol.value === '0' ? '🔇' : '🔊';
                });

                volCtrl.append(volIcon, vol);
                extras.push(volCtrl);
            }

            tile.append(video, avatar, label, fsBtn, ...extras);
            grid.appendChild(tile);
        }
        const tileVideo = tile.querySelector('video');
        tileVideo.srcObject = stream;
        _syncTileMode(tile, stream);
        _playMedia(tileVideo);
    },

    removeVideoTile(userId) {
        const tile = document.getElementById('vtile-' + userId);
        const wasFocused = tile?.classList.contains('focused');
        tile?.remove();
        const grid = document.getElementById('video-grid');
        if (wasFocused) grid.classList.remove('has-focus');
        if (grid.children.length === 0) grid.classList.add('hidden');
    },

    // 
    // Media control button states
    // 

    setMediaBtn(id, active) {
        document.getElementById(id)?.classList.toggle('active', active);
    },

    // 
    // Upload progress bar
    // 

    showUploadProgress(filename) {
        document.getElementById('upload-progress-name').textContent = filename;
        document.getElementById('upload-progress-pct').textContent = '0%';
        document.getElementById('upload-progress-fill').style.width = '0%';
        document.getElementById('upload-progress').classList.remove('hidden');
        document.getElementById('upload-preview').classList.remove('hidden');
    },

    updateUploadProgress(fraction) {
        const pct = Math.round(fraction * 100);
        document.getElementById('upload-progress-pct').textContent = pct + '%';
        document.getElementById('upload-progress-fill').style.width = pct + '%';
    },

    hideUploadProgress() {
        document.getElementById('upload-progress').classList.add('hidden');
    },

    // 
    // Toast notifications
    // 

    toast(msg, kind = 'info') {
        const el = document.createElement('div');
        el.className = `toast toast-${kind}`;
        el.textContent = msg;
        document.getElementById('toasts').appendChild(el);
        requestAnimationFrame(() => el.classList.add('visible'));
        setTimeout(() => {
            el.classList.remove('visible');
            setTimeout(() => el.remove(), 400);
        }, 3500);
    },
};

//
// Private helpers
//

/**
 * Start playback of a remote media element. Browsers block autoplay of media
 * with sound until the page has a user gesture, so a stream that arrives while
 * the viewer is idle (e.g. a screen share with audio) would otherwise stay
 * paused and silent. Try to play immediately; if blocked, retry on the next
 * user interaction anywhere on the page.
 */
function _playMedia(video) {
    video.play().catch(() => {
        const resume = () => {
            video.play().then(() => document.removeEventListener('pointerdown', resume))
                         .catch(() => {});
        };
        document.addEventListener('pointerdown', resume);
    });
}

/**
 * Toggle a tile into audio-only mode (avatar shown, video hidden) when its
 * stream has no live video track. Recomputed reactively on every track change.
 */
function _syncTileMode(tile, stream) {
    const hasVideo = stream.getVideoTracks().some(t => t.readyState === 'live');
    tile.classList.toggle('audio-only', !hasVideo);
}

/**
 * Toggle in-app focus (spotlight) for a tile: the focused stream fills the
 * stage while the rest collapse to a thumbnail strip. Focusing a different
 * tile moves the spotlight; clicking the focused tile clears it.
 */
function _focusTile(tile) {
    const grid = document.getElementById('video-grid');
    const focus = !tile.classList.contains('focused');
    grid.querySelectorAll('.video-tile.focused').forEach(t => t.classList.remove('focused'));
    tile.classList.toggle('focused', focus);
    grid.classList.toggle('has-focus', focus);
}

/** Revoke all blob URLs tracked within a container before it is cleared. */
function _revokeBlobUrls(container) {
    container.querySelectorAll('[data-blob-url]').forEach(el => {
        URL.revokeObjectURL(el.dataset.blobUrl);
    });
}

function _appendUserItem(list, user) {
    const li = document.createElement('li');
    li.className  = 'user-item';
    li.dataset.uid = user.id;

    const av = _makeAvatar(user.username, 28);

    const name = document.createElement('span');
    name.textContent = user.username;

    li.append(av, name);
    list.appendChild(li);
}

function _makeAvatar(username, size = 36) {
    const av = document.createElement('div');
    av.className = 'avatar';
    av.style.cssText = `width:${size}px;height:${size}px;font-size:${Math.round(size * 0.45)}px`;
    av.textContent = username[0].toUpperCase();
    av.style.background = _strColor(username);
    return av;
}

/** Deterministic HSL colour from a string. */
function _strColor(str) {
    let hash = 0;
    for (let i = 0; i < str.length; i++) {
        hash = str.charCodeAt(i) + ((hash << 5) - hash);
    }
    return `hsl(${Math.abs(hash) % 360},55%,40%)`;
}

function _fmtTime(tsMs) {
    return new Date(tsMs).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

/** Heuristic: is this attachment an image we can preview inline? */
function _isImage(mimeType, filename) {
    if (mimeType && mimeType.startsWith('image/')) return true;
    return /\.(png|jpe?g|gif|webp|bmp|svg|avif)$/i.test(filename || '');
}

/** Heuristic: is this attachment an audio file? */
function _isAudio(mimeType, filename) {
    if (mimeType && mimeType.startsWith('audio/')) return true;
    return /\.(mp3|wav|ogg|flac|aac|m4a|opus|weba)$/i.test(filename || '');
}

/** Heuristic: is this attachment a video file? */
function _isVideo(mimeType, filename) {
    if (mimeType && mimeType.startsWith('video/')) return true;
    return /\.(mp4|webm|ogv|mov|avi|mkv|m4v)$/i.test(filename || '');
}

function _fmtBytes(bytes) {
    if (bytes < 1024)        return bytes + ' B';
    if (bytes < 1_048_576)   return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1_048_576).toFixed(1) + ' MB';
}

/**
 * Extract unique YouTube video IDs from a string.
 * Handles youtube.com/watch?v=, youtu.be/, youtube.com/shorts/, and /embed/ URLs.
 * Returns at most 3 IDs to prevent embed spam.
 */
function _extractYouTubeIds(text) {
    const seen = new Set();
    const ids  = [];
    const re   = /(?:youtube\.com\/(?:watch\?(?:[^\s&]*&)*v=|embed\/|shorts\/)|youtu\.be\/)([A-Za-z0-9_-]{11})/g;
    let m;
    while ((m = re.exec(text)) !== null && ids.length < 3) {
        if (!seen.has(m[1])) {
            seen.add(m[1]);
            ids.push(m[1]);
        }
    }
    return ids;
}
