/**
 * webrtc.js — WebRTC peer connection manager using the Perfect Negotiation
 * pattern (RFC 8829 §4.3). Polite/impolite roles are assigned by comparing
 * local and remote user-ID strings (lower = polite).
 */

import { loadRnnoise, RnnoiseWorkletNode } from '../vendor/noise-suppressor/index.js?v=7';

const ICE_SERVERS = [
    { urls: 'stun:stun.l.google.com:19302'  },
    { urls: 'stun:stun1.l.google.com:19302' },
];

const NS_DIR        = '/vendor/noise-suppressor';
const RNNOISE_FRAME = 48000; // RNNoise runs at 48 kHz

// getUserMedia audio constraints — echo cancellation + AGC always on; the
// browser's own noise suppression is left on as a baseline (RNNoise, when the
// user toggles it, is an additional pass layered on top).
const AUDIO_CONSTRAINTS = {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl:  true,
};

/**
 * Fetch a worklet script and hand back a blob URL with an explicit JS MIME
 * type. AudioWorklet.addModule rejects ("Unable to load a worklet's module")
 * if the server returns the script with a non-JS content type or via a proxy/
 * SPA fallback; loading from a same-origin blob sidesteps that.
 */
async function _workletURL(src) {
    const res = await fetch(src);
    if (!res.ok) throw new Error(`worklet fetch ${res.status} for ${src}`);
    const code = await res.text();
    return URL.createObjectURL(new Blob([code], { type: 'text/javascript' }));
}

export class WebRTCManager {
    /**
     * @param {import('./ws.js').WSManager} ws
     */
    constructor(ws) {
        this._ws          = ws;
        this._localUserId = null;

        /** @type {Record<string, {pc: RTCPeerConnection, makingOffer: boolean, ignoreOffer: boolean, senders: RTCRtpSender[], remoteStream: MediaStream}>} */
        this._peers    = {};
        /** @type {Record<string, RTCIceCandidateInit[]>} */
        this._pending  = {};

        this._localStream  = null; // camera/mic MediaStream
        this._screenStream = null; // getDisplayMedia stream

        this._micTrack = null; // raw microphone track (mute target + RNNoise input)
        this._rnnoise  = null; // { ctx, node, source, dest, track } when active

        // Callbacks set by app.js
        /** @type {((userId: string, stream: MediaStream) => void) | null} */
        this.onRemoteStream        = null;
        /** @type {((userId: string) => void) | null} */
        this.onRemoteStreamRemoved = null;
        /** @type {Set<(channel: RTCDataChannel, userId: string) => void>} */
        this._dataChannelHandlers = new Set();
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
        this.disableRnnoise();
        this._stopStreamTracks(this._localStream);
        this._localStream = null;
        this._micTrack = null;
        this._stopStreamTracks(this._screenStream);
        this._screenStream = null;
    }

    /**
     * Subscribe to incoming DataChannels. Returns an unsubscribe function so
     * independent features can share this extension point.
     */
    addDataChannelHandler(handler) {
        this._dataChannelHandlers.add(handler);
        return () => this._dataChannelHandlers.delete(handler);
    }

    /** Create a DataChannel for a connected peer without exposing its PC. */
    createDataChannel(userId, label, options) {
        const peer = this._peers[userId];
        return peer ? peer.pc.createDataChannel(label, options) : null;
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
        if (!peer) return;
        // Do NOT gate on peer.ignoreOffer here. ignoreOffer reflects the most
        // recent *offer* we chose to drop during glare; gating answers on it
        // discarded the answer to our own offer, so a track added under glare
        // (e.g. turning the camera on while the peer renegotiates) never
        // reached the other side. An answer is only valid in have-local-offer;
        // in any other state it is stale, so skip it instead of throwing.
        if (peer.pc.signalingState !== 'have-local-offer') return;
        try {
            await peer.pc.setRemoteDescription(sdp);
            await this._flushPending(fromUserId);
        } catch (err) {
            console.warn('handleAnswer:', err);
        }
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
        const stream = await navigator.mediaDevices.getUserMedia({ audio: AUDIO_CONSTRAINTS });
        for (const track of stream.getAudioTracks()) {
            this._micTrack = track;
            this._ensureLocalStream().addTrack(track);
            for (const peer of Object.values(this._peers)) {
                const sender = peer.pc.addTrack(track, this._localStream);
                peer.senders.push(sender);
            }
        }
        return stream;
    }

    toggleAudioMute() {
        // Mute the raw mic track. When RNNoise is active the mic feeds the
        // worklet graph, so silencing it silences the processed output too.
        if (!this._micTrack) return false;
        const mute = this._micTrack.enabled; // will become muted
        this._micTrack.enabled = !mute;
        return mute; // returns true if now muted
    }

    stopAudio() {
        this.disableRnnoise();
        if (!this._localStream) return;
        for (const track of this._localStream.getAudioTracks()) {
            track.stop();
            this._localStream.removeTrack(track);
            this._removeSenderTrack(track);
        }
        this._micTrack = null;
    }

    //
    // Media — RNNoise noise suppression (per-user, toggleable)
    //

    isRnnoiseActive() {
        return this._rnnoise !== null;
    }

    /** The audio track currently being sent: RNNoise output if active, else raw mic. */
    _outgoingAudioTrack() {
        return this._rnnoise ? this._rnnoise.track : this._micTrack;
    }

    /**
     * Route the mic through the RNNoise worklet and swap the processed track
     * onto every peer (via replaceTrack — no renegotiation needed).
     */
    async enableRnnoise() {
        if (this._rnnoise || !this._micTrack) return;

        const ctx = new AudioContext({ sampleRate: RNNOISE_FRAME });
        await ctx.audioWorklet.addModule(await _workletURL(`${NS_DIR}/rnnoise-worklet.js?v=7`));
        const wasmBinary = await loadRnnoise({
            url:     `${NS_DIR}/rnnoise.wasm`,
            simdUrl: `${NS_DIR}/rnnoise_simd.wasm`,
        });

        const source = ctx.createMediaStreamSource(new MediaStream([this._micTrack]));
        const node   = new RnnoiseWorkletNode(ctx, { maxChannels: 1, wasmBinary });
        const dest   = ctx.createMediaStreamDestination();
        source.connect(node).connect(dest);

        const track = dest.stream.getAudioTracks()[0];
        this._rnnoise = { ctx, node, source, dest, track };

        for (const sender of this._audioSenders()) await sender.replaceTrack(track);
    }

    /** Stop RNNoise and restore the raw mic track on every peer. */
    disableRnnoise() {
        if (!this._rnnoise) return;
        const { ctx, node, track } = this._rnnoise;
        for (const sender of this._audioSenders(track)) sender.replaceTrack(this._micTrack);
        this._rnnoise = null;
        node.destroy?.();
        ctx.close().catch(() => {});
    }

    /** Audio senders across all peers (optionally only those carrying `track`). */
    _audioSenders(track) {
        const out = [];
        for (const peer of Object.values(this._peers)) {
            for (const s of peer.pc.getSenders()) {
                if (s.track && s.track.kind === 'audio' && (!track || s.track === track)) out.push(s);
            }
        }
        return out;
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
        this._screenStream = await navigator.mediaDevices.getDisplayMedia({
            video: true,
            audio: true,
            systemAudio: 'include',
        });
        for (const track of this._screenStream.getTracks()) {
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
        const peer = { pc, makingOffer: false, ignoreOffer: false, senders: [], remoteStream: new MediaStream() };
        this._peers[userId] = peer;

        pc.onicecandidate = ({ candidate }) => {
            if (candidate) {
                this._ws.send('ice_candidate', {
                    target_user_id: userId,
                    candidate: candidate.toJSON(),
                });
            }
        };

        // Combine all incoming tracks into one MediaStream so that mic audio and
        // screen-share video/audio are always played together in the same tile.
        pc.ontrack = ({ track }) => {
            peer.remoteStream.addTrack(track);
            track.onended = () => peer.remoteStream.removeTrack(track);
            if (this.onRemoteStream) {
                this.onRemoteStream(userId, peer.remoteStream);
            }
        };

        pc.ondatachannel = ({ channel }) => {
            for (const handler of this._dataChannelHandlers) handler(channel, userId);
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

        // Add any active local tracks to the new peer connection. When RNNoise
        // is active, send its processed track in place of the raw mic.
        if (this._localStream) {
            for (const track of this._localStream.getTracks()) {
                const outgoing = (track === this._micTrack) ? this._outgoingAudioTrack() : track;
                const sender = pc.addTrack(outgoing, this._localStream);
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
