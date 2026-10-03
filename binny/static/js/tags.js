/**
 * Tags: the ones in use, the tag syntax the search box and the tag editor share, and the sidebar's
 * Tags section.
 *
 * The syntax follows imgy. "tag" searches for or adds a tag, "-tag" excludes or removes one, "+tag"
 * creates one, "@text" matches names and runs to the end of the line, "old>new" renames a tag and
 * "--tag" deletes it. Tags are lowercase and can't hold spaces, commas or ">".
 */
import * as api from './api.js';
import { currentSearch, searchHash } from './paths.js';
import { $, esc, icon } from './ui.js';

const NAV_SIZE = 8; // tags the sidebar lists; the search box lists them all

let list = [];          // [{ tag, count }], most used first
let counts = new Map(); // tag -> how many items have it
let latest = 0;

export const allTags = () => list;
export const tagCount = (tag) => counts.get(tag) ?? 0;
export const tagExists = (tag) => counts.has(tag);

export async function loadTags() {
    const request = ++latest;
    let data;
    try {
        data = await api.get('/api/tags');
    } catch {
        return;
    }
    if (request !== latest) return;
    list = data.tags;
    counts = new Map(list.map(({ tag, count }) => [tag, count]));
    renderNav();
    window.dispatchEvent(new Event('tags-loaded'));
}

/** The words of an expression; spaces and commas separate them. */
export const splitTokens = (text) => text.split(/[\s,]+/).filter(Boolean);

/** Why tag can't be a tag, or '' when it can. The server checks the same in tags.clean(). */
export function tagProblem(tag) {
    if (!tag) return 'A tag can\'t be empty';
    if ([...tag].length > 100) return 'A tag can be at most 100 characters';
    if (/[\s,>\x00-\x1f\x7f]/.test(tag)) return `"${tag}" can't be a tag: no spaces, commas or ">"`;
    if ('-+@'.includes(tag[0])) return `A tag can't start with "${tag[0]}"`;
    return '';
}

/** The word being typed at the end of text: { token, prefix ("--", "-", "+" or ""), term }. */
export function lastToken(text) {
    const token = text.split(/[\s,]+/).pop();
    const prefix = token.match(/^(--|-|\+)?/)[0];
    return { token, prefix, term: token.slice(prefix.length).toLowerCase() };
}

/** text with the word being typed swapped for replacement. */
export const replaceLast = (text, replacement) => text.slice(0, text.length - lastToken(text).token.length) + replacement;

/** Tags with term in them, those starting with it first, each part most used first. */
export function matchingTags(term, skip = new Set()) {
    const starts = [];
    const contains = [];
    for (const { tag } of list) {
        if (skip.has(tag)) continue;
        if (tag.startsWith(term)) starts.push(tag);
        else if (tag.includes(term)) contains.push(tag);
    }
    return [...starts, ...contains];
}

/**
 * Tab completion: text with the word being typed completed from matches, to the one match (and a
 * space) or as far as the matches agree. Null when that adds nothing.
 */
export function completeLast(text, matches) {
    const { token, prefix, term } = lastToken(text);
    if (!term || token.includes('>')) return null;
    const starting = matches.filter((tag) => tag.startsWith(term));
    const pool = starting.length ? starting : matches;
    if (pool.length === 1) return replaceLast(text, `${prefix}${pool[0]} `);
    if (!starting.length) return null;
    let common = starting[0];
    for (const tag of starting) while (!tag.startsWith(common)) common = common.slice(0, -1);
    return common.length > term.length ? replaceLast(text, prefix + common) : null;
}

/** tag as HTML with term marked. */
export function highlight(tag, term) {
    const at = term ? tag.indexOf(term) : -1;
    if (at < 0) return esc(tag);
    return `${esc(tag.slice(0, at))}<mark>${esc(tag.slice(at, at + term.length))}</mark>${esc(tag.slice(at + term.length))}`;
}

/** Where the name part of a search starts: an "@" at the start of a word. */
const NAME_START = /(?:^|[\s,])@/;
export const hasName = (text) => NAME_START.test(text);

/**
 * A search, "tag -tag @name", as { tags, not, name }. Renames and deletes aren't searches, so their
 * words are left out.
 */
export function parseQuery(text) {
    const at = text.search(NAME_START);
    const terms = { tags: [], not: [], name: at < 0 ? '' : text.slice(text.indexOf('@', at) + 1).trim().toLowerCase() };
    for (const token of splitTokens(at < 0 ? text : text.slice(0, at))) {
        if (token.includes('>') || token.startsWith('--')) continue;
        const tag = token.replace(/^[-+]/, '').toLowerCase();
        const into = token.startsWith('-') ? terms.not : terms.tags;
        if (tag && !into.includes(tag)) into.push(tag);
    }
    return terms;
}

/** The search terms written out the one way the address bar keeps them. */
export const formatQuery = ({ tags, not, name }) => [...tags, ...not.map((tag) => `-${tag}`), ...(name ? [`@${name}`] : [])].join(' ');

/** The sidebar's Tags section: the most used tags, each a search for it. */
function renderNav() {
    const search = currentSearch();
    $('tagSection').hidden = !list.length;
    $('tagNav').innerHTML = list.slice(0, NAV_SIZE).map(({ tag, count }) => `
        <a class="cc-shell__row" href="${esc(searchHash(tag))}"${search === tag ? ' aria-current="page"' : ''}>${icon('tag')}<span>${esc(tag)}</span><span class="count">${count.toLocaleString()}</span></a>`).join('')
        + (list.length > NAV_SIZE ? `<button class="cc-shell__row" type="button" data-all-tags>${icon('search')}<span>All ${list.length.toLocaleString()} tags</span></button>` : '');
}

export function initTags() {
    window.addEventListener('files-changed', loadTags);
    window.addEventListener('hashchange', renderNav);
    loadTags();
}
