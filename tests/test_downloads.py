import http.server
import threading
import time

import pytest

from binny import downloader

from .conftest import listing

REAL_LAUNCH = downloader.launch  # before in_the_request swaps it out


class Remote(http.server.BaseHTTPRequestHandler):
    """The far end of a download: each path answers the way a test needs."""

    routes = {
        '/files/notes.txt': (200, {'Content-Type': 'text/plain'}, b'hello'),
        '/my%20notes.txt': (200, {}, b'spaced'),
        '/report': (200, {'Content-Disposition': "attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf"}, b'%PDF'),
        '/evil': (200, {'Content-Disposition': 'attachment; filename="../../.evil.sh"'}, b'#!'),
        '/picture': (200, {'Content-Type': 'image/png'}, b'png'),
        '/short': (200, {'Content-Length': '100'}, b'only ten b'),
        '/moved': (302, {'Location': '/files/notes.txt'}, b''),
        '/to-disk': (302, {'Location': 'file:///etc/passwd'}, b''),
        '/to-ftp': (301, {'Location': 'ftp://127.0.0.1/file'}, b''),
    }

    def do_GET(self):
        if self.path == '/slow':
            return self.trickle()
        if self.path not in self.routes:
            return self.send_error(404)
        status, headers, body = self.routes[self.path]
        self.send_response(status)
        for name, value in {'Content-Length': str(len(body)), **headers}.items():
            self.send_header(name, value)
        self.end_headers()
        self.wfile.write(body)

    def trickle(self):
        """1000 bytes, ten at a time, for a test to catch a download in the middle."""
        self.send_response(200)
        self.send_header('Content-Length', '1000')
        self.end_headers()
        try:
            for _ in range(100):
                self.wfile.write(b'x' * 10)
                time.sleep(0.02)
        except OSError:  # the download hung up
            pass

    def log_message(self, *args):
        pass


@pytest.fixture
def remote():
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Remote)
    threading.Thread(target=server.serve_forever, args=(0.01,), daemon=True).start()
    yield f'http://127.0.0.1:{server.server_address[1]}'
    server.shutdown()
    server.server_close()


@pytest.fixture(autouse=True)
def in_the_request(monkeypatch):
    """Downloads run before the request returns, and the list starts empty."""
    monkeypatch.setattr(downloader, 'launch', downloader.run)
    monkeypatch.setattr(downloader, 'jobs', {})


@pytest.fixture
def in_the_background(monkeypatch):
    """Downloads run in threads of their own, as they do for real."""
    monkeypatch.setattr(downloader, 'launch', REAL_LAUNCH)


def download(client, url, folder='', tags=()):
    response = client.post('/api/downloads', json={'url': url, 'folder': folder, 'tags': list(tags)})
    assert response.status_code == 201, response.json
    return response.json


def wait_for(client, job_id, done, timeout=10):
    """The job once done(job) holds, polling the list the way the page does."""
    deadline = time.monotonic() + timeout
    while True:
        [job] = [job for job in client.get('/api/downloads').json['downloads'] if job['id'] == job_id]
        if done(job):
            return job
        assert time.monotonic() < deadline, f'Gave up waiting: {job}'
        time.sleep(0.01)


def test_a_link_downloads_into_the_folder_with_its_tags(client, files, remote):
    client.post('/api/folders', json={'parent': '', 'name': 'docs'})
    job = download(client, f'{remote}/files/notes.txt', 'docs', ['Notes'])
    assert (job['state'], job['path'], job['name'], job['size'], job['received'], job['error']) == \
        ('done', 'docs/notes.txt', 'notes.txt', 5, 5, '')
    assert (files / 'docs' / 'notes.txt').read_bytes() == b'hello'
    assert listing(client, 'docs')['notes.txt']['tags'] == ['notes']


@pytest.mark.parametrize('path, name', [
    ('/report', 'résumé.pdf'),       # the server's name for it wins
    ('/evil', 'evil.sh'),            # but it can't pick a folder or hide the file
    ('/picture', 'picture.png'),     # no extension: one from the type
    ('/moved', 'notes.txt'),         # redirects are followed
    ('/my notes.txt', 'my notes.txt'),  # a space gets escaped on the way out, as a browser would
])
def test_the_name_it_is_saved_as(client, files, remote, path, name):
    assert download(client, remote + path)['path'] == name
    assert [p.name for p in files.iterdir()] == [name]


def test_a_taken_name_is_numbered(client, files, remote):
    download(client, f'{remote}/files/notes.txt')
    assert download(client, f'{remote}/files/notes.txt')['path'] == 'notes (1).txt'


@pytest.mark.parametrize('path, error', [
    ('/missing', 'The server answered 404 Not Found'),
    ('/short', 'The connection closed before the end'),
    ('/to-disk', 'It redirected to something other than a web address'),
    ('/to-ftp', 'It redirected to something other than a web address'),
])
def test_a_failed_download_leaves_nothing_behind(client, files, remote, path, error):
    job = download(client, remote + path)
    assert (job['state'], job['error'], job['path']) == ('failed', error, '')
    assert list(files.iterdir()) == []


def test_an_unreachable_server_says_so(client, files):
    job = download(client, 'http://127.0.0.1:9/file.iso')
    assert job['state'] == 'failed'
    assert job['error'].startswith('Couldn\'t reach it: ')


def test_a_download_too_big_for_the_disk_stops(client, files, remote, monkeypatch):
    monkeypatch.setattr(downloader, 'KEEP_FREE', 1 << 62)
    job = download(client, f'{remote}/files/notes.txt')
    assert (job['state'], job['error']) == ('failed', 'There isn\'t room for it on the disk')
    assert list(files.iterdir()) == []


@pytest.mark.parametrize('body, status', [
    ({'url': 'ftp://example.com/a.iso'}, 400),
    ({'url': 'file:///etc/passwd'}, 400),
    ({'url': 'example.com/a.iso'}, 400),
    ({'url': 'https://'}, 400),
    ({'url': 'https://example.com:99999/a.iso'}, 400),
    ({'url': 'https://example.com:0/a.iso'}, 400),
    ({'url': 'https://[::1/a.iso'}, 400),
    ({'url': 'https://me:secret@example.com/a.iso'}, 400),
    ({}, 400),
    ({'url': 'https://example.com/a.iso', 'folder': 'nope'}, 404),
    ({'url': 'https://example.com/a.iso', 'tags': ['two words']}, 400),
])
def test_bad_requests_are_refused(client, files, body, status):
    assert client.post('/api/downloads', json=body).status_code == status
    assert client.get('/api/downloads').json == {'downloads': []}


def test_the_list_shows_newest_first_and_clears_finished(client, files, remote):
    download(client, f'{remote}/files/notes.txt')
    download(client, f'{remote}/missing')
    assert [job['state'] for job in client.get('/api/downloads').json['downloads']] == ['failed', 'done']
    assert client.post('/api/downloads/clear').json == {'cleared': 2}
    assert client.get('/api/downloads').json == {'downloads': []}


def test_a_queued_download_cancels_at_once(client, files, remote, monkeypatch):
    queued = []
    monkeypatch.setattr(downloader, 'launch', queued.append)
    job = download(client, f'{remote}/files/notes.txt')
    assert job['state'] == 'queued'
    assert client.post(f'/api/downloads/{job["id"]}/cancel').json == {'cancelled': job['id']}
    downloader.run(queued[0])  # its turn comes
    [listed] = client.get('/api/downloads').json['downloads']
    assert listed['state'] == 'cancelled'
    assert list(files.iterdir()) == []
    assert client.post(f'/api/downloads/{job["id"]}/cancel').status_code == 404
    assert client.post('/api/downloads/nope/cancel').status_code == 404


def test_a_download_runs_in_the_background(client, files, remote, in_the_background):
    job = download(client, f'{remote}/files/notes.txt', tags=['notes'])
    assert job['state'] in ('queued', 'downloading', 'done')
    assert wait_for(client, job['id'], lambda job: job['state'] == 'done')['path'] == 'notes.txt'
    assert listing(client)['notes.txt']['tags'] == ['notes']


def test_a_running_download_cancels(client, files, remote, in_the_background):
    job = download(client, f'{remote}/slow')
    wait_for(client, job['id'], lambda job: job['received'] > 0)
    client.post(f'/api/downloads/{job["id"]}/cancel')
    assert wait_for(client, job['id'], lambda job: job['state'] != 'cancelling')['state'] == 'cancelled'
    assert list(files.iterdir()) == []


def test_a_download_whose_folder_moves_fails(client, files, remote, in_the_background):
    client.post('/api/folders', json={'parent': '', 'name': 'docs'})
    job = download(client, f'{remote}/slow', 'docs')
    wait_for(client, job['id'], lambda job: job['received'] > 0)
    client.post('/api/rename', json={'path': 'docs', 'name': 'papers'})
    job = wait_for(client, job['id'], lambda job: job['state'] != 'downloading')
    assert (job['state'], job['error']) == ('failed', 'The folder it was going into moved')
    assert listing(client, 'papers') == {}


def test_downloads_need_a_login(anon):
    assert anon.get('/api/downloads').status_code == 401
    assert anon.post('/api/downloads', json={'url': 'https://example.com/a'}).status_code == 401
