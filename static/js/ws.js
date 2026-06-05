/**
 * ws.js — WebSocket connection manager.
 * Dispatches server messages to registered handlers.
 */

export class WSManager {
    constructor() {
        this._ws       = null;
        this._handlers = {};
    }

    /** Open a WebSocket connection. Returns a Promise that resolves on open. */
    connect(url) {
        return new Promise((resolve, reject) => {
            this._ws = new WebSocket(url);
            let opened = false;
            this._ws.onopen    = () => { opened = true; resolve(); };
            // Only reject from onerror when the connection never opened.
            // If it was open and then dropped, onclose handles reconnection.
            this._ws.onerror   = (e) => { if (!opened) reject(e); };
            this._ws.onmessage = (e) => this._dispatch(e.data);
            this._ws.onclose   = () => {
                // Only fire the close handler when the socket was previously
                // open. A never-opened socket's close event is already handled
                // by the onerror → reject path above, so firing here too would
                // schedule two concurrent reconnect attempts.
                if (!opened) return;
                const h = this._handlers['__close__'];
                if (h) h();
            };
        });
    }

    /** Register a handler for a message type. Chainable. */
    on(type, handler) {
        this._handlers[type] = handler;
        return this;
    }

    /** Send a typed message to the server. */
    send(type, payload = {}) {
        if (this._ws && this._ws.readyState === WebSocket.OPEN) {
            this._ws.send(JSON.stringify({ type, payload }));
        }
    }

    close() {
        if (this._ws) this._ws.close();
    }

    _dispatch(raw) {
        let msg;
        try { msg = JSON.parse(raw); } catch { return; }
        const h = this._handlers[msg.type];
        if (h) h(msg.payload);
    }
}
