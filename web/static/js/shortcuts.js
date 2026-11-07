/** Keyboard shortcuts. They stay out of the way while typing in a field or answering a dialog. */
import { uploadFromLink } from './downloads.js';
import { clearSelection, downloadSelected, goUp, moveCursor, moveSelected, newFolder, openSelected, renameSelected, selectAll, trashSelected } from './explorer.js';
import { isTrash } from './paths.js';
import { focusSearch } from './search.js';
import { tagSelected } from './tagger.js';
import { clearTrashSelection, deleteSelectedForever, moveTrashCursor, selectAllTrash } from './trash.js';
import { pickFiles } from './upload.js';

const FILE_KEYS = {
    '/': focusSearch,
    t: tagSelected,
    u: pickFiles,
    l: uploadFromLink,
    n: newFolder,
    m: moveSelected,
    r: renameSelected,
    F2: renameSelected,
    s: downloadSelected, // save
    Delete: trashSelected,
    Backspace: trashSelected, // the delete key on a Mac
    Escape: clearSelection,
};

let lastG = 0; // when g was pressed, so a second g within a second is "gg"

/**
 * Moving through the rows, with the arrow keys or vim's: j and k, gg and G, Home and End; Shift
 * extends the selection. move is the view's moveCursor.
 */
const rowKeys = (move) => ({
    ArrowDown: (event) => move(1, event.shiftKey),
    j: (event) => move(1, event.shiftKey),
    ArrowUp: (event) => move(-1, event.shiftKey),
    k: (event) => move(-1, event.shiftKey),
    Home: (event) => move('first', event.shiftKey),
    End: (event) => move('last', event.shiftKey),
    g: (event) => {
        if (event.key === 'G') return move('last', false);
        const second = Date.now() - lastG < 1000;
        lastG = second ? 0 : Date.now();
        if (second) move('first', false);
    },
});

// Opening and going up: right and Enter open the selected item, left and h go to the folder above.
// (Not l, which is upload from link.)
const FILE_ROW_KEYS = { ...rowKeys(moveCursor), ArrowRight: openSelected, Enter: openSelected, ArrowLeft: goUp, h: goUp };
const TRASH_ROW_KEYS = rowKeys(moveTrashCursor);

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
        const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
        // The row keys work on the page or in the list, not from a menu, the sidebar's resize handle or a focused button's Enter.
        const inList = event.target === document.body || (event.target.closest('#list, #trashList') && !(key === 'Enter' && event.target.closest('a, button')));
        const action = (inList && (trash ? TRASH_ROW_KEYS : FILE_ROW_KEYS)[key]) || (trash ? TRASH_KEYS : FILE_KEYS)[key];
        if (action) {
            event.preventDefault();
            action(event);
        }
    });
}
