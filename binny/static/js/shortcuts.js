/** Keyboard shortcuts. They stay out of the way while typing in a field or answering a dialog. */
import { clearSelection, moveSelected, newFolder, renameSelected, selectAll } from './explorer.js';
import { toggleSidebar } from './sidebar.js';
import { pickFiles } from './upload.js';

const KEYS = {
    '[': toggleSidebar,
    u: pickFiles,
    n: newFolder,
    m: moveSelected,
    F2: renameSelected,
    Escape: clearSelection,
};

export function initShortcuts() {
    document.addEventListener('keydown', (event) => {
        if (event.target.closest('dialog') || event.target.matches('input:not([type="checkbox"]), textarea, select')) return;
        if ((event.ctrlKey || event.metaKey) && event.key === 'a') {
            event.preventDefault();
            selectAll();
            return;
        }
        if (event.ctrlKey || event.metaKey || event.altKey) return;
        const action = KEYS[event.key.length === 1 ? event.key.toLowerCase() : event.key];
        if (action) {
            event.preventDefault();
            action();
        }
    });
}
