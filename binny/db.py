"""SQLite access. The database holds metadata only; data/files/ is the source of truth."""
import sqlite3
from contextlib import contextmanager

from . import config

SCHEMA = '''
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
