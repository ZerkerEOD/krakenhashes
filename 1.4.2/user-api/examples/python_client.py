#!/usr/bin/env python3
"""
KrakenHashes User API - Python Client Example

This script demonstrates how to use the KrakenHashes User API with Python.
It shows common workflows like creating clients, uploading hashlists,
managing agents, and creating/monitoring jobs.

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
    def create_client(self, name: str, description: Optional[str] = None,
                     domain: Optional[str] = None,
                     data_retention_months: Optional[int] = None) -> Dict:
        """
        Create a new client

        Args:
            name: Client name (required, must be unique)
            description: Optional description
            domain: Optional domain/website
            data_retention_months: Optional data retention period (0=keep forever, null=use default)
        """
        data = {'name': name}
        if description:
            data['description'] = description
        if domain:
            data['domain'] = domain
        if data_retention_months is not None:
            data['data_retention_months'] = data_retention_months
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
                     description: Optional[str] = None,
                     domain: Optional[str] = None) -> Dict:
        """Update a client"""
        data = {}
        if name:
            data['name'] = name
        if description is not None:
            data['description'] = description
        if domain is not None:
            data['domain'] = domain
        response = self._request('PATCH', f'/clients/{client_id}', json=data)
        return response.json()

    def delete_client(self, client_id: str) -> None:
        """Delete a client (only if it has no hashlists)"""
        self._request('DELETE', f'/clients/{client_id}')

    # Hashlist Management
    def create_hashlist(self, name: str, hash_type_id: int, file_path: str,
                       client_id: Optional[str] = None) -> Dict:
        """
        Upload a hashlist

        Args:
            name: Hashlist name (required)
            hash_type_id: Hashcat mode number (required)
            file_path: Path to the file containing hashes (required)
            client_id: Optional client UUID (may be required based on system settings)
        """
        # Remove Content-Type header for multipart upload
        headers = {k: v for k, v in self.session.headers.items()
                  if k.lower() != 'content-type'}

        with open(file_path, 'rb') as f:
            files = {'file': (Path(file_path).name, f, 'text/plain')}
            data = {
                'name': name,
                'hash_type_id': str(hash_type_id)
            }
            if client_id:
                data['client_id'] = client_id

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
                      client_id: Optional[str] = None,
                      search: Optional[str] = None) -> Dict:
        """List hashlists"""
        params = {'page': page, 'page_size': page_size}
        if client_id:
            params['client_id'] = client_id
        if search:
            params['search'] = search
        response = self._request('GET', '/hashlists', params=params)
        return response.json()

    def get_hashlist(self, hashlist_id: int) -> Dict:
        """Get hashlist details"""
        response = self._request('GET', f'/hashlists/{hashlist_id}')
        return response.json()

    def delete_hashlist(self, hashlist_id: int) -> None:
        """Delete a hashlist (only if it has no active jobs)"""
        self._request('DELETE', f'/hashlists/{hashlist_id}')

    # Agent Management
    def generate_voucher(self, is_continuous: bool = False) -> Dict:
        """
        Generate an agent registration voucher

        Args:
            is_continuous: If True, voucher can be used multiple times
        """
        data = {
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

    # Job Management
    def create_job(self, name: str, hashlist_id: int, preset_job_id: int,
                  priority: int = 100, max_agents: int = 0) -> Dict:
        """
        Create a new job

        Args:
            name: Job name (required)
            hashlist_id: ID of the hashlist to crack (required)
            preset_job_id: ID of the preset job configuration (required)
            priority: Job priority (higher = processed first, default: 100, max varies by system)
            max_agents: Maximum concurrent agents (0 = unlimited)
        """
        data = {
            'name': name,
            'hashlist_id': hashlist_id,
            'preset_job_id': preset_job_id,
            'priority': priority,
            'max_agents': max_agents
        }
        response = self._request('POST', '/jobs', json=data)
        return response.json()

    def list_jobs(self, page: int = 1, page_size: int = 20,
                 status: Optional[str] = None,
                 hashlist_id: Optional[int] = None,
                 client_id: Optional[str] = None) -> Dict:
        """
        List jobs with optional filtering

        Args:
            page: Page number (1-indexed)
            page_size: Items per page
            status: Filter by status (pending, running, paused, completed, failed)
            hashlist_id: Filter by hashlist ID
            client_id: Filter by client ID
        """
        params = {'page': page, 'page_size': page_size}
        if status:
            params['status'] = status
        if hashlist_id:
            params['hashlist_id'] = hashlist_id
        if client_id:
            params['client_id'] = client_id
        response = self._request('GET', '/jobs', params=params)
        return response.json()

    def get_job(self, job_id: int) -> Dict:
        """Get job details"""
        response = self._request('GET', f'/jobs/{job_id}')
        return response.json()

    def update_job(self, job_id: int, name: Optional[str] = None,
                  priority: Optional[int] = None,
                  max_agents: Optional[int] = None) -> Dict:
        """
        Update job settings

        Args:
            job_id: ID of the job to update
            name: New job name
            priority: New priority value
            max_agents: New max agents value
        """
        data = {}
        if name:
            data['name'] = name
        if priority is not None:
            data['priority'] = priority
        if max_agents is not None:
            data['max_agents'] = max_agents
        response = self._request('PATCH', f'/jobs/{job_id}', json=data)
        return response.json()

    def get_job_layers(self, job_id: int) -> Dict:
        """
        Get job layers (for increment mode jobs)

        Returns layer information including status and progress.
        """
        response = self._request('GET', f'/jobs/{job_id}/layers')
        return response.json()

    def get_job_layer_tasks(self, job_id: int, layer_id: int,
                           page: int = 1, page_size: int = 20) -> Dict:
        """
        Get tasks for a specific job layer

        Args:
            job_id: ID of the job
            layer_id: ID of the layer
            page: Page number
            page_size: Items per page
        """
        params = {'page': page, 'page_size': page_size}
        response = self._request('GET', f'/jobs/{job_id}/layers/{layer_id}', params=params)
        return response.json()

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
        description="Example client for testing",
        domain="example.com"
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
    #     hash_type_id=1000,  # NTLM
    #     file_path="/path/to/hashes.txt",
    #     client_id=client_id  # Optional based on system settings
    # )
    # print(f"   Uploaded hashlist: {hashlist['name']} (ID: {hashlist['id']})")

    # 4. List preset jobs
    print("\n4. Listing preset jobs...")
    preset_jobs = client.list_preset_jobs()
    print(f"   Found {preset_jobs['total']} preset jobs")
    for pj in preset_jobs['preset_jobs'][:5]:
        print(f"   - {pj['id']}: {pj['name']}")

    # 5. Create a job (if we had a hashlist)
    print("\n5. Creating a job...")
    # Uncomment when you have uploaded a hashlist:
    # job = client.create_job(
    #     name="Attack Example Hashes",
    #     hashlist_id=hashlist['id'],
    #     preset_job_id=1,  # Use first preset job
    #     priority=100,
    #     max_agents=3
    # )
    # print(f"   Created job: {job['name']} (ID: {job['id']})")

    # 6. Generate an agent voucher
    print("\n6. Generating agent registration voucher...")
    voucher = client.generate_voucher(is_continuous=False)
    print(f"   Voucher code: {voucher['code']}")
    print(f"   Active: {voucher.get('is_active', True)}")
    print("\n   Use this code to register a new agent:")
    print(f"   ./agent --host your-server:31337 --claim {voucher['code']}")

    # 7. List agents
    print("\n7. Listing agents...")
    agents = client.list_agents()
    print(f"   Total agents: {agents['total']}")
    for agent in agents['agents']:
        print(f"   - {agent['name']}: {agent['status']} "
              f"({len(agent.get('hardware', {}).get('gpus', []))} GPUs)")

    # 8. List and monitor jobs
    print("\n8. Listing jobs...")
    jobs = client.list_jobs()
    print(f"   Total jobs: {jobs['total']}")
    for job in jobs.get('jobs', [])[:5]:
        print(f"   - {job['name']}: {job['status']} ({job.get('progress', 0):.1f}%)")

    # 9. List workflows
    print("\n9. Listing workflows...")
    workflows = client.list_workflows()
    print(f"   Total workflows: {workflows['total']}")
    for wf in workflows['workflows']:
        print(f"   - {wf['name']} ({len(wf.get('steps', []))} steps)")

    print("\n✓ Example workflow completed successfully!")


def job_monitoring_example():
    """Example showing job creation and monitoring workflow"""

    client = KrakenHashesClient(
        base_url='http://localhost:31337/api/v1',
        email='user@example.com',
        api_key='your-64-character-api-key-here'
    )

    print("KrakenHashes - Job Monitoring Example\n")

    # Assume hashlist ID 1 exists
    hashlist_id = 1

    # Get preset jobs and use the first one
    preset_jobs = client.list_preset_jobs()
    if not preset_jobs['preset_jobs']:
        print("No preset jobs available!")
        return

    preset_job_id = preset_jobs['preset_jobs'][0]['id']
    print(f"Using preset job: {preset_jobs['preset_jobs'][0]['name']}")

    # Create a job
    job = client.create_job(
        name="Monitoring Example Job",
        hashlist_id=hashlist_id,
        preset_job_id=preset_job_id,
        priority=100,
        max_agents=2
    )
    print(f"Created job ID: {job['id']}")

    # Monitor job progress
    import time
    while True:
        job_status = client.get_job(job['id'])
        print(f"Status: {job_status['status']}, Progress: {job_status.get('progress', 0):.1f}%")

        if job_status['status'] in ('completed', 'failed'):
            break

        # Check layers for increment mode jobs
        if job_status.get('increment_mode') != 'off':
            layers = client.get_job_layers(job['id'])
            for layer in layers.get('layers', []):
                print(f"  Layer {layer['id']}: {layer['status']} ({layer.get('progress', 0):.1f}%)")

        time.sleep(10)

    print(f"\nJob completed with status: {job_status['status']}")
    print(f"Cracked: {job_status.get('cracked_count', 0)}/{job_status.get('total_hashes', 0)}")


if __name__ == '__main__':
    try:
        example_workflow()
        # Uncomment to run job monitoring example:
        # job_monitoring_example()
    except requests.RequestException as e:
        print(f"\n✗ Error: {e}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("\n\nInterrupted by user", file=sys.stderr)
        sys.exit(130)
