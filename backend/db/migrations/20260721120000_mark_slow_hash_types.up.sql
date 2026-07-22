-- Mark computationally-expensive ("slow") hashcat hash types as slow = TRUE.
--
-- The hash_types.slow column (migration 000015) was seeded FALSE for EVERY type by
-- 000016, so nothing was ever flagged slow. The scheduler/agent use this flag to decide
-- whether to pass hashcat -S (--slow-candidates): on a slow hash, host-side candidate
-- generation keeps the GPU saturated even when a keyspace-split --limit makes the
-- per-chunk base wordlist small. Without it, a slow hash + large rule/mask amplifier +
-- tiny base slice badly underutilizes the GPU (observed: phpass -m 400 + a 610k-rule
-- file dropped a 5090 from ~16 MH/s to ~131 KH/s as chunks shrank).
--
-- Conservative by design: marking a FAST hash slow could HURT it (-S would starve the
-- GPU), so only genuinely iterated/expensive algorithms are included. Fast variants that
-- merely share a family name are intentionally excluded — PMK-provided WPA (2501/16801),
-- RC4 Kerberos etype 23 (7500/13100), old MS Office MD5/SHA1+RC4 (9700-9820), Citrix
-- NetScaler SHA1/SHA512 (8100/22200), AxCrypt in-memory SHA1 (13300), Blockchain My
-- Wallet v1 low-iteration (12700).
UPDATE hash_types SET slow = TRUE WHERE id IN (
    -- Unix crypt / iterated password hashes
    400,   -- phpass (WordPress/Joomla)
    500,   -- md5crypt
    1600,  -- Apache $apr1$
    1800,  -- sha512crypt
    7400,  -- sha256crypt
    7401,  -- MySQL $A$ (sha256crypt)
    3200,  -- bcrypt
    7900,  -- Drupal7
    15100, -- Juniper/NetBSD sha1crypt
    -- WPA (PBKDF2)
    2500,  -- WPA-EAPOL-PBKDF2
    16800, -- WPA-PMKID-PBKDF2
    -- Domain Cached Credentials 2 (PBKDF2)
    2100, 31600,
    -- PBKDF2 families
    7100,  -- macOS v10.8+ (PBKDF2-SHA512)
    9200,  -- Cisco-IOS $8$ (PBKDF2-SHA256)
    10000, -- Django (PBKDF2-SHA256)
    10900, -- PBKDF2-HMAC-SHA256
    10901, -- RedHat 389-DS (PBKDF2-HMAC-SHA256)
    11900, -- PBKDF2-HMAC-MD5
    12000, -- PBKDF2-HMAC-SHA1
    12001, -- Atlassian (PBKDF2-HMAC-SHA1)
    12100, -- PBKDF2-HMAC-SHA512
    12800, -- MS-AzureSync (PBKDF2-HMAC-SHA256)
    33900, -- Citrix NetScaler (PBKDF2-HMAC-SHA256)
    -- scrypt
    8900,  -- scrypt
    9300,  -- Cisco-IOS $9$ (scrypt)
    15700, -- Ethereum Wallet, SCRYPT
    -- Password managers / boot / misc iterated
    5200,  -- Password Safe v3
    9000,  -- Password Safe v2
    6600,  -- 1Password, agilekeychain
    8200,  -- 1Password, cloudkeychain
    6800,  -- LastPass
    7200,  -- GRUB 2
    13200, -- AxCrypt 1
    -- Crypto wallets (PBKDF2)
    11300, -- Bitcoin/Litecoin wallet.dat
    15200, -- Blockchain, My Wallet, V2
    15600, -- Ethereum Wallet, PBKDF2-HMAC-SHA256
    16300, -- Ethereum Pre-Sale Wallet, PBKDF2-HMAC-SHA256
    -- MS Office 2007-2013
    9400, 9500, 9600,
    -- Archives
    11600, -- 7-Zip
    12500, -- RAR3-hp
    13000, -- RAR5
    -- KeePass
    13400,
    -- Apple iTunes backup (PBKDF2)
    14700, 14800,
    -- DPAPI masterkey (PBKDF2)
    15300, 15310, 15900, 15910,
    -- Kerberos 5 AES (PBKDF2-based etype 17/18)
    19600, 19700, 19800, 19900,
    -- LUKS
    14600,
    -- TrueCrypt (PBKDF2)
    6211, 6212, 6213, 6221, 6222, 6223, 6231, 6232, 6233, 6241, 6242, 6243,
    -- VeraCrypt (PBKDF2)
    13711, 13712, 13713, 13721, 13722, 13723, 13731, 13732, 13733,
    13741, 13742, 13743, 13751, 13752, 13753, 13761, 13762, 13763,
    13771, 13772, 13773, 13781, 13782, 13783
);
