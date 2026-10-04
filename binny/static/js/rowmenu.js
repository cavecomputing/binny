/** The "…" menu at the end of a row: a row's actions behind one button, so a long name keeps its room. */
import { $, esc, icon } from './ui.js';

let opener = null; // the "…" button the menu hangs from

/** The button that opens a row's menu. */
export const menuButton = `<button class="icon-btn" type="button" data-act="menu" title="Actions" aria-label="Actions" aria-haspopup="menu" aria-expanded="false">${icon('more')}</button>`;

function closeMenu() {
    $('rowMenu').hidden = true;
    opener?.setAttribute('aria-expanded', 'false');
    opener = null;
}

/**
 * Open the menu under a row's "…" button, or close it if that button's menu is already open.
 * entries are { act, label, iconName, key?, danger? }; choose(act) runs the one picked.
 */
export function openMenu(button, entries, choose) {
    const menu = $('rowMenu');
    if (opener === button) return closeMenu();
    closeMenu();
    menu.innerHTML = entries.map(({ act, label, iconName, key = '', danger = false }) => `
        <button type="button" role="menuitem" data-act="${act}"${danger ? ' class="danger"' : ''}>${icon(iconName)}<span>${esc(label)}</span>${key && `<span class="kbd">${key}</span>`}</button>`).join('');
    menu.hidden = false;
    menu.onclick = (event) => {
        const act = event.target.closest('[data-act]')?.dataset.act;
        if (!act) return;
        closeMenu();
        choose(act);
    };
    opener = button;
    button.setAttribute('aria-expanded', 'true');
    // Under the button and against its right edge, or above it when the window ends first.
    const box = button.getBoundingClientRect();
    const { width, height } = menu.getBoundingClientRect();
    menu.style.left = `${Math.max(8, Math.min(box.right - width, innerWidth - width - 8))}px`;
    menu.style.top = `${box.bottom + height + 8 > innerHeight ? Math.max(8, box.top - height - 4) : box.bottom + 4}px`;
    menu.firstElementChild.focus();
}

export function initRowMenu() {
    const menu = $('rowMenu');
    menu.addEventListener('keydown', (event) => {
        const items = [...menu.children];
        const at = items.indexOf(document.activeElement);
        const move = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: items.length - 1 }[event.key];
        if (move !== undefined) {
            event.preventDefault();
            items[(move + items.length) % items.length].focus();
        } else if (event.key === 'Escape' || event.key === 'Tab') {
            event.preventDefault();
            event.stopPropagation(); // not the page's Escape, which clears the selection
            const button = opener;
            closeMenu();
            button.focus();
        }
    });
    document.addEventListener('click', (event) => !event.target.closest('#rowMenu, [data-act=menu]') && closeMenu());
    document.addEventListener('scroll', closeMenu, true);
    window.addEventListener('resize', closeMenu);
    window.addEventListener('files-changed', closeMenu);
    window.addEventListener('hashchange', closeMenu);
}
