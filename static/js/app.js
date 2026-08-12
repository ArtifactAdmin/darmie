/**
 * Browser composition root. It owns application/session state and coordinates
 * server events; media and uploads are isolated in their own controllers.
 */

import { MSG } from './protocol.js?v=10';
import { WSManager } from './ws.js?v=10';
import { WebRTCManager } from './webrtc.js?v=11';
import { FileTransfer } from './filetransfer.js?v=11';
import { UI } from './ui.js?v=10';
import { applyIcons } from './icons.js?v=10';
import { MediaController } from './media-controller.js?v=12';
import { UploadController } from './upload-controller.js?v=12';

const SESSION_KEY = 'darmie_session';

const state = {
    userId: null,
    username: null,
    sessionToken: null,
    rooms: [],
    currentRoom: null,
    roomUsers: [],
};

const ws = new WSManager();
const webrtc = new WebRTCManager(ws);
const fileTransfer = new FileTransfer(webrtc);
const media = new MediaController({
    webrtc,
    ui: UI,
    getUsername: () => state.username,
});
const uploads = new UploadController({
    ui: UI,
    getUploadTarget: () => state.currentRoom && state.sessionToken
        ? { token: state.sessionToken, roomId: state.currentRoom.id }
        : null,
});

webrtc.onRemoteStream = (userId, stream) => {
    const user = state.roomUsers.find((roomUser) => roomUser.id === userId);
    UI.addVideoTile(userId, user?.username ?? userId, stream);
};

webrtc.onRemoteStreamRemoved = (userId) => UI.removeVideoTile(userId);

webrtc.onConnectionState = (userId, connectionState) => {
    if (connectionState === 'connected') {
        const user = state.roomUsers.find((roomUser) => roomUser.id === userId);
        if (user) UI.toast(`Connected to ${user.username}`, 'success');
    }
};

fileTransfer.onFileReceived = (fromUserId, name, url, size, mimeType) => {
    const user = state.roomUsers.find((roomUser) => roomUser.id === fromUserId);
    UI.addFileMessage({
        fromUsername: user?.username ?? fromUserId,
        filename: name,
        url,
        size,
        mimeType,
    });
};

fileTransfer.onProgress = (_userId, fraction) => {
    if (fraction >= 1) UI.toast('File sent ✓', 'success');
};

ws.on(MSG.AUTH_SUCCESS, (payload) => {
    state.userId = payload.user_id;
    state.username = payload.username;
    state.sessionToken = payload.session_token;
    _storeSession(payload.session_token);
    webrtc.init(payload.user_id);
    UI.showApp(payload.username);
    ws.send(MSG.LIST_ROOMS);
    if (_isMobile()) _openDrawer(_sidebar);
});

ws.on(MSG.AUTH_ERROR, (payload) => {
    _clearSession();
    if (state.userId) {
        _resetRoom();
        state.userId = null;
        state.username = null;
        state.rooms = [];
        UI.showAuth();
    }
    UI.setAuthError(payload.message);
});

ws.on(MSG.ROOM_LIST, (payload) => {
    state.rooms = payload.rooms;
    _renderRooms();
});

ws.on(MSG.ROOM_CREATED, (payload) => {
    state.rooms.push(payload.room);
    _renderRooms();
    _joinRoom(payload.room.id);
});

ws.on(MSG.ROOM_JOINED, (payload) => {
    webrtc.closeAll();
    media.reset();
    uploads.reset();
    UI.clearVideoGrid();

    state.currentRoom = { id: payload.room.id, name: payload.room.name };
    state.roomUsers = [...payload.users];
    const localUser = { id: state.userId, username: state.username };
    _setRoomUserCount(payload.room.id, payload.room.user_count);

    UI.showRoom(payload.room.name);
    UI.updateUserList([localUser, ...payload.users]);
    _renderRooms();
    _renderHistory(payload.history);
    if (_isMobile()) _closeDrawers();
});

ws.on(MSG.ROOM_LEFT, () => {
    _resetRoom();
    if (_isMobile()) _openDrawer(_sidebar);
});

ws.on(MSG.USER_JOINED, (payload) => {
    const { user } = payload;
    if (!state.roomUsers.find((roomUser) => roomUser.id === user.id)) {
        state.roomUsers.push(user);
    }
    UI.addUser(user);
    UI.toast(`${user.username} joined`, 'info');
    webrtc.initiatePeer(user.id);
    _changeRoomUserCount(payload.room_id, 1);
});

ws.on(MSG.USER_LEFT, (payload) => {
    state.roomUsers = state.roomUsers.filter((user) => user.id !== payload.user_id);
    UI.removeUser(payload.user_id);
    webrtc.closePeer(payload.user_id);
    _changeRoomUserCount(payload.room_id, -1);
});

ws.on(MSG.TEXT_MESSAGE, (payload) => {
    UI.addMessage({
        fromUsername: payload.from_username,
        content: payload.content,
        timestamp: payload.timestamp,
        isSelf: payload.from_user_id === state.userId,
    });
});

ws.on(MSG.FILE_MESSAGE, (payload) => {
    UI.addFileMessage({
        fromUsername: payload.from_username,
        filename: payload.filename,
        url: payload.url,
        size: payload.size,
        mimeType: payload.mime_type,
        isSelf: payload.from_user_id === state.userId,
    });
});

ws.on(MSG.OFFER, (payload) => webrtc.handleOffer(payload.from_user_id, payload.sdp).catch(console.error));
ws.on(MSG.ANSWER, (payload) => webrtc.handleAnswer(payload.from_user_id, payload.sdp).catch(console.error));
ws.on(MSG.ICE_CANDIDATE, (payload) => webrtc.handleICECandidate(payload.from_user_id, payload.candidate).catch(console.error));
ws.on(MSG.VIDEO_STOPPED, (payload) => UI.removeVideoTile(payload.user_id));
ws.on(MSG.ERROR, (payload) => UI.toast(payload.message, 'error'));
ws.on(MSG.DISCONNECTED, () => {
    if (state.userId) {
        UI.toast('Connection lost — reconnecting…', 'error');
        setTimeout(_connectWebSocket, 3000);
    }
});

function _joinRoom(roomId) {
    if (roomId !== state.currentRoom?.id) {
        ws.send(MSG.JOIN_ROOM, { room_id: roomId });
    }
}

function _resetRoom() {
    webrtc.closeAll();
    media.reset();
    uploads.reset();
    state.currentRoom = null;
    state.roomUsers = [];
    UI.hideRoom();
    _renderRooms();
}

function _renderRooms() {
    UI.updateRoomList(state.rooms, state.currentRoom?.id, _joinRoom);
}

function _setRoomUserCount(roomId, userCount) {
    const room = state.rooms.find((entry) => entry.id === roomId);
    if (room) room.user_count = userCount;
}

function _changeRoomUserCount(roomId, change) {
    const room = state.rooms.find((entry) => entry.id === roomId);
    if (!room) return;
    room.user_count = Math.max(0, room.user_count + change);
    _renderRooms();
}

function _renderHistory(history = []) {
    for (const entry of history) {
        if (entry.kind === 'file') {
            UI.addFileMessage({
                fromUsername: entry.from_username,
                filename: entry.filename,
                url: entry.url,
                size: entry.size,
                mimeType: entry.mime_type,
                isSelf: entry.from_user_id === state.userId,
            });
            continue;
        }
        UI.addMessage({
            fromUsername: entry.from_username,
            content: entry.content,
            timestamp: entry.timestamp,
            isSelf: entry.from_user_id === state.userId,
        });
    }
}

document.querySelectorAll('.auth-tab').forEach((tab) => {
    tab.addEventListener('click', () => {
        document.querySelectorAll('.auth-tab').forEach((item) => item.classList.remove('active'));
        tab.classList.add('active');
        document.querySelectorAll('.auth-form').forEach((form) => form.classList.add('hidden'));
        document.getElementById(tab.dataset.form).classList.remove('hidden');
        UI.clearAuthError();
    });
});

document.getElementById('login-btn').addEventListener('click', _login);
document.getElementById('login-password').addEventListener('keydown', (event) => {
    if (event.key === 'Enter') _login();
});

function _login() {
    const username = document.getElementById('login-username').value.trim();
    const password = document.getElementById('login-password').value;
    if (!username || !password) return;
    UI.clearAuthError();
    ws.send(MSG.LOGIN, { username, password });
}

document.getElementById('register-btn').addEventListener('click', _register);
document.getElementById('register-password').addEventListener('keydown', (event) => {
    if (event.key === 'Enter') _register();
});

function _register() {
    const username = document.getElementById('register-username').value.trim();
    const password = document.getElementById('register-password').value;
    if (!username || !password) return;
    UI.clearAuthError();
    ws.send(MSG.REGISTER, { username, password });
}

document.getElementById('logout-btn').addEventListener('click', () => {
    ws.send(MSG.LOGOUT);
    _clearSession();
    _resetRoom();
    _closeDrawers();
    state.userId = null;
    state.username = null;
    state.sessionToken = null;
    state.rooms = [];
    ws.close();
    UI.showAuth();
    setTimeout(_connectWebSocket, 100);
});

const createRoomModal = document.getElementById('create-room-modal');
const roomNameInput = document.getElementById('room-name-input');

document.getElementById('create-room-btn').addEventListener('click', () => {
    createRoomModal.classList.remove('hidden');
    roomNameInput.value = '';
    roomNameInput.focus();
});
document.getElementById('cancel-room-btn').addEventListener('click', () => createRoomModal.classList.add('hidden'));
document.getElementById('confirm-room-btn').addEventListener('click', _createRoom);
roomNameInput.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') _createRoom();
    if (event.key === 'Escape') createRoomModal.classList.add('hidden');
});
createRoomModal.addEventListener('click', (event) => {
    if (event.target === event.currentTarget) createRoomModal.classList.add('hidden');
});

function _createRoom() {
    const name = roomNameInput.value.trim();
    if (!name) return;
    ws.send(MSG.CREATE_ROOM, { name });
    createRoomModal.classList.add('hidden');
}

document.getElementById('btn-leave').addEventListener('click', () => {
    if (!state.currentRoom) return;
    ws.send(MSG.LEAVE_ROOM, { room_id: state.currentRoom.id });
    _resetRoom();
});

document.getElementById('send-btn').addEventListener('click', _sendMessage);
document.getElementById('msg-input').addEventListener('keydown', (event) => {
    if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        _sendMessage();
    }
});

function _sendMessage() {
    const input = document.getElementById('msg-input');
    const content = input.value.trim();
    if (!content || !state.currentRoom) return;
    ws.send(MSG.TEXT_MESSAGE, { room_id: state.currentRoom.id, content });
    input.value = '';
}

const mediaQuery = window.matchMedia('(max-width: 768px)');
const _sidebar = document.getElementById('sidebar');
const _usersPanel = document.getElementById('users-panel');
const _backdrop = document.getElementById('drawer-backdrop');

function _isMobile() {
    return mediaQuery.matches;
}

function _closeDrawers() {
    _sidebar.classList.remove('open');
    _usersPanel.classList.remove('open');
    _backdrop.classList.add('hidden');
}

function _openDrawer(drawer) {
    (drawer === _sidebar ? _usersPanel : _sidebar).classList.remove('open');
    drawer.classList.add('open');
    _backdrop.classList.remove('hidden');
}

function _toggleDrawer(drawer) {
    if (drawer.classList.contains('open')) _closeDrawers();
    else _openDrawer(drawer);
}

document.getElementById('btn-menu').addEventListener('click', () => _toggleDrawer(_sidebar));
document.getElementById('btn-users').addEventListener('click', () => _toggleDrawer(_usersPanel));
_backdrop.addEventListener('click', _closeDrawers);
mediaQuery.addEventListener('change', (event) => {
    if (!event.matches) _closeDrawers();
});

function _storeSession(token) {
    try { localStorage.setItem(SESSION_KEY, token); } catch { /* private mode */ }
}

function _clearSession() {
    try { localStorage.removeItem(SESSION_KEY); } catch { /* private mode */ }
}

async function _connectWebSocket() {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${location.host}/ws`;
    try {
        await ws.connect(url);
        let token = null;
        try { token = localStorage.getItem(SESSION_KEY); } catch { /* private mode */ }
        if (token) ws.send(MSG.RESUME, { session_token: token });
    } catch {
        UI.toast('Cannot reach server — retrying in 5s…', 'error');
        setTimeout(_connectWebSocket, 5000);
    }
}

applyIcons();
media.bind();
uploads.bind();
_connectWebSocket();
