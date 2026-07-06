/**
 * Represents a client entity in the KrakenHashes system.
 */
export interface Client {
  id: string; // Assuming UUID as string
  name: string;
  description?: string;
  contactInfo?: string;
  dataRetentionMonths?: number | null; // Added: number of months, null means use default
  exclude_from_potfile?: boolean; // Flag to exclude from global potfile
  exclude_from_client_potfile?: boolean; // Flag to exclude from client-specific potfile
  remove_from_global_potfile_on_hashlist_delete?: boolean | null; // Remove from global potfile when hashlist deleted (null = use system default)
  remove_from_client_potfile_on_hashlist_delete?: boolean | null; // Remove from client potfile when hashlist deleted (null = use system default)
  createdAt?: string; // Assuming ISO string format
  updatedAt?: string; // Assuming ISO string format
  cracked_count?: number; // Count of cracked hashes for this client
  wordlist_count?: number; // Total client wordlists + potfile + association wordlists
}

/**
 * Represents a client-specific uploaded wordlist.
 */
export interface ClientWordlist {
  id: string;
  client_id: string;
  file_name: string;
  file_size: number;
  line_count: number;
  md5_hash?: string;
  created_at: string;
}

/**
 * Represents a client's auto-generated potfile.
 */
export interface ClientPotfile {
  id: number;
  client_id: string;
  file_path: string;
  file_size: number;
  line_count: number;
  md5_hash?: string;
  created_at: string;
  updated_at: string;
}

/**
 * Represents an association wordlist with its parent hashlist name.
 */
export interface AssociationWordlistWithHashlist {
  id: string;
  hashlist_id: number;
  hashlist_name?: string;
  file_name: string;
  file_size: number;
  line_count: number;
  md5_hash?: string;
  created_at: string;
}