/** Keyboard shortcuts. They stay out of the way while typing in a field or answering a dialog. */
import { uploadFromLink } from './downloads.js';
import { clearSelection, moveSelected, newFolder, renameSelected, selectAll, trashSelected } from './explorer.js';
import { isTrash } from './paths.js';
import { focusSearch } from './search.js';
import { tagSelected } from './tagger.js';
import { clearTrashSelection, deleteSelectedForever, selectAllTrash } from './trash.js';
import { pickFiles } from './upload.js';

const FILE_KEYS = {
    '/': focusSearch,
    t: tagSelected,
    u: pickFiles,
    l: uploadFromLink,
    n: newFolder,
    m: moveSelected,
    F2: renameSelected,
    Delete: trashSelected,
    Backspace: trashSelected, // the delete key on a Mac
    Escape: clearSelection,
};

const TRASH_KEYS = {
    '/': focusSearch,
    Delete: deleteSelectedForever,
    Backspace: deleteSelectedForever,
    Escape: clearTrashSelection,
};

export function initShortcuts() {
    document.addEventListener('keydown', (event) => {
        if (event.target.closest('dialog') || event.target.matches('input:not([type="checkbox"]), textarea, select')) return;
        const trash = isTrash();
        if ((event.ctrlKey || event.metaKey) && event.key === 'a') {
            event.preventDefault();
            (trash ? selectAllTrash : selectAll)();
            return;
        }
        if (event.ctrlKey || event.metaKey || event.altKey) return;
        const action = (trash ? TRASH_KEYS : FILE_KEYS)[event.key.length === 1 ? event.key.toLowerCase() : event.key];
        if (action) {
            event.preventDefault();
            action();
        }
    });
}
