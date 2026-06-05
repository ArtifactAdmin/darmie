/**
 * filetransfer.js — Peer-to-peer file transfer over WebRTC DataChannels.
 *
 * Protocol (per DataChannel, label starts with "file:"):
 *   1. Initiator sends JSON: { type:"file_meta", name, size, mimeType }
 *   2. Initiator sends N ArrayBuffer chunks of ≤ CHUNK_SIZE bytes.
 *   3. Initiator sends JSON: { type:"file_end" }
 *
 * Backpressure is enforced via bufferedAmount / bufferedAmountLowThreshold.
 */

const CHUNK_SIZE = 16_384;   // 16 KB
const MAX_BUFFER = 262_144;  // 256 KB — pause when buffered amount exceeds this
const FILE_CHANNEL_PREFIX = 'file:';

export class FileTransfer {
    /**
     * @param {import('./webrtc.js').WebRTCManager} webrtc
     */
    constructor(webrtc) {
        this._webrtc = webrtc;

        /** Fired when a complete file has been received. */
        this.onFileReceived = null; // (fromUserId, name, url, size) => void
        /** Fired with progress updates while sending. */
        this.onProgress = null;     // (toUserId, fraction: 0–1) => void

        // Wire up incoming data channel handler
        webrtc.onDataChannel = (channel, fromUserId) => {
            if (channel.label.startsWith(FILE_CHANNEL_PREFIX)) {
                this._receiveFile(channel, fromUserId);
            }
            // Non-file channels (e.g. "control") are intentionally ignored here.
        };
    }

    /**
     * Send a File to a specific peer.
     * @param {string}  userId
     * @param {File}    file
     */
    sendFile(userId, file) {
        const peer = this._webrtc._peers[userId];
        if (!peer) {
            console.warn('FileTransfer: no peer connection to', userId);
            return;
        }

        const label   = FILE_CHANNEL_PREFIX + crypto.randomUUID();
        const channel = peer.pc.createDataChannel(label, {
            ordered:         true,
            maxRetransmits:  30,
        });

        channel.binaryType = 'arraybuffer';
        channel.onopen  = () => this._sendFile(channel, file, userId);
        channel.onerror = (err) => {
            console.error('File send error:', err);
            channel.close();
        };
    }

    // 
    // Internal sender side
    // 

    async _sendFile(channel, file, userId) {
        channel.send(JSON.stringify({
            type:     'file_meta',
            name:     file.name,
            size:     file.size,
            mimeType: file.type || 'application/octet-stream',
        }));

        let offset = 0;
        while (offset < file.size) {
            if (channel.readyState !== 'open') break;

            // Backpressure: pause when the send buffer is too full
            if (channel.bufferedAmount > MAX_BUFFER) {
                await new Promise(resolve => {
                    channel.bufferedAmountLowThreshold = CHUNK_SIZE * 4;
                    channel.onbufferedamountlow = resolve;
                });
            }

            const chunk  = file.slice(offset, offset + CHUNK_SIZE);
            const buffer = await chunk.arrayBuffer();
            channel.send(buffer);
            offset += CHUNK_SIZE;

            if (this.onProgress) {
                this.onProgress(userId, Math.min(offset / file.size, 1));
            }
        }

        if (channel.readyState === 'open') {
            channel.send(JSON.stringify({ type: 'file_end' }));
        }
        // Delay close to ensure the last message is flushed
        setTimeout(() => channel.close(), 600);
    }

    // 
    // Internal — receiver side
    // 

    _receiveFile(channel, fromUserId) {
        channel.binaryType = 'arraybuffer';

        let meta   = null;
        const chunks = [];

        channel.onmessage = (event) => {
            if (typeof event.data === 'string') {
                let msg;
                try { msg = JSON.parse(event.data); } catch { return; }

                if (msg.type === 'file_meta') {
                    meta = msg;
                    chunks.length = 0;
                } else if (msg.type === 'file_end' && meta) {
                    const blob = new Blob(chunks, { type: meta.mimeType });
                    const url  = URL.createObjectURL(blob);
                    if (this.onFileReceived) {
                        this.onFileReceived(fromUserId, meta.name, url, meta.size);
                    }
                    meta = null;
                    chunks.length = 0;
                }
            } else {
                // ArrayBuffer chunk
                chunks.push(event.data);
            }
        };

        channel.onerror = (err) => console.error('File receive error:', err);
    }
}
