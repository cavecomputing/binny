"""The page, and the stored files themselves."""
import mimetypes

from flask import Blueprint, abort, render_template, request, send_file

from . import archive, storage
from .api.common import cleaned, items_arg

bp = Blueprint('views', __name__)


@bp.get('/')
def index():
    return render_template('index.html')


def shown_as(name):
    """(mimetype, whether the browser may show it in the tab rather than download it).

    Only media, PDFs and plain text open in the tab. Text of every kind, HTML included, is sent as
    text/plain, so a stored page can never run as part of Binny; everything else downloads.
    """
    mimetype = mimetypes.guess_type(name)[0] or 'application/octet-stream'
    if mimetype.startswith('text/') or storage.kind_of(name, False) == 'code':
        return 'text/plain', True
    return mimetype, mimetype.startswith(('image/', 'video/', 'audio/')) or mimetype == 'application/pdf'


@bp.get('/files/', defaults={'path': ''})
@bp.get('/files/<path:path>')
def stored_file(path):
    """A file, shown or downloaded (always downloaded with ?download), with range requests for
    seeking and resuming. A folder downloads as a zip."""
    rel, item = cleaned(path)
    if item.is_symlink() or not item.exists():
        abort(404)
    name = storage.name_of(rel)
    if item.is_dir():
        return archive.download([(item, name or 'files')], name or 'files')
    mimetype, inline = shown_as(name)
    response = send_file(item, mimetype=mimetype, as_attachment=not inline or 'download' in request.args,
                         download_name=name, conditional=True)
    response.headers['X-Content-Type-Options'] = 'nosniff'
    if mimetype == 'image/svg+xml':  # the one inline type that can carry script
        response.headers['Content-Security-Policy'] = 'sandbox'
    return response


@bp.post('/zip')
def zip_download():
    """Several files and folders as one zip named after the folder they're in. It is a plain form
    post, so the browser saves the response as it arrives instead of the page holding it."""
    items = items_arg(request.form.getlist('paths'))
    folder = storage.name_of(storage.parent_of(items[0][0])) or 'files'
    return archive.download([(item, storage.name_of(rel)) for rel, item in items], folder)
