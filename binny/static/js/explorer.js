/**
 * The main pane: breadcrumb, summary, selection bar and the table of the folder on screen, or of a
 * search's results from anywhere.
 */
import * as api from './api.js';
import { openMove } from './move.js';
import { ancestors, byName, currentFolder, currentSearch, fileUrl, folderHash, isTrash, nameOf, parentOf, searchHash } from './paths.js';
import { menuButton, openMenu } from './rowmenu.js';
import { clickSelect, keySelect, markRows, revealRow } from './selection.js';
import { filesChanged, state } from './state.js';
import { openTagger } from './tagger.js';
import { formatQuery, parseQuery } from './tags.js';
import { trashItems } from './trash.js';
import { $, ask, esc, formatDate, formatSize, icon, plural } from './ui.js';

const SORTS = { name: 'Name', size: 'Size', mtime: 'Modified' };
let sort = savedSort();
let anchor = null; // the row a shift-click or shift-arrow selects from
let cursor = null; // the row the keyboard moves from: the last one clicked or moved to
let latest = 0;    // only the newest listing gets drawn

function savedSort() {
    try {
        const saved = JSON.parse(localStorage.getItem('binny-sort'));
        if (saved?.key in SORTS) return saved;
    } catch { /* storage blocked or junk */ }
    return { key: 'name', desc: false };
}

/** The /api/search parameters for a search. */
function searchParams(query) {
    const { tags, not, name } = parseQuery(query);
    return [...tags.map((tag) => ['tag', tag]), ...not.map((tag) => ['not', tag]), ...(name ? [['name', name]] : [])];
}

/**
 * Load what the address bar names, a folder or a search, and draw it. A search keeps state.folder,
 * so uploads and new folders still go where the search started. The trash has a view of its own.
 */
export async function load() {
    $('filesView').hidden = isTrash();
    if (isTrash()) return;
    const search = currentSearch();
    const folder = search === null ? currentFolder() : state.folder;
    const request = ++latest;
    let listing;
    try {
        listing = search === null ? await api.get('/api/list', { folder }) : await api.get('/api/search', searchParams(search));
    } catch (error) {
        if (request === latest && error.status === 404 && search === null && folder) location.hash = folderHash(parentOf(folder));
        return;
    }
    if (request !== latest) return;
    if (folder !== state.folder || search !== state.search) {
        state.selected.clear();
        anchor = cursor = null;
    }
    state.folder = folder;
    state.search = search;
    state.items = listing.items;
    state.truncated = Boolean(listing.truncated);
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

/** What a row's "…" menu does, by the action each entry names. */
const ROW_ACTIONS = {
    tags: (item) => openTagger([item.path]),
    rename,
    move: (item) => openMove([item.path]),
    download: (item) => download([item.path]),
    trash: (item) => trashItems([item.path]),
};

const rowEntries = (item) => [
    { act: 'tags', label: 'Tags', iconName: 'tag', key: 'T' },
    { act: 'rename', label: 'Rename', iconName: 'rename', key: 'F2' },
    { act: 'move', label: 'Move to…', iconName: 'move', key: 'M' },
    { act: 'download', label: item.is_dir ? 'Download zip' : 'Download', iconName: 'download' },
    { act: 'trash', label: 'Move to trash', iconName: 'trash', key: 'Del', danger: true },
];

function sizeLabel(item) {
    if (!item.is_dir) return formatSize(item.size);
    return item.items ? `<span class="sub">${plural(item.items, 'item')} · </span>${formatSize(item.size)}` : '<span class="sub">empty</span>';
}

/** An item's tags as chips, each a search for it. */
const tagChips = (item, className) => (item.tags.length
    ? `<div class="tags ${className}">${item.tags.map((tag) => `<a class="cc-tag" href="${esc(searchHash(tag))}">${esc(tag)}</a>`).join('')}</div>`
    : '');

function row(item) {
    const [stem, ext] = splitName(item);
    const link = item.is_dir ? `href="${esc(folderHash(item.path))}"` : `href="${esc(fileUrl(item.path))}" target="_blank" rel="noopener"`;
    const date = formatDate(item.mtime);
    const where = state.search === null ? '' : `<a class="path" href="${esc(folderHash(parentOf(item.path)))}" title="Open the folder it's in">/${esc(parentOf(item.path))}</a>`;
    return `<tr class="row${item.is_dir ? ' dir' : ''}" data-path="${esc(item.path)}" draggable="true"${item.is_dir ? ` data-drop="${esc(item.path)}"` : ''}>
        <td class="chk"><input type="checkbox" aria-label="Select ${esc(item.name)}"></td>
        <td><div class="name">
            <span class="ficon ${item.kind}">${icon(item.kind)}</span>
            <div class="name-text"><a class="fname" ${link}>${esc(stem)}<span class="ext">${esc(ext)}</span></a>${where}<div class="meta">${sizeLabel(item)} · ${date}</div>${tagChips(item, 'meta-tags')}</div>
        </div></td>
        <td class="col-tags">${tagChips(item, '')}</td>
        <td class="num r col-size">${sizeLabel(item)}</td>
        <td class="num col-mod" title="${esc(new Date(item.mtime * 1000).toLocaleString())}">${date}</td>
        <td><div class="acts">${menuButton}</div></td>
    </tr>`;
}

function heading(key, className = '') {
    const current = sort.key === key;
    return `<th class="${className}"${current ? ` aria-sort="${sort.desc ? 'descending' : 'ascending'}"` : ''}>
        <button class="sort" type="button" data-sort="${key}">${SORTS[key]}${current ? icon('chevron-down') : ''}</button></th>`;
}

/** The breadcrumb of the folder on screen; every crumb takes drops. */
function folderCrumbs() {
    const crumb = (path) => `<a href="${esc(folderHash(path))}" data-drop="${esc(path)}">${esc(path ? nameOf(path) : 'files')}</a>`;
    return ['', ...ancestors(state.folder)].map(crumb).join('<span>/</span>');
}

/** In place of the breadcrumb during a search: its terms, each one a link to the search without it. */
function searchCrumbs() {
    const terms = parseQuery(state.search);
    const without = (change) => {
        const query = formatQuery(change({ ...terms }));
        return esc(query ? searchHash(query) : folderHash(state.folder));
    };
    const term = (label, text, href) => `<a class="cc-tag" href="${href}" title="Take this out of the search">${label ? `<b>${label}</b> ` : ''}${esc(text)}${icon('x')}</a>`;
    return [
        '<b class="crumbs__label">search</b>',
        ...terms.tags.map((tag) => term('', tag, without((t) => ({ ...t, tags: t.tags.filter((x) => x !== tag) })))),
        ...terms.not.map((tag) => term('not', tag, without((t) => ({ ...t, not: t.not.filter((x) => x !== tag) })))),
        ...(terms.name ? [term('name', terms.name, without((t) => ({ ...t, name: '' })))] : []),
        `<a class="crumbs__clear" href="${esc(folderHash(state.folder))}">clear</a>`,
    ].join('');
}

function render() {
    const searching = state.search !== null;
    $('crumbs').classList.toggle('crumbs--search', searching);
    $('crumbs').innerHTML = searching ? searchCrumbs() : folderCrumbs();
    $('newFolderBtn').hidden = searching;

    const items = sorted(state.items);
    const size = formatSize(items.reduce((sum, item) => sum + item.size, 0));
    $('summary').innerHTML = state.truncated
        ? `<b>the first ${plural(items.length, 'item')}</b> · ${size} · narrow the search to see the rest`
        : `<b>${plural(items.length, 'item')}</b> · ${size}`;
    const empty = searching
        ? `<div class="empty">${icon('search')}<p>Nothing matches.</p></div>`
        : `<div class="empty">${icon('folder')}<p>This folder is empty.</p><p>Drop files here, or upload them with <b>+</b>.</p></div>`;
    $('list').innerHTML = items.length
        ? `<table class="files${items.some((item) => item.tags.length) ? '' : ' files--no-tags'}">
            <thead><tr>
                <th class="chk"><input type="checkbox" aria-label="Select all"></th>
                ${heading('name')}<th class="col-tags">Tags</th>${heading('size', 'r col-size')}${heading('mtime', 'col-mod')}<th></th>
            </tr></thead>
            <tbody>${items.map(row).join('')}</tbody>
        </table>`
        : empty;
    renderSelection();
}

/** Mark the selected rows and fit the selection bar to them, without redrawing the table. */
function renderSelection() {
    const count = state.selected.size;
    markRows($('list'), state.selected, (row) => row.dataset.path);
    $('selbar').hidden = !count;
    $('selCount').textContent = `${count.toLocaleString()} selected`;
    $('selRename').hidden = count !== 1;
    const single = count === 1 && itemAt([...state.selected][0]);
    $('selDownloadLabel').textContent = single && !single.is_dir ? 'Download' : 'Download zip';
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
    if (state.search !== null) return; // it would land out of sight
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

export const trashSelected = () => trashItems([...state.selected]);

/** Move the selection down (or up) by move rows, or to 'first' or 'last'; extend adds the rows between. */
export function moveCursor(move, extend) {
    ({ cursor, anchor } = keySelect(state.selected, sorted(state.items).map((item) => item.path), { cursor, anchor }, move, extend));
    renderSelection();
    revealRow($('list'), cursor, (row) => row.dataset.path);
}

/** Open the one selected item: a folder is gone into, a file opens in a new tab. */
export function openSelected() {
    const item = state.selected.size === 1 && itemAt([...state.selected][0]);
    if (!item) return;
    if (item.is_dir) location.hash = folderHash(item.path);
    else window.open(fileUrl(item.path), '_blank', 'noopener');
}

/** One folder up, or from a search back to the folder it started in. */
export function goUp() {
    if (state.search !== null) location.hash = folderHash(state.folder);
    else if (state.folder) location.hash = folderHash(parentOf(state.folder));
}

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
        if (action === 'menu') {
            const item = itemAt(tr.dataset.path);
            return openMenu(event.target.closest('[data-act]'), rowEntries(item), (act) => ROW_ACTIONS[act](item));
        }
        if (event.target.closest('a')) return; // links open or download by themselves
        anchor = clickSelect(state.selected, sorted(state.items).map((item) => item.path), tr.dataset.path, event, anchor);
        cursor = tr.dataset.path;
        renderSelection();
    });
    list.addEventListener('dblclick', (event) => {
        const item = itemAt(event.target.closest('tr.row')?.dataset.path);
        if (!item || event.target.closest('a, button, input')) return;
        if (item.is_dir) location.hash = folderHash(item.path);
        else window.open(fileUrl(item.path), '_blank', 'noopener');
    });
    list.addEventListener('change', (event) => {
        if (event.target.closest('thead')) event.target.checked ? selectAll() : clearSelection();
    });

    $('newFolderBtn').addEventListener('click', newFolder);
    $('selTag').addEventListener('click', () => openTagger([...state.selected]));
    $('selRename').addEventListener('click', renameSelected);
    $('selMove').addEventListener('click', moveSelected);
    $('selDownload').addEventListener('click', () => download([...state.selected]));
    $('selTrash').addEventListener('click', trashSelected);
    $('selClear').addEventListener('click', clearSelection);
    load();
}
