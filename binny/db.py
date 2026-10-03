"""SQLite access. The database holds metadata only; data/files/ is the source of truth."""
import sqlite3
from contextlib import contextmanager

from . import config

SCHEMA = '''
    -- One row per visible file and folder under data/files/ (see index.py). path is relative to
    -- data/files/ with "/" separators; parent is '' for the top folder. Folders keep size 0: their
    -- totals are summed from the rows below them.
    CREATE TABLE IF NOT EXISTS entries (
        path TEXT PRIMARY KEY,
        parent TEXT NOT NULL,
        is_dir INTEGER NOT NULL,
        size INTEGER NOT NULL,
        mtime REAL NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_entries_parent ON entries(parent);
    -- What was trashed through the app. name is its name in data/files/.trash/,
    -- "<time_ns>_<original name>"; original is where it came from, so restore can put it back;
    -- size counts everything inside a folder, so the trash never has to walk one.
    CREATE TABLE IF NOT EXISTS trash (
        name TEXT PRIMARY KEY,
        original TEXT NOT NULL,
        size INTEGER NOT NULL
    );
    -- Tags, keyed by path like entries (see tags.py). A trashed item's tags wait under
    -- ".trash/<its name there>" until it is restored or deleted.
    CREATE TABLE IF NOT EXISTS tags (
        path TEXT NOT NULL,
        tag TEXT NOT NULL,
        PRIMARY KEY (path, tag)
    );
    CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);
    CREATE TABLE IF NOT EXISTS settings (
        key TEXT PRIMARY KEY,
        value TEXT NOT NULL
    );
'''


@contextmanager
def get_db():
    """Open a short-lived connection. WAL and a busy timeout let request threads share the file."""
    conn = sqlite3.connect(config.DATABASE)
    conn.row_factory = sqlite3.Row
    conn.execute('PRAGMA journal_mode=WAL')
    conn.execute('PRAGMA busy_timeout=5000')
    try:
        yield conn
    finally:
        conn.close()


def init_db():
    """Create the schema if needed. Safe to run on every start."""
    with get_db() as conn:
        conn.executescript(SCHEMA)
        conn.commit()
