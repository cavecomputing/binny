/** Calls to the JSON API under /api. A failed call shows the server's message (unless it's quiet) and throws it. */
import { showError } from './ui.js';

function failure(status, data) {
    if (status === 401) location.assign('/login'); // signed out, e.g. the password changed
    const error = new Error(data.error || `The server answered ${status}`);
    error.status = status;
    return error;
}

async function request(method, url, body, quiet = false) {
    try {
        const response = await fetch(url, {
            method,
            headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
            body: body === undefined ? undefined : JSON.stringify(body),
        });
        const data = await response.json().catch(() => ({}));
        if (!response.ok) throw failure(response.status, data);
        return data;
    } catch (error) {
        if (!quiet) showError(error.message);
        throw error;
    }
}

/**
 * GET url with query parameters, an object or a list of [name, value] pairs. quiet keeps a failure
 * off the screen, for polling, where the next try comes soon anyway.
 */
export const get = (url, params = {}, { quiet = false } = {}) => request('GET', `${url}?${new URLSearchParams(params)}`, undefined, quiet);

export const post = (url, body) => request('POST', url, body);

/**
 * PUT one file to /api/upload as path inside folder, calling onProgress(bytes sent) along the way.
 * Returns { done, abort }: done resolves to the server's answer, or rejects with its error.
 */
export function upload(folder, path, file, onProgress) {
    const xhr = new XMLHttpRequest();
    const done = new Promise((resolve, reject) => {
        xhr.open('PUT', `/api/upload?${new URLSearchParams({ folder, path })}`);
        xhr.upload.onprogress = (event) => onProgress(event.loaded);
        xhr.onload = () => {
            let data = {};
            try { data = JSON.parse(xhr.responseText); } catch { /* not JSON: a proxy error page */ }
            if (xhr.status >= 200 && xhr.status < 300) resolve(data);
            else reject(failure(xhr.status, data));
        };
        xhr.onerror = () => reject(new Error('The connection dropped'));
        xhr.onabort = () => reject(new Error('Cancelled'));
        xhr.send(file);
    });
    return { done, abort: () => xhr.abort() };
}
