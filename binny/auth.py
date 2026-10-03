"""The single-password login: the sign-in page, the guard in front of every other route, sign-out."""
import hashlib
import hmac
import secrets
import time
from datetime import timedelta

from flask import Blueprint, jsonify, redirect, render_template, request, session, url_for
from flask.sessions import SecureCookieSessionInterface

from . import config
from .db import get_db

bp = Blueprint('auth', __name__)

# "Keep this device signed in" lasts as long as browsers allow a cookie to (Chrome caps it at 400 days).
REMEMBER_FOR = timedelta(days=400)

# Everything else needs a signed-in session, so a new route is protected without opting in.
OPEN_ENDPOINTS = {'auth.login', 'static'}


class SessionInterface(SecureCookieSessionInterface):
    def get_cookie_secure(self, app):
        """Mark the cookie Secure whenever the browser reached Binny over HTTPS.

        Behind Caddy that comes from X-Forwarded-Proto (ProxyFix in create_app). Over plain HTTP on
        the LAN a Secure cookie would never come back, so it isn't set there.
        """
        return request.is_secure


def signing_key():
    """The key that signs session cookies: a random secret kept in the database, mixed with the password.

    Changing BINNY_PASSWORD signs every device out. So does deleting the `secret_key` setting and
    restarting.
    """
    with get_db() as conn:
        conn.execute("INSERT OR IGNORE INTO settings (key, value) VALUES ('secret_key', ?)", (secrets.token_hex(32),))
        conn.commit()
        secret = conn.execute("SELECT value FROM settings WHERE key = 'secret_key'").fetchone()['value']
    return hmac.new(bytes.fromhex(secret), config.PASSWORD.encode(), hashlib.sha256).digest()


def require_login():
    """before_request guard: API calls get a 401, page loads go to the sign-in page."""
    if session.get('signed_in') or request.endpoint in OPEN_ENDPOINTS:
        return None
    if request.path.startswith('/api/'):
        return jsonify({'error': 'Sign in first'}), 401
    return redirect(url_for('auth.login', next=request.full_path.rstrip('?')))


def local_target(target):
    """target if it is a path on this site, else '/', so ?next= can't send the browser elsewhere."""
    if target and target.startswith('/') and not target.startswith('//') and '\\' not in target:
        return target
    return '/'


@bp.route('/login', methods=['GET', 'POST'])
def login():
    if session.get('signed_in'):
        return redirect(local_target(request.args.get('next')))
    if request.method == 'GET':
        return render_template('login.html', error=None)
    password = request.form.get('password', '')
    if not hmac.compare_digest(password.encode(), config.PASSWORD.encode()):
        time.sleep(1)  # makes guessing slow
        return render_template('login.html', error='That password is wrong.'), 401
    session.clear()
    session['signed_in'] = True
    session.permanent = request.form.get('remember') == 'on'
    return redirect(local_target(request.args.get('next')))


@bp.post('/logout')
def logout():
    session.clear()
    return redirect(url_for('auth.login'))
