from .conftest import listing, upload


def test_create_a_folder(client, files):
    response = client.post('/api/folders', json={'parent': '', 'name': 'photos'})
    assert response.status_code == 201
    assert response.json == {'path': 'photos'}
    assert (files / 'photos').is_dir()
    assert client.post('/api/folders', json={'parent': 'photos', 'name': '2026'}).json == {'path': 'photos/2026'}
    assert listing(client)['photos']['items'] == 1


def test_create_a_folder_refuses_a_taken_name(client, files):
    (files / 'notes').write_text('a file')
    response = client.post('/api/folders', json={'parent': '', 'name': 'notes'})
    assert response.status_code == 409
    assert response.json == {'error': 'There is already a "notes" here'}


def test_create_a_folder_in_a_missing_parent_is_404(client):
    assert client.post('/api/folders', json={'parent': 'nope', 'name': 'x'}).status_code == 404


def test_tree_lists_the_subfolders_of_open_folders(client):
    upload(client, 'a/b/c/x.txt', b'12345')
    upload(client, 'd/y.txt', b'12')
    response = client.get('/api/folders', query_string=[('open', 'a'), ('open', '../etc')])
    tree = response.json['tree']
    assert set(tree) == {'', 'a'}
    assert sorted(tree[''], key=lambda f: f['path']) == [
        {'path': 'a', 'name': 'a', 'has_children': True},
        {'path': 'd', 'name': 'd', 'has_children': False},
    ]
    assert tree['a'] == [{'path': 'a/b', 'name': 'b', 'has_children': True}]
    assert response.json['files_size'] == 7
    assert response.json['disk']['total'] >= response.json['disk']['used'] > 0
