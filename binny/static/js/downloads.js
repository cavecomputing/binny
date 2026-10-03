/**
 * Upload from link: the + button's menu, and the downloads panel, which hands a link to the server
 * and follows its downloads. The server does the downloading (downloader.py), so the page only
 * polls the list: once a second while the panel is open or something is downloading.
 */
import * as api from './api.js';
import { folderHash } from './paths.js';
import { filesChanged, state } from './state.js';
import { allTags, completeLast, splitTokens, tagProblem } from './tags.js';
import { $, formatSize, icon, showError } from './ui.js';

const POLL_EVERY = 1000;
const RUNNING = new Set(['queued', 'downloading', 'cancelling']);
const BADGES = { downloading: 'cc-badge--open', done: 'cc-badge--done', failed: 'cc-badge--warn' };

let downloads = []; // as /api/downloads lists them, newest first
let seen = new Map(); // id -> state at the last look, to notice one finishing
let timer = null;
let latest = 0;

const panel = () => $('dlPanel');
const running = (job) => RUNNING.has(job.state);

/** Fetch the list and redraw, then go again in a second while that's worth it. */
async function refresh() {
    clearTimeout(timer);
    const request = ++latest;
    let listed = downloads;
    try {
        ({ downloads: listed } = await api.get('/api/downloads', {}, { quiet: true }));
    } catch { /* the next look will do */ }
    if (request !== latest) return; // a newer look took over
    downloads = listed;
    const landed = downloads.some((job) => job.state === 'done' && seen.has(job.id) && seen.get(job.id) !== 'done');
    seen = new Map(downloads.map((job) => [job.id, job.state]));
    if (landed) filesChanged();
    render();
    if (!document.hidden && (panel().open || downloads.some(running))) timer = setTimeout(refresh, POLL_EVERY);
}

function render() {
    const count = downloads.filter(running).length;
    $('dlBtn').hidden = !downloads.length;
    $('dlBtn').title = count ? `Downloads: ${count} running` : 'Downloads';
    $('dlBadge').textContent = count;
    $('dlBadge').hidden = !count;
    if (!panel().open) return;
    $('dlClear').hidden = count === downloads.length;
    $('dlNone').hidden = downloads.length > 0;
    // Cards are kept and updated in place, so a focused or hovered button survives the redraw.
    const list = $('dlList');
    downloads.forEach((job, i) => {
        const card = list.querySelector(`[data-id="${job.id}"]`) ?? newCard(job.id);
        update(card, job);
        if (list.children[i] !== card) list.insertBefore(card, list.children[i] ?? null);
    });
    while (list.children.length > downloads.length) list.lastElementChild.remove();
}

function newCard(id) {
    const card = document.createElement('div');
    card.dataset.id = id;
    card.innerHTML = `
        <div class="dl__top">
            <b class="dl__name"></b><span class="cc-badge"></span>
            <button class="icon-btn" type="button" data-cancel title="Cancel" aria-label="Cancel this download">${icon('x')}</button>
        </div>
        <div class="bar"><i></i></div>
        <div class="dl__bottom"><a class="path"></a><span class="num"></span></div>
        <div class="dl__error"></div>`;
    return card;
}

function update(card, job) {
    const percent = job.size ? Math.min(100, Math.floor((job.received / job.size) * 100)) : 0;
    const part = (selector) => card.querySelector(selector);
    card.className = `dl dl--${job.state}`;
    part('.dl__name').textContent = job.name;
    part('.dl__name').title = job.url;
    part('.cc-badge').className = `cc-badge ${BADGES[job.state] ?? ''}`;
    part('.cc-badge').textContent = job.state !== 'downloading' ? job.state : job.size ? `${percent}%` : formatSize(job.received);
    part('[data-cancel]').hidden = job.state !== 'queued' && job.state !== 'downloading';
    part('.bar').classList.toggle('bar--busy', job.state === 'downloading' && !job.size);
    part('.bar i').style.width = `${job.state === 'done' ? 100 : percent}%`;
    part('.path').href = folderHash(job.folder);
    part('.path').textContent = `→ /${job.folder}`;
    part('.num').textContent = progress(job);
    part('.dl__error').textContent = job.error;
}

/** How far job has got, in words. */
function progress(job) {
    const rate = job.rate ? ` · ${formatSize(job.rate)}/s` : '';
    switch (job.state) {
        case 'queued': return 'Waiting its turn';
        case 'cancelling': return 'Cancelling…';
        case 'done': return formatSize(job.received);
        case 'downloading':
            if (!job.received) return 'Connecting…';
            return `${formatSize(job.received)}${job.size ? ` of ${formatSize(job.size)}` : ''}${rate}`;
        default: return '';
    }
}

/** Fill "Save to" with every folder, in tree order, set to folder. */
async function listFolders(folder) {
    const select = $('dlFolder');
    select.replaceChildren(new Option(`/${folder}`, folder)); // until the rest arrive
    let folders = [];
    try {
        ({ folders } = await api.get('/api/folders/all'));
    } catch { /* api.js showed why; the folder on screen is still there to pick */ }
    const order = (a, b) => {
        const [x, y] = [a.split('/'), b.split('/')];
        for (let i = 0; i < Math.min(x.length, y.length); i++) {
            const by = x[i].localeCompare(y[i], undefined, { numeric: true, sensitivity: 'base' });
            if (by) return by;
        }
        return x.length - y.length;
    };
    select.replaceChildren(...[...new Set(['', folder, ...folders])].sort(order)
        .map((path) => new Option(`/${path}`, path, false, path === folder)));
}

/** The tags typed for a download, or null after saying what's wrong with one. "+new" is fine too. */
function tagsTyped() {
    const tags = splitTokens($('dlTags').value).map((word) => word.replace(/^\+/, '').toLowerCase());
    const problem = tags.map(tagProblem).find(Boolean);
    if (problem) {
        showError(problem);
        return null;
    }
    return [...new Set(tags)];
}

async function start(event) {
    event.preventDefault();
    const tags = tagsTyped();
    if (!tags) return;
    $('dlStart').disabled = true;
    try {
        const job = await api.post('/api/downloads', { url: $('dlUrl').value, folder: $('dlFolder').value, tags });
        seen.set(job.id, 'started');
        downloads = [job, ...downloads.filter((other) => other.id !== job.id)];
        $('dlUrl').value = ''; // the folder and tags stay for the next link
        render();
        refresh();
    } catch { /* api.js showed why */ }
    $('dlStart').disabled = false;
    $('dlUrl').focus();
}

/** Open the panel. With withLink, ready for a link to the folder on screen. */
export function openDownloads(withLink = false) {
    closeMenu();
    if (!panel().open) {
        panel().showModal();
        listFolders(state.folder);
        render(); // what the last look found, until this one answers
    }
    if (withLink) $('dlUrl').focus();
    refresh();
}

export const uploadFromLink = () => openDownloads(true);

function closeMenu() {
    $('uploadMenu').hidden = true;
    $('uploadCaret').setAttribute('aria-expanded', 'false');
}

export function initDownloads() {
    $('uploadCaret').addEventListener('click', () => {
        if (!$('uploadMenu').hidden) return closeMenu();
        $('uploadMenu').hidden = false;
        $('uploadCaret').setAttribute('aria-expanded', 'true');
        $('fromLink').focus();
    });
    $('upload').addEventListener('keydown', (event) => {
        if (event.key !== 'Escape' || $('uploadMenu').hidden) return;
        event.stopPropagation(); // not the page's Escape, which clears the selection
        closeMenu();
        $('uploadCaret').focus();
    });
    document.addEventListener('click', (event) => !event.target.closest('#uploadCaret, #uploadMenu') && closeMenu());
    $('fromLink').addEventListener('click', uploadFromLink);
    $('dlBtn').addEventListener('click', () => openDownloads());

    $('dlForm').addEventListener('submit', start);
    $('dlTags').addEventListener('keydown', (event) => {
        if (event.key !== 'Tab' || event.shiftKey || !event.target.value.trim()) return;
        const completed = completeLast(event.target.value, allTags().map(({ tag }) => tag));
        if (completed === null) return;
        event.preventDefault();
        event.target.value = completed;
    });
    $('dlList').addEventListener('click', async (event) => {
        if (event.target.closest('.path')) return panel().close(); // the link goes to the folder
        const cancel = event.target.closest('[data-cancel]');
        if (!cancel) return;
        cancel.disabled = true;
        try {
            await api.post(`/api/downloads/${cancel.closest('[data-id]').dataset.id}/cancel`);
        } catch { /* it finished meanwhile; the list will say so */ }
        cancel.disabled = false;
        refresh();
    });
    $('dlClear').addEventListener('click', async () => {
        try {
            await api.post('/api/downloads/clear');
        } catch {
            return;
        }
        refresh();
    });
    document.addEventListener('visibilitychange', () => !document.hidden && refresh());
    refresh();
}
