#!/usr/bin/env python3
"""
KrakenHashes User API - Python Client Example

This script demonstrates how to use the KrakenHashes User API with Python.
It shows common workflows like creating clients, uploading hashlists, and
managing agents.

Requirements:
    pip install requests

Usage:
    python python_client.py
"""

import requests
import json
import sys
from typing import Dict, List, Optional
from pathlib import Path


class KrakenHashesClient:
    """Client for the KrakenHashes User API"""

    def __init__(self, base_url: str, email: str, api_key: str):
        """
        Initialize the client

        Args:
            base_url: API base URL (e.g., "https://your-domain.com/api/v1")
            email: Your user email address
            api_key: Your 64-character API key
        """
        self.base_url = base_url.rstrip('/')
        self.session = requests.Session()
        self.session.headers.update({
            'X-User-Email': email,
            'X-API-Key': api_key,
            'Content-Type': 'application/json'
        })

    def _request(self, method: str, endpoint: str, **kwargs) -> requests.Response:
        """Make an API request"""
        url = f"{self.base_url}/{endpoint.lstrip('/')}"
        response = self.session.request(method, url, **kwargs)

        # Raise exception for error responses
        if not response.ok:
            try:
                error_data = response.json()
                print(f"API Error: {error_data.get('error', 'Unknown error')}", file=sys.stderr)
                print(f"Error Code: {error_data.get('code', 'UNKNOWN')}", file=sys.stderr)
            except:
                print(f"HTTP {response.status_code}: {response.text}", file=sys.stderr)
            response.raise_for_status()

        return response

    # Client Management
    def create_client(self, name: str, description: Optional[str] = None) -> Dict:
        """Create a new client"""
        data = {'name': name}
        if description:
            data['description'] = description
        response = self._request('POST', '/clients', json=data)
        return response.json()

    def list_clients(self, page: int = 1, page_size: int = 20) -> Dict:
        """List all clients"""
        params = {'page': page, 'page_size': page_size}
        response = self._request('GET', '/clients', params=params)
        return response.json()

    def get_client(self, client_id: str) -> Dict:
        """Get a specific client"""
        response = self._request('GET', f'/clients/{client_id}')
        return response.json()

    def update_client(self, client_id: str, name: Optional[str] = None,
                     description: Optional[str] = None) -> Dict:
        """Update a client"""
        data = {}
        if name:
            data['name'] = name
        if description is not None:
            data['description'] = description
        response = self._request('PATCH', f'/clients/{client_id}', json=data)
        return response.json()

    def delete_client(self, client_id: str) -> None:
        """Delete a client"""
        self._request('DELETE', f'/clients/{client_id}')

    # Hashlist Management
    def create_hashlist(self, name: str, client_id: str, hash_type: int,
                       file_path: str) -> Dict:
        """Upload a hashlist"""
        # Remove Content-Type header for multipart upload
        headers = {k: v for k, v in self.session.headers.items()
                  if k.lower() != 'content-type'}

        with open(file_path, 'rb') as f:
            files = {'file': (Path(file_path).name, f, 'text/plain')}
            data = {
                'name': name,
                'client_id': client_id,
                'hash_type': str(hash_type)
            }

            url = f"{self.base_url}/hashlists"
            response = requests.post(url, headers=headers, files=files, data=data)

            if not response.ok:
                try:
                    error_data = response.json()
                    print(f"API Error: {error_data.get('error', 'Unknown error')}",
                         file=sys.stderr)
                except:
                    print(f"HTTP {response.status_code}: {response.text}",
                         file=sys.stderr)
                response.raise_for_status()

            return response.json()

    def list_hashlists(self, page: int = 1, page_size: int = 20,
                      client_id: Optional[str] = None) -> Dict:
        """List hashlists"""
        params = {'page': page, 'page_size': page_size}
        if client_id:
            params['client_id'] = client_id
        response = self._request('GET', '/hashlists', params=params)
        return response.json()

    def get_hashlist(self, hashlist_id: int) -> Dict:
        """Get hashlist details"""
        response = self._request('GET', f'/hashlists/{hashlist_id}')
        return response.json()

    def delete_hashlist(self, hashlist_id: int) -> None:
        """Delete a hashlist"""
        self._request('DELETE', f'/hashlists/{hashlist_id}')

    # Agent Management
    def generate_voucher(self, expires_in: int = 604800,
                        is_continuous: bool = False) -> Dict:
        """Generate an agent registration voucher"""
        data = {
            'expires_in': expires_in,
            'is_continuous': is_continuous
        }
        response = self._request('POST', '/agents/vouchers', json=data)
        return response.json()

    def list_agents(self, page: int = 1, page_size: int = 20,
                   status: Optional[str] = None) -> Dict:
        """List agents"""
        params = {'page': page, 'page_size': page_size}
        if status:
            params['status'] = status
        response = self._request('GET', '/agents', params=params)
        return response.json()

    def get_agent(self, agent_id: int) -> Dict:
        """Get agent details"""
        response = self._request('GET', f'/agents/{agent_id}')
        return response.json()

    def update_agent(self, agent_id: int, name: Optional[str] = None,
                    extra_parameters: Optional[str] = None,
                    is_enabled: Optional[bool] = None) -> Dict:
        """Update agent settings"""
        data = {}
        if name:
            data['name'] = name
        if extra_parameters is not None:
            data['extra_parameters'] = extra_parameters
        if is_enabled is not None:
            data['is_enabled'] = is_enabled
        response = self._request('PATCH', f'/agents/{agent_id}', json=data)
        return response.json()

    def delete_agent(self, agent_id: int) -> None:
        """Disable an agent"""
        self._request('DELETE', f'/agents/{agent_id}')

    # Metadata/Helper Endpoints
    def list_hash_types(self, enabled_only: bool = False) -> Dict:
        """List available hash types"""
        params = {'enabled_only': str(enabled_only).lower()}
        response = self._request('GET', '/hash-types', params=params)
        return response.json()

    def list_workflows(self) -> Dict:
        """List available job workflows"""
        response = self._request('GET', '/workflows')
        return response.json()

    def list_preset_jobs(self) -> Dict:
        """List available preset jobs"""
        response = self._request('GET', '/preset-jobs')
        return response.json()


def example_workflow():
    """Example workflow demonstrating common API operations"""

    # Initialize client
    client = KrakenHashesClient(
        base_url='http://localhost:31337/api/v1',
        email='user@example.com',
        api_key='your-64-character-api-key-here'
    )

    print("KrakenHashes User API - Example Workflow\n")

    # 1. Create a client
    print("1. Creating a new client...")
    client_obj = client.create_client(
        name="Example Corp",
        description="Example client for testing"
    )
    client_id = client_obj['id']
    print(f"   Created client: {client_obj['name']} (ID: {client_id})")

    # 2. List available hash types
    print("\n2. Listing hash types...")
    hash_types = client.list_hash_types(enabled_only=True)
    print(f"   Found {hash_types['total']} enabled hash types")
    # Show first 5
    for ht in hash_types['hash_types'][:5]:
        print(f"   - {ht['id']}: {ht['name']}")

    # 3. Upload a hashlist (assuming you have a file)
    print("\n3. Uploading a hashlist...")
    # Uncomment and modify when you have a hash file:
    # hashlist = client.create_hashlist(
    #     name="Example Hashes",
    #     client_id=client_id,
    #     hash_type=1000,  # NTLM
    #     file_path="/path/to/hashes.txt"
    # )
    # print(f"   Uploaded hashlist: {hashlist['name']} (ID: {hashlist['id']})")

    # 4. Generate an agent voucher
    print("\n4. Generating agent registration voucher...")
    voucher = client.generate_voucher(
        expires_in=604800,  # 7 days
        is_continuous=False
    )
    print(f"   Voucher code: {voucher['code']}")
    print(f"   Expires at: {voucher.get('expires_at', 'N/A')}")
    print("\n   Use this code to register a new agent:")
    print(f"   ./agent --host your-server:31337 --claim {voucher['code']}")

    # 5. List agents
    print("\n5. Listing agents...")
    agents = client.list_agents()
    print(f"   Total agents: {agents['total']}")
    for agent in agents['agents']:
        print(f"   - {agent['name']}: {agent['status']} "
              f"({len(agent['hardware'].get('gpus', []))} GPUs)")

    # 6. List workflows
    print("\n6. Listing workflows...")
    workflows = client.list_workflows()
    print(f"   Total workflows: {workflows['total']}")
    for wf in workflows['workflows']:
        print(f"   - {wf['name']} ({len(wf.get('steps', []))} steps)")

    print("\n✓ Example workflow completed successfully!")


if __name__ == '__main__':
    try:
        example_workflow()
    except requests.RequestException as e:
        print(f"\n✗ Error: {e}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("\n\nInterrupted by user", file=sys.stderr)
        sys.exit(130)
