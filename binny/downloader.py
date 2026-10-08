"""Upload from link: the server fetches a URL into a folder, off the request thread.

Jobs live in memory, which is why Binny runs one worker process; the list starts empty after a
restart, and what was downloaded stays as ordinary files. A download streams into a partial file
beside its target and is renamed into place once complete, like an upload. The remote server never
picks the path: its name for the file goes through the same checks as an upload's.
"""
import http.client
import logging
import mimetypes
import os
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from dataclasses import dataclass, field

from . import index, storage, tags
from .db import get_db

logger = logging.getLogger(__name__)

AT_ONCE = 3             # downloads running side by side; the rest wait their turn
TIMEOUT = 30            # seconds to connect, and to wait for each read
CHUNK = 1 << 20
KEEP_FREE = 1 << 30     # a download stops rather than leave the disk with less than this
FINISHED_KEPT = 50      # finished downloads the list remembers

slots = threading.BoundedSemaphore(AT_ONCE)  # a queued download waits for one
jobs = {}               # id -> Job, oldest first
lock = threading.Lock() # held to change a job's state, or the jobs


class Stopped(Exception):
    """A download Binny gave up on; the message says why."""


class Redirects(urllib.request.HTTPRedirectHandler):
    """Follow redirects to http and https only. urllib's own check lets ftp through, and turns the
    rest away with an error that reads as if the server had said it."""

    def http_error_302(self, req, fp, code, msg, headers):
        location = headers.get('Location') or headers.get('URI')
        if location and urllib.parse.urlsplit(urllib.parse.urljoin(req.full_url, location)).scheme not in ('http', 'https'):
            fp.close()
            raise Stopped('It redirected to something other than a web address')
        return super().http_error_302(req, fp, code, msg, headers)

    http_error_301 = http_error_303 = http_error_307 = http_error_308 = http_error_302


opener = urllib.request.build_opener(Redirects)
opener.addheaders = [('User-Agent', 'binny')]


@dataclass
class Job:
    url: str
    folder: str
    tags: list
    name: str
    id: str = field(default_factory=lambda: uuid.uuid4().hex)
    state: str = 'queued'  # then downloading, and done, failed or cancelled
    size: int | None = None
    received: int = 0
    path: str = ''
    error: str = ''
    started: float = 0.0
    ended: float = 0.0
    cancel: threading.Event = field(default_factory=threading.Event)

    def as_json(self):
        elapsed = (self.ended or time.time()) - self.started if self.started else 0
        # A running download stops at its next read, so it says "cancelling" until then.
        state = 'cancelling' if self.cancel.is_set() and not self.ended else self.state
        return {'id': self.id, 'url': self.url, 'folder': self.folder, 'name': self.name, 'state': state,
                'size': self.size, 'received': self.received, 'path': self.path, 'error': self.error,
                'rate': self.received / elapsed if elapsed > 0 else 0}


def safe_name(name):
    """A name from the remote end as one check_name() accepts: no folders, control characters or
    leading dots, short enough to take a " (1)". 'download' when nothing usable is left."""
    name = ''.join(c for c in name.replace('\\', '/').rsplit('/', 1)[-1] if c >= ' ').strip().lstrip('.').strip()
    stem, ext = os.path.splitext(name)
    while len((stem + ext).encode()) > 240 and stem:
        stem = stem[:-1]
    try:
        return storage.check_name(stem + ext)
    except storage.InvalidPath:
        return 'download'


def name_in_url(url):
    return safe_name(urllib.parse.unquote(urllib.parse.urlsplit(url).path))


def name_from(response):
    """The file's name: the server's Content-Disposition, else the end of the address it ended at,
    with an extension from the Content-Type when that has none."""
    given = response.headers.get_filename()
    name = safe_name(given) if given else name_in_url(response.url)
    content_type = response.headers.get_content_type() if response.headers.get('Content-Type') else None
    if not os.path.splitext(name)[1] and content_type not in (None, 'application/octet-stream'):
        name += mimetypes.guess_extension(content_type) or ''
    return name


def room(where):
    """Bytes a download may still write to the disk holding where, a path or an open file."""
    disk = os.statvfs(where)
    return disk.f_bavail * disk.f_frsize - KEEP_FREE


def start(url, folder, tag_list):
    """Queue a download of url into folder, to be tagged with tag_list. Returns its job."""
    job = Job(url, folder, tag_list, name_in_url(url))
    with lock:
        jobs[job.id] = job
        finished = [old for old in jobs.values() if old.ended]
        for old in finished[:-FINISHED_KEPT]:
            del jobs[old.id]
    launch(job)
    return job


def launch(job):
    """Run job in a thread of its own. A daemon thread, so a download never holds up a restart; one
    cut short that way leaves a partial file, which index.py deletes later."""
    threading.Thread(target=run, args=(job,), name='download', daemon=True).start()


def listed():
    """Every job, newest first."""
    with lock:
        return list(jobs.values())[::-1]


def cancel(job_id):
    """Stop a download that hasn't finished. Returns False when there's none such."""
    with lock:
        job = jobs.get(job_id)
        if not job or job.ended:
            return False
        job.cancel.set()
        if job.state == 'queued':  # run() will find it cancelled
            job.state, job.ended = 'cancelled', time.time()
    return True


def clear():
    """Forget the finished downloads. Returns how many."""
    with lock:
        finished = [job_id for job_id, job in jobs.items() if job.ended]
        for job_id in finished:
            del jobs[job_id]
    return len(finished)


def run(job):
    """Wait for a slot, then download job and record how it ended."""
    with slots:
        with lock:
            if job.cancel.is_set():
                return
            job.state, job.started = 'downloading', time.time()
        try:
            fetch(job)
            state = 'done'
        except Exception as e:
            state, job.error = ('cancelled', '') if job.cancel.is_set() else ('failed', failure(job, e))
    with lock:
        job.state, job.ended = state, time.time()


def failure(job, e):
    """Why job failed, in words for the downloads list."""
    if isinstance(e, Stopped):
        return str(e)
    if isinstance(e, urllib.error.HTTPError):
        return f'The server answered {e.code} {e.reason}'
    if isinstance(e, urllib.error.URLError):
        return f'Couldn\'t reach it: {getattr(e.reason, "strerror", None) or e.reason}'
    if isinstance(e, TimeoutError):
        return 'It stopped sending'
    if isinstance(e, (OSError, http.client.HTTPException)):
        return getattr(e, 'strerror', None) or 'The connection broke off'
    logger.error('Downloading %s failed', job.url, exc_info=e)
    return 'Something went wrong on the server'


def fetch(job):
    with opener.open(job.url, timeout=TIMEOUT) as response:
        job.name = name_from(response)
        length = response.headers.get('Content-Length', '')
        job.size = int(length) if length.isdigit() else None
        folder = storage.resolve(job.folder)
        if folder.is_symlink() or not folder.is_dir():
            raise Stopped('The folder it was going into is gone')
        if job.size and job.size > room(folder):
            raise Stopped('There isn\'t room for it on the disk')
        partial = storage.partial_in(folder)
        try:
            with open(partial, 'wb') as out:
                # read1() returns what has arrived, so a slow download still sees a cancel soon.
                while chunk := response.read1(CHUNK):
                    if job.cancel.is_set():
                        raise Stopped('Cancelled')
                    out.write(chunk)
                    job.received += len(chunk)
                    if job.received % (64 * CHUNK) < len(chunk):  # every 64 MB or so
                        if room(out.fileno()) < 0:
                            raise Stopped('The disk is nearly full')
                        if not partial.exists():  # its folder was renamed, moved or trashed: stop rather than fill it
                            raise Stopped('The folder it was going into moved')
            if job.size is not None and job.received < job.size:
                raise Stopped('The connection closed before the end')
            with storage.NAME_LOCK:
                if not partial.exists():  # the folder was renamed, moved or trashed meanwhile
                    raise Stopped('The folder it was going into moved')
                name = storage.free_name(folder, job.name)
                os.rename(partial, folder / name)
        finally:
            partial.unlink(missing_ok=True)
    job.name, job.path = name, storage.child(job.folder, name)
    with get_db() as conn:
        index.record(conn, job.path)
        tags.drop(conn, job.path)
        conn.executemany('INSERT OR IGNORE INTO tags (path, tag) VALUES (?, ?)', [(job.path, tag) for tag in job.tags])
        conn.commit()
