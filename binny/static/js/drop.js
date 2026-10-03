/**
 * Drag and drop. Files from the computer upload; rows dragged within Binny move. A folder row, a
 * sidebar folder or a breadcrumb takes the drop into that folder; files dropped anywhere else go into
 * the folder on screen.
 */
import { moveItems } from './move.js';
import { state } from './state.js';
import { $ } from './ui.js';
import { uploadDropped } from './upload.js';

const ROWS = 'application/x-binny-paths';
let target = null; // the [data-drop] element under the pointer
let endTimer;

function setTarget(element) {
    target?.classList.remove('drop-target');
    target = element;
    target?.classList.add('drop-target');
}

function end() {
    setTarget(null);
    $('dropHint').hidden = true;
}

export function initDrop() {
    document.addEventListener('dragstart', (event) => {
        const path = event.target.closest?.('tr.row')?.dataset.path;
        if (path === undefined) return;
        const paths = state.selected.has(path) ? [...state.selected] : [path];
        event.dataTransfer.setData(ROWS, JSON.stringify(paths));
        event.dataTransfer.effectAllowed = 'move';
    });

    document.addEventListener('dragover', (event) => {
        const rows = event.dataTransfer.types.includes(ROWS);
        if (!rows && !event.dataTransfer.types.includes('Files')) return;
        event.preventDefault();
        setTarget(event.target.closest?.('[data-drop]') ?? null);
        event.dataTransfer.dropEffect = rows ? (target ? 'move' : 'none') : 'copy';
        if (!rows) {
            $('dropHintText').textContent = `Drop to upload into /${target ? target.dataset.drop : state.folder}`;
            $('dropHint').hidden = false;
        }
        // dragover repeats while the drag is over the page; when it stops, the drag left or ended.
        clearTimeout(endTimer);
        endTimer = setTimeout(end, 200);
    });

    document.addEventListener('drop', (event) => {
        const to = target ? target.dataset.drop : state.folder;
        const onFolder = target !== null;
        end();
        if (event.dataTransfer.types.includes(ROWS)) {
            event.preventDefault();
            const paths = JSON.parse(event.dataTransfer.getData(ROWS));
            if (onFolder && !paths.includes(to)) moveItems(paths, to);
        } else if (event.dataTransfer.types.includes('Files')) {
            event.preventDefault();
            uploadDropped(event.dataTransfer, to);
        }
    });

    document.addEventListener('dragend', end);
}
