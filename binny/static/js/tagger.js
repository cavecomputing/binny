/**
 * The tag editor, for one item or a selection. Its line takes the tag syntax: "tag" adds a tag in
 * use, "+tag" a new one, "-tag" takes one off, "old>new" renames one on these items and "--" takes
 * them all off. Under the line it says what Enter will do; each Enter applies right away.
 */
import * as api from './api.js';
import { filesChanged, state } from './state.js';
import { allTags, completeLast, highlight, lastToken, matchingTags, replaceLast, splitTokens, tagCount, tagExists, tagProblem } from './tags.js';
import { $, esc, icon, plural, showError } from './ui.js';

const SUGGESTIONS = 12;
let items = []; // [{ path, name, is_dir, tags }] being tagged

const field = () => $('tagInput');
const have = (tag) => items.filter((item) => item.tags.includes(tag)).length;
const tagsOnItems = () => [...new Set(items.flatMap((item) => item.tags))].sort((a, b) => have(b) - have(a) || a.localeCompare(b));

/**
 * What Enter would do with the line: { rows, add, remove, rename, problem }. rows describe each word
 * for the preview ({ code, cls: add|rm|warn|alert|'', html }); problem is the first alert, which
 * stops Enter.
 */
function plan(text) {
    const result = { rows: [], add: new Set(), remove: new Set(), rename: [], problem: '' };
    const n = items.length;
    const all = n > 1 ? `all ${n} items` : items[0].is_dir ? 'this folder' : 'this file';
    const these = (count, without) => (count === n ? all
        : `the ${plural(count, 'item')} ${without ? 'without it' : count === 1 ? 'that has it' : 'that have it'}`);
    const none = n > 1 ? 'any of them' : 'it';
    const on = tagsOnItems();
    const row = (code, cls, html) => result.rows.push({ code, cls, html });
    const alert = (code, html, problem) => {
        row(code, 'alert', html);
        result.problem ||= problem;
    };
    const b = (tag) => `<b>${esc(tag)}</b>`;

    for (const token of splitTokens(text)) {
        if (token === '-' || token === '+') continue; // still typing
        if (token === '--') {
            if (!on.length) row(token, 'warn', `No tags on ${none} to take off.`);
            else row(token, 'rm', `Takes ${on.length === 1 ? 'its one tag' : `all ${on.length} tags`} off ${all}.`);
            on.forEach((tag) => result.remove.add(tag));
        } else if (token.startsWith('--')) {
            alert(token, `${b('-tag')} takes one tag off.`, 'Take one tag off with -tag');
        } else if (token.includes('>')) {
            const [old, renamed = ''] = token.toLowerCase().split('>');
            const problem = old && renamed ? tagProblem(renamed) : 'Rename a tag with old>new';
            const count = have(old);
            if (problem) alert(token, esc(problem), problem);
            else if (!count) alert(token, `${b(old)} isn't on ${none}.`, `"${old}" isn't on ${none}`);
            else {
                row(token, '', `Renames ${b(old)} to ${b(renamed)} on ${these(count)}.`);
                result.rename.push([old, renamed]);
            }
        } else if (token.startsWith('-')) {
            const tag = token.slice(1).toLowerCase();
            const count = have(tag);
            if (!count) row(token, 'warn', `Not on ${none}.`);
            else {
                row(token, 'rm', `Takes it off ${these(count)}.`);
                result.remove.add(tag);
            }
        } else {
            const create = token.startsWith('+');
            const tag = (create ? token.slice(1) : token).toLowerCase();
            const exists = tagExists(tag) || on.includes(tag);
            const problem = tagProblem(tag);
            const missing = n - have(tag);
            if (problem) alert(token, esc(problem), problem);
            else if (!exists && !create) alert(token, `No tag named ${b(tag)} yet. ${b(`+${tag}`)} creates it.`, `No tag named "${tag}" yet: +${tag} creates it`);
            else if (!missing) row(token, 'warn', `Already on ${all}.`);
            else {
                row(token, 'add', `${exists ? '' : 'New tag. '}Adds it to ${these(missing, true)}.`);
                result.add.add(tag);
            }
        }
    }
    return result;
}

function render() {
    const text = field().value;
    const { rows, add, remove } = plan(text);
    const n = items.length;
    const on = tagsOnItems();

    // The tags on the items, marked with what Enter would change.
    $('tagCurrent').innerHTML = on.map((tag) => `
        <span class="chip${remove.has(tag) ? ' chip--rm' : ''}">${esc(tag)}${n > 1 && have(tag) < n ? `<small>${have(tag)}/${n}</small>` : ''}
            <button type="button" data-remove="${esc(tag)}" title="Take it off" aria-label="Take ${esc(tag)} off">${icon('x')}</button></span>`).join('')
        + [...add].filter((tag) => !on.includes(tag)).map((tag) => `<span class="chip chip--add">${esc(tag)}</span>`).join('')
        || `<span class="tagger__none">${n > 1 ? 'None of them have tags yet.' : 'No tags yet.'}</span>`;

    $('tagPlan').innerHTML = rows.map(({ code, cls, html }) => `<li class="${cls}"><code>${esc(code)}</code><span>${html}</span></li>`).join('');
    $('tagPlan').hidden = !rows.length;

    // Tags to pick: the most used when nothing is being typed, else those matching the word.
    const { prefix, term } = lastToken(text);
    let label = 'Matching tags';
    let tags;
    if (prefix === '-') {
        label = n > 1 ? 'On them' : 'On it';
        tags = on.filter((tag) => tag.includes(term));
    } else if (term) {
        tags = matchingTags(term).slice(0, SUGGESTIONS);
    } else {
        label = 'Most used';
        tags = allTags().map(({ tag }) => tag).filter((tag) => have(tag) < n).slice(0, SUGGESTIONS);
    }
    $('tagSuggestLabel').textContent = label;
    $('tagSuggest').innerHTML = tags.map((tag) => `<button class="cc-tag" type="button" data-tag="${esc(tag)}"><span>${highlight(tag, term)}</span><small>${tagCount(tag).toLocaleString()}</small></button>`).join('');
    $('tagSuggestBox').hidden = !tags.length;
    $('tagApply').firstElementChild.textContent = text.trim() ? 'Apply' : 'Done';
}

async function change(body) {
    let tags;
    try {
        ({ tags } = await api.post('/api/tags', { paths: items.map((item) => item.path), ...body }));
    } catch {
        return false; // api.js showed why
    }
    for (const item of items) item.tags = tags[item.path] ?? item.tags;
    filesChanged();
    return true;
}

/** Enter: apply the line, or close when it's empty. */
async function apply() {
    const text = field().value;
    if (!splitTokens(text).length) return $('tagDialog').close();
    const { add, remove, rename, problem } = plan(text);
    if (problem) return showError(problem);
    if ((add.size || remove.size || rename.length) && !(await change({ add: [...add], remove: [...remove], rename }))) return;
    field().value = '';
    render();
}

/** Open the editor on paths among the items on screen. */
export function openTagger(paths) {
    items = paths.map((path) => state.items.find((item) => item.path === path)).filter(Boolean)
        .map(({ path, name, is_dir, tags }) => ({ path, name, is_dir, tags: [...tags] }));
    if (!items.length) return;
    $('tagWhat').textContent = items.length === 1 ? items[0].name : plural(items.length, 'item');
    field().value = '';
    render();
    $('tagDialog').showModal();
    field().focus();
}

export const tagSelected = () => openTagger([...state.selected]);

export function initTagger() {
    const dialog = $('tagDialog');
    dialog.querySelector('form').addEventListener('submit', (event) => {
        event.preventDefault();
        apply();
    });
    field().addEventListener('input', render);
    field().addEventListener('keydown', (event) => {
        if (event.key !== 'Tab' || event.shiftKey || !field().value.trim()) return;
        const { prefix } = lastToken(field().value);
        const completed = completeLast(field().value, prefix === '-' ? tagsOnItems() : allTags().map(({ tag }) => tag));
        if (!completed) return;
        event.preventDefault();
        field().value = completed;
        render();
    });
    dialog.addEventListener('click', async (event) => {
        const remove = event.target.closest('[data-remove]');
        const pick = event.target.closest('[data-tag]');
        if (remove) {
            if (await change({ remove: [remove.dataset.remove] })) render();
            field().focus(); // the button is gone with the chip
        } else if (pick) {
            const { tag } = pick.dataset;
            if (field().value.trim()) { // into the line, in place of the word being typed
                field().value = replaceLast(field().value, lastToken(field().value).prefix + tag) + ' ';
                render();
            } else if (await change({ add: [tag] })) {
                render();
            }
            field().focus();
        }
    });
    window.addEventListener('tags-loaded', () => dialog.open && render());
}
