import pytest

from binny import config, create_app

PASSWORD = 'correct horse battery staple'


@pytest.fixture
def data_dir(tmp_path, monkeypatch):
    """Point every path in binny.config at a fresh temporary data directory."""
    monkeypatch.setattr(config, 'DATA_DIR', tmp_path)
    monkeypatch.setattr(config, 'FILES_DIR', tmp_path / 'files')
    monkeypatch.setattr(config, 'TRASH_DIR', tmp_path / 'files' / '.trash')
    monkeypatch.setattr(config, 'DATABASE', tmp_path / 'binny.db')
    monkeypatch.setattr(config, 'PASSWORD', PASSWORD)
    return tmp_path


@pytest.fixture
def app(data_dir):
    app = create_app(index_files=False)
    app.config['TESTING'] = True
    return app


@pytest.fixture
def anon(app):
    """A client that has not signed in."""
    return app.test_client()


@pytest.fixture
def client(app):
    """A signed-in client."""
    client = app.test_client()
    assert client.post('/login', data={'password': PASSWORD}).status_code == 302
    return client


@pytest.fixture
def files(app):
    """The app's data/files/ folder."""
    return config.FILES_DIR


def upload(client, path, data=b'hello', folder=''):
    """PUT data to /api/upload as path inside folder."""
    return client.put('/api/upload', query_string={'folder': folder, 'path': path}, data=data)


def listing(client, folder=''):
    """The items of a folder listing by name."""
    response = client.get('/api/list', query_string={'folder': folder})
    assert response.status_code == 200, response.json
    return {item['name']: item for item in response.json['items']}
