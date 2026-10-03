import io
import zipfile

import pytest


@pytest.fixture
def stored(files):
    """A few files written straight to data/files/."""
    for name, data in {'photo.png': b'\x89PNG', 'page.html': b'<script>alert(1)</script>', 'data.bin': b'\0\1',
                       'logo.svg': b'<svg/>', 'movie.mp4': b'0123456789', 'trip/a.txt': b'aaa',
                       'trip/sub/b.txt': b'bb', 'trip/.hidden': b'h'}.items():
        (files / name).parent.mkdir(parents=True, exist_ok=True)
        (files / name).write_bytes(data)
    return files


def zip_names(response):
    return zipfile.ZipFile(io.BytesIO(response.data)).namelist()


def test_files_need_a_login(anon, stored):
    response = anon.get('/files/photo.png')
    assert response.status_code == 302
    assert response.headers['Location'].startswith('/login')


def test_media_shows_in_the_tab(client, stored):
    response = client.get('/files/photo.png')
    assert response.data == b'\x89PNG'
    assert response.mimetype == 'image/png'
    assert response.headers['Content-Disposition'].startswith('inline')
    assert response.headers['X-Content-Type-Options'] == 'nosniff'


def test_download_param_forces_a_download(client, stored):
    assert client.get('/files/photo.png?download').headers['Content-Disposition'] == 'attachment; filename=photo.png'


def test_html_is_sent_as_plain_text(client, stored):
    response = client.get('/files/page.html')
    assert response.content_type == 'text/plain; charset=utf-8'
    assert response.headers['Content-Disposition'].startswith('inline')


def test_svg_is_sandboxed(client, stored):
    assert client.get('/files/logo.svg').headers['Content-Security-Policy'] == 'sandbox'


def test_other_types_download(client, stored):
    assert client.get('/files/data.bin').headers['Content-Disposition'].startswith('attachment')


def test_range_requests_seek(client, stored):
    response = client.get('/files/movie.mp4', headers={'Range': 'bytes=2-4'})
    assert response.status_code == 206
    assert response.data == b'234'


@pytest.mark.parametrize('url', ['/files/.trash/x', '/files/trip/.hidden', '/files/%2e%2e/binny.db'])
def test_hidden_and_escaping_paths_are_refused(client, stored, url):
    assert client.get(url).status_code == 400


def test_missing_file_is_404(client, stored):
    assert client.get('/files/nope.txt').status_code == 404


def test_a_folder_downloads_as_a_zip(client, stored):
    response = client.get('/files/trip')
    assert response.mimetype == 'application/zip'
    assert response.headers['Content-Disposition'] == 'attachment; filename=trip.zip; filename*=UTF-8\'\'trip.zip'
    assert zip_names(response) == ['trip/', 'trip/a.txt', 'trip/sub/', 'trip/sub/b.txt']
    assert zipfile.ZipFile(io.BytesIO(response.data)).read('trip/sub/b.txt') == b'bb'


def test_a_selection_downloads_as_a_zip_named_for_its_folder(client, stored):
    response = client.post('/zip', data={'paths': ['trip/a.txt', 'trip/sub']})
    assert 'filename=trip.zip' in response.headers['Content-Disposition']
    assert zip_names(response) == ['a.txt', 'sub/', 'sub/b.txt']


def test_zip_names_keep_their_accents(client, files):
    (files / 'café').mkdir()
    response = client.get('/files/caf%C3%A9')
    assert response.headers['Content-Disposition'] == "attachment; filename=cafe.zip; filename*=UTF-8''caf%C3%A9.zip"
