/** Uploads: the + button's file picker, files and folders dropped on the page, and the progress card. */
import * as api from './api.js';
import { filesChanged, state } from './state.js';
import { $, esc, formatSize, plural, showError } from './ui.js';

const AT_ONCE = 3; // files sent side by side
let jobs = [];     // this batch: { file, path, folder, sent, status: waiting|sending|done|failed, error, request }
let frame = 0;
let hideTimer;
let changedTimer;

const active = (job) => job.status === 'waiting' || job.status === 'sending';

export const pickFiles = () => $('filePicker').click();

/** Upload [{ file, path }] into folder; path is the file's name, or "sub/folders/name" from a dropped folder. */
export function uploadFiles(files, folder) {
    if (!files.length) return;
    clearTimeout(hideTimer);
    if (!jobs.some(active)) jobs = []; // a finished batch makes way
    jobs.push(...files.map(({ file, path }) => ({ file, path, folder, sent: 0, status: 'waiting' })));
    send();
    draw();
}

/** Upload what was dropped into folder, folders and all. Call it during the drop event: the browser
 * empties the DataTransfer when the event ends. */
export function uploadDropped(dataTransfer, folder) {
    const entries = [...dataTransfer.items].map((item) => item.webkitGetAsEntry?.()).filter(Boolean);
    if (!entries.length) return uploadFiles([...dataTransfer.files].map((file) => ({ file, path: file.name })), folder);
    Promise.all(entries.map((entry) => collect(entry, '')))
        .then((found) => uploadFiles(found.flat(), folder))
        .catch((error) => showError(`Couldn't read what was dropped: ${error.message}`));
}

/** The files in a dropped entry with their paths from the drop. Hidden files and folders stay behind. */
async function collect(entry, prefix) {
    if (entry.name.startsWith('.')) return [];
    const path = prefix + entry.name;
    if (entry.isFile) return [{ file: await new Promise((resolve, reject) => entry.file(resolve, reject)), path }];
    const reader = entry.createReader();
    const files = [];
    for (;;) { // readEntries hands them over in batches until an empty one
        const batch = await new Promise((resolve, reject) => reader.readEntries(resolve, reject));
        if (!batch.length) return files;
        for (const child of batch) files.push(...await collect(child, `${path}/`));
    }
}

/** Start waiting files until AT_ONCE are on their way. */
function send() {
    let sending = jobs.filter((job) => job.status === 'sending').length;
    for (const job of jobs) {
        if (sending >= AT_ONCE) break;
        if (job.status !== 'waiting') continue;
        job.status = 'sending';
        sending++;
        job.request = api.upload(job.folder, job.path, job.file, (sent) => {
            job.sent = sent;
            draw();
        });
        job.request.done.then(
            () => { job.status = 'done'; },
            (error) => { job.status = 'failed'; job.error = error.message; },
        ).then(finished);
    }
}

function finished() {
    // Show new files as they land, at most about once a second.
    changedTimer ??= setTimeout(() => { changedTimer = null; filesChanged(); }, 1000);
    send();
    draw();
    if (!jobs.some(active) && !jobs.some((job) => job.status === 'failed')) {
        hideTimer = setTimeout(() => { jobs = []; draw(); }, 4000);
    }
}

/** Redraw the progress card, at most once a frame. */
function draw() {
    frame ||= requestAnimationFrame(() => {
        frame = 0;
        const card = $('uploads');
        card.hidden = !jobs.length;
        if (!jobs.length) return;
        const total = jobs.reduce((sum, job) => sum + job.file.size, 0);
        const sent = jobs.reduce((sum, job) => sum + (job.status === 'done' ? job.file.size : Math.min(job.sent, job.file.size)), 0);
        const done = jobs.filter((job) => job.status === 'done').length;
        const failed = jobs.filter((job) => job.status === 'failed');
        const busy = jobs.some(active);
        const percent = total ? Math.floor((sent / total) * 100) : 100;
        const current = jobs.find((job) => job.status === 'sending') ?? jobs[0];

        card.classList.toggle('uploads--done', !busy && !failed.length);
        card.classList.toggle('uploads--failed', !busy && failed.length > 0);
        $('uploadTitle').textContent = busy ? `Uploading ${done + 1} of ${jobs.length}`
            : failed.length ? `${plural(failed.length, 'upload')} failed` : `Uploaded ${plural(done, 'file')}`;
        $('uploadPercent').textContent = busy ? `${percent}%` : formatSize(total);
        $('uploadBar').style.width = `${percent}%`;
        $('uploadNow').textContent = busy ? `${current.path} → /${current.folder}` : `into /${current.folder}`;
        $('uploadErrors').innerHTML = failed.map((job) => `<li><b>${esc(job.path)}</b> ${esc(job.error)}</li>`).join('');
        $('uploadClose').title = busy ? 'Cancel' : 'Close';
    });
}

/** The card's button: cancels what hasn't finished uploading (finished files stay), and closes the card. */
function closeCard() {
    for (const job of jobs) if (job.status === 'sending') job.request.abort();
    jobs = [];
    draw();
}

export function initUpload() {
    const picker = $('filePicker');
    $('uploadBtn').addEventListener('click', pickFiles);
    picker.addEventListener('change', () => {
        uploadFiles([...picker.files].map((file) => ({ file, path: file.name })), state.folder);
        picker.value = '';
    });
    $('uploadClose').addEventListener('click', closeCard);
    window.addEventListener('beforeunload', (event) => {
        if (jobs.some(active)) event.preventDefault(); // leaving would cut them off
    });
}
