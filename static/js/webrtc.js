/**
 * webrtc.js — WebRTC peer connection manager using the Perfect Negotiation
 * pattern (RFC 8829 §4.3). Polite/impolite roles are assigned by comparing
 * local and remote user-ID strings (lower = polite).
 */

const ICE_SERVERS = [
    { urls: 'stun:stun.l.google.com:19302'  },
    { urls: 'stun:stun1.l.google.com:19302' },
];

export class WebRTCManager {
    /**
     * @param {import('./ws.js').WSManager} ws
     */
    constructor(ws) {
        this._ws          = ws;
        this._localUserId = null;

        /** @type {Record<string, {pc: RTCPeerConnection, makingOffer: boolean, ignoreOffer: boolean, senders: RTCRtpSender[]}>} */
        this._peers    = {};
        /** @type {Record<string, RTCIceCandidateInit[]>} */
        this._pending  = {};

        this._localStream  = null; // camera/mic MediaStream
        this._screenStream = null; // getDisplayMedia stream

        // Callbacks set by app.js
        /** @type {((userId: string, stream: MediaStream) => void) | null} */
        this.onRemoteStream        = null;
        /** @type {((userId: string) => void) | null} */
        this.onRemoteStreamRemoved = null;
        /** @type {((channel: RTCDataChannel, userId: string) => void) | null} */
        this.onDataChannel         = null;
        /** @type {((userId: string, state: string) => void) | null} */
        this.onConnectionState     = null;
    }

    /** Call once after authentication. */
    init(userId) {
        this._localUserId = userId;
    }

    // 
    // Peer lifecycle
    // 

    /**
     * Called by an existing room member when a new user joins.
     * Creates a peer connection and triggers negotiation via a control DataChannel.
     */
    async initiatePeer(userId) {
        if (this._peers[userId]) return;
        const peer = this._createPeer(userId);
        // Creating a DataChannel triggers onnegotiationneeded on the initiator side
        // even when no media tracks have been added.
        peer.pc.createDataChannel('control');
    }

    closePeer(userId) {
        const peer = this._peers[userId];
        if (!peer) return;
        peer.pc.close();
        delete this._peers[userId];
        delete this._pending[userId];
        if (this.onRemoteStreamRemoved) this.onRemoteStreamRemoved(userId);
    }

    closeAll() {
        for (const userId of Object.keys(this._peers)) this.closePeer(userId);
        this._stopStreamTracks(this._localStream);
        this._localStream = null;
        this._stopStreamTracks(this._screenStream);
        this._screenStream = null;
    }

    // 
    // Perfect Negotiation — incoming signaling
    // 

    async handleOffer(fromUserId, sdp) {
        let peer = this._peers[fromUserId];
        if (!peer) peer = this._createPeer(fromUserId);

        const polite = this._localUserId < fromUserId;
        const collision = sdp.type === 'offer' &&
            (peer.makingOffer || peer.pc.signalingState !== 'stable');

        peer.ignoreOffer = !polite && collision;
        if (peer.ignoreOffer) return;

        await peer.pc.setRemoteDescription(sdp);
        await this._flushPending(fromUserId);

        if (sdp.type === 'offer') {
            await peer.pc.setLocalDescription();
            this._ws.send('answer', {
                target_user_id: fromUserId,
                sdp: peer.pc.localDescription,
            });
        }
    }

    async handleAnswer(fromUserId, sdp) {
        const peer = this._peers[fromUserId];
        if (!peer || peer.ignoreOffer) return;
        await peer.pc.setRemoteDescription(sdp);
        await this._flushPending(fromUserId);
    }

    async handleICECandidate(fromUserId, candidateInit) {
        const peer = this._peers[fromUserId];
        if (!peer || peer.pc.remoteDescription === null) {
            // No peer yet, or remote description not set (answer not yet received).
            // Buffer the candidate; handleOffer / handleAnswer will flush it.
            (this._pending[fromUserId] ??= []).push(candidateInit);
            return;
        }
        try {
            await peer.pc.addIceCandidate(candidateInit);
        } catch {
            // Silently discard — happens legitimately with ignored offers
        }
    }

    // 
    // Media — audio
    // 

    async startAudio() {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        for (const track of stream.getAudioTracks()) {
            this._ensureLocalStream().addTrack(track);
            for (const peer of Object.values(this._peers)) {
                const sender = peer.pc.addTrack(track, this._localStream);
                peer.senders.push(sender);
            }
        }
        return stream;
    }

    toggleAudioMute() {
        if (!this._localStream) return false;
        const tracks = this._localStream.getAudioTracks();
        if (!tracks.length) return false;
        const mute = tracks[0].enabled; // will become muted
        tracks.forEach(t => { t.enabled = !mute; });
        return mute; // returns true if now muted
    }

    stopAudio() {
        if (!this._localStream) return;
        for (const track of this._localStream.getAudioTracks()) {
            track.stop();
            this._localStream.removeTrack(track);
            this._removeSenderTrack(track);
        }
    }

    // 
    // Media — video (camera)
    // 

    async startVideo() {
        const stream = await navigator.mediaDevices.getUserMedia({ video: true });
        for (const track of stream.getVideoTracks()) {
            this._ensureLocalStream().addTrack(track);
            for (const peer of Object.values(this._peers)) {
                const sender = peer.pc.addTrack(track, this._localStream);
                peer.senders.push(sender);
            }
        }
        return this._localStream;
    }

    stopVideo() {
        if (!this._localStream) return;
        for (const track of this._localStream.getVideoTracks()) {
            track.stop();
            this._localStream.removeTrack(track);
            this._removeSenderTrack(track);
        }
        // Notify peers to remove our video tile if no other video source remains.
        if (!this._hasActiveVideo()) {
            this._ws.send('video_stopped', {});
        }
    }

    // 
    // Media — screen share
    // 

    async startScreenShare() {
        this._screenStream = await navigator.mediaDevices.getDisplayMedia({ video: true });
        for (const track of this._screenStream.getVideoTracks()) {
            for (const peer of Object.values(this._peers)) {
                const sender = peer.pc.addTrack(track, this._screenStream);
                peer.senders.push(sender);
            }
        }
        return this._screenStream;
    }

    stopScreenShare() {
        if (!this._screenStream) return;
        for (const track of this._screenStream.getTracks()) {
            track.stop();
            this._removeSenderTrack(track);
        }
        this._screenStream = null;
        // Notify peers to remove our video tile if no other video source remains.
        if (!this._hasActiveVideo()) {
            this._ws.send('video_stopped', {});
        }
    }

    // 
    // Internal helpers
    // 

    _createPeer(userId) {
        const pc = new RTCPeerConnection({ iceServers: ICE_SERVERS });
        const peer = { pc, makingOffer: false, ignoreOffer: false, senders: [] };
        this._peers[userId] = peer;

        pc.onicecandidate = ({ candidate }) => {
            if (candidate) {
                this._ws.send('ice_candidate', {
                    target_user_id: userId,
                    candidate: candidate.toJSON(),
                });
            }
        };

        pc.ontrack = ({ track, streams }) => {
            if (this.onRemoteStream && streams[0]) {
                this.onRemoteStream(userId, streams[0]);
            }
        };

        pc.ondatachannel = ({ channel }) => {
            if (this.onDataChannel) this.onDataChannel(channel, userId);
        };

        pc.onnegotiationneeded = async () => {
            try {
                peer.makingOffer = true;
                await pc.setLocalDescription();
                this._ws.send('offer', {
                    target_user_id: userId,
                    sdp: pc.localDescription,
                });
            } catch (err) {
                console.error('onnegotiationneeded error:', err);
            } finally {
                peer.makingOffer = false;
            }
        };

        pc.onconnectionstatechange = () => {
            if (this.onConnectionState) this.onConnectionState(userId, pc.connectionState);
            if (pc.connectionState === 'failed') this.closePeer(userId);
        };

        // Add any active local tracks to the new peer connection
        if (this._localStream) {
            for (const track of this._localStream.getTracks()) {
                const sender = pc.addTrack(track, this._localStream);
                peer.senders.push(sender);
            }
        }
        if (this._screenStream) {
            for (const track of this._screenStream.getTracks()) {
                const sender = pc.addTrack(track, this._screenStream);
                peer.senders.push(sender);
            }
        }

        // Do NOT flush _pending here: there is no remote description yet, so
        // addIceCandidate would fail. handleOffer / handleAnswer flush after
        // setRemoteDescription, which is the correct time.

        return peer;
    }

    async _flushPending(userId) {
        const peer = this._peers[userId];
        if (!peer) return;
        const candidates = this._pending[userId] ?? [];
        this._pending[userId] = [];
        for (const c of candidates) {
            try { await peer.pc.addIceCandidate(c); } catch { /* ignored */ }
        }
    }

    _ensureLocalStream() {
        if (!this._localStream) this._localStream = new MediaStream();
        return this._localStream;
    }

    /** Returns true if any local video track (camera or screen) is still live. */
    _hasActiveVideo() {
        const camLive    = this._localStream?.getVideoTracks().some(t => t.readyState === 'live') ?? false;
        const screenLive = this._screenStream?.getVideoTracks().some(t => t.readyState === 'live') ?? false;
        return camLive || screenLive;
    }

    _removeSenderTrack(track) {
        for (const peer of Object.values(this._peers)) {
            const sender = peer.senders.find(s => s.track === track);
            if (sender) {
                peer.pc.removeTrack(sender);
                peer.senders = peer.senders.filter(s => s !== sender);
            }
        }
    }

    _stopStreamTracks(stream) {
        if (stream) stream.getTracks().forEach(t => t.stop());
    }
}
