import os
import time

import pytest

from binny import index
from binny.db import get_db

from .conftest import listing, upload


def rows():
    with get_db() as conn:
        return [tuple(row) for row in conn.execute('SELECT path, parent FROM entries ORDER BY path')]


def test_upload_writes_the_file_and_lists_it(client, files):
    response = upload(client, 'notes.txt', b'hello')
    assert response.status_code == 201
    assert response.json == {'path': 'notes.txt'}
    assert (files / 'notes.txt').read_bytes() == b'hello'
    item = listing(client)['notes.txt']
    assert (item['path'], item['size'], item['kind'], item['is_dir']) == ('notes.txt', 5, 'doc', False)


def test_upload_never_overwrites(client, files):
    upload(client, 'a.txt', b'one')
    assert upload(client, 'a.txt', b'two').json == {'path': 'a (1).txt'}
    assert (files / 'a.txt').read_bytes() == b'one'
    assert (files / 'a (1).txt').read_bytes() == b'two'


def test_upload_creates_the_subfolders_of_a_dropped_folder(client, files):
    assert upload(client, 'trip/day 1/a.jpg', folder='').json == {'path': 'trip/day 1/a.jpg'}
    assert upload(client, 'trip/day 1/b.jpg').status_code == 201
    assert rows() == [('trip', ''), ('trip/day 1', 'trip'), ('trip/day 1/a.jpg', 'trip/day 1'),
                      ('trip/day 1/b.jpg', 'trip/day 1')]
    trip = listing(client)['trip']
    assert (trip['is_dir'], trip['items'], trip['size']) == (True, 1, 10)


def test_upload_into_a_folder_that_is_a_file_is_refused(client, files):
    upload(client, 'trip')
    assert upload(client, 'trip/a.jpg').status_code == 409


@pytest.mark.parametrize('folder, path', [('', '../escape.txt'), ('', '.trash/x.txt'), ('', '.hidden'),
                                          ('', ''), ('..', 'escape.txt'), ('missing', 'a.txt')])
def test_upload_refuses_bad_targets(client, files, folder, path):
    assert upload(client, path, folder=folder).status_code in (400, 404)
    assert not (files.parent / 'escape.txt').exists()
    assert list(files.iterdir()) == []


def test_an_upload_cut_short_leaves_nothing_behind(client, files):
    response = client.put('/api/upload', query_string={'path': 'big.iso'}, data=b'abc',
                          environ_overrides={'CONTENT_LENGTH': '10'})
    assert response.status_code == 400
    assert list(files.iterdir()) == []


def test_listing_deletes_partial_files_left_behind(client, files):
    (files / 'docs').mkdir()
    stale, fresh, other = files / 'docs' / f'.binny-{"a" * 32}.part', files / f'.binny-{"b" * 32}.part', files / '.notes.part'
    for path in (stale, fresh, other):
        path.write_bytes(b'half')
    long_ago = time.time() - index.STALE_PARTIAL - 60
    for path in (stale, other):
        os.utime(path, (long_ago, long_ago))
    listing(client)
    listing(client, 'docs')
    assert (stale.exists(), fresh.exists(), other.exists()) == (False, True, True)


def test_listing_picks_up_changes_made_outside_the_app(client, files):
    (files / 'a.txt').write_text('one')
    (files / 'b').mkdir()
    assert set(listing(client)) == {'a.txt', 'b'}

    (files / 'a.txt').unlink()
    (files / 'c.txt').write_text('three')
    (files / 'b' / 'inner.txt').write_text('in')
    items = listing(client)
    assert set(items) == {'b', 'c.txt'}
    assert items['c.txt']['size'] == 5
    assert (items['b']['items'], items['b']['size']) == (1, 2)  # b's time moved, so b was synced too


def test_listing_indexes_a_folder_added_outside_all_the_way_down(client, files):
    (files / 'new' / 'deep').mkdir(parents=True)
    (files / 'new' / 'deep' / 'x.txt').write_text('12345')
    new = listing(client)['new']
    assert (new['items'], new['size']) == (1, 5)


def test_listing_hides_dot_names_and_symlinks(client, files):
    (files / 'a.txt').write_text('x')
    (files / '.secret').write_text('x')
    (files / '.trash').mkdir()
    (files / 'link.txt').symlink_to(files / 'a.txt')
    assert set(listing(client)) == {'a.txt'}


def test_listing_a_missing_folder_is_404(client):
    response = client.get('/api/list', query_string={'folder': 'nope'})
    assert response.status_code == 404
    assert response.json == {'error': 'That folder no longer exists'}


def test_folder_totals_count_everything_below(client):
    upload(client, 'a/b/c.bin', b'x' * 10)
    upload(client, 'a/d.bin', b'x' * 5)
    a = listing(client)['a']
    assert (a['items'], a['size']) == (2, 15)


def test_rename_a_file(client, files):
    upload(client, 'a.txt')
    assert client.post('/api/rename', json={'path': 'a.txt', 'name': ' b.txt '}).json == {'path': 'b.txt'}
    assert sorted(os.listdir(files)) == ['b.txt']
    assert rows() == [('b.txt', '')]


def test_rename_refuses_a_taken_name(client, files):
    upload(client, 'a.txt', b'a')
    upload(client, 'b.txt', b'b')
    response = client.post('/api/rename', json={'path': 'a.txt', 'name': 'b.txt'})
    assert response.status_code == 409
    assert response.json == {'error': 'There is already a "b.txt" here'}
    assert (files / 'a.txt').read_bytes() == b'a' and (files / 'b.txt').read_bytes() == b'b'


def test_rename_a_folder_rekeys_everything_below(client, files):
    upload(client, 'old/sub/x.txt')
    upload(client, 'older.txt')  # sorts between "old" and "old/..." without a slash: must not move
    client.post('/api/rename', json={'path': 'old', 'name': 'new'})
    assert rows() == [('new', ''), ('new/sub', 'new'), ('new/sub/x.txt', 'new/sub'), ('older.txt', '')]
    assert (files / 'new' / 'sub' / 'x.txt').exists()


@pytest.mark.parametrize('body', [{'path': '', 'name': 'x'}, {'path': 'a.txt', 'name': '.x'},
                                  {'path': 'a.txt', 'name': 'a/b'}, {'path': 'a.txt'}])
def test_rename_refuses_bad_requests(client, files, body):
    upload(client, 'a.txt')
    assert client.post('/api/rename', json=body).status_code == 400
    assert os.listdir(files) == ['a.txt']


def test_move_into_a_folder(client, files):
    upload(client, 'a.txt')
    upload(client, 'b/inner.txt')
    upload(client, 'dest/keep.txt')
    assert client.post('/api/move', json={'paths': ['a.txt', 'b'], 'to': 'dest'}).json == {'moved': 2, 'renamed': 0}
    assert sorted(os.listdir(files / 'dest')) == ['a.txt', 'b', 'keep.txt']
    assert rows() == [('dest', ''), ('dest/a.txt', 'dest'), ('dest/b', 'dest'), ('dest/b/inner.txt', 'dest/b'),
                      ('dest/keep.txt', 'dest')]


def test_move_numbers_a_taken_name(client, files):
    upload(client, 'a.txt', b'top')
    upload(client, 'dest/a.txt', b'inner')
    assert client.post('/api/move', json={'paths': ['a.txt'], 'to': 'dest'}).json == {'moved': 1, 'renamed': 1}
    assert (files / 'dest' / 'a.txt').read_bytes() == b'inner'
    assert (files / 'dest' / 'a (1).txt').read_bytes() == b'top'


def test_move_into_itself_is_refused(client, files):
    upload(client, 'a/b/x.txt')
    upload(client, 'c.txt')
    response = client.post('/api/move', json={'paths': ['c.txt', 'a'], 'to': 'a/b'})
    assert response.status_code == 400
    assert response.json == {'error': '"a" can\'t go inside itself'}
    assert (files / 'c.txt').exists()


def test_move_to_where_it_already_is_does_nothing(client, files):
    upload(client, 'a.txt')
    assert client.post('/api/move', json={'paths': ['a.txt'], 'to': ''}).json == {'moved': 0, 'renamed': 0}


def test_move_of_a_missing_item_is_404(client, files):
    response = client.post('/api/move', json={'paths': ['ghost.txt'], 'to': ''})
    assert response.status_code == 404
    assert response.json == {'error': '"ghost.txt" no longer exists'}


def test_disk_errors_say_what_failed(client, files, monkeypatch):
    upload(client, 'b.txt')
    (files / 'dest').mkdir()

    def refuse(src, dst):
        raise PermissionError(13, 'Permission denied', str(src))
    monkeypatch.setattr(os, 'rename', refuse)
    response = client.post('/api/move', json={'paths': ['b.txt'], 'to': 'dest'})
    assert response.status_code == 500
    assert response.json == {'error': 'Permission denied ("b.txt")'}
