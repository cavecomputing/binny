/** Moving files and folders: the "Move to…" dialog, and the move itself, which drag-and-drop uses too. */
import * as api from './api.js';
import { ancestors, byName, nameOf, parentOf } from './paths.js';
import { filesChanged, state } from './state.js';
import { $, esc, icon, plural, toast } from './ui.js';

let moving = []; // paths of the items in the dialog
let browsing = ''; // the folder the dialog shows

/** Move paths into the folder to. Resolves to whether it worked; api.js shows any error. */
export async function moveItems(paths, to) {
    try {
        const { moved, renamed } = await api.post('/api/move', { paths, to });
        for (const path of paths) state.selected.delete(path);
        if (moved) {
            const what = paths.length === 1 ? `"${esc(nameOf(paths[0]))}"` : plural(moved, 'item');
            toast(`Moved <b>${what}</b> to /${esc(to)}${renamed ? ` · ${renamed} renamed to keep both` : ''}`);
        }
        filesChanged();
        return true;
    } catch {
        return false;
    }
}

export function openMove(paths) {
    if (!paths.length) return;
    moving = paths;
    $('moveTitle').textContent = paths.length === 1 ? `Move "${nameOf(paths[0])}"` : `Move ${plural(paths.length, 'item')}`;
    $('moveDialog').showModal();
    browse(state.folder);
}

/** Show folder's subfolders in the dialog. A folder being moved can't be entered: nothing goes inside itself. */
async function browse(folder) {
    browsing = folder;
    const crumb = (path) => `<button type="button" data-browse="${esc(path)}">${esc(path ? nameOf(path) : 'files')}</button>`;
    $('moveCrumbs').innerHTML = ['', ...ancestors(folder)].map(crumb).join('<span>/</span>');
    $('moveHere').disabled = moving.every((path) => parentOf(path) === folder);
    let tree;
    try {
        ({ tree } = await api.get('/api/folders', [['open', folder]]));
    } catch {
        return; // api.js showed why
    }
    if (folder !== browsing) return;
    const inside = (sub) => moving.some((path) => sub.path === path || sub.path.startsWith(`${path}/`));
    $('movePicker').innerHTML = tree[folder].sort(byName).map((sub) => `
        <button type="button" data-browse="${esc(sub.path)}"${inside(sub) ? ' disabled title="It\'s being moved"' : ''}>
            ${icon('folder')}<span>${esc(sub.name)}</span>${sub.has_children ? icon('chevron-right') : ''}
        </button>`).join('') || '<p class="empty">No folders in here</p>';
}

export function initMove() {
    const dialog = $('moveDialog');
    dialog.addEventListener('click', (event) => {
        const target = event.target.closest('[data-browse]');
        if (target) browse(target.dataset.browse);
    });
    dialog.querySelector('form').addEventListener('submit', async (event) => {
        event.preventDefault();
        $('moveHere').disabled = true;
        if (await moveItems(moving, browsing)) dialog.close();
        $('moveHere').disabled = false;
    });
}
