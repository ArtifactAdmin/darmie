/**
 * icons.js — Inline SVG icon set (Mumble-inspired clean line style).
 *
 * Each value is a trusted, static `<svg>` string drawn on a 24×24 grid with
 * `stroke: currentColor`, so an icon inherits the colour of its button and is
 * sized purely by CSS (`button svg { width; height }`). Because the strings are
 * static and developer-authored, assigning them via innerHTML carries no XSS
 * risk (no user data is ever interpolated).
 */

const svg = (paths) =>
    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" ` +
    `stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

export const ICONS = Object.freeze({
    mic: svg(`
        <rect x="9" y="3" width="6" height="11" rx="3"/>
        <path d="M5 11a7 7 0 0 0 14 0"/>
        <line x1="12" y1="18" x2="12" y2="21"/>
        <line x1="8.5" y1="21" x2="15.5" y2="21"/>`),

    micOff: svg(`
        <rect x="9" y="3" width="6" height="11" rx="3"/>
        <path d="M5 11a7 7 0 0 0 14 0"/>
        <line x1="12" y1="18" x2="12" y2="21"/>
        <line x1="8.5" y1="21" x2="15.5" y2="21"/>
        <line x1="3" y1="3" x2="21" y2="21"/>`),

    camera: svg(`
        <rect x="3" y="6" width="13" height="12" rx="2"/>
        <path d="M16 10l5-3v10l-5-3z"/>`),

    screen: svg(`
        <rect x="3" y="4" width="18" height="12" rx="2"/>
        <line x1="9" y1="20" x2="15" y2="20"/>
        <line x1="12" y1="16" x2="12" y2="20"/>`),

    leave: svg(`
        <path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4"/>
        <polyline points="9 16 4 12 9 8"/>
        <line x1="4" y1="12" x2="15" y2="12"/>`),

    paperclip: svg(`
        <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/>`),

    power: svg(`
        <path d="M18.36 6.64a9 9 0 1 1-12.73 0"/>
        <line x1="12" y1="2" x2="12" y2="12"/>`),

    plus: svg(`
        <line x1="12" y1="5" x2="12" y2="19"/>
        <line x1="5" y1="12" x2="19" y2="12"/>`),

    fullscreen: svg(`
        <path d="M8 3H5a2 2 0 0 0-2 2v3"/>
        <path d="M21 8V5a2 2 0 0 0-2-2h-3"/>
        <path d="M3 16v3a2 2 0 0 0 2 2h3"/>
        <path d="M16 21h3a2 2 0 0 0 2-2v-3"/>`),

    minimize: svg(`
        <path d="M8 3v3a2 2 0 0 1-2 2H3"/>
        <path d="M21 8h-3a2 2 0 0 1-2-2V3"/>
        <path d="M3 16h3a2 2 0 0 1 2 2v3"/>
        <path d="M16 21v-3a2 2 0 0 1 2-2h3"/>`),

    menu: svg(`
        <line x1="3" y1="6" x2="21" y2="6"/>
        <line x1="3" y1="12" x2="21" y2="12"/>
        <line x1="3" y1="18" x2="21" y2="18"/>`),

    users: svg(`
        <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
        <circle cx="9" cy="7" r="4"/>
        <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
        <path d="M16 3.13a4 4 0 0 1 0 7.75"/>`),
});

/**
 * Replace the content of every element with a `data-icon` attribute with the
 * matching SVG. Call once after the DOM is ready.
 */
export function applyIcons(root = document) {
    for (const el of root.querySelectorAll('[data-icon]')) {
        const icon = ICONS[el.dataset.icon];
        if (icon) el.innerHTML = icon;
    }
}
