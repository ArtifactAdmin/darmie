/**
 * app.js — Main application entry point.
 * Wires together the WebSocket, WebRTC, FileTransfer, and UI modules.
 */

import { MSG }            from './protocol.js?v=7';
import { WSManager }      from './ws.js?v=7';
import { WebRTCManager}   from './webrtc.js?v=7';
import { FileTransfer }   from './filetransfer.js?v=7';
import { UI }             from './ui.js?v=7';
import { ICONS, applyIcons } from './icons.js?v=7';

// Key under which the resumable session token is persisted across reloads.
const SESSION_KEY = 'darmie_session';

// Application state

const state = {
    userId:       null,
    username:     null,
    sessionToken: null,  // persisted session token; also the upload credential
    rooms:        [],       // RoomInfo[]
    currentRoom:  null,     // { id, name } | null
    roomUsers:    [],       // UserInfo[]
    hasAudio:     false,
    audioMuted:   false,
    hasVideo:     false,
    hasScreen:    false,
};

// Module instances 
const ws           = new WSManager();
const webrtc       = new WebRTCManager(ws);
const fileTransfer = new FileTransfer(webrtc);

// WebRTC callbacks 

webrtc.onRemoteStream = (userId, stream) => {
    const user = state.roomUsers.find(u => u.id === userId);
    UI.addVideoTile(userId, user?.username ?? userId, stream);
};

webrtc.onRemoteStreamRemoved = (userId) => UI.removeVideoTile(userId);

webrtc.onConnectionState = (userId, connState) => {
    if (connState === 'connected') {
        const user = state.roomUsers.find(u => u.id === userId);
        if (user) UI.toast(`Connected to ${user.username}`, 'success');
    }
};

// File transfer callbacks 

fileTransfer.onFileReceived = (fromUserId, name, url, size, mimeType) => {
    const user = state.roomUsers.find(u => u.id === fromUserId);
    UI.addFileMessage({ fromUsername: user?.username ?? fromUserId, filename: name, url, size, mimeType });
};

fileTransfer.onProgress = (_userId, fraction) => {
    if (fraction >= 1) UI.toast('File sent ✓', 'success');
};

// WebSocket message handlers

ws.on(MSG.AUTH_SUCCESS, (p) => {
    state.userId       = p.user_id;
    state.username     = p.username;
    state.sessionToken = p.session_token;
    // Persist the token so a page reload or dropped connection can resume the
    // session silently instead of forcing the user to sign in again.
    try { localStorage.setItem(SESSION_KEY, p.session_token); } catch { /* private mode */ }
    webrtc.init(p.user_id);
    UI.showApp(p.username);
    ws.send(MSG.LIST_ROOMS);
    // No room is active yet — on mobile, open the rooms drawer to pick one.
    if (_isMobile()) _openDrawer(_sidebarEl);
});

ws.on(MSG.AUTH_ERROR, (p) => {
    // A failed login/register, or a stale session token that no longer resumes.
    // Drop the token and fall back to the auth screen either way.
    _clearSession();
    if (state.userId) {
        _resetRoom();
        state.userId   = null;
        state.username = null;
        state.rooms    = [];
        UI.showAuth();
    }
    UI.setAuthError(p.message);
});

ws.on(MSG.ROOM_LIST, (p) => {
    state.rooms = p.rooms;
    UI.updateRoomList(p.rooms, state.currentRoom?.id, _joinRoom);
});

ws.on(MSG.ROOM_CREATED, (p) => {
    state.rooms.push(p.room);
    UI.updateRoomList(state.rooms, state.currentRoom?.id, _joinRoom);
    _joinRoom(p.room.id);
});

ws.on(MSG.ROOM_JOINED, (p) => {
    // Entering a room (possibly switching from another). Tear down the previous
    // room's peer connections and local media FIRST so audio/video/screen never
    // leak across channels — each channel is its own call. The server already
    // removed us from the old room (single-room invariant); this mirrors that on
    // the client. On a first join these are no-ops.
    webrtc.closeAll();
    _resetMediaState();
    UI.clearVideoGrid();

    state.currentRoom = { id: p.room.id, name: p.room.name };
    state.roomUsers   = [...p.users];

    // Sync the server-authoritative user count into our local room list.
    const joinedEntry = state.rooms.find(r => r.id === p.room.id);
    if (joinedEntry) joinedEntry.user_count = p.room.user_count;

    UI.showRoom(p.room.name);
    UI.updateUserList(p.users);
    UI.updateRoomList(state.rooms, p.room.id, _joinRoom);

    // Replay message history so rejoining users see past messages.
    if (p.history) {
        for (const entry of p.history) {
            if (entry.kind === 'file') {
                UI.addFileMessage({
                    fromUsername: entry.from_username,
                    filename:     entry.filename,
                    url:          entry.url,
                    size:         entry.size,
                    mimeType:     entry.mime_type,
                    isSelf:       entry.from_user_id === state.userId,
                });
            } else {
                UI.addMessage({
                    fromUsername: entry.from_username,
                    content:      entry.content,
                    timestamp:    entry.timestamp,
                    isSelf:       entry.from_user_id === state.userId,
                });
            }
        }
    }
    // On mobile, collapse the rooms drawer so the chat is in full view.
    if (_isMobile()) _closeDrawers();
    // Existing peers will send offers to us via onnegotiationneeded — we wait.
});

ws.on(MSG.ROOM_LEFT, () => {
    _resetRoom();
    // Back to a no-room state: surface the rooms drawer so the user can pick one.
    if (_isMobile()) _openDrawer(_sidebarEl);
});

ws.on(MSG.USER_JOINED, (p) => {
    const { user } = p;
    if (!state.roomUsers.find(u => u.id === user.id)) {
        state.roomUsers.push(user);
    }
    UI.addUser(user);
    UI.toast(`${user.username} joined`, 'info');
    // As an existing member we initiate the peer connection.
    webrtc.initiatePeer(user.id);

    // Keep sidebar badge in sync.
    const roomEntry = state.rooms.find(r => r.id === p.room_id);
    if (roomEntry) {
        roomEntry.user_count++;
        UI.updateRoomList(state.rooms, state.currentRoom?.id, _joinRoom);
    }
});

ws.on(MSG.USER_LEFT, (p) => {
    state.roomUsers = state.roomUsers.filter(u => u.id !== p.user_id);
    UI.removeUser(p.user_id);
    webrtc.closePeer(p.user_id);

    // Keep sidebar badge in sync.
    const roomEntry = state.rooms.find(r => r.id === p.room_id);
    if (roomEntry) {
        roomEntry.user_count = Math.max(0, roomEntry.user_count - 1);
        UI.updateRoomList(state.rooms, state.currentRoom?.id, _joinRoom);
    }
});

ws.on(MSG.TEXT_MESSAGE, (p) => {
    UI.addMessage({
        fromUsername: p.from_username,
        content:      p.content,
        timestamp:    p.timestamp,
        isSelf:       p.from_user_id === state.userId,
    });
});

ws.on(MSG.FILE_MESSAGE, (p) => {
    UI.addFileMessage({
        fromUsername: p.from_username,
        filename:     p.filename,
        url:          p.url,
        size:         p.size,
        mimeType:     p.mime_type,
        isSelf:       p.from_user_id === state.userId,
    });
});

ws.on(MSG.OFFER, (p) => {
    webrtc.handleOffer(p.from_user_id, p.sdp).catch(console.error);
});

ws.on(MSG.ANSWER, (p) => {
    webrtc.handleAnswer(p.from_user_id, p.sdp).catch(console.error);
});

ws.on(MSG.ICE_CANDIDATE, (p) => {
    webrtc.handleICECandidate(p.from_user_id, p.candidate).catch(console.error);
});

ws.on(MSG.VIDEO_STOPPED, (p) => {
    UI.removeVideoTile(p.user_id);
});

ws.on(MSG.ERROR, (p) => UI.toast(p.message, 'error'));

ws.on(MSG.DISCONNECTED, () => {
    if (state.userId) {
        UI.toast('Connection lost — reconnecting…', 'error');
        setTimeout(_connectWS, 3000);
    }
});

// Action helpers

function _joinRoom(roomId) {
    if (roomId === state.currentRoom?.id) return;
    ws.send(MSG.JOIN_ROOM, { room_id: roomId });
}

function _resetRoom() {
    webrtc.closeAll();
    _resetMediaState();
    state.currentRoom = null;
    state.roomUsers   = [];
    UI.hideRoom();
    UI.updateRoomList(state.rooms, null, _joinRoom);
}

function _resetMediaState() {
    state.hasAudio          = false;
    state.audioMuted        = false;
    state.hasVideo          = false;
    state.hasScreen         = false;
    _screenAutoStartedAudio = false;
    const audioBtn = document.getElementById('btn-audio');
    audioBtn.innerHTML = ICONS.mic;
    audioBtn.title = 'Start microphone';
    audioBtn.classList.remove('muted');
    UI.setMediaBtn('btn-audio',  false);
    UI.setMediaBtn('btn-rnnoise', false);
    UI.setMediaBtn('btn-video',  false);
    UI.setMediaBtn('btn-screen', false);
}

// Event: auth tabs

document.querySelectorAll('.auth-tab').forEach(tab => {
    tab.addEventListener('click', () => {
        document.querySelectorAll('.auth-tab').forEach(t => t.classList.remove('active'));
        tab.classList.add('active');
        document.querySelectorAll('.auth-form').forEach(f => f.classList.add('hidden'));
        document.getElementById(tab.dataset.form).classList.remove('hidden');
        UI.clearAuthError();
    });
});

// Event: login

document.getElementById('login-btn').addEventListener('click', _doLogin);
document.getElementById('login-password').addEventListener('keydown', e => {
    if (e.key === 'Enter') _doLogin();
});

function _doLogin() {
    const username = document.getElementById('login-username').value.trim();
    const password = document.getElementById('login-password').value;
    if (!username || !password) return;
    UI.clearAuthError();
    ws.send(MSG.LOGIN, { username, password });
}

// Event: register

document.getElementById('register-btn').addEventListener('click', _doRegister);
document.getElementById('register-password').addEventListener('keydown', e => {
    if (e.key === 'Enter') _doRegister();
});

function _doRegister() {
    const username = document.getElementById('register-username').value.trim();
    const password = document.getElementById('register-password').value;
    if (!username || !password) return;
    UI.clearAuthError();
    ws.send(MSG.REGISTER, { username, password });
}

// Event: logout

document.getElementById('logout-btn').addEventListener('click', () => {
    // Invalidate the session server-side so the token can never be resumed,
    // then forget it locally before tearing down the socket.
    ws.send(MSG.LOGOUT);
    _clearSession();
    _resetRoom();
    _closeDrawers(); // clear any drawer/backdrop before returning to the auth screen
    state.userId       = null;
    state.username     = null;
    state.sessionToken = null;
    state.rooms        = [];
    ws.close();
    UI.showAuth();
    setTimeout(_connectWS, 100);
});

// Event: create room 
document.getElementById('create-room-btn').addEventListener('click', () => {
    const modal = document.getElementById('create-room-modal');
    modal.classList.remove('hidden');
    document.getElementById('room-name-input').value = '';
    document.getElementById('room-name-input').focus();
});

document.getElementById('cancel-room-btn').addEventListener('click', () => {
    document.getElementById('create-room-modal').classList.add('hidden');
});

document.getElementById('confirm-room-btn').addEventListener('click', _doCreateRoom);
document.getElementById('room-name-input').addEventListener('keydown', e => {
    if (e.key === 'Enter')  _doCreateRoom();
    if (e.key === 'Escape') document.getElementById('create-room-modal').classList.add('hidden');
});

document.getElementById('create-room-modal').addEventListener('click', e => {
    if (e.target === e.currentTarget) e.target.classList.add('hidden');
});

function _doCreateRoom() {
    const name = document.getElementById('room-name-input').value.trim();
    if (!name) return;
    ws.send(MSG.CREATE_ROOM, { name });
    document.getElementById('create-room-modal').classList.add('hidden');
}

// Event: leave room

document.getElementById('btn-leave').addEventListener('click', () => {
    if (!state.currentRoom) return;
    ws.send(MSG.LEAVE_ROOM, { room_id: state.currentRoom.id });
    _resetRoom();
});

// Event: audio toggle

document.getElementById('btn-audio').addEventListener('click', async () => {
    if (!state.hasAudio) {
        try {
            await webrtc.startAudio();
            state.hasAudio   = true;
            state.audioMuted = false;
            _reflectAudioState();
        } catch (err) {
            UI.toast(err.message, 'error');
        }
    } else {
        state.audioMuted = webrtc.toggleAudioMute();
        _reflectAudioState();
    }
});

// Reflect mic state on the button: live mic vs muted is shown by the icon
// (mic / mic-off), a red "muted" style, and the title — so muting is unmistakable.
function _reflectAudioState() {
    const btn = document.getElementById('btn-audio');
    btn.innerHTML = state.audioMuted ? ICONS.micOff : ICONS.mic;
    btn.title       = state.audioMuted ? 'Unmute microphone' : 'Mute microphone';
    btn.classList.toggle('muted', state.audioMuted);
    UI.setMediaBtn('btn-audio', state.hasAudio && !state.audioMuted);
}

// Event: RNNoise noise-suppression toggle

document.getElementById('btn-rnnoise').addEventListener('click', async () => {
    if (!state.hasAudio) {
        UI.toast('Turn on your microphone first', 'info');
        return;
    }
    const btn = document.getElementById('btn-rnnoise');
    btn.disabled = true;
    try {
        if (webrtc.isRnnoiseActive()) {
            webrtc.disableRnnoise();
            UI.toast('Noise suppression off', 'info');
        } else {
            await webrtc.enableRnnoise();
            UI.toast('Noise suppression on', 'success');
        }
        UI.setMediaBtn('btn-rnnoise', webrtc.isRnnoiseActive());
    } catch (err) {
        UI.toast('Noise suppression failed: ' + err.message, 'error');
    } finally {
        btn.disabled = false;
    }
});

// Event: video toggle

document.getElementById('btn-video').addEventListener('click', async () => {
    if (!state.hasVideo) {
        try {
            const stream = await webrtc.startVideo();
            state.hasVideo = true;
            UI.setMediaBtn('btn-video', true);
            UI.addVideoTile('local', state.username, stream);
        } catch (err) {
            UI.toast(err.message, 'error');
        }
    } else {
        webrtc.stopVideo();
        state.hasVideo = false;
        UI.setMediaBtn('btn-video', false);
        UI.removeVideoTile('local');
    }
});

// Event: screen share toggle

// Tracks whether the microphone was auto-started to accompany a screen share,
// so it can be automatically stopped when the screen share ends.
let _screenAutoStartedAudio = false;

async function _stopScreenShare() {
    webrtc.stopScreenShare();
    state.hasScreen = false;
    UI.setMediaBtn('btn-screen', false);
    UI.removeVideoTile('local-screen');
    if (_screenAutoStartedAudio) {
        webrtc.stopAudio();
        state.hasAudio          = false;
        state.audioMuted        = false;
        _screenAutoStartedAudio = false;
        UI.setMediaBtn('btn-audio', false);
    }
}

document.getElementById('btn-screen').addEventListener('click', async () => {
    if (!state.hasScreen) {
        try {
            const stream = await webrtc.startScreenShare();
            state.hasScreen = true;
            UI.setMediaBtn('btn-screen', true);
            UI.addVideoTile('local-screen', state.username + ' (screen)', stream);

            // Display audio is only available when the user checks "Share audio"
            // in the browser dialog, and some browsers/surfaces never provide it.
            // Fall back to microphone audio so there is always an audio track
            // accompanying the screen share.
            if (!stream.getAudioTracks().length && !state.hasAudio) {
                try {
                    await webrtc.startAudio();
                    state.hasAudio          = true;
                    state.audioMuted        = false;
                    _screenAutoStartedAudio = true;
                    UI.setMediaBtn('btn-audio', true);
                } catch {
                    // Mic unavailable — screen share continues without audio
                }
            }

            // Handle user clicking browser's built-in "Stop Sharing" button
            stream.getVideoTracks()[0].addEventListener('ended', () => {
                _stopScreenShare();
            }, { once: true });
        } catch (err) {
            if (err.name !== 'NotAllowedError') UI.toast(err.message, 'error');
        }
    } else {
        _stopScreenShare();
    }
});

// Event: send text message

document.getElementById('send-btn').addEventListener('click', _sendMessage);
document.getElementById('msg-input').addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); _sendMessage(); }
});

function _sendMessage() {
    const input   = document.getElementById('msg-input');
    const content = input.value.trim();
    if (!content || !state.currentRoom) return;
    ws.send(MSG.TEXT_MESSAGE, { room_id: state.currentRoom.id, content });
    input.value = '';
}

// Event: file send — upload to server via HTTP POST

document.getElementById('file-input').addEventListener('change', async e => {
    const file = e.target.files[0];
    e.target.value = '';
    if (!file || !state.currentRoom) return;

    const url = `/upload?token=${encodeURIComponent(state.sessionToken)}&room_id=${encodeURIComponent(state.currentRoom.id)}`;
    const body = new FormData();
    body.append('file', file);

    UI.toast(`Uploading "${file.name}"…`, 'info');
    try {
        const res = await fetch(url, { method: 'POST', body });
        if (!res.ok) {
            const data = await res.json().catch(() => ({}));
            UI.toast(data.error || 'Upload failed', 'error');
        }
        // On success the server broadcasts file_message to the room,
        // so the file appears in chat automatically — no extra UI call needed.
    } catch (err) {
        UI.toast('Upload error: ' + err.message, 'error');
    }
});

// Mobile drawers — the sidebar (rooms) and users panel slide in over a backdrop
// on narrow screens; on desktop they are always-visible grid columns and these
// helpers are inert (the .open class has no effect outside the mobile query).

const _mql        = window.matchMedia('(max-width: 768px)');
const _sidebarEl  = document.getElementById('sidebar');
const _usersEl    = document.getElementById('users-panel');
const _backdropEl = document.getElementById('drawer-backdrop');

function _isMobile() { return _mql.matches; }

function _closeDrawers() {
    _sidebarEl.classList.remove('open');
    _usersEl.classList.remove('open');
    _backdropEl.classList.add('hidden');
}

function _openDrawer(el) {
    // Only one drawer open at a time.
    (el === _sidebarEl ? _usersEl : _sidebarEl).classList.remove('open');
    el.classList.add('open');
    _backdropEl.classList.remove('hidden');
}

function _toggleDrawer(el) {
    if (el.classList.contains('open')) _closeDrawers();
    else _openDrawer(el);
}

document.getElementById('btn-menu').addEventListener('click', () => _toggleDrawer(_sidebarEl));
document.getElementById('btn-users').addEventListener('click', () => _toggleDrawer(_usersEl));
_backdropEl.addEventListener('click', _closeDrawers);

// If the viewport grows back to desktop, clear any open drawer state.
_mql.addEventListener('change', (e) => { if (!e.matches) _closeDrawers(); });

// Session helpers

function _clearSession() {
    try { localStorage.removeItem(SESSION_KEY); } catch { /* private mode */ }
}

// Bootstrap

async function _connectWS() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url   = `${proto}//${location.host}/ws`;
    try {
        await ws.connect(url);
        // If we hold a persisted session, resume it silently. The server replies
        // with auth_success (→ app) or auth_error (→ token cleared, auth screen).
        let token = null;
        try { token = localStorage.getItem(SESSION_KEY); } catch { /* private mode */ }
        if (token) ws.send(MSG.RESUME, { session_token: token });
    } catch {
        UI.toast('Cannot reach server — retrying in 5s…', 'error');
        setTimeout(_connectWS, 5000);
    }
}

// Paint the SVG icon set, then open the connection.
applyIcons();
_connectWS();
