import pytest

from binny import auth, config, create_app

from .conftest import PASSWORD


@pytest.fixture(autouse=True)
def no_wrong_password_delay(monkeypatch):
    monkeypatch.setattr(auth.time, 'sleep', lambda seconds: None)
    monkeypatch.setattr(auth, 'next_try', 0.0)


def session_cookie(response):
    return next(h for h in response.headers.getlist('Set-Cookie') if h.startswith('binny_session='))


def test_refuses_to_start_without_a_password(data_dir, monkeypatch):
    monkeypatch.setattr(config, 'PASSWORD', '')
    with pytest.raises(RuntimeError, match='BINNY_PASSWORD'):
        create_app()


def test_pages_send_you_to_sign_in(anon):
    response = anon.get('/')
    assert response.status_code == 302
    assert response.headers['Location'] == '/login?next=/'


def test_api_answers_401_without_a_session(anon):
    response = anon.get('/api/list')
    assert response.status_code == 401
    assert response.get_json() == {'error': 'Sign in first'}


def test_static_files_need_no_session(anon):
    assert anon.get('/static/css/style.css').status_code == 200


def test_wrong_password_is_refused(anon):
    response = anon.post('/login', data={'password': 'nope'})
    assert response.status_code == 401
    assert b'That password is wrong.' in response.data
    assert not any(h.startswith('binny_session=') for h in response.headers.getlist('Set-Cookie'))
    assert anon.get('/').status_code == 302


def test_wrong_password_holds_off_every_sign_in_for_a_second(anon, monkeypatch):
    now = 1000.0
    monkeypatch.setattr(auth.time, 'monotonic', lambda: now)
    assert anon.post('/login', data={'password': 'nope'}).status_code == 401
    response = anon.post('/login', data={'password': PASSWORD})  # even the right one, from anywhere
    assert response.status_code == 429
    assert b'Too many wrong passwords' in response.data
    assert anon.get('/').status_code == 302
    now += auth.WRONG_PASSWORD_WAIT
    assert anon.post('/login', data={'password': PASSWORD}).status_code == 302


def test_remembered_device_gets_a_lasting_cookie(anon):
    response = anon.post('/login', data={'password': PASSWORD, 'remember': 'on'})
    assert response.status_code == 302
    cookie = session_cookie(response)
    assert 'Expires=' in cookie and 'HttpOnly' in cookie and 'SameSite=Lax' in cookie
    assert anon.get('/').status_code == 200


def test_unremembered_device_gets_a_browser_session_cookie(anon):
    response = anon.post('/login', data={'password': PASSWORD})
    assert 'Expires=' not in session_cookie(response)
    assert anon.get('/').status_code == 200


@pytest.mark.parametrize('target, expected', [
    ('/api/list?path=docs', '/api/list?path=docs'),
    ('//evil.example/', '/'),
    ('https://evil.example/', '/'),
    ('/\\evil.example', '/'),
    ('/\t/evil.example', '/'),
    ('/\n/evil.example', '/'),
    ('/\r\nSet-Cookie: x=1', '/'),
])
def test_sign_in_returns_only_to_this_site(anon, target, expected):
    response = anon.post('/login', query_string={'next': target}, data={'password': PASSWORD})
    assert response.headers['Location'] == expected


def test_sign_out(client):
    assert client.get('/').status_code == 200
    client.post('/logout')
    assert client.get('/').status_code == 302


def test_cookie_survives_a_restart(app, client):
    cookie = client.get_cookie('binny_session').value
    restarted = create_app().test_client()
    restarted.set_cookie('binny_session', cookie)
    assert restarted.get('/').status_code == 200


def test_new_password_signs_every_device_out(app, client, monkeypatch):
    cookie = client.get_cookie('binny_session').value
    monkeypatch.setattr(config, 'PASSWORD', 'a new password')
    restarted = create_app().test_client()
    restarted.set_cookie('binny_session', cookie)
    assert restarted.get('/').status_code == 302


def test_cookie_is_secure_when_caddy_says_https(app):
    response = app.test_client().post('/login', data={'password': PASSWORD}, headers={'X-Forwarded-Proto': 'https'})
    assert 'Secure' in session_cookie(response)
    response = app.test_client().post('/login', data={'password': PASSWORD})
    assert 'Secure' not in session_cookie(response)


def test_cross_site_writes_are_blocked(client):
    response = client.post('/logout', headers={'Sec-Fetch-Site': 'cross-site'})
    assert response.status_code == 403
    response = client.post('/logout', headers={'Origin': 'http://evil.example'})
    assert response.status_code == 403
    assert client.get('/').status_code == 200
