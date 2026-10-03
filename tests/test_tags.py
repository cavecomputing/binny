import io
import os
import zipfile

import pytest

from binny.api import tags as tags_api

from .conftest import listing, upload


def tag(client, paths, add=(), remove=(), rename=()):
    response = client.post('/api/tags', json={'paths': list(paths), 'add': list(add), 'remove': list(remove),
                                              'rename': [list(pair) for pair in rename]})
    assert response.status_code == 200, response.json
    return response.json['tags']


def counts(client):
    return {row['tag']: row['count'] for row in client.get('/api/tags').json['tags']}


def search(client, **params):
    response = client.get('/api/search', query_string=params)
    assert response.status_code == 200, response.json
    return [item['path'] for item in response.json['items']]


def trash(client, *paths):
    return client.post('/api/trash', json={'paths': list(paths)}).json['names']


def test_tags_are_added_lowercased_and_listed_with_items(client, files):
    upload(client, 'a.jpg')
    upload(client, 'b.jpg')
    assert tag(client, ['a.jpg', 'b.jpg'], add=['Family', ' beach ']) == {'a.jpg': ['beach', 'family'], 'b.jpg': ['beach', 'family']}
    assert listing(client)['a.jpg']['tags'] == ['beach', 'family']
    assert counts(client) == {'beach': 2, 'family': 2}


def test_tags_are_removed_and_renamed_on_just_the_items_that_have_them(client, files):
    for name in ('a.jpg', 'b.jpg', 'c.jpg'):
        upload(client, name)
    tag(client, ['a.jpg', 'b.jpg'], add=['beach'])
    tag(client, ['b.jpg'], add=['sea'])
    result = tag(client, ['a.jpg', 'b.jpg', 'c.jpg'], rename=[('beach', 'sea')], remove=['nope'])
    assert result == {'a.jpg': ['sea'], 'b.jpg': ['sea'], 'c.jpg': []}
    assert tag(client, ['b.jpg'], remove=['sea']) == {'b.jpg': []}
    assert counts(client) == {'sea': 1}


@pytest.mark.parametrize('bad', ['', '  ', 'two words', 'a,b', 'old>new', '-x', '+x', '@x', 'x' * 101, 'tab\there', 7])
def test_bad_tags_are_refused(client, files, bad):
    upload(client, 'a.jpg')
    response = client.post('/api/tags', json={'paths': ['a.jpg'], 'add': [bad]})
    assert response.status_code == 400
    assert response.json['error']


def test_tagging_needs_existing_items(client, files):
    assert client.post('/api/tags', json={'paths': ['missing.jpg'], 'add': ['x']}).status_code == 404
    assert client.post('/api/tags', json={'paths': [''], 'add': ['x']}).status_code == 400
    assert client.post('/api/tags', json={'add': ['x']}).status_code == 400


def test_a_folder_and_something_inside_it_are_both_tagged(client, files):
    upload(client, 'trip/a.jpg')
    assert tag(client, ['trip', 'trip/a.jpg'], add=['x']) == {'trip': ['x'], 'trip/a.jpg': ['x']}


def test_rename_everywhere_merges_into_an_existing_tag(client, files):
    upload(client, 'a.jpg')
    upload(client, 'b.jpg')
    tag(client, ['a.jpg'], add=['beach', 'sea'])
    tag(client, ['b.jpg'], add=['beach'])
    response = client.post('/api/tags/rename', json={'old': 'Beach', 'new': 'sea'})
    assert response.json == {'tag': 'sea', 'items': 2}
    assert counts(client) == {'sea': 2}
    assert client.post('/api/tags/rename', json={'old': 'beach', 'new': 'x'}).status_code == 404
    assert client.post('/api/tags/rename', json={'old': 'sea', 'new': 'a b'}).status_code == 400


def test_delete_everywhere(client, files):
    upload(client, 'a.jpg')
    upload(client, 'b.jpg')
    tag(client, ['a.jpg', 'b.jpg'], add=['beach', 'keep'])
    assert client.post('/api/tags/delete', json={'tag': 'beach'}).json == {'items': 2}
    assert counts(client) == {'keep': 2}
    assert client.post('/api/tags/delete', json={'tag': 'beach'}).status_code == 404


def test_tags_follow_renames_and_moves_all_the_way_down(client, files):
    upload(client, 'trip/day/a.jpg')
    client.post('/api/folders', json={'parent': '', 'name': 'archive'})
    tag(client, ['trip', 'trip/day/a.jpg'], add=['x'])
    client.post('/api/rename', json={'path': 'trip', 'name': 'holiday'})
    assert listing(client)['holiday']['tags'] == ['x']
    client.post('/api/move', json={'paths': ['holiday'], 'to': 'archive'})
    assert listing(client, 'archive/holiday/day')['a.jpg']['tags'] == ['x']
    assert search(client, tag='x') == ['archive/holiday', 'archive/holiday/day/a.jpg']


def test_tags_wait_in_the_trash_and_come_back_on_restore(client, files):
    upload(client, 'trip/a.jpg')
    tag(client, ['trip', 'trip/a.jpg'], add=['x'])
    names = trash(client, 'trip')
    assert counts(client) == {}
    assert search(client, tag='x') == []
    client.post('/api/trash/restore', json={'names': names})
    assert counts(client) == {'x': 2}
    assert listing(client, 'trip')['a.jpg']['tags'] == ['x']


def test_renaming_or_deleting_a_tag_reaches_the_trash(client, files):
    upload(client, 'a.jpg')
    upload(client, 'b.jpg')
    tag(client, ['a.jpg', 'b.jpg'], add=['old', 'gone'])
    names = trash(client, 'a.jpg')
    assert client.post('/api/tags/rename', json={'old': 'old', 'new': 'new'}).json == {'tag': 'new', 'items': 1}
    assert client.post('/api/tags/delete', json={'tag': 'gone'}).json == {'items': 1}
    client.post('/api/trash/restore', json={'names': names})
    assert listing(client)['a.jpg']['tags'] == ['new']


@pytest.mark.parametrize('how', ['delete', 'empty'])
def test_deleting_from_the_trash_drops_the_tags(client, files, how):
    upload(client, 'a.jpg')
    tag(client, ['a.jpg'], add=['x'])
    names = trash(client, 'a.jpg')
    client.post(f'/api/trash/{how}', json={'names': names} if how == 'delete' else None)
    upload(client, 'a.jpg')
    assert listing(client)['a.jpg']['tags'] == []
    assert client.post('/api/tags/delete', json={'tag': 'x'}).status_code == 404


def test_tags_stay_behind_when_a_file_moves_outside_the_app(client, files):
    upload(client, 'a.jpg')
    tag(client, ['a.jpg'], add=['x'])
    os.rename(files / 'a.jpg', files / 'b.jpg')
    assert listing(client)['b.jpg']['tags'] == []
    assert counts(client) == {}
    os.rename(files / 'b.jpg', files / 'a.jpg')
    assert listing(client)['a.jpg']['tags'] == ['x']


def test_new_things_dont_pick_up_tags_left_behind(client, files):
    upload(client, 'trip/a.jpg')
    tag(client, ['trip', 'trip/a.jpg'], add=['x'])
    (files / 'trip' / 'a.jpg').unlink()
    (files / 'trip').rmdir()
    listing(client)
    upload(client, 'trip/a.jpg')  # makes the folder again
    assert listing(client)['trip']['tags'] == []
    assert listing(client, 'trip')['a.jpg']['tags'] == []
    tag(client, ['trip'], add=['y'])
    (files / 'trip' / 'a.jpg').unlink()
    (files / 'trip').rmdir()
    listing(client)
    client.post('/api/folders', json={'parent': '', 'name': 'trip'})
    assert listing(client)['trip']['tags'] == []


def test_search_by_tags(client, files):
    upload(client, 'photos/beach.jpg')
    upload(client, 'photos/city.jpg')
    upload(client, 'docs/beach-trip.pdf')
    tag(client, ['photos', 'photos/beach.jpg', 'photos/city.jpg', 'docs/beach-trip.pdf'], add=['2024'])
    tag(client, ['photos/beach.jpg', 'docs/beach-trip.pdf'], add=['sea'])
    tag(client, ['docs/beach-trip.pdf'], add=['work'])
    assert search(client, tag='2024') == ['photos', 'docs/beach-trip.pdf', 'photos/beach.jpg', 'photos/city.jpg']
    assert search(client, tag=['2024', 'SEA']) == ['docs/beach-trip.pdf', 'photos/beach.jpg']
    assert search(client, tag='sea', **{'not': 'work'}) == ['photos/beach.jpg']
    assert search(client, **{'not': '2024'}) == ['docs']
    assert search(client, tag='nope') == []


def test_search_by_name_ignores_case_and_separators(client, files):
    upload(client, 'Beach_House.jpg')
    upload(client, 'beach-house plans.pdf')
    upload(client, 'beach/other.txt')
    upload(client, 'beachhouse.txt')
    assert search(client, name='beach  house') == ['Beach_House.jpg', 'beach-house plans.pdf']
    assert search(client, name='BEACH') == ['beach', 'Beach_House.jpg', 'beach-house plans.pdf', 'beachhouse.txt']
    assert search(client, name='.TXT') == ['beach/other.txt', 'beachhouse.txt']
    tag(client, ['beachhouse.txt'], add=['x'])
    assert search(client, name='beach', tag='x') == ['beachhouse.txt']


def test_search_results_carry_tags_and_folder_totals(client, files):
    upload(client, 'trip/a.jpg', b'123')
    tag(client, ['trip'], add=['x'])
    [item] = client.get('/api/search', query_string={'tag': 'x'}).json['items']
    assert (item['path'], item['tags'], item['items'], item['size']) == ('trip', ['x'], 1, 3)


def test_search_stops_at_its_limit(client, files, monkeypatch):
    monkeypatch.setattr(tags_api, 'SEARCH_LIMIT', 2)
    for name in ('a.txt', 'b.txt', 'c.txt'):
        upload(client, name)
    response = client.get('/api/search', query_string={'name': 'txt'}).json
    assert ([item['path'] for item in response['items']], response['truncated']) == (['a.txt', 'b.txt'], True)
    assert client.get('/api/search').json == {'items': [], 'truncated': False}


def test_moving_or_trashing_a_folder_with_something_inside_it_picked_too(client, files):
    upload(client, 'trip/a.jpg')
    client.post('/api/folders', json={'parent': '', 'name': 'archive'})
    assert client.post('/api/move', json={'paths': ['trip/a.jpg', 'trip'], 'to': 'archive'}).json == {'moved': 1, 'renamed': 0}
    assert (files / 'archive' / 'trip' / 'a.jpg').exists()
    assert len(trash(client, 'archive/trip', 'archive/trip/a.jpg')) == 1


def test_a_zip_from_several_folders_numbers_names_that_clash(client, files):
    upload(client, 'one/a.txt', b'1')
    upload(client, 'two/A.txt', b'2')
    response = client.post('/zip', data={'paths': ['one/a.txt', 'two/A.txt']})
    assert "filename=files.zip" in response.headers['Content-Disposition']
    with zipfile.ZipFile(io.BytesIO(response.data)) as zf:
        assert {name: zf.read(name) for name in zf.namelist()} == {'a.txt': b'1', 'A (1).txt': b'2'}
