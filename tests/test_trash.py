import pytest

from binny import config
from binny.db import get_db

from .conftest import listing, upload


def trash(client, *paths):
    response = client.post('/api/trash', json={'paths': list(paths)})
    assert response.status_code == 200, response.json
    return response.json['names']


def trash_items(client):
    return client.get('/api/trash').json['items']


def indexed(prefix):
    with get_db() as conn:
        return [row[0] for row in conn.execute("SELECT path FROM entries WHERE path LIKE ? || '%' ORDER BY path", (prefix,))]


def test_trash_moves_a_file_into_the_hidden_trash(client, files):
    upload(client, 'docs/a.txt', b'12345')
    [name] = trash(client, 'docs/a.txt')
    assert name.endswith('_a.txt')
    assert not (files / 'docs' / 'a.txt').exists()
    assert (config.TRASH_DIR / name).read_bytes() == b'12345'
    assert listing(client, 'docs') == {}
    [item] = trash_items(client)
    assert (item['name'], item['original'], item['size'], item['is_dir'], item['kind']) == (name, 'docs/a.txt', 5, False, 'doc')
    assert client.get('/api/folders').json['trash_items'] == 1


def test_a_name_as_long_as_the_disk_allows_trashes_and_restores(client, files):
    name = 'a' * 251 + '.txt'
    upload(client, name, b'long')
    names = trash(client, name)
    [item] = trash_items(client)
    assert (item['original'], item['kind']) == (name, 'doc')
    upload(client, name, b'taken')
    assert client.post('/api/trash/restore', json={'names': names}).json == {'restored': ['a' * 247 + ' (1).txt']}
    assert (files / ('a' * 247 + ' (1).txt')).read_bytes() == b'long'


def test_trashing_a_folder_drops_its_rows_and_remembers_its_size(client, files):
    upload(client, 'trip/day/a.jpg', b'123')
    upload(client, 'trip/b.jpg', b'45')
    trash(client, 'trip')
    assert indexed('trip') == []
    assert listing(client) == {}
    [item] = trash_items(client)
    assert (item['original'], item['size'], item['is_dir'], item['kind']) == ('trip', 5, True, 'folder')


def test_restore_puts_it_back_with_its_rows(client, files):
    upload(client, 'trip/day/a.jpg', b'123')
    names = trash(client, 'trip')
    assert client.post('/api/trash/restore', json={'names': names}).json == {'restored': ['trip']}
    assert (files / 'trip' / 'day' / 'a.jpg').read_bytes() == b'123'
    assert indexed('trip') == ['trip', 'trip/day', 'trip/day/a.jpg']
    assert trash_items(client) == []


def test_restore_keeps_both_when_the_name_is_taken(client, files):
    upload(client, 'a.txt', b'old')
    names = trash(client, 'a.txt')
    upload(client, 'a.txt', b'new')
    assert client.post('/api/trash/restore', json={'names': names}).json == {'restored': ['a (1).txt']}
    assert (files / 'a.txt').read_bytes() == b'new'
    assert (files / 'a (1).txt').read_bytes() == b'old'


def test_restore_makes_the_folder_it_was_in_again(client, files):
    upload(client, 'gone/deeper/a.txt')
    names = trash(client, 'gone/deeper/a.txt')
    trash(client, 'gone')
    client.post('/api/trash/restore', json={'names': names})
    assert (files / 'gone' / 'deeper' / 'a.txt').exists()
    assert indexed('gone') == ['gone', 'gone/deeper', 'gone/deeper/a.txt']


def test_restore_refuses_when_a_file_is_where_its_folder_was(client, files):
    upload(client, 'docs/a.txt')
    names = trash(client, 'docs/a.txt')
    trash(client, 'docs')
    upload(client, 'docs', b'a file now')
    response = client.post('/api/trash/restore', json={'names': names})
    assert response.status_code == 409
    assert response.json == {'error': 'Can\'t restore "a.txt": "docs" is a file, not a folder'}


def test_things_put_in_the_trash_by_hand_show_and_restore_to_the_top(client, files):
    config.TRASH_DIR.mkdir()
    (config.TRASH_DIR / 'stray.txt').write_text('hi')
    [item] = trash_items(client)
    assert (item['name'], item['original'], item['size']) == ('stray.txt', 'stray.txt', 2)
    client.post('/api/trash/restore', json={'names': ['stray.txt']})
    assert (files / 'stray.txt').read_text() == 'hi'


def test_a_symlink_put_in_the_trash_by_hand_is_hidden(client, files):
    """Restoring it would put a symlink, which listings hide, back among the files."""
    config.TRASH_DIR.mkdir()
    (files / 'real.txt').write_text('hi')
    (config.TRASH_DIR / 'link.txt').symlink_to(files / 'real.txt')
    assert trash_items(client) == []
    assert client.post('/api/trash/restore', json={'names': ['link.txt']}).status_code == 404
    assert not (files / 'link.txt').exists()


def test_delete_forever_removes_only_what_was_picked(client, files):
    upload(client, 'a.txt')
    upload(client, 'folder/b.txt')
    names = trash(client, 'a.txt', 'folder')
    assert client.post('/api/trash/delete', json={'names': names[1:]}).json == {'deleted': 1}
    assert [item['original'] for item in trash_items(client)] == ['a.txt']
    assert sorted(path.name for path in config.TRASH_DIR.iterdir()) == [names[0]]


def test_empty_removes_everything(client, files):
    upload(client, 'a.txt')
    upload(client, 'folder/b.txt')
    trash(client, 'a.txt', 'folder')
    assert client.post('/api/trash/empty').json == {'deleted': 2}
    assert list(config.TRASH_DIR.iterdir()) == []
    assert trash_items(client) == []
    with get_db() as conn:
        assert conn.execute('SELECT COUNT(*) FROM trash').fetchone()[0] == 0


def test_the_top_folder_and_the_trash_itself_cant_be_trashed(client, files):
    config.TRASH_DIR.mkdir()
    assert client.post('/api/trash', json={'paths': ['']}).status_code == 400
    assert client.post('/api/trash', json={'paths': ['.trash']}).status_code == 400


@pytest.mark.parametrize('name, status', [('../a.txt', 400), ('.secret', 400), ('', 400), ('nope', 404)])
def test_bad_trash_names_are_refused(client, files, name, status):
    assert client.post('/api/trash/restore', json={'names': [name]}).status_code == status
    assert client.post('/api/trash/delete', json={'names': [name]}).status_code == status
