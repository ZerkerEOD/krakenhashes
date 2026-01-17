-- Add is_salted column to hash_types table
-- This is used to determine if chunk calculations need salt-aware adjustments
-- For salted hashes, benchmark speed must be divided by remaining hash count

ALTER TABLE hash_types ADD COLUMN is_salted BOOLEAN NOT NULL DEFAULT FALSE;

-- Mark known salted hash types
-- These hash types have per-hash salts where each hash = 1 unique salt
-- Hashcat reports speed as hash_ops/sec (candidate_rate × salt_count) for these

-- Pattern-based classification for common salted types
UPDATE hash_types SET is_salted = TRUE WHERE
    name ILIKE '%crypt%' OR              -- md5crypt, sha512crypt, bcrypt, descrypt, etc.
    name ILIKE '%pbkdf%' OR              -- All PBKDF2 variants
    name ILIKE '%scrypt%' OR             -- scrypt
    name ILIKE '%netntlm%' OR            -- NetNTLMv1, NetNTLMv2
    name ILIKE '%kerberos%' OR           -- All Kerberos types
    name ILIKE '%veracrypt%' OR          -- VeraCrypt
    name ILIKE '%truecrypt%' OR          -- TrueCrypt
    name ILIKE '%wpa%' OR                -- WPA/WPA2/WPA3
    name ILIKE '%argon%' OR              -- Argon2
    name ILIKE '%ecryptfs%' OR           -- eCryptfs
    name ILIKE '%luks%' OR               -- LUKS
    name ILIKE '%filevault%' OR          -- FileVault
    name ILIKE '%itunes%' OR             -- iTunes backup
    name ILIKE '%keepass%' OR            -- KeePass
    name ILIKE '%lastpass%' OR           -- LastPass
    name ILIKE '%1password%' OR          -- 1Password
    name ILIKE '%bitwarden%' OR          -- Bitwarden
    name ILIKE '%ansible%' OR            -- Ansible Vault
    name ILIKE '%bitcoin%' OR            -- Bitcoin/Litecoin wallets
    name ILIKE '%ethereum%' OR           -- Ethereum wallets
    name ILIKE '%electrum%' OR           -- Electrum wallets
    name ILIKE '%cisco%' OR              -- Cisco IOS passwords (most are salted)
    name ILIKE '%mssql%' OR              -- MS SQL Server
    name ILIKE '%postgresql%' OR         -- PostgreSQL
    name ILIKE '%mysql%' OR              -- MySQL (some variants)
    name ILIKE '%oracle%' OR             -- Oracle DB
    name ILIKE '%django%' OR             -- Django
    name ILIKE '%jwt%' OR                -- JWT tokens
    name ILIKE '%pdf%' OR                -- PDF passwords
    name ILIKE '%ms office%' OR          -- MS Office documents
    name ILIKE '%openoffice%' OR         -- OpenOffice documents
    name ILIKE '%7-zip%' OR              -- 7-Zip archives
    name ILIKE '%winzip%' OR             -- WinZip archives
    name ILIKE '%rar%' OR                -- RAR archives
    name ILIKE '%zip%' OR                -- ZIP archives (PKZIP etc)
    name ILIKE '%gpg%' OR                -- GPG/PGP
    name ILIKE '%pgp%';                  -- PGP

-- Explicit IDs for edge cases and verification
-- NetNTLM family
UPDATE hash_types SET is_salted = TRUE WHERE id IN (5500, 5600);

-- Kerberos family
UPDATE hash_types SET is_salted = TRUE WHERE id IN (
    7500,   -- Kerberos 5, etype 23, AS-REQ Pre-Auth
    13100,  -- Kerberos 5, etype 23, TGS-REP
    18200,  -- Kerberos 5, etype 23, AS-REP
    19600,  -- Kerberos 5, etype 17, TGS-REP
    19700,  -- Kerberos 5, etype 18, TGS-REP
    19800,  -- Kerberos 5, etype 17, Pre-Auth
    19900   -- Kerberos 5, etype 18, Pre-Auth
);

-- Unix crypt family (explicit to ensure coverage)
UPDATE hash_types SET is_salted = TRUE WHERE id IN (
    500,    -- md5crypt
    1500,   -- descrypt
    1800,   -- sha512crypt
    3200,   -- bcrypt
    7400,   -- sha256crypt
    7401,   -- MySQL $A$
    8900,   -- scrypt
    12400   -- BSDi Crypt
);

-- Create an index for faster lookups
CREATE INDEX IF NOT EXISTS idx_hash_types_is_salted ON hash_types(is_salted);

-- Add comment for documentation
COMMENT ON COLUMN hash_types.is_salted IS 'Whether this hash type uses per-hash salts. If true, chunk keyspace calculations must divide benchmark speed by remaining hash count.';
