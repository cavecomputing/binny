/** The main pane: breadcrumb, summary, selection bar and the table of the folder on screen. */
import * as api from './api.js';
import { openMove } from './move.js';
import { ancestors, byName, currentFolder, fileUrl, folderHash, nameOf, parentOf } from './paths.js';
import { filesChanged, state } from './state.js';
import { $, ask, esc, formatDate, formatSize, icon, plural } from './ui.js';

const SORTS = { name: 'Name', size: 'Size', mtime: 'Modified' };
let sort = savedSort();
let anchor = null; // the row a shift-click selects from
let latest = 0;    // only the newest listing gets drawn

function savedSort() {
    try {
        const saved = JSON.parse(localStorage.getItem('binny-sort'));
        if (saved?.key in SORTS) return saved;
    } catch { /* storage blocked or junk */ }
    return { key: 'name', desc: false };
}

/** Load the folder the address bar names, and draw it. */
export async function load() {
    const folder = currentFolder();
    const request = ++latest;
    let listing;
    try {
        listing = await api.get('/api/list', { folder });
    } catch (error) {
        if (request === latest && error.status === 404 && folder) location.hash = folderHash(parentOf(folder));
        return;
    }
    if (request !== latest) return;
    if (folder !== state.folder) {
        state.selected.clear();
        anchor = null;
    }
    state.folder = folder;
    state.items = listing.items;
    const present = new Set(listing.items.map((item) => item.path));
    for (const path of state.selected) if (!present.has(path)) state.selected.delete(path);
    render();
}

/** Folders first, then the chosen column. */
function sorted(items) {
    const direction = sort.desc ? -1 : 1;
    return [...items].sort((a, b) => (b.is_dir - a.is_dir)
        || direction * ((sort.key === 'name' ? 0 : a[sort.key] - b[sort.key]) || byName(a, b)));
}

/** ["photo", ".jpg"], keeping double extensions like ".tar.gz" whole. Folders have no extension. */
function splitName(item) {
    const match = !item.is_dir && item.name.match(/^(.+?)(\.(?:tar\.(?:gz|bz2|xz|zst)|[^.]+))$/i);
    return match ? [match[1], match[2]] : [item.name, ''];
}

const itemAt = (path) => state.items.find((item) => item.path === path);

function sizeLabel(item) {
    if (!item.is_dir) return formatSize(item.size);
    return item.items ? `<span class="sub">${plural(item.items, 'item')} · </span>${formatSize(item.size)}` : '<span class="sub">empty</span>';
}

function row(item) {
    const [stem, ext] = splitName(item);
    const link = item.is_dir ? `href="${esc(folderHash(item.path))}"` : `href="${esc(fileUrl(item.path))}" target="_blank" rel="noopener"`;
    const date = formatDate(item.mtime);
    return `<tr class="row${item.is_dir ? ' dir' : ''}" data-path="${esc(item.path)}" draggable="true"${item.is_dir ? ` data-drop="${esc(item.path)}"` : ''}>
        <td class="chk"><input type="checkbox" aria-label="Select ${esc(item.name)}"></td>
        <td><div class="name">
            <span class="ficon ${item.kind}">${icon(item.kind)}</span>
            <div class="name-text"><a class="fname" ${link}>${esc(stem)}<span class="ext">${esc(ext)}</span></a><div class="meta">${sizeLabel(item)} · ${date}</div></div>
        </div></td>
        <td class="num r col-size">${sizeLabel(item)}</td>
        <td class="num col-mod" title="${esc(new Date(item.mtime * 1000).toLocaleString())}">${date}</td>
        <td><div class="acts">
            <button class="icon-btn opt" type="button" data-act="rename" title="Rename (F2)" aria-label="Rename">${icon('rename')}</button>
            <button class="icon-btn opt" type="button" data-act="move" title="Move (M)" aria-label="Move">${icon('move')}</button>
            <a class="icon-btn" href="${esc(fileUrl(item.path, true))}" download title="Download${item.is_dir ? ' as zip' : ''}" aria-label="Download">${icon('download')}</a>
        </div></td>
    </tr>`;
}

function heading(key, className = '') {
    const current = sort.key === key;
    return `<th class="${className}"${current ? ` aria-sort="${sort.desc ? 'descending' : 'ascending'}"` : ''}>
        <button class="sort" type="button" data-sort="${key}">${SORTS[key]}${current ? icon('chevron-down') : ''}</button></th>`;
}

function render() {
    const crumb = (path) => `<a href="${esc(folderHash(path))}" data-drop="${esc(path)}">${esc(path ? nameOf(path) : 'files')}</a>`;
    $('crumbs').innerHTML = ['', ...ancestors(state.folder)].map(crumb).join('<span>/</span>');

    const items = sorted(state.items);
    $('summary').innerHTML = `<b>${plural(items.length, 'item')}</b> · ${formatSize(items.reduce((sum, item) => sum + item.size, 0))}`;
    $('list').innerHTML = items.length
        ? `<table class="files">
            <thead><tr>
                <th class="chk"><input type="checkbox" id="selAll" aria-label="Select all"></th>
                ${heading('name')}${heading('size', 'r col-size')}${heading('mtime', 'col-mod')}<th></th>
            </tr></thead>
            <tbody>${items.map(row).join('')}</tbody>
        </table>`
        : `<div class="empty">${icon('folder')}<p>This folder is empty.</p><p>Drop files here, or upload them with <b>+</b>.</p></div>`;
    renderSelection();
}

/** Mark the selected rows and fit the selection bar to them, without redrawing the table. */
function renderSelection() {
    const count = state.selected.size;
    for (const tr of $('list').querySelectorAll('tr.row')) {
        const selected = state.selected.has(tr.dataset.path);
        tr.classList.toggle('sel', selected);
        tr.querySelector('input').checked = selected;
    }
    const all = $('selAll');
    if (all) {
        all.checked = count > 0 && count === state.items.length;
        all.indeterminate = count > 0 && count < state.items.length;
    }
    $('selbar').hidden = !count;
    $('selCount').textContent = `${count.toLocaleString()} selected`;
    $('selRename').hidden = count !== 1;
    const single = count === 1 && itemAt([...state.selected][0]);
    $('selDownloadLabel').textContent = single && !single.is_dir ? 'Download' : 'Download zip';
}

/** A click selects just that row; Ctrl/Cmd-click or its checkbox toggles it; Shift-click adds a range. */
function clickRow(path, event) {
    const paths = sorted(state.items).map((item) => item.path);
    if (event.shiftKey && paths.includes(anchor)) {
        const [from, to] = [paths.indexOf(anchor), paths.indexOf(path)].sort((a, b) => a - b);
        for (const each of paths.slice(from, to + 1)) state.selected.add(each);
    } else if (event.ctrlKey || event.metaKey || event.target.type === 'checkbox') {
        if (!state.selected.delete(path)) state.selected.add(path);
        anchor = path;
    } else {
        const onlyThis = state.selected.size === 1 && state.selected.has(path);
        state.selected.clear();
        if (!onlyThis) state.selected.add(path);
        anchor = path;
    }
    renderSelection();
}

function setSort(key) {
    sort = { key, desc: sort.key === key && !sort.desc };
    try { localStorage.setItem('binny-sort', JSON.stringify(sort)); } catch { /* lasts this visit */ }
    render();
}

function download(paths) {
    if (paths.length === 1) { // a file, or one folder as a zip
        const link = Object.assign(document.createElement('a'), { href: fileUrl(paths[0], true), download: '' });
        document.body.append(link);
        link.click();
        link.remove();
        return;
    }
    // A form post, so the browser saves the zip as it streams in.
    const form = Object.assign(document.createElement('form'), { method: 'post', action: '/zip' });
    for (const path of paths) form.append(Object.assign(document.createElement('input'), { type: 'hidden', name: 'paths', value: path }));
    document.body.append(form);
    form.submit();
    form.remove();
}

async function rename(item) {
    const answer = await ask({
        title: `Rename ${item.is_dir ? 'folder' : 'file'}`,
        iconName: 'rename',
        value: item.name,
        select: splitName(item)[0].length,
        ok: 'Rename',
        action: async (name) => {
            const { path } = await api.post('/api/rename', { path: item.path, name });
            if (state.selected.delete(item.path)) state.selected.add(path);
        },
    });
    if (answer !== null) filesChanged();
}

export async function newFolder() {
    const answer = await ask({
        title: 'New folder',
        iconName: 'folder-plus',
        value: 'New folder',
        ok: 'Create',
        action: (name) => api.post('/api/folders', { parent: state.folder, name }),
    });
    if (answer !== null) filesChanged();
}

export function renameSelected() {
    if (state.selected.size === 1) rename(itemAt([...state.selected][0]));
}

export const moveSelected = () => openMove([...state.selected]);

export function selectAll() {
    for (const item of state.items) state.selected.add(item.path);
    renderSelection();
}

export function clearSelection() {
    state.selected.clear();
    renderSelection();
}

export function initExplorer() {
    window.addEventListener('hashchange', load);
    window.addEventListener('files-changed', load);
    // Back in the tab: show what changed meanwhile, here or from outside the app.
    document.addEventListener('visibilitychange', () => document.hidden || filesChanged());

    const list = $('list');
    list.addEventListener('mousedown', (event) => event.shiftKey && event.preventDefault()); // no text selection on shift-click
    list.addEventListener('click', (event) => {
        const sortButton = event.target.closest('[data-sort]');
        if (sortButton) return setSort(sortButton.dataset.sort);
        const tr = event.target.closest('tr.row');
        if (!tr) {
            if (!event.target.closest('thead')) clearSelection(); // a click on empty space
            return;
        }
        const action = event.target.closest('[data-act]')?.dataset.act;
        if (action === 'rename') return rename(itemAt(tr.dataset.path));
        if (action === 'move') return openMove([tr.dataset.path]);
        if (!event.target.closest('a')) clickRow(tr.dataset.path, event); // links open or download by themselves
    });
    list.addEventListener('dblclick', (event) => {
        const item = itemAt(event.target.closest('tr.row')?.dataset.path);
        if (!item || event.target.closest('a, button, input')) return;
        if (item.is_dir) location.hash = folderHash(item.path);
        else window.open(fileUrl(item.path), '_blank', 'noopener');
    });
    list.addEventListener('change', (event) => {
        if (event.target.id === 'selAll') event.target.checked ? selectAll() : clearSelection();
    });

    $('newFolderBtn').addEventListener('click', newFolder);
    $('selRename').addEventListener('click', renameSelected);
    $('selMove').addEventListener('click', moveSelected);
    $('selDownload').addEventListener('click', () => download([...state.selected]));
    $('selClear').addEventListener('click', clearSelection);
    load();
}
