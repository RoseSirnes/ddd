#!/usr/bin/env python3
"""Cria manifesto assinado de release do DDD usando chave privada fora do repositório."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey


def create_manifest(version: str, repo: str, binary: Path, private_key_b64: str) -> dict:
    if version.startswith('v'):
        version = version[1:]
    parts = version.split('.')
    if len(parts) != 3 or any(not p.isdigit() or len(p) > 6 for p in parts):
        raise ValueError('Use a versão no formato 1.2.0')
    if len(repo.split('/')) != 2 or any(c in repo for c in (' ', '..', '\\', ':', '?', '#')):
        raise ValueError('Use o repositório usuario/repositorio')
    raw = base64.b64decode(private_key_b64.strip(), validate=True)
    if len(raw) != 32:
        raise ValueError('Chave de publicação inválida')
    binary_bytes = binary.read_bytes()
    if binary_bytes[:2] != b'MZ':
        raise ValueError('O arquivo não é um executável Windows')
    digest = hashlib.sha256(binary_bytes).hexdigest()
    url = f'https://github.com/{repo}/releases/download/v{version}/DDD.exe'
    payload = f'{version}\n{url}\n{digest}'.encode()
    signature = base64.b64encode(Ed25519PrivateKey.from_private_bytes(raw).sign(payload)).decode()
    return {'version': version, 'url': url, 'sha256': digest, 'signature': signature}


def main() -> None:
    parser = argparse.ArgumentParser(description='Assinar atualização do DDD')
    parser.add_argument('--version', required=True, help='Ex.: 1.2.0')
    parser.add_argument('--repo', required=True, help='Ex.: seuusuario/ddd')
    parser.add_argument('--exe', default='DDD.exe', type=Path)
    parser.add_argument('--output', default='ddd-update.json', type=Path)
    parser.add_argument('--key-file', type=Path, help='Caminho para a chave privada, fora do repositório')
    args = parser.parse_args()
    private = os.getenv('DDD_SIGNING_KEY', '')
    if not private and args.key_file:
        private = args.key_file.read_text(encoding='utf-8')
    if not private:
        parser.error('Defina DDD_SIGNING_KEY ou informe --key-file')
    manifest = create_manifest(args.version, args.repo, args.exe, private)
    args.output.write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + '\n', encoding='utf-8')
    print(f'Manifesto assinado: {args.output} (versão {manifest["version"]})')


if __name__ == '__main__':
    main()
