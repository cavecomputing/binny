"""Upload from link: start a download into a folder, list the downloads, cancel one, clear the finished."""
import string
from urllib.parse import quote, urlsplit, urlunsplit

from flask import Blueprint, abort

from .. import downloader
from .common import folder_arg, json_body
from .tags import tags_arg

bp = Blueprint('downloads', __name__)


@bp.get('/downloads')
def list_downloads():
    return {'downloads': [job.as_json() for job in downloader.listed()]}


@bp.post('/downloads')
def start_download():
    """{url, folder, tags}: fetch url into folder on the server, tagged with tags."""
    data = json_body()
    url = url_arg(data.get('url'))
    folder, _ = folder_arg(data.get('folder', ''))
    return downloader.start(url, folder, tags_arg(data.get('tags'))).as_json(), 201


def url_arg(url):
    """The http or https link the client sent, with spaces and other characters a browser would
    escape escaped (urllib won't), or a 400."""
    url = url.strip() if isinstance(url, str) else ''
    try:
        parts = urlsplit(url)
        valid = parts.scheme in ('http', 'https') and bool(parts.hostname) and parts.port != 0
    except ValueError:  # a malformed host or port
        valid = False
    if not valid:
        abort(400, 'Paste a link that starts with http:// or https://')
    if parts.username is not None:
        abort(400, 'Links with a user name or password in them aren\'t supported')
    path, query = (quote(text, safe=string.punctuation) for text in (parts.path, parts.query))  # %-escapes stay
    return urlunsplit(parts._replace(path=path, query=query, fragment=''))


@bp.post('/downloads/<job_id>/cancel')
def cancel_download(job_id):
    if not downloader.cancel(job_id):
        abort(404, 'That download has already finished')
    return {'cancelled': job_id}


@bp.post('/downloads/clear')
def clear_downloads():
    return {'cleared': downloader.clear()}
