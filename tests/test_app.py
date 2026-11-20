"""Testes de integração da API Go, executados em Linux/macOS no binário de teste."""
import base64
import json
import os
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

BINARY = Path(__file__).parent / 'ddd-server-test'

def call(base, route, method='GET', body=None, token=None, content_type='application/json'):
    if body is not None and not isinstance(body, bytes):
        body = json.dumps(body).encode()
    headers = {}
    if body is not None:
        headers['Content-Type'] = content_type
    if token:
        headers['Cookie'] = f'ddd_session={token}'
    req = urllib.request.Request(base + route, method=method, data=body, headers=headers)
    try:
        r = urllib.request.urlopen(req, timeout=10)
    except urllib.error.HTTPError as e:
        r = e
    with r:
        ctype = r.headers.get_content_type()
        raw = r.read()
        data = json.loads(raw) if ctype == 'application/json' else raw
        cookie = r.headers.get('Set-Cookie', '').split(';')[0].removeprefix('ddd_session=')
        return r.status, data, cookie


def main():
    with tempfile.TemporaryDirectory() as data_dir:
        sock = socket.socket()
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
        sock.close()
        base = f'http://127.0.0.1:{port}'

        def start():
            proc = subprocess.Popen([str(BINARY), '-server-only', '-data-dir', data_dir, '-port', str(port)], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            for _ in range(80):
                try:
                    if call(base, '/api/me')[0] == 200:
                        return proc
                except OSError:
                    time.sleep(.08)
            raise Exception('DDD não iniciou.')

        proc = start()
        try:
            assert b'<html' in call(base, '/')[1].lower()
            assert call(base, '/app.js')[0] == 200
            assert call(base, '/styles.css')[0] == 200
            assert call(base, '/api/me')[1]['user'] is None
            assert call(base, '/api/update/status')[0] == 401
            status, alice, ca = call(base, '/api/auth/register', 'POST', {'nickname': 'Alice', 'password': 'alice-password'})
            assert status == 201
            status, bobby, cb = call(base, '/api/auth/register', 'POST', {'nickname': 'Bobby', 'password': 'bobby-password'})
            assert status == 201
            status, carol, cc = call(base, '/api/auth/register', 'POST', {'nickname': 'Carol', 'password': 'carol-password'})
            assert status == 201
            assert call(base, '/api/auth/login', 'POST', {'nickname': 'Alice', 'password': 'erro'})[0] == 401
            assert call(base, '/api/auth/login', 'POST', {'nickname': 'Alice', 'password': 'alice-password'})[0] == 200
            assert call(base, '/api/server/status')[1]['mode'] == 'local'
            # Profile details belong only to the logged-in owner; everyone logged in can view them.
            alice_id = alice['user']['id']
            bobby_id = bobby['user']['id']
            assert call(base, f'/api/profile/{alice_id}')[0] == 401
            bio = 'Curto jogos, filmes e programação! 🎮'
            stat = 'Disponível para conversar'
            status, updated, _ = call(base, '/api/profile/details', 'POST', {'bio': bio, 'status': stat}, ca)
            assert status == 200 and updated['user']['bio'] == bio and updated['user']['status'] == stat
            assert call(base, f'/api/profile/{alice_id}', token=cb)[1]['user']['bio'] == bio
            assert call(base, f'/api/profile/{alice_id}', token=cb)[1]['user']['status'] == stat
            assert call(base, '/api/profile/details', 'POST', {'bio': 'x'*281, 'status':'A'}, ca)[0] == 400
            assert call(base, '/api/profile/details', 'POST', {'bio': 'x', 'status':'x'*61}, ca)[0] == 400
            assert call(base, f'/api/profile/{alice_id}', token=cb)[1]['user']['bio'] == bio
            assert call(base, '/api/profile/details', 'POST', {'bio': '', 'status':'Ocupado'}, cb)[0] == 200
            assert call(base, f'/api/profile/{alice_id}', token=ca)[1]['user']['status'] == stat
            assert call(base, '/api/users?q=bo', token=ca)[1]['users'][0]['nickname'] == 'Bobby'
            assert call(base, '/api/users?q=bObBy', token=ca)[1]['users'][0]['nickname'] == 'Bobby'
            assert call(base, '/api/users?q=al', token=ca)[1]['users'] == []  # seu nick não aparece
            assert call(base, '/api/server/config', 'POST', {'url': 'http://servidor.exemplo.com'}, ca)[0] == 400
            assert call(base, '/api/update/status', token=ca)[1]['version'] == '1.5.0'
            assert call(base, '/api/update/check', 'POST', {}, ca)[0] == 400
            assert call(base, '/api/update/config', 'POST', {'repo': 'https://malicioso.com/ddd'}, ca)[0] == 400
            assert call(base, '/api/update/config', 'POST', {'repo': 'usuario/ddd'}, ca)[0] == 200
            assert call(base, '/api/update/status', token=ca)[1]['repo'] == 'usuario/ddd'
            assert call(base, '/api/messages')[0] == 401
            assert call(base, '/api/messages', 'POST', {'channel':'dm', 'recipientId': bobby['user']['id'], 'text':'oi'}, ca)[0] == 403
            assert call(base, '/api/requests', 'POST', {'userId': bobby['user']['id']}, ca)[0] == 201
            pending = call(base, '/api/requests', token=cb)[1]['incoming']
            assert len(pending) == 1
            reqid = pending[0]['id']
            assert call(base, f'/api/requests/{reqid}/accept', 'POST', {}, ca)[0] == 404
            assert call(base, f'/api/requests/{reqid}/accept', 'POST', {}, cb)[0] == 200
            assert len(call(base, '/api/friends', token=ca)[1]['friends']) == 1
            assert call(base, '/api/messages', 'POST', {'channel':'global','text':'Olá, mundo!'}, ca)[0] == 201
            assert call(base, '/api/messages?channel=global', token=cc)[1]['messages'][0]['body'] == 'Olá, mundo!'
            assert call(base, '/api/messages', 'POST', {'channel':'dm', 'recipientId': bobby['user']['id'], 'text':'Mensagem privada'}, ca)[0] == 201
            assert len(call(base, '/api/messages?channel=dm&with=' + bobby['user']['id'], token=ca)[1]['messages']) == 1
            assert call(base, '/api/messages?channel=dm&with=' + bobby['user']['id'], token=cc)[0] == 403
            png = base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/uT0AAAAASUVORK5CYII=')
            status, upload, _ = call(base, '/api/images', 'POST', png, ca, 'image/png')
            assert status == 201
            image = '/api/images/' + upload['imageId']
            assert call(base, image, token=cb)[0] == 403
            assert call(base, '/api/messages', 'POST', {'channel':'dm', 'recipientId':bobby['user']['id'], 'text':'Olha', 'imageId':upload['imageId']}, ca)[0] == 201
            assert call(base, image, token=cb)[0] == 200
            assert call(base, image, token=cc)[0] == 403
            assert call(base, '/api/profile/avatar', 'POST', {'imageId': upload['imageId']}, ca)[0] == 200
            assert call(base, image, token=cc)[0] == 200  # fotos de perfil são visíveis aos membros
            assert call(base, '/api/users?q=alice', token=cb)[1]['users'][0]['avatarImageId'] == upload['imageId']
            assert call(base, '/api/profile/avatar', 'POST', {'imageId': upload['imageId']}, cb)[0] == 403
            assert call(base, '/api/auth/logout', 'POST', {}, ca)[0] == 200
            assert call(base, '/api/me', token=ca)[1]['user'] is None
            status, _, new_ca = call(base, '/api/auth/login', 'POST', {'nickname':'Alice','password':'alice-password'})
            assert status == 200
            assert call(base, '/api/update/status', token=new_ca)[1]['repo'] == 'usuario/ddd'
        finally:
            proc.terminate()
            proc.wait(5)
        proc = start()
        try:
            assert call(base, '/api/auth/login', 'POST', {'nickname':'Alice','password':'alice-password'})[0] == 200
            assert call(base, f'/api/profile/{alice_id}', token=call(base, '/api/auth/login', 'POST', {'nickname':'Alice','password':'alice-password'})[2])[1]['user']['bio'] == bio
            print('OK: cadastro, login, amizades, mensagens globais, privadas, imagens, permissões e persistência.')
        finally:
            proc.terminate()
            proc.wait(5)

if __name__ == '__main__':
    main()
