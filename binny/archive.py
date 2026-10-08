"""Zips streamed to the browser while they are written, so a folder of any size downloads without a
temporary file and without holding it in memory.

Files are stored, not compressed: the big ones (photos, video, archives) don't shrink, and storing
keeps the server's CPU out of the way.
"""
import logging
import os
import unicodedata
import zipfile
from pathlib import Path
from urllib.parse import quote

from flask import Response

logger = logging.getLogger(__name__)

CHUNK = 1 << 20


class Pipe:
    """The write-only file zipfile writes into. drain() hands on whatever it has written so far."""

    def __init__(self):
        self.chunks = []

    def write(self, data):
        self.chunks.append(bytes(data))
        return len(data)

    def flush(self):
        pass

    def drain(self):
        chunks, self.chunks = self.chunks, []
        return chunks


def walk(path, name):
    """(path, name in the zip) for path and, for a folder, every plain file and folder visible below
    it. Symlinks are left out, and so are pipes and devices, which would block the read."""
    yield path, name
    if path.is_dir():
        with os.scandir(path) as entries:
            children = sorted(entries, key=lambda entry: entry.name)
        for entry in children:
            if not entry.name.startswith('.') and (entry.is_file(follow_symlinks=False) or entry.is_dir(follow_symlinks=False)):
                yield from walk(Path(entry.path), f'{name}/{entry.name}')


def stream(items):
    """The bytes of a zip of items, pairs of (absolute path, name in the zip), as they are written."""
    pipe = Pipe()
    with zipfile.ZipFile(pipe, 'w', zipfile.ZIP_STORED) as zf:
        for top, top_name in items:
            for path, name in walk(top, top_name):
                if path.is_dir():
                    zf.mkdir(name)
                    continue
                try:
                    info = zipfile.ZipInfo.from_file(path, name, strict_timestamps=False)
                    src = open(path, 'rb')
                except OSError as e:  # gone or unreadable since the walk listed it: leave it out
                    logger.warning('Left %s out of a zip: %s', path, e)
                    continue
                with src, zf.open(info, 'w') as dest:
                    while chunk := src.read(CHUNK):
                        dest.write(chunk)
                        yield from pipe.drain()
                yield from pipe.drain()
    yield from pipe.drain()


def download(items, name):
    """A response that downloads items as name.zip."""
    response = Response(stream(items), mimetype='application/zip')
    ascii_name = unicodedata.normalize('NFKD', name).encode('ascii', 'ignore').decode()
    response.headers.set('Content-Disposition', 'attachment', filename=f'{ascii_name or "files"}.zip',
                         **{'filename*': f"UTF-8''{quote(name + '.zip', safe='')}"})
    return response
