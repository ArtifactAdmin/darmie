/**
 * Owns local media state and the controls that reflect it. It delegates all
 * peer-connection work to WebRTCManager.
 */

import { ICONS } from './icons.js?v=10';

export class MediaController {
    constructor({ webrtc, ui, getUsername }) {
        this._webrtc = webrtc;
        this._ui = ui;
        this._getUsername = getUsername;
        this._hasAudio = false;
        this._audioMuted = false;
        this._hasVideo = false;
        this._hasScreen = false;
        this._screenAutoStartedAudio = false;
    }

    bind() {
        document.getElementById('btn-audio').addEventListener('click', () => this._toggleAudio());
        document.getElementById('btn-rnnoise').addEventListener('click', () => this._toggleNoiseSuppression());
        document.getElementById('btn-video').addEventListener('click', () => this._toggleVideo());
        document.getElementById('btn-screen').addEventListener('click', () => this._toggleScreenShare());
    }

    /** Reset visual state after the caller has stopped the WebRTC streams. */
    reset() {
        this._hasAudio = false;
        this._audioMuted = false;
        this._hasVideo = false;
        this._hasScreen = false;
        this._screenAutoStartedAudio = false;

        const audioButton = document.getElementById('btn-audio');
        audioButton.innerHTML = ICONS.mic;
        audioButton.title = 'Start microphone';
        audioButton.classList.remove('muted');
        this._ui.setMediaBtn('btn-audio', false);
        this._ui.setMediaBtn('btn-rnnoise', false);
        this._ui.setMediaBtn('btn-video', false);
        this._ui.setMediaBtn('btn-screen', false);
    }

    async _toggleAudio() {
        if (!this._hasAudio) {
            try {
                await this._webrtc.startAudio();
                this._hasAudio = true;
                this._audioMuted = false;
                this._reflectAudioState();
            } catch (err) {
                this._ui.toast(err.message, 'error');
            }
            return;
        }

        this._audioMuted = this._webrtc.toggleAudioMute();
        this._reflectAudioState();
    }

    _reflectAudioState() {
        const button = document.getElementById('btn-audio');
        button.innerHTML = this._audioMuted ? ICONS.micOff : ICONS.mic;
        button.title = this._audioMuted ? 'Unmute microphone' : 'Mute microphone';
        button.classList.toggle('muted', this._audioMuted);
        this._ui.setMediaBtn('btn-audio', this._hasAudio && !this._audioMuted);
    }

    async _toggleNoiseSuppression() {
        if (!this._hasAudio) {
            this._ui.toast('Turn on your microphone first', 'info');
            return;
        }

        const button = document.getElementById('btn-rnnoise');
        button.disabled = true;
        try {
            if (this._webrtc.isRnnoiseActive()) {
                this._webrtc.disableRnnoise();
                this._ui.toast('Noise suppression off', 'info');
            } else {
                await this._webrtc.enableRnnoise();
                this._ui.toast('Noise suppression on', 'success');
            }
            this._ui.setMediaBtn('btn-rnnoise', this._webrtc.isRnnoiseActive());
        } catch (err) {
            this._ui.toast('Noise suppression failed: ' + err.message, 'error');
        } finally {
            button.disabled = false;
        }
    }

    async _toggleVideo() {
        if (!this._hasVideo) {
            try {
                const stream = await this._webrtc.startVideo();
                this._hasVideo = true;
                this._ui.setMediaBtn('btn-video', true);
                this._ui.addVideoTile('local', this._getUsername(), stream);
            } catch (err) {
                this._ui.toast(err.message, 'error');
            }
            return;
        }

        this._webrtc.stopVideo();
        this._hasVideo = false;
        this._ui.setMediaBtn('btn-video', false);
        this._ui.removeVideoTile('local');
    }

    async _toggleScreenShare() {
        if (this._hasScreen) {
            this._stopScreenShare();
            return;
        }

        try {
            const stream = await this._webrtc.startScreenShare();
            this._hasScreen = true;
            this._ui.setMediaBtn('btn-screen', true);
            this._ui.addVideoTile('local-screen', this._getUsername() + ' (screen)', stream);

            if (!stream.getAudioTracks().length && !this._hasAudio) {
                await this._startFallbackAudio();
            }

            stream.getVideoTracks()[0].addEventListener('ended', () => {
                this._stopScreenShare();
            }, { once: true });
        } catch (err) {
            if (err.name !== 'NotAllowedError') this._ui.toast(err.message, 'error');
        }
    }

    async _startFallbackAudio() {
        try {
            await this._webrtc.startAudio();
            this._hasAudio = true;
            this._audioMuted = false;
            this._screenAutoStartedAudio = true;
            this._reflectAudioState();
        } catch {
            // Screen sharing without audio remains useful when mic permission is unavailable.
        }
    }

    _stopScreenShare() {
        this._webrtc.stopScreenShare();
        this._hasScreen = false;
        this._ui.setMediaBtn('btn-screen', false);
        this._ui.removeVideoTile('local-screen');

        if (this._screenAutoStartedAudio) {
            this._webrtc.stopAudio();
            this._hasAudio = false;
            this._audioMuted = false;
            this._screenAutoStartedAudio = false;
            this._reflectAudioState();
        }
    }
}
