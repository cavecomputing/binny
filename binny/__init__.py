"""Binny: a small, single-user file host."""
import logging
import mimetypes
import threading
from urllib.parse import urlsplit

from flask import Flask, abort, request
from werkzeug.middleware.proxy_fix import ProxyFix

from . import api, auth, config, index, views
from .db import init_db


def create_app(index_files=True):
    """The app. index_files=False skips the startup scan of data/files/, for tests."""
    if not config.PASSWORD:
        raise RuntimeError('Set BINNY_PASSWORD to the password that signs devices in.')
    logging.basicConfig(level=logging.INFO)
    # Browsers refuse module scripts not served as JavaScript, and some systems map .js to text/plain.
    mimetypes.add_type('text/javascript', '.js')

    app = Flask(__name__)
    # Caddy tells us the browser used HTTPS; that decides whether the session cookie is Secure.
    app.wsgi_app = ProxyFix(app.wsgi_app, x_for=0, x_proto=1)
    app.session_interface = auth.SessionInterface()
    app.config.update(
        # Cookies ignore the port, so a plain "session" would collide with other apps on the same host.
        SESSION_COOKIE_NAME='binny_session',
        SESSION_COOKIE_SAMESITE='Lax',
        PERMANENT_SESSION_LIFETIME=auth.REMEMBER_FOR,
    )

    config.FILES_DIR.mkdir(parents=True, exist_ok=True)
    init_db()
    app.secret_key = auth.signing_key()

    app.before_request(reject_cross_site_writes)
    app.before_request(auth.require_login)
    app.register_blueprint(auth.bp)
    app.register_blueprint(api.bp)
    app.register_blueprint(views.bp)

    if index_files:
        threading.Thread(target=index.index_everything, name='index', daemon=True).start()
    return app


def reject_cross_site_writes():
    """Refuse changes sent by another site's page in the same browser.

    Browsers send Sec-Fetch-Site over HTTPS and to localhost. Over plain HTTP elsewhere they send
    only Origin, which must then match the host and port exactly. Scripts like curl send neither.
    """
    if request.method in ('GET', 'HEAD', 'OPTIONS'):
        return
    site = request.headers.get('Sec-Fetch-Site')
    origin = request.headers.get('Origin')
    if site in ('cross-site', 'same-site') \
            or (site is None and origin is not None and urlsplit(origin).netloc != request.host):
        abort(403, 'Cross-site request blocked')
