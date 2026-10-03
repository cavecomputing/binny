/**
 * The sidebar: the folder tree, open along the folder on screen (other branches open by their arrow),
 * and the disk usage. On phones it is a drawer.
 */
import * as api from './api.js';
import { ancestors, byName, currentFolder, currentSearch, folderHash, isTrash } from './paths.js';
import { $, esc, formatSize, icon } from './ui.js';

const expanded = new Set(); // branches opened by their arrow
const closed = new Set();   // branches along the folder on screen, closed by their arrow
let shownFolder = null;
let latest = 0; // only the newest tree gets drawn

export async function loadTree() {
    const folder = currentFolder();
    if (folder !== shownFolder) { // a new folder opens its whole branch again
        closed.clear();
        shownFolder = folder;
    }
    const open = new Set([...expanded, ...ancestors(folder).filter((path) => !closed.has(path))]);
    const request = ++latest;
    let data;
    try {
        data = await api.get('/api/folders', [...open].map((path) => ['open', path]));
    } catch {
        return; // keep the tree as it was
    }
    if (request !== latest) return;

    const rows = [treeRow({ path: '', name: 'files' }, 0, folder, open)];
    const walk = (parent, depth) => {
        for (const sub of (data.tree[parent] ?? []).sort(byName)) {
            rows.push(treeRow(sub, depth, folder, open));
            if (sub.has_children && open.has(sub.path)) walk(sub.path, depth + 1);
        }
    };
    walk('', 1);
    $('tree').innerHTML = rows.join('');
    $('trashCount').textContent = data.trash_items || '';
    if (isTrash()) $('trashNav').setAttribute('aria-current', 'page');
    else $('trashNav').removeAttribute('aria-current');

    const { used, total } = data.disk;
    const percent = total ? Math.round((used / total) * 100) : 0;
    $('storage').innerHTML = `
        <div>${formatSize(used)} of ${formatSize(total)} used</div>
        <div class="meter${percent >= 90 ? ' meter--full' : ''}"><i style="width: ${percent}%"></i></div>
        <div>binny holds <b>${formatSize(data.files_size)}</b></div>`;
}

function treeRow(sub, depth, folder, open) {
    const isOpen = open.has(sub.path);
    const twisty = sub.has_children
        ? `<button class="twisty" type="button" data-toggle="${esc(sub.path)}" aria-expanded="${isOpen}" aria-label="${isOpen ? 'Collapse' : 'Expand'} ${esc(sub.name)}">${icon('chevron-right')}</button>`
        : '<span class="twisty"></span>';
    const current = sub.path === folder && !isTrash() && currentSearch() === null;
    return `<div class="tree-row" style="--depth: ${depth}">${twisty}<a class="cc-shell__row" href="${esc(folderHash(sub.path))}" data-drop="${esc(sub.path)}"${current ? ' aria-current="page"' : ''}>${icon('folder')}<span>${esc(sub.name)}</span></a></div>`;
}

function toggleBranch(path, wasOpen) {
    const onPath = ancestors(currentFolder()).includes(path);
    if (wasOpen) {
        expanded.delete(path);
        if (onPath) closed.add(path);
    } else {
        expanded.add(path);
        closed.delete(path);
    }
    loadTree();
}

function setDrawer(open) {
    $('side').classList.toggle('open', open);
    $('scrim').hidden = !open;
}

export function initSidebar() {
    $('sideBtn').addEventListener('click', () => setDrawer(!$('side').classList.contains('open')));
    $('scrim').addEventListener('click', () => setDrawer(false));
    // A link closes the drawer even when it leads where the page already is, which changes no hash.
    $('side').addEventListener('click', (event) => event.target.closest('a[href], [data-all-tags]') && setDrawer(false));
    $('tree').addEventListener('click', (event) => {
        const twisty = event.target.closest('[data-toggle]');
        if (twisty) toggleBranch(twisty.dataset.toggle, twisty.getAttribute('aria-expanded') === 'true');
    });
    window.addEventListener('hashchange', () => {
        setDrawer(false);
        loadTree();
    });
    window.addEventListener('files-changed', loadTree);
    loadTree();
}
