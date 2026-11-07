/**
 * The search box. It suggests tags for the word being typed; Enter searches all of data/files/ (the
 * address becomes #search/<query>), or renames (old>new) or deletes (--tag) a tag everywhere.
 */
import * as api from './api.js';
import { currentSearch, folderHash, searchHash } from './paths.js';
import { filesChanged, state } from './state.js';
import {
    allTags, completeLast, formatQuery, hasName, highlight, lastToken, matchingTags, parseQuery, replaceLast,
    splitTokens, tagCount, tagExists, tagProblem,
} from './tags.js';
import { $, ask, esc, plural, showError, toast } from './ui.js';

const MODES = {
    include: ['Include', 'shows items that have this tag'],
    exclude: ['Exclude', 'hides items that have this tag'],
    name: ['Name', 'shows items with this in their name'],
    rename: ['Rename', 'old>new renames a tag on every item'],
    delete: ['Delete', 'takes a tag off every item'],
};

let matches = []; // the tags the suggestions list
let active = -1;  // the highlighted one

const input = () => $('searchInput');

/** The box's text as typing would leave it: when it's all selected, the next key replaces it. */
function typed() {
    const box = input();
    return box.value && box.selectionStart === 0 && box.selectionEnd === box.value.length ? '' : box.value;
}

function modeOf(text) {
    if (hasName(text)) return 'name';
    const { token, prefix } = lastToken(text);
    if (token.includes('>')) return 'rename';
    if (prefix === '--') return 'delete';
    return prefix === '-' ? 'exclude' : 'include';
}

const note = (html, warn = false) => `<div class="suggest__note${warn ? ' warn' : ''}">${html}</div>`;
const quoted = (tag) => `<b>${esc(tag)}</b>`;

/** What Enter would do with old>new, for the suggestions. */
function renameNote(token) {
    const [old, renamed = ''] = token.toLowerCase().split('>');
    if (!old) return note('Type the tag to rename, then <code>&gt;</code> and its new name.');
    if (!tagExists(old)) return note(`No tag named ${quoted(old)}.`, true);
    if (!renamed) return note(`Renames ${quoted(old)} on ${plural(tagCount(old), 'item')} to…`);
    const problem = tagProblem(renamed);
    if (problem) return note(esc(problem), true);
    if (tagExists(renamed)) return note(`Merges ${quoted(old)} (${plural(tagCount(old), 'item')}) into ${quoted(renamed)} (${plural(tagCount(renamed), 'item')}).`);
    return note(`Renames ${quoted(old)} to ${quoted(renamed)} on ${plural(tagCount(old), 'item')}.`);
}

function renderSuggestions() {
    const text = typed();
    const mode = modeOf(text);
    const { token, term } = lastToken(text);
    let body;
    matches = [];
    if (mode === 'name') {
        body = note(parseQuery(text).name ? 'Enter finds every item with this in its name.' : 'Type part of a name, spaces and all, like <code>@beach house</code> or <code>@.mp4</code>.');
    } else if (mode === 'rename') {
        body = renameNote(token);
    } else if (!allTags().length) {
        body = note('No tags yet. Select files and press <b>T</b> to tag them.');
    } else {
        const before = parseQuery(replaceLast(text, ''));
        matches = matchingTags(term, new Set([...before.tags, ...before.not]));
        active = Math.min(active, matches.length - 1);
        body = matches.map((tag, i) => `
            <div class="suggest__row${i === active ? ' active' : ''}" role="option" id="suggest-${i}" aria-selected="${i === active}" data-tag="${esc(tag)}">
                <span>${highlight(tag, term)}</span><span class="count">${tagCount(tag).toLocaleString()}</span>
            </div>`).join('') || note(`No tag matches ${quoted(term)}.`);
        if (mode === 'delete' && tagExists(term)) body = note(`Enter takes ${quoted(term)} off ${plural(tagCount(term), 'item')}.`, true) + body;
    }
    const [label, detail] = MODES[mode];
    $('suggest').innerHTML = `<div class="suggest__head"><span class="suggest__mode ${mode}">${label}</span><small>${detail}</small></div>${body}`;
    $('suggest').hidden = false;
    input().setAttribute('aria-expanded', 'true');
    if (active >= 0) {
        input().setAttribute('aria-activedescendant', `suggest-${active}`);
        $(`suggest-${active}`).scrollIntoView({ block: 'nearest' });
    } else {
        input().removeAttribute('aria-activedescendant');
    }
}

function hideSuggestions() {
    $('suggest').hidden = true;
    active = -1;
    input().setAttribute('aria-expanded', 'false');
    input().removeAttribute('aria-activedescendant');
}

/** Put tag in place of the word being typed, keeping its "-" or "--". */
function pick(tag, trailingSpace) {
    const text = typed();
    input().value = replaceLast(text, lastToken(text).prefix + tag) + (trailingSpace ? ' ' : '');
    active = -1;
}

/** Back to the folder the search started from. */
const leaveSearch = () => { location.hash = folderHash(state.folder); };

/** Enter: search, or run a rename or delete. A mistake leaves the suggestions up to fix it with. */
async function submit() {
    const text = input().value.trim();
    if (!text) {
        hideSuggestions();
        if (currentSearch() !== null) leaveSearch();
        return;
    }
    const words = splitTokens(text);
    const command = words.find((word) => word.includes('>') || word.startsWith('--'));
    if (command) {
        if (words.length > 1) return showError(`"${command}" has to be on its own`);
        hideSuggestions();
        if (command.includes('>')) return renameEverywhere(...command.toLowerCase().split('>'));
        return deleteEverywhere(command.slice(2).toLowerCase());
    }
    const terms = parseQuery(text);
    const unknown = [...terms.tags, ...terms.not].find((tag) => !tagExists(tag));
    if (unknown) return showError(`No tag named "${unknown}"`);
    const query = formatQuery(terms);
    if (!query) return;
    hideSuggestions();
    input().value = query;
    if (query === currentSearch()) filesChanged(); // the same search again: run it again
    else location.hash = searchHash(query);
}

/** After a tag was renamed (or deleted, renamed null): carry the search on screen over to it. */
function followTag(old, renamed) {
    const query = currentSearch();
    if (query !== null) {
        const terms = parseQuery(query);
        const swap = (tags) => [...new Set(tags.flatMap((tag) => (tag !== old ? [tag] : renamed ? [renamed] : [])))];
        const next = formatQuery({ ...terms, tags: swap(terms.tags), not: swap(terms.not) });
        if (next !== query) location.replace(next ? searchHash(next) : folderHash(state.folder));
    }
    syncBox();
    filesChanged();
}

async function renameEverywhere(old, renamed) {
    if (!old || !renamed) return showError('Rename a tag with old>new');
    if (!tagExists(old)) return showError(`No tag named "${old}"`);
    const problem = tagProblem(renamed);
    if (problem) return showError(problem);
    if (renamed === old) return;
    const merge = tagExists(renamed);
    const run = async () => {
        const { items } = await api.post('/api/tags/rename', { old, new: renamed });
        toast(`${merge ? 'Merged' : 'Renamed'} <b>${esc(old)}</b> ${merge ? 'into' : 'to'} <b>${esc(renamed)}</b> on ${plural(items, 'item')}`);
        followTag(old, renamed);
    };
    if (!merge) {
        try { await run(); } catch { /* api.js showed why */ }
        return;
    }
    await ask({
        title: 'Merge tags?',
        iconName: 'tag',
        text: `Everything tagged "${old}" (${plural(tagCount(old), 'item')}) gets "${renamed}" instead, which ${plural(tagCount(renamed), 'item')} already ${tagCount(renamed) === 1 ? 'has' : 'have'}. They can't be told apart afterwards.`,
        ok: 'Merge',
        action: run,
    });
}

async function deleteEverywhere(tag) {
    if (!tagExists(tag)) return showError(`No tag named "${tag}"`);
    await ask({
        title: 'Delete tag?',
        iconName: 'tag',
        text: `This takes "${tag}" off ${plural(tagCount(tag), 'item')}. There's no undo.`,
        ok: 'Delete tag',
        danger: true,
        action: async () => {
            const { items } = await api.post('/api/tags/delete', { tag });
            toast(`Took <b>${esc(tag)}</b> off ${plural(items, 'item')}`);
            followTag(tag, null);
        },
    });
}

/** "/" and the sidebar's "All tags": into the search box, with every tag listed. */
export function focusSearch() {
    input().focus();
    input().select();
    renderSuggestions(); // again, now that the text is selected: every tag
}

function onKey(event) {
    const shown = !$('suggest').hidden;
    if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && shown && matches.length) {
        event.preventDefault();
        active = event.key === 'ArrowDown' ? (active + 1) % matches.length : (active <= 0 ? matches.length : active) - 1;
        renderSuggestions();
    } else if (event.key === 'Tab') {
        if (!input().value.trim() || event.shiftKey) return; // Tab moves on as usual
        event.preventDefault();
        if (active >= 0) pick(matches[active], true);
        else input().value = completeLast(typed(), matches) ?? input().value;
        renderSuggestions();
    } else if (event.key === 'Enter') {
        event.preventDefault();
        if (active >= 0) pick(matches[active], false);
        submit();
    } else if (event.key === 'Escape') {
        event.preventDefault();
        if (shown) return hideSuggestions();
        if (input().value !== (currentSearch() ?? '')) syncBox();
        else if (currentSearch() !== null) leaveSearch();
        else input().blur();
    }
}

/** The search on screen into the box; the clear button shows while there's something to clear. */
function syncBox() {
    input().value = currentSearch() ?? '';
    syncClear();
}

const syncClear = () => { $('searchClear').hidden = !input().value && currentSearch() === null; };

export function initSearch() {
    const box = input();
    syncBox();
    box.addEventListener('focus', renderSuggestions);
    box.addEventListener('blur', hideSuggestions);
    box.addEventListener('click', () => $('suggest').hidden && renderSuggestions()); // back after an Enter
    box.addEventListener('input', () => {
        active = -1;
        syncClear();
        renderSuggestions();
    });
    box.addEventListener('keydown', onKey);
    $('searchClear').addEventListener('mousedown', (event) => event.preventDefault()); // keep the focus
    $('searchClear').addEventListener('click', () => {
        box.value = '';
        syncClear();
        if (currentSearch() !== null) leaveSearch();
        box.focus();
    });
    // Clicking a suggestion picks it without taking the focus from the box.
    $('suggest').addEventListener('mousedown', (event) => event.preventDefault());
    $('suggest').addEventListener('click', (event) => {
        const row = event.target.closest('[data-tag]');
        if (!row) return;
        pick(row.dataset.tag, false);
        submit();
    });
    document.addEventListener('click', (event) => event.target.closest('[data-all-tags]') && focusSearch());
    window.addEventListener('hashchange', syncBox);
}
