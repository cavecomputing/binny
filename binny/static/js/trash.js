/** The trash: moving things into it (with undo), and the trash view, where they're restored or deleted for good. */
import * as api from './api.js';
import { isTrash, nameOf, parentOf } from './paths.js';
import { menuButton, openMenu } from './rowmenu.js';
import { clickSelect, keySelect, markRows, revealRow } from './selection.js';
import { filesChanged, state } from './state.js';
import { $, ask, esc, formatDate, formatSize, icon, plural, toast } from './ui.js';

let items = [];             // what /api/trash lists
const selected = new Set(); // names in the trash
let anchor = null;
let cursor = null;

const itemNamed = (name) => items.find((item) => item.name === name);

const ROW_ENTRIES = [
    { act: 'restore', label: 'Restore', iconName: 'restore' },
    { act: 'delete', label: 'Delete forever', iconName: 'trash', danger: true },
];

/** Move paths to the trash, with an Undo in the toast. */
export async function trashItems(paths) {
    if (!paths.length) return;
    let names;
    try {
        ({ names } = await api.post('/api/trash', { paths }));
    } catch {
        return; // api.js showed why
    }
    for (const path of paths) state.selected.delete(path);
    const what = paths.length === 1 ? `"${esc(nameOf(paths[0]))}"` : plural(paths.length, 'item');
    toast(`Moved <b>${what}</b> to the trash`, { action: { label: 'Undo', run: () => restore(names) } });
    filesChanged();
}

async function restore(names) {
    let restored;
    try {
        ({ restored } = await api.post('/api/trash/restore', { names }));
    } catch {
        return;
    }
    const what = restored.length === 1 ? `"${esc(nameOf(restored[0]))}"` : plural(restored.length, 'item');
    const folders = new Set(restored.map(parentOf));
    toast(`Restored <b>${what}</b>${folders.size === 1 ? ` to /${esc([...folders][0])}` : ''}`);
    filesChanged();
}

async function deleteForever(names) {
    const size = names.reduce((sum, name) => sum + (itemNamed(name)?.size ?? 0), 0);
    const answer = await ask({
        title: 'Delete forever?',
        iconName: 'trash',
        text: `This deletes ${names.length === 1 ? nameOf(itemNamed(names[0]).original) : plural(names.length, 'item')} (${formatSize(size)}) for good. There's no undo.`,
        ok: 'Delete forever',
        danger: true,
        action: () => api.post('/api/trash/delete', { names }),
    });
    if (answer) filesChanged();
}

async function emptyTrash() {
    const size = items.reduce((sum, item) => sum + item.size, 0);
    const answer = await ask({
        title: 'Empty the trash?',
        iconName: 'trash',
        text: `This deletes ${plural(items.length, 'item')} (${formatSize(size)}) for good. There's no undo.`,
        ok: 'Delete forever',
        danger: true,
        action: () => api.post('/api/trash/empty'),
    });
    if (answer) filesChanged();
}

async function load() {
    $('trashView').hidden = !isTrash();
    if (!isTrash()) {
        selected.clear();
        return;
    }
    try {
        ({ items } = await api.get('/api/trash'));
    } catch {
        return;
    }
    const present = new Set(items.map((item) => item.name));
    for (const name of selected) if (!present.has(name)) selected.delete(name);
    render();
}

function row(item) {
    const where = `/${parentOf(item.original)}`;
    const date = formatDate(item.trashed_at);
    return `<tr class="row" data-name="${esc(item.name)}">
        <td class="chk"><input type="checkbox" aria-label="Select ${esc(nameOf(item.original))}"></td>
        <td><div class="name">
            <span class="ficon ${item.kind}">${icon(item.kind)}</span>
            <div class="name-text"><span class="fname">${esc(nameOf(item.original))}</span><div class="meta">${formatSize(item.size)} · from ${esc(where)}</div></div>
        </div></td>
        <td class="num r col-size">${formatSize(item.size)}</td>
        <td class="col-mod"><span class="path">${esc(where)}</span></td>
        <td class="num col-mod" title="${esc(new Date(item.trashed_at * 1000).toLocaleString())}">${date}</td>
        <td><div class="acts">${menuButton}</div></td>
    </tr>`;
}

function render() {
    const size = items.reduce((sum, item) => sum + item.size, 0);
    $('trashSummary').innerHTML = `<b>${plural(items.length, 'item')}</b> · ${formatSize(size)}`;
    $('emptyTrashBtn').disabled = !items.length;
    $('trashList').innerHTML = items.length
        ? `<table class="files">
            <thead><tr>
                <th class="chk"><input type="checkbox" aria-label="Select all"></th>
                <th>Name</th><th class="r col-size">Size</th><th class="col-mod">Was in</th><th class="col-mod">Trashed</th><th></th>
            </tr></thead>
            <tbody>${items.map(row).join('')}</tbody>
        </table>
        <div class="cc-callout"><span class="cc-callout__label">Note</span><span>Things stay here until you empty the trash. Restoring puts them back where they were.</span></div>`
        : `<div class="empty">${icon('trash')}<p>The trash is empty.</p></div>`;
    renderSelection();
}

function renderSelection() {
    markRows($('trashList'), selected, (row) => row.dataset.name);
    $('trashSelbar').hidden = !selected.size;
    $('trashSelCount').textContent = `${selected.size.toLocaleString()} selected`;
}

export function moveTrashCursor(move, extend) {
    ({ cursor, anchor } = keySelect(selected, items.map((item) => item.name), { cursor, anchor }, move, extend));
    renderSelection();
    revealRow($('trashList'), cursor, (row) => row.dataset.name);
}

export function selectAllTrash() {
    for (const item of items) selected.add(item.name);
    renderSelection();
}

export function clearTrashSelection() {
    selected.clear();
    renderSelection();
}

export const deleteSelectedForever = () => selected.size && deleteForever([...selected]);

export function initTrash() {
    window.addEventListener('hashchange', load);
    window.addEventListener('files-changed', load);

    const list = $('trashList');
    list.addEventListener('mousedown', (event) => event.shiftKey && event.preventDefault());
    list.addEventListener('click', (event) => {
        const tr = event.target.closest('tr.row');
        if (!tr) {
            if (!event.target.closest('thead')) clearTrashSelection();
            return;
        }
        const action = event.target.closest('[data-act]')?.dataset.act;
        if (action === 'menu') {
            const name = tr.dataset.name;
            return openMenu(event.target.closest('[data-act]'), ROW_ENTRIES, (act) => (act === 'restore' ? restore : deleteForever)([name]));
        }
        anchor = clickSelect(selected, items.map((item) => item.name), tr.dataset.name, event, anchor);
        cursor = tr.dataset.name;
        renderSelection();
    });
    list.addEventListener('change', (event) => {
        if (event.target.closest('thead')) event.target.checked ? selectAllTrash() : clearTrashSelection();
    });

    $('emptyTrashBtn').addEventListener('click', emptyTrash);
    $('trashSelRestore').addEventListener('click', () => restore([...selected]));
    $('trashSelDelete').addEventListener('click', deleteSelectedForever);
    $('trashSelClear').addEventListener('click', clearTrashSelection);
    load();
}
