/** Selecting rows by clicking, shared by the file table and the trash. Rows are keyed by a string. */

/**
 * Apply a click on the row with key to selected: a plain click selects just that row (or clears it
 * if it was the only one), Ctrl/Cmd-click or its checkbox toggles it, Shift-click adds the range
 * from anchor in order (the keys as shown). Returns the next anchor.
 */
export function clickSelect(selected, order, key, event, anchor) {
    if (event.shiftKey && order.includes(anchor)) {
        const [from, to] = [order.indexOf(anchor), order.indexOf(key)].sort((a, b) => a - b);
        for (const each of order.slice(from, to + 1)) selected.add(each);
        return anchor;
    }
    if (event.ctrlKey || event.metaKey || event.target.type === 'checkbox') {
        if (!selected.delete(key)) selected.add(key);
    } else {
        const onlyThis = selected.size === 1 && selected.has(key);
        selected.clear();
        if (!onlyThis) selected.add(key);
    }
    return key;
}

/** Mark list's rows (keyed by keyOf(row)) as selected or not, and its select-all box to match. */
export function markRows(list, selected, keyOf) {
    const rows = list.querySelectorAll('tr.row');
    for (const row of rows) {
        const isSelected = selected.has(keyOf(row));
        row.classList.toggle('sel', isSelected);
        row.querySelector('input').checked = isSelected;
    }
    const all = list.querySelector('thead input[type="checkbox"]');
    if (all) {
        all.checked = selected.size > 0 && selected.size === rows.length;
        all.indeterminate = selected.size > 0 && selected.size < rows.length;
    }
}
