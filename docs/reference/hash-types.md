# Hash Types Reference

This document provides comprehensive information about the hash types supported by KrakenHashes for password cracking operations.

## Overview

Hash types in KrakenHashes correspond to hashcat modes and define the algorithm and format used to hash passwords. Each hash type has a unique numerical identifier (mode number) that matches hashcat's mode system. KrakenHashes currently supports **504 different hash types** covering a wide range of algorithms and applications.

## Hash Type Categories

### Fast Hash Types
Fast hash algorithms are computationally inexpensive and can be cracked at high speeds. These include basic cryptographic hashes and simple salted variants.

**Examples:**
- MD5 (mode 0)
- SHA1 (mode 100)
- SHA2-256 (mode 1400)
- SHA2-512 (mode 1700)
- NTLM (mode 1000)

### Slow Hash Types
Slow hash algorithms are intentionally computationally expensive to resist brute-force attacks. These require significantly more time and resources to crack but provide better security. KrakenHashes supports **132 slow hash types**.

**Examples:**
- bcrypt (mode 3200)
- PBKDF2 variants (modes 10900, 12000, 12100)
- scrypt (mode 8900)
- Argon2 variants
- TrueCrypt/VeraCrypt containers

### Application-Specific Hashes
Many hash types are specific to particular applications, operating systems, or protocols.

**Categories include:**
- **Database Systems**: MySQL, PostgreSQL, Oracle, MSSQL
- **Operating Systems**: Windows (NTLM, NetNTLM), Unix/Linux (crypt variants), macOS
- **Web Applications**: WordPress, Joomla, Django, Drupal
- **Network Protocols**: WPA/WPA2, Kerberos, SNMP
- **Archive Formats**: ZIP, RAR, 7-Zip
- **Cryptocurrency**: Bitcoin wallets, Ethereum wallets

## Hash Type Structure

Each hash type in KrakenHashes has the following properties:

- **ID**: Unique numerical identifier (hashcat mode number)
- **Name**: Descriptive name of the hash algorithm
- **Example**: Sample hash format showing the expected input structure
- **Needs Processing**: Flag indicating if special preprocessing is required
- **Is Enabled**: Whether the hash type is currently supported
- **Slow**: Flag indicating if this is a computationally expensive algorithm

## Common Hash Types by Use Case

### Web Application Security Testing
| Mode | Algorithm | Common Applications |
|------|-----------|-------------------|
| 400 | phpass | WordPress, phpBB |
| 500 | md5crypt | Traditional Unix systems |
| 1600 | Apache $apr1$ | Apache htpasswd |
| 7900 | Drupal7 | Drupal CMS |
| 124 | Django (SHA-1) | Django framework |
| 10000 | Django (PBKDF2-SHA256) | Modern Django |

### Enterprise/Active Directory
| Mode | Algorithm | Use Case |
|------|-----------|----------|
| 1000 | NTLM | Windows password hashes |
| 3000 | LM | Legacy LAN Manager hashes (pre-Vista) |
| 5500 | NetNTLMv1 | Network authentication |
| 5600 | NetNTLMv2 | Network authentication |
| 7500 | Kerberos 5 AS-REQ | Domain authentication |
| 13100 | Kerberos 5 TGS-REP | Ticket Granting Service |

### Database Security
| Mode | Algorithm | Database |
|------|-----------|----------|
| 12 | PostgreSQL | PostgreSQL MD5 |
| 300 | MySQL4.1/MySQL5 | MySQL SHA1 |
| 200 | MySQL323 | Legacy MySQL |
| 131 | MSSQL (2000) | SQL Server |
| 132 | MSSQL (2005) | SQL Server |
| 1731 | MSSQL (2012, 2014) | SQL Server |

### File/Archive Security
| Mode | Algorithm | Application |
|------|-----------|-------------|
| 13000 | RAR5 | WinRAR archives |
| 12500 | RAR3-hp | WinRAR archives |
| 11600 | 7-Zip | 7-Zip archives |
| 17200 | PKZIP (Compressed) | ZIP archives |
| 17210 | PKZIP (Uncompressed) | ZIP archives |

## Performance Considerations

### Speed Classifications

**Ultra-Fast (>1 billion attempts/sec on modern GPUs):**
- MD4 (900), MD5 (0), SHA1 (100)
- Simple salted variants

**Fast (100M-1B attempts/sec):**
- SHA2 variants, NTLM
- Basic application-specific hashes

**Medium (1M-100M attempts/sec):**
- Multiple iteration hashes
- Complex salted variants

**Slow (<1M attempts/sec):**
- PBKDF2, bcrypt, scrypt
- Full disk encryption
- Cryptocurrency wallets

### Resource Requirements

**GPU Memory Considerations:**
- Large wordlists may require significant GPU memory
- Rule-based attacks can multiply memory requirements
- Some hash types have higher per-hash memory overhead

**CPU vs GPU Performance:**
- Most hash types benefit significantly from GPU acceleration
- Some algorithms may perform better on CPU for small datasets
- Hybrid attacks may utilize both CPU and GPU resources

## Hash Format Examples

### Basic Formats
```
MD5:           8743b52063cd84097a65d1633f5c74f5
SHA1:          b89eaac7e61417341b710b727768294d0e6a277b
SHA256:        127e6fbfe24a750e72930c220a8e138275656b8e5d8f48a98c3c92df2caba935
NTLM:          b4b9b02e6f09a9bd760f388b67351e2b
```

### Salted Formats
```
md5($pass.$salt):     01dfae6e5d4d90d9892622325959afbe:7050461
sha1($salt.$pass):    cac35ec206d868b7d7cb0b55f31d9425b075082b:5363620024
sha256($pass.$salt):  c73d08de890479518ed60cf670d17faa26a4a71f995c1dcc978165399401a6c4:53743528
```

### Application-Specific Formats
```
WordPress:      $P$984478476IagS59wHZvyQMArzfx58u.
bcrypt:         $2a$05$LhayLxezLhK1LhWvKxCyLOj0j1u.Kj0jZ0pEmm134uzrQlFvQJLF6
Django:         pbkdf2_sha256$20000$H0dPx8NeajVu$GiC4k5kqbbR9qWBlsRgDywNqC2vd9kqfk7zdorEnNas=
NetNTLMv2:      admin::N46iSNekpT:08ca45b7d7ea58ee:88dcbe4446168966a153a0064958dac6
```

### Username and Domain Extraction

KrakenHashes v1.1+ automatically extracts username and domain information from hash formats that contain identity data. This enables better client reporting, user tracking, and prioritization of high-value accounts.

#### Supported Hash Types

| Hash Type | Mode | Extraction Pattern | Example |
|-----------|------|-------------------|---------|
| NTLM | 1000 | `DOMAIN\username:sid:LM:NT:::` | `CONTOSO\Administrator:500:...` |
| NetNTLMv1 | 5500 | `username::domain:challenge:response` | `alice::CORP:1122334455667788:...` |
| NetNTLMv2 | 5600 | `username::domain:challenge:response` | `bob::ENTERPRISE:abcdef01:...` |
| Kerberos AS-REP | 18200 | `$krb5asrep$23$user@domain:hash` | `$krb5asrep$23$john@CORP.COM:...` |
| LastPass | 6800 | `hash:iterations:email` | `a1b2c3...:500:user@example.com` |
| DCC/MS Cache | 1100 | `hash:username` | `a1b2c3...:jsmith` |

#### Domain Formats

The system recognizes two standard domain notation formats:

**Windows/NetBIOS Style:**
```
DOMAIN\username    →    username: "username", domain: "DOMAIN"
CORP\alice         →    username: "alice", domain: "CORP"
```

**Kerberos/Email Style:**
```
user@domain.com         →    username: "user", domain: "domain.com"
john@CORP.LOCAL         →    username: "john", domain: "CORP.LOCAL"
admin@sales.corp.com    →    username: "admin", domain: "sales.corp.com"
```

#### Machine Account Support

Machine accounts (computer accounts) are identified by a trailing `$` character. The extraction system preserves this indicator:

```
CONTOSO\COMPUTER01$     →    username: "COMPUTER01$", domain: "CONTOSO"
WKS01$::DOMAIN:...      →    username: "WKS01$", domain: "DOMAIN"
SRV-WEB$@CORP.LOCAL     →    username: "SRV-WEB$", domain: "CORP.LOCAL"
```

**Note**: Machine accounts are important security indicators as they can provide lateral movement opportunities in domain environments.

#### Fallback Extraction

For hash types without custom extractors, a heuristic approach attempts extraction:

1. Checks for common separators (`:`, `\`, `@`)
2. Identifies potential username patterns
3. Preserves special characters like `$` for machine accounts
4. Stores `NULL` for domain when not detected

### LM Hash Special Processing (v1.2.1+)

LM (LAN Manager) hashes have unique characteristics that require special handling in KrakenHashes. This section explains the structure, weaknesses, and processing approach.

#### Structure and Weaknesses

**Hash Structure:**
- **Full hash**: 32 hexadecimal characters
- **Two halves**: Each 16 characters (representing 7-character DES-encrypted password segments)
- **Maximum password length**: 14 characters total (7 + 7)

**Example:**
```
Full LM Hash: 01FC5A6BE7BC6929AAD3B435B51404EE
First Half:   01FC5A6BE7BC6929 (represents chars 1-7: "PASSWOR")
Second Half:  AAD3B435B51404EE (represents chars 8-14 or blank)
```

**Critical Weaknesses:**
1. **Uppercase Conversion**: Passwords are converted to uppercase before hashing, eliminating case complexity
2. **Split Processing**: Each 7-character half is encrypted independently using DES
3. **No Salting**: All identical password halves produce identical hashes
4. **Weak Encryption**: DES encryption is cryptographically weak and fast to brute-force

**Security Impact:**
- **Keyspace Reduction**: Uppercase-only dramatically reduces combinations (26 vs 52 for letters)
- **Parallel Cracking**: Each half can be cracked independently in parallel
- **Pattern Recognition**: Common password halves can be precomputed (rainbow tables)
- **Maximum 7-char Complexity**: Each half limited to 7 characters reduces search space

#### KrakenHashes Processing

**Agent Download Behavior:**

KrakenHashes processes LM hashes differently from other hash types to optimize cracking efficiency:

1. **Hash Splitting**: Backend splits each 32-char LM hash into two 16-char halves
2. **Unique Halves**: System sends only unique 16-char halves to agents (not full 32-char hashes)
3. **Automatic Deduplication**: Common halves (like blank constant) appear only once
4. **Hashcat Processing**: Agents crack each 16-char half as an independent hash

**Why This Approach:**
- **Hashcat Requirement**: Hashcat mode 3000 expects 16-char halves, not 32-char full hashes
- **Efficiency**: Deduplication eliminates redundant work on common password halves
- **Parallel Capability**: Two halves can be cracked simultaneously on different agents
- **Partial Crack Support**: System can track when only one half is cracked

**Example Workflow:**
```
Upload: DOMAIN\user:500:01FC5A6BE7BC6929AAD3B435B51404EE:hash::: (pwdump format)

Processing:
1. Extract LM hash: 01FC5A6BE7BC6929AAD3B435B51404EE
2. Split into halves:
   - First:  01FC5A6BE7BC6929
   - Second: AAD3B435B51404EE
3. Check second half = blank constant (aad3b435b51404ee)
4. Send only first half to agents for cracking
5. Agents crack: 01FC5A6BE7BC6929 → "PASSWOR"

Result: Partial crack (first 7 chars known)
```

#### Partial Crack Tracking

KrakenHashes v1.2.1+ tracks partial crack status for LM hashes in the `lm_hash_metadata` table:

**Partial Crack States:**
- **First Half Only**: First 7 characters cracked (e.g., `PASSWOR`)
- **Second Half Only**: Last 7 characters cracked (e.g., `D123`)
- **Both Halves**: Full password assembled (e.g., `PASSWORD123`)

**Database Storage:**
```sql
lm_hash_metadata table:
- hash_id: Reference to main hash
- first_half_cracked: Boolean
- first_half_password: VARCHAR(7)  -- Up to 7 chars
- second_half_cracked: Boolean
- second_half_password: VARCHAR(7) -- Up to 7 chars
```

**Strategic Value:**
Partial cracks provide significant intelligence:
- Knowing one half reduces remaining keyspace from ~95^14 to ~95^7 combinations
- Pattern recognition helps inform attacks on other hashes
- Can generate targeted masks for NTLM attacks (see Analytics section)

See [Hashlists - LM Hash Special Processing](../user-guide/hashlists.md#lm-hash-special-processing-v121) for upload and processing details.

#### Pwdump Format Support

KrakenHashes automatically detects and processes pwdump-format files containing both LM and NTLM hashes:

**Pwdump Format:**
```
DOMAIN\username:RID:LM_HASH:NTLM_HASH:::
```

**Example:**
```
CORP\Administrator:500:01FC5A6BE7BC6929AAD3B435B51404EE:0CB6948805F797BF2A82807973B89537:::
CORP\Guest:501:AAD3B435B51404EEAAD3B435B51404EE:31D6CFE0D16AE931B73C59D7E0C089C0:::
```

**Automatic Linking:**
- System can create separate but linked LM and NTLM hashlists
- Links maintained by matching username and domain
- Analytics show correlation between LM and NTLM crack status
- See [Hashlists - LM/NTLM Linked Hashlists](../user-guide/hashlists.md#lmntlm-linked-hashlists-v121)

#### Blank LM Hash Constant

The constant `aad3b435b51404eeaad3b435b51404ee` appears in pwdump files when:

**Causes:**
- Password was blank/empty when LM hash was created
- LM hash storage was disabled (Windows Vista+ default setting)
- Account created after LM storage was disabled via Group Policy

**KrakenHashes Handling:**
- Automatically detected and filtered during processing
- Not counted in hash totals
- Not sent to agents for cracking
- Displayed in upload dialog for user awareness

**Group Policy Setting:**
```
"Network security: Do not store LAN Manager hash value on next password change"
```
When enabled, Windows stores only the blank constant instead of the actual LM hash.

#### Security Implications and Deprecation

**Industry Status:**
- **Deprecated**: Microsoft deprecated LM hashing with Windows Vista (2007)
- **Disabled by Default**: Windows Vista+ do not store LM hashes by default
- **Legacy Support Only**: May still be found in older Active Directory environments

**Security Risk Assessment:**
- **CRITICAL**: Presence of LM hashes indicates severe security misconfiguration
- **Fast Cracking**: LM hashes can be cracked in seconds to minutes with modern hardware
- **No Defense**: Uppercase conversion and 7-char splitting make LM indefensible
- **Compliance Violations**: Many security frameworks prohibit LM hash storage

**Remediation:**
1. **Disable LM Hash Storage**: Configure Group Policy on all domain controllers
2. **Force Password Resets**: Require all users to change passwords after policy change
3. **Verify Removal**: Audit domain controllers to confirm LM hashes are no longer stored
4. **Network Segmentation**: Isolate any systems that still require LM support

**Analytics Integration:**
KrakenHashes automatically generates CRITICAL severity recommendations when LM hashes are detected in analytics reports. See [Analytics Reports - Windows Hash Analytics](../user-guide/analytics-reports.md#windows-hash-analytics-v121).

## Hash Type Identification

### Automatic Detection
KrakenHashes can often identify hash types based on:
- **Length**: Different algorithms produce different output lengths
- **Character set**: Hex vs base64 vs custom encoding
- **Delimiters**: Colons, dollar signs, or other separators
- **Prefixes**: Algorithm identifiers like `$2a$`, `{SHA}`, etc.

### Manual Identification Guidelines

**By Length (hex-encoded):**
- 32 characters: MD5, NTLM, MD4
- 40 characters: SHA1, MySQL4.1
- 56 characters: SHA2-224
- 64 characters: SHA2-256, BLAKE2s-256
- 96 characters: SHA2-384
- 128 characters: SHA2-512, BLAKE2b-512

**By Format Patterns:**
- `$algorithm$`: crypt-style formats (bcrypt, sha512crypt, etc.)
- `{ALGORITHM}`: LDAP-style formats
- `hash:salt`: Simple salted hashes
- Complex delimited formats for specific applications

### Common Identification Mistakes
- Confusing MD5 with NTLM (both 32 hex characters)
- Misidentifying base64-encoded vs hex-encoded hashes
- Not recognizing application-specific wrapper formats

## Best Practices

### Hash Type Selection
1. **Verify the source**: Confirm the application/system that generated the hashes
2. **Check format carefully**: Pay attention to delimiters, prefixes, and encoding
3. **Test with known samples**: Use test hashes to verify correct identification
4. **Consider variations**: Many applications have multiple hash format variants

### Performance Optimization
1. **Start with fast hashes**: Test identification with quick attacks first
2. **Use appropriate wordlists**: Match wordlist complexity to hash strength
3. **Consider slow hash implications**: Budget time appropriately for PBKDF2, bcrypt, etc.
4. **Monitor resource usage**: Slow hashes can consume significant GPU memory

### Security Considerations
1. **Handle sensitive data properly**: Ensure secure storage and transmission
2. **Use appropriate attack methods**: Don't waste resources on over-engineered attacks
3. **Respect rate limits**: Some hash types may benefit from attack rate limiting
4. **Document findings**: Keep records of successful techniques for similar engagements

## Advanced Features

### Processing Requirements
Some hash types require special preprocessing before cracking:
- **NTLM (mode 1000)**: Requires UTF-16LE encoding conversion
- Character set normalization for international passwords
- Case conversion requirements for specific applications

### Multi-Hash Support
KrakenHashes supports attacking multiple hashes of the same type simultaneously:
- Efficient memory usage for large hashlist processing
- Optimized GPU kernels for batch operations
- Progress tracking per individual hash

### Custom Hash Types
For specialized requirements:
- Contact development team for custom hash type implementation
- Provide detailed specification and test vectors
- Consider performance implications for custom algorithms

## Troubleshooting

### Common Issues
1. **Hash not cracking**: Verify hash type identification
2. **Slow performance**: Check if hash type is marked as "slow"
3. **GPU errors**: Some hash types require specific GPU capabilities
4. **Memory errors**: Large hashlists may exceed available GPU memory

### Getting Help
- Check the hash format against provided examples
- Verify the source application and version
- Test with known password/hash pairs
- Consult the troubleshooting guide for hardware-specific issues

## Complete Hash Type List

For a complete list of all supported hash types with examples, consult the database directly or use the admin interface. The list includes detailed information about:
- Algorithm specifications
- Example hash formats
- Performance characteristics
- Special requirements or limitations

---

*Last updated: Based on KrakenHashes database with 504 supported hash types*
*For the most current information, always check the application's hash type management interface*