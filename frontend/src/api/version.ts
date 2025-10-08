import { apiUrl } from '../config';

export interface VersionInfo {
    release?: string;
    backend: string;
    frontend: string;
    agent: string;
    api: string;
    database: string;
}

export const getVersionInfo = async (): Promise<VersionInfo> => {
    const response = await fetch(`${apiUrl}/api/version`, {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json',
        },
        credentials: 'include',
    });

    if (!response.ok) {
        throw new Error(`Failed to fetch version info: ${response.statusText}`);
    }

    return response.json();
}; 