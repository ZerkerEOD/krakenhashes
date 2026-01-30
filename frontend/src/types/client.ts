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
  enable_client_potfile?: boolean; // Enable client-specific potfile
  contribute_to_global_potfile?: boolean; // When client potfile enabled, also contribute to global
  remove_passwords_on_hashlist_delete?: boolean | null; // Remove passwords from potfile when hashlist deleted (null = use system default)
  createdAt?: string; // Assuming ISO string format
  updatedAt?: string; // Assuming ISO string format
  cracked_count?: number; // Count of cracked hashes for this client
} 