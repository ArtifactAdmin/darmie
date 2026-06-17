/**
 * ui.js — All DOM manipulation lives here.
 * ALL user-generated strings are set via textContent (never innerHTML)
 * to prevent XSS.
 */

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
            li.className = 'room-item' + (room.id === currentRoomId ? ' active' : '');

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
        div.append(av, body);
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
    },

    addFileMessage({ fromUsername, filename, url, size, isSelf = false }) {
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
        div.append(av, body);
        messages.appendChild(div);
        messages.scrollTop = messages.scrollHeight;
    },

    // 
    // Video grid
    // 

    addVideoTile(userId, username, stream) {
        const grid = document.getElementById('video-grid');
        grid.classList.remove('hidden');

        let tile = document.getElementById('vtile-' + userId);
        if (!tile) {
            tile = document.createElement('div');
            tile.id        = 'vtile-' + userId;
            tile.className = 'video-tile';

            const video = document.createElement('video');
            video.autoplay   = true;
            video.playsInline = true;
            if (userId.startsWith('local')) {
                video.muted = true;
                video.classList.add('mirrored');
            }

            const label = document.createElement('div');
            label.className = 'video-label';
            label.textContent = userId.startsWith('local') ? 'You' : username;

            const fsBtn = document.createElement('button');
            fsBtn.className = 'video-fs-btn';
            fsBtn.title = 'Fullscreen';
            fsBtn.textContent = '⛶';

            fsBtn.addEventListener('click', () => {
                if (document.fullscreenElement === tile) {
                    document.exitFullscreen();
                } else {
                    tile.requestFullscreen();
                }
            });

            tile.addEventListener('fullscreenchange', () => {
                const isFs = document.fullscreenElement === tile;
                fsBtn.textContent = isFs ? '✕' : '⛶';
                fsBtn.title       = isFs ? 'Exit fullscreen' : 'Fullscreen';
            });

            // Per-user volume slider (remote tiles only — you never hear yourself).
            const extras = [];
            if (!userId.startsWith('local')) {
                const volCtrl = document.createElement('div');
                volCtrl.className = 'video-vol-ctrl';

                const icon = document.createElement('span');
                icon.className = 'video-vol-icon';
                icon.textContent = '🔊';

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
                    icon.textContent = vol.value === '0' ? '🔇' : '🔊';
                });

                volCtrl.append(icon, vol);
                extras.push(volCtrl);
            }

            tile.append(video, label, fsBtn, ...extras);
            grid.appendChild(tile);
        }
        tile.querySelector('video').srcObject = stream;
    },

    removeVideoTile(userId) {
        document.getElementById('vtile-' + userId)?.remove();
        const grid = document.getElementById('video-grid');
        if (grid.children.length === 0) grid.classList.add('hidden');
    },

    // 
    // Media control button states
    // 

    setMediaBtn(id, active) {
        document.getElementById(id)?.classList.toggle('active', active);
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

function _fmtBytes(bytes) {
    if (bytes < 1024)        return bytes + ' B';
    if (bytes < 1_048_576)   return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1_048_576).toFixed(1) + ' MB';
}
