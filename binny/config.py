"""Runtime configuration, read once from the environment at import time.

Other modules read these as `config.NAME` when they need them, never `from .config import NAME`,
so the tests can point them at a temporary directory.
"""
import os
from pathlib import Path

# Resolved so paths never depend on the working directory.
DATA_DIR = Path(os.getenv('BINNY_DATA_DIR', 'data')).resolve()
FILES_DIR = DATA_DIR / 'files'
TRASH_DIR = FILES_DIR / '.trash'
DATABASE = DATA_DIR / 'binny.db'

# The one password that signs a device in. create_app() refuses to start without it.
PASSWORD = os.getenv('BINNY_PASSWORD', '')
