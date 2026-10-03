import pytest

from binny import storage


@pytest.mark.parametrize('path', ['..', 'a/../..', '.trash', 'a/.trash/b', '.binny-1.part', 'a\0b', None, 5])
def test_clean_path_refuses_parents_hidden_names_and_junk(path):
    with pytest.raises(storage.InvalidPath):
        storage.clean_path(path)


def test_clean_path_normalizes_slashes():
    assert storage.clean_path('/a//b/') == 'a/b'
    assert storage.clean_path('') == ''


def test_resolve_refuses_a_symlink_that_leads_out(files):
    (files / 'out').symlink_to(files.parent)
    for path in ('out', 'out/binny.db'):
        with pytest.raises(storage.InvalidPath):
            storage.resolve(path)


def test_resolve_allows_a_symlink_that_stays_inside(files):
    (files / 'real').mkdir()
    (files / 'alias').symlink_to(files / 'real')
    assert storage.resolve('alias') == files / 'alias'


@pytest.mark.parametrize('name', ['', '   ', 'a/b', '.hidden', 'line\nbreak', 'x' * 256])
def test_check_name_refuses(name):
    with pytest.raises(storage.InvalidPath):
        storage.check_name(name)


def test_check_name_strips_surrounding_spaces():
    assert storage.check_name('  notes.txt ') == 'notes.txt'


@pytest.mark.parametrize('name, expected', [
    ('a.txt', 'a (1).txt'),
    ('backup.tar.gz', 'backup (1).tar.gz'),
    ('README', 'README (1)'),
])
def test_free_name_numbers_a_taken_name(files, name, expected):
    (files / name).write_text('x')
    assert storage.free_name(files, name) == expected


def test_free_name_counts_past_taken_numbers(files):
    for name in ('a.txt', 'a (1).txt'):
        (files / name).write_text('x')
    assert storage.free_name(files, 'a.txt') == 'a (2).txt'
    assert storage.free_name(files, 'free.txt') == 'free.txt'


def test_free_name_keeps_a_folders_dots(files):
    (files / 'v1.2').mkdir()
    assert storage.free_name(files, 'v1.2', is_dir=True) == 'v1.2 (1)'


def test_kind_comes_from_the_extension():
    assert storage.kind_of('Photo.JPG', False) == 'image'
    assert storage.kind_of('photos.jpg', True) == 'folder'
    assert storage.kind_of('mystery', False) == 'file'
