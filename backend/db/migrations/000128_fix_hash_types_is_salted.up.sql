-- Fix is_salted flag for ~130 hash types missed by migration 000107
--
-- Migration 000107 used ILIKE patterns that missed:
--   - Generic salted types (md5($pass.$salt), sha256($salt.$pass), etc.)
--   - Application types (vBulletin, WordPress, Drupal, etc.)
--   - HMAC types (message/key acts as salt)
--   - Various other salted types
--
-- This caused rule-splitting chunk calculations to produce wildly inflated
-- rulesPerChunk values because benchmark_speed (hash-ops/sec, salt-aware)
-- was compared against keyspacePerRule (password candidates, NOT salt-aware).

-- 1. All types with $salt in algorithm name (~81 types)
-- These explicitly show salt in their formula (e.g., md5($pass.$salt))
UPDATE hash_types SET is_salted = true WHERE name LIKE '%$salt%';

-- 2. All HMAC types — message or key always acts as salt (~32 types)
UPDATE hash_types SET is_salted = true WHERE name ILIKE '%hmac%';

-- 3. All SSHA types (~9 types: nsldaps, LDAP SSHA, AIX ssha, SAP, AS/400)
UPDATE hash_types SET is_salted = true WHERE name ILIKE '%ssha%';

-- 4. All SNMPv3 types (~7 types)
UPDATE hash_types SET is_salted = true WHERE name ILIKE '%snmpv3%';

-- 5. All DPAPI types (~4 types)
UPDATE hash_types SET is_salted = true WHERE name ILIKE '%dpapi%';

-- 6. All SCRAM types (~2 types: MongoDB)
UPDATE hash_types SET is_salted = true WHERE name ILIKE '%scram%';

-- 7. "with Salt" in name (~5 types: NetIQ/Adobe AEM)
UPDATE hash_types SET is_salted = true WHERE name ILIKE '%with salt%';

-- 8. "SALTEDHASH" in name (~2 types: SAP CODVN H)
UPDATE hash_types SET is_salted = true WHERE name ILIKE '%saltedhash%';

-- Explicit IDs for named application types not caught by patterns above
-- These use salts internally but don't show $salt in their display name
UPDATE hash_types SET is_salted = true WHERE id IN (
    -- Forum/CMS platforms
    11,     -- Joomla < 2.5.18
    21,     -- osCommerce, xt:Commerce
    121,    -- SMF (Simple Machines Forum) > v1.1
    400,    -- phpass, WordPress (MD5), Joomla (MD5)
    2611,   -- vBulletin < v3.8.5
    2612,   -- PHPS
    2711,   -- vBulletin >= v3.8.5
    2811,   -- MyBB 1.2+, IPB2+ (Invision Power Board)
    3711,   -- MediaWiki B type
    4521,   -- Redmine
    4522,   -- PunBB
    7900,   -- Drupal7
    8400,   -- WBB3 (Woltlab Burning Board)
    11000,  -- PrestaShop
    13900,  -- OpenCart
    22200,  -- Citrix NetScaler (SHA512)
    30700,  -- AnopeIRCServices (enc_sha256)
    32300,  -- Empire CMS (Admin password)
    35700,  -- phpass(md5($pass))

    -- macOS / Apple
    122,    -- macOS v10.4, v10.5, v10.6
    1722,   -- macOS v10.7
    16200,  -- Apple Secure Notes
    18300,  -- Apple File System (APFS)
    23100,  -- Apple Keychain
    23300,  -- Apple iWork

    -- Windows / Microsoft
    1100,   -- Domain Cached Credentials (DCC), MS Cache
    2100,   -- Domain Cached Credentials 2 (DCC2), MS Cache 2
    13800,  -- Windows Phone 8+ PIN/password
    22100,  -- BitLocker
    28100,  -- Windows Hello PIN/Password
    31500,  -- DCC, MS Cache (NT)
    31600,  -- DCC2, MS Cache 2 (NT)

    -- Network equipment / protocols
    22,     -- Juniper NetScreen/SSG (ScreenOS)
    23,     -- Skype
    24,     -- SolarWinds Serv-U
    125,    -- ArubaOS
    501,    -- Juniper IVE
    4800,   -- iSCSI CHAP authentication, MD5(CHAP)
    5300,   -- IKE-PSK MD5
    5400,   -- IKE-PSK SHA1
    7000,   -- FortiGate (FortiOS)
    8100,   -- Citrix NetScaler (SHA1)
    8300,   -- DNSSEC (NSEC3)
    10200,  -- CRAM-MD5
    11400,  -- SIP digest authentication (MD5)
    16100,  -- TACACS+
    16400,  -- CRAM-MD5 Dovecot
    26300,  -- FortiGate256 (FortiOS256)
    31300,  -- MS SNTP

    -- Enterprise / ERP
    141,    -- Episerver 6.x < .NET 4
    1421,   -- hMailServer
    1441,   -- Episerver 6.x >= .NET 4
    1600,   -- Apache $apr1$ MD5, md5apr1
    6300,   -- AIX {smd5}
    7200,   -- GRUB 2
    7700,   -- SAP CODVN B (BCODE)
    7701,   -- SAP CODVN B (BCODE) fromRFC_READ_TABLE
    7800,   -- SAP CODVN F/G (PASSCODE)
    7801,   -- SAP CODVN F/G (PASSCODE) fromRFC_READ_TABLE
    8000,   -- Sybase ASE
    8500,   -- RACF
    8501,   -- AS/400 DES
    12150,  -- Apache Shiro 1 SHA-512
    12600,  -- ColdFusion 10+
    13500,  -- PeopleSoft PS_TOKEN
    14200,  -- RACF KDFAES
    14400,  -- sha1(CX)
    15000,  -- FileZilla Server >= 0.9.55
    21500,  -- SolarWinds Orion
    21501,  -- SolarWinds Orion v2

    -- Mobile / device
    5800,   -- Samsung Android Password/PIN
    8800,   -- Android FDE <= 4.3
    12900,  -- Android FDE (Samsung DEK)
    18900,  -- Android Backup
    26500,  -- iPhone passcode (UID key + System Keybag)
    22301,  -- Telegram Mobile App Passcode (SHA256)

    -- Wallet / blockchain
    12700,  -- Blockchain, My Wallet
    15200,  -- Blockchain, My Wallet, V2
    18800,  -- Blockchain, My Wallet, Second Password
    22500,  -- MultiBit Classic .key (MD5)
    25500,  -- Stargazer Stellar Wallet XLM
    26600,  -- MetaMask Wallet
    26610,  -- MetaMask Wallet (short hash)
    31900,  -- MetaMask Mobile Wallet
    32500,  -- Dogechain.info Wallet
    34700,  -- Blockchain, My Wallet, Legacy Wallets

    -- Encrypted files / containers
    5200,   -- Password Safe v3
    9000,   -- Password Safe v2
    15400,  -- ChaCha20
    15500,  -- JKS Java Key Store Private Keys (SHA1)
    18400,  -- Open Document Format (ODF) 1.2
    18600,  -- Open Document Format (ODF) 1.1
    22911,  -- RSA/DSA/EC/OpenSSH Private Keys ($0$)
    22921,  -- RSA/DSA/EC/OpenSSH Private Keys ($6$)
    22931,  -- RSA/DSA/EC/OpenSSH Private Keys ($1, $3$)
    22941,  -- RSA/DSA/EC/OpenSSH Private Keys ($4$)
    22951,  -- RSA/DSA/EC/OpenSSH Private Keys ($5$)
    24600,  -- SQLCipher
    24700,  -- Stuffit5
    25900,  -- KNX IP Secure
    26000,  -- Mozilla key3.db
    26100,  -- Mozilla key4.db
    29930,  -- ENCsecurity Datavault (MD5/no keychain)
    29940,  -- ENCsecurity Datavault (MD5/keychain)
    31200,  -- Veeam VBK
    31400,  -- SecureCRT MasterPassphrase v2

    -- Other salted types
    8600,   -- Lotus Notes/Domino 5
    8700,   -- Lotus Notes/Domino 6
    9100,   -- Lotus Notes/Domino 8
    10100,  -- SipHash
    16501,  -- Perl Mojolicious session cookie (HMAC-SHA256)
    19000,  -- QNX /etc/shadow (MD5)
    19100,  -- QNX /etc/shadow (SHA256)
    19200,  -- QNX /etc/shadow (SHA512)
    19210,  -- QNX 7 /etc/shadow (SHA512)
    19500,  -- Ruby on Rails Restful-Authentication
    20711,  -- AuthMe sha256
    20712,  -- RSA Security Analytics / NetWitness (sha256)
    24900,  -- Dahua Authentication MD5
    24901,  -- Besder Authentication MD5
    27200,  -- Ruby on Rails Restful Auth (one round, no sitekey)
    29200   -- Radmin3
);
