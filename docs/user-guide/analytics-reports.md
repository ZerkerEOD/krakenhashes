# Analytics Reports

## Overview

Analytics Reports provide comprehensive statistical analysis of cracked passwords across multiple dimensions. These reports help security professionals identify password patterns, assess organizational security posture, and provide evidence-based recommendations to clients.

### Key Features

- **13 Analytics Sections**: From length distribution to strength metrics
- **Domain-Based Filtering**: Analyze password patterns by domain in multi-domain environments
- **Custom Pattern Detection**: Define and track organization-specific password patterns
- **Pre-Calculated Analytics**: Fast report generation with no performance impact during analysis
- **Client-Specific Reports**: Generate reports for specific clients or across multiple engagements
- **Time-Based Filtering**: Analyze trends over specific time periods

### When to Use Analytics Reports

- **Client Reporting**: Generate comprehensive password analysis for security assessments
- **Trend Analysis**: Track password security improvements over time
- **Multi-Domain Environments**: Compare password practices across different organizational units
- **Security Posture Assessment**: Identify weaknesses and improvement areas
- **Compliance Reporting**: Document password policy compliance

## Generating Analytics Reports

### Creating a New Report

1. **Navigate to Analytics**
   - Go to **Clients** in the main menu
   - Select the client you want to analyze
   - Click **Generate Analytics Report**

2. **Select Hashlists**
   - Choose which hashlists to include in the analysis
   - Reports can analyze one or multiple hashlists
   - Hashlists must be from the same client

3. **Generate Report**
   - Click **Generate Report**
   - The system will calculate all analytics sections
   - Generation typically takes 5-15 seconds depending on dataset size

4. **View Report**
   - Once generated, the report appears in the analytics list
   - Click **View Report** to see the full analysis
   - Reports are saved and can be re-viewed at any time

### Report Scope Options

#### Client-Specific Reports
- Analyze all hashlists for a specific client
- Compare password practices across different engagements
- Track improvements over time for the same organization

#### Hashlist-Specific Reports
- Focus analysis on a specific hashlist
- Useful for targeted assessments (e.g., executive accounts only)
- Compare different departments or organizational units

## Understanding Analytics Sections

### Overview Statistics

The **Overview** section provides high-level metrics:

- **Total Hashes**: Number of hashes analyzed
- **Total Cracked**: Number of successfully cracked passwords
- **Crack Rate**: Percentage of hashes cracked
- **Hash Mode Breakdown**: Distribution of hash types (NTLM, NetNTLMv2, etc.)

**Use Case**: Executive summary statistics for client reports

### Domain-Based Filtering 🆕

**New in v1.2+**: Analytics reports now support domain-based filtering for multi-domain environments.

#### How It Works

When your analyzed hashlists contain hashes with domain information (e.g., NetNTLMv2, NTLM pwdump, Kerberos), the system:

1. **Automatically extracts domains** from hash formats
2. **Creates dynamic tabs** at the top of the Overview section:
   - **"All" tab**: Shows aggregated statistics across all domains
   - **Domain tabs**: One tab per unique domain found (e.g., `acme.local`, `example.com`)
3. **Filters all analytics sections** when a domain is selected

#### Domain Breakdown Table

When the **"All" tab** is selected, a domain breakdown table appears showing:

| Domain | Total Hashes | Cracked | Percentage |
|--------|--------------|---------|------------|
| acme.local | 14,638 | 14,638 | 100.00% |
| contoso.local | 14,638 | 14,638 | 100.00% |
| example.com | 14,638 | 14,638 | 100.00% |

#### Filtering by Domain

**To filter analytics by domain:**

1. Click a domain tab (e.g., `acme.local`)
2. All sections automatically update to show only that domain's data:
   - Overview statistics show only the domain's hashes
   - Hash Mode Breakdown shows only hash types present in that domain
   - All other sections (Length, Complexity, etc.) filter accordingly
3. Click **"All"** to return to aggregated view

#### Use Cases for Domain Filtering

**Multi-Domain Active Directory Environments:**
```
Scenario: Client has acquired multiple companies, each with their own AD domain
Use Domain Filtering to:
- Compare password security between legacy vs. new domains
- Identify which organizational units need security training
- Report on password patterns specific to each business unit
```

**Department/Location Analysis:**
```
Scenario: Large organization with geographic domains (us.corp.com, eu.corp.com, apac.corp.com)
Use Domain Filtering to:
- Compare regional password practices
- Identify location-specific security issues
- Target training to specific regions
```

**Client Reporting:**
```
Scenario: Security assessment covering multiple subsidiaries
Use Domain Filtering to:
- Generate per-subsidiary security reports
- Show executives domain-specific vulnerabilities
- Provide targeted recommendations per organizational unit
```

### Length Distribution

Analyzes password lengths to identify patterns:

- **Distribution Chart**: Visual representation of password lengths
- **Average Length**: Mean password length across cracked passwords
- **Most Common Length**: Mode of the distribution
- **Length Range**: Minimum and maximum password lengths

**Key Metrics:**
- Percentage of passwords under 8 characters
- Percentage meeting typical complexity requirements (12+ characters)
- Distribution curve shape (indicates policy enforcement)

**Example Findings:**
- "78% of passwords are exactly 8 characters, indicating a minimum-length-only policy"
- "No passwords exceed 12 characters, suggesting users choose minimum-required lengths"

### Complexity Analysis

Evaluates password composition and character usage:

- **Character Class Usage**:
  - Lowercase only
  - Uppercase only
  - Numbers only
  - Mixed alphanumeric
  - Special characters

- **Complexity Metrics**:
  - Percentage meeting corporate policies
  - Common character substitutions (e.g., `@` for `a`, `3` for `e`)
  - Entropy calculations

**Example Findings:**
- "45% of passwords use only lowercase letters"
- "92% of passwords with special characters use `!` as the final character"

### Positional Analysis

Examines where different character types appear in passwords:

- **First Character Analysis**: Common starting characters (capital letters, digits)
- **Last Character Analysis**: Common ending patterns (special chars, digits, years)
- **Middle Patterns**: Character placement in password body

**Example Findings:**
- "67% of passwords start with an uppercase letter (indicates capital-first policy)"
- "84% of passwords end with a digit or exclamation point"
- "Year patterns (2023, 2024) commonly appear at password end"

### Pattern Detection

Identifies common password patterns and structures:

- **Keyboard Patterns**: `qwerty`, `asdf`, `12345`
- **Common Sequences**: `abc123`, `password1`
- **Name + Number**: `John2024`, `Alice123`
- **Base Word + Modification**: `Password!`, `Welcome1`

**Pattern Categories:**
- Dictionary words
- Keyboard walks
- Repeating characters
- Sequential characters
- Common substitutions

**Example Findings:**
- "234 passwords follow the pattern [Name][Year]"
- "156 passwords use keyboard walks (qwerty, asdfgh)"

### Username Correlation

Analyzes relationships between usernames and passwords:

- **Username in Password**: Passwords containing the username
- **Partial Username Match**: Password contains part of username
- **Name-Based Passwords**: FirstName/LastName in password
- **Common Variations**: Username + season/year

**Example Findings:**
- "23% of passwords contain the user's first or last name"
- "89 users have password that equals their username"

### Password Reuse

Identifies password reuse across accounts:

- **Reuse Count**: Number of accounts sharing the same password
- **Most Reused Passwords**: Top passwords by usage count
- **Reuse Percentage**: Percentage of users with non-unique passwords

**Example Findings:**
- "The password 'Welcome123' is used by 45 different accounts"
- "34% of all accounts share passwords with at least one other account"

### Temporal Patterns

Examines time-based patterns in passwords:

- **Season References**: `Summer2024`, `Winter23`
- **Month Names**: `January`, `March2024`
- **Year Patterns**: Current year, previous years
- **Date Formats**: `01012024`, `2024-01-01`

**Example Findings:**
- "78% of passwords containing years use the current year (2024)"
- "Seasonal passwords most commonly reference 'Summer' and 'Fall'"

### Mask Analysis

Shows password structure patterns using hashcat mask format:

- **Top Masks**: Most common password structures
- **Mask Distribution**: Frequency of each pattern
- **Complexity by Mask**: Strength assessment per structure

**Mask Format:**
- `?l` = lowercase letter
- `?u` = uppercase letter
- `?d` = digit
- `?s` = special character

**Example Masks:**
```
?u?l?l?l?l?l?l?d    = Capital + 6 lowercase + 1 digit (Welcome1)
?u?l?l?l?l?l?l?l?d?d = Capital + 7 lowercase + 2 digits (Password24)
```

**Example Findings:**
- "Most common mask: ?u?l?l?l?l?l?l?l?d (43% of passwords)"
- "Top 5 masks account for 87% of all cracked passwords"

### Custom Patterns

Track organization-specific password patterns:

- **Company Name Usage**: Passwords containing company name
- **Product Names**: References to company products/services
- **Department Names**: IT, HR, Finance references
- **Custom Keywords**: Any administrator-defined patterns

**Configuration**: Administrators can define custom patterns to track in the Admin interface.

**Example Findings:**
- "123 passwords contain the company name 'Acme'"
- "34% of IT department passwords reference 'admin' or 'root'"

### Strength Metrics

Overall password strength assessment:

- **Weak Passwords**: Crackable in under 1 minute
- **Moderate Passwords**: Crackable in 1 minute to 1 hour
- **Strong Passwords**: Crackable in over 1 hour
- **Very Strong**: Not yet cracked despite extensive attacks

**Strength Calculation Factors:**
- Password length
- Character diversity
- Pattern presence
- Dictionary word usage
- Brute-force resistance estimate

**Example Findings:**
- "89% of cracked passwords classified as 'Weak'"
- "Only 2% of passwords show 'Strong' resistance to attacks"

### Top Passwords

List of most frequently used passwords:

- **Top 10/25/50**: Configurable list size
- **Usage Count**: How many accounts use each password
- **Strength Rating**: Security assessment of each password
- **Pattern Type**: Classification (dictionary, keyboard walk, etc.)

**Example Top Passwords:**
1. `Welcome123` - 45 accounts
2. `Password1!` - 38 accounts
3. `Summer2024` - 29 accounts
4. `Compan!` - 23 accounts

### Windows Hash Analytics (v1.2.1+)

Comprehensive statistics for Windows-related hash types, providing detailed analysis of enterprise authentication security.

**Supported Hash Types:**
- **NTLM** (mode 1000): Current Windows password hashes
- **LM** (mode 3000): Legacy LAN Manager hashes
- **NetNTLMv1** (modes 5500, 27000): Network authentication challenges
- **NetNTLMv2** (modes 5600, 27100): Improved network authentication
- **DCC/MS Cache** (mode 1100): Domain Cached Credentials
- **DCC2/MS Cache 2** (mode 2100): Improved cached credentials
- **Kerberos** (modes 7500, 13100, 18200, 19600, 19700): Domain authentication tickets

#### Overview Card

The overview section provides enterprise-wide Windows authentication metrics:

**Key Metrics:**
- **Total Hash Records**: Count of all Windows hash entries (includes LM, NTLM, and other types)
- **Unique Users**: Distinct usernames across all Windows hashes
- **Cracked**: Number of successfully cracked Windows hashes
- **Success Rate**: Percentage of cracked Windows hashes
- **Linked Pairs**: Number of LM/NTLM pairs linked during upload (if applicable)

**Important Note on Linked Pairs:**
When hashlists are created as linked LM/NTLM pairs (from pwdump format files), the system counts them as ONE hashlist entry in the overview statistics. This prevents double-counting the same user's credentials. Individual hash type cards show raw counts, but the overview reflects the effective count.

**Example:**
```
Overview:
- Total Hash Records: 15,000
- Unique Users: 5,000 (distinct usernames)
- Cracked: 12,500
- Success Rate: 83.33%
- Linked Pairs: 4,800 (LM/NTLM pairs from pwdump upload)
```

#### Hash Type Cards

Individual cards for each hash type show:

**Standard Metrics:**
- Total count
- Cracked count
- Crack percentage

**LM-Specific Metrics:**
- **Length Distribution**: Passwords ≤7 chars vs 8-14 chars (based on hash structure)
- **Partially Cracked**: Count of hashes with only one half cracked (see [LM Partial Cracks](#lm-partial-cracks-v121))

**Kerberos Breakdown:**
- **etype 23 (RC4)**: Weak encryption, vulnerable to brute-force
- **etype 17 (AES128)**: Stronger encryption
- **etype 18 (AES256)**: Strongest encryption

#### Linked Hash Correlation

For hashlists created as linked LM/NTLM pairs, this section shows correlation statistics:

**Correlation States:**
- **Both Cracked**: Both LM and NTLM hashes cracked for the user
- **NTLM Only**: NTLM cracked (implies LM can be derived from password)
- **LM Only**: LM cracked but NTLM still unknown (use LM-to-NTLM masks)
- **Neither Cracked**: Both hashes still uncracked

**Use Case:**
Identify which users have partial compromise and prioritize follow-up attacks. When LM is cracked but NTLM is not, use the LM-to-NTLM mask generation feature to create targeted attacks.

#### Security Recommendations

The system generates automatic recommendations based on Windows hash analysis:

**CRITICAL Severity:**
- **LM Hashes Detected**: Presence of LM hashes indicates legacy authentication enabled
- Recommendation: Disable LM hash storage via Group Policy immediately

**HIGH Severity:**
- **NetNTLMv1 Detected**: Vulnerable to relay attacks
- Recommendation: Upgrade to NetNTLMv2 minimum authentication level

**MEDIUM Severity:**
- **Kerberos RC4 (etype 23)**: Weak encryption
- Recommendation: Enable AES256/AES128 Kerberos encryption

**Use Case**: Provide these recommendations in client reports as evidence-based security improvements.

### Hash Reuse Analysis (v1.2.1+)

Detects when the same hash value appears across multiple user accounts, indicating shared passwords.

**Difference from Password Reuse:**
- **Password Reuse**: Same plaintext password used by one user for multiple accounts
- **Hash Reuse**: Same hash value (same password) shared by multiple different users
- Hash reuse detection works on cracked AND uncracked hashes

#### Key Metrics

**Overview Statistics:**
- **Total Reused**: Number of hash values appearing 2+ times
- **Percentage Reused**: Percentage of total unique hashes that are reused
- **Total Unique**: Count of distinct hash values in the dataset

**Hash Reuse Details Table:**
Sorted by occurrence count (most reused first), showing:

- **Hash Value**: The actual hash (truncated for display)
- **Hash Type**: Algorithm (e.g., NTLM, LM)
- **Password**: Cracked plaintext (if available)
- **User Count**: Number of distinct users with this hash
- **Total Occurrences**: Number of times hash appears across all hashlists
- **Users**: List of affected usernames with domain information

**Example:**
```
Top Reused Hash:
Hash: 5f4dcc3b...
Type: NTLM
Password: password (cracked)
Users: 47 distinct users
Occurrences: 52 (some users appear in multiple hashlists)

Affected Users:
- DOMAIN\user1
- DOMAIN\user2
- CORP\testaccount
- ...
```

#### Domain Filtering

When domain filtering is active, hash reuse analysis filters to show only hashes belonging to the selected domain. This enables:

- **Department-Specific Analysis**: Identify password sharing within organizational units
- **Multi-Domain Comparison**: Compare reuse rates between different domains
- **Targeted Remediation**: Focus password reset efforts on specific domains

#### Security Implications

**High-Risk Scenarios:**
- **Default Passwords**: Same hash across many accounts indicates default/template passwords
- **Service Accounts**: Shared credentials for system accounts
- **IT Admin Practices**: Multiple admins using same password (bad practice)
- **Compromise Blast Radius**: One cracked password = multiple compromised accounts

**Use in Client Reports:**
Highlight top reused hashes as critical findings. If hash is cracked, all affected accounts are compromised. If uncracked, cracking one reveals all.

### LM Partial Cracks (v1.2.1+)

Analyzes LM hashes where only one of the two 7-character halves has been cracked, providing actionable intelligence for completing the crack.

**Background:**
LM hashes consist of two independent 16-character halves (each representing up to 7 characters of the password). Each half can be cracked independently, resulting in "partial cracks" where one half is known but the other is not.

#### Key Metrics

**Overview Statistics:**
- **Total Partial**: Count of LM hashes with exactly one half cracked
- **First Half Only**: Hashes with first 7 characters known
- **Second Half Only**: Hashes with last 7 characters known
- **Percentage Partial**: Percentage of total LM hashes that are partially cracked

**Partial Crack Details Table:**

Shows each partially cracked hash with:

- **Username**: Account name (if available)
- **Domain**: Domain/workgroup (if available)
- **First Half Status**: ✓ cracked or ✗ pending
- **First Half Password**: Up to 7 characters (if cracked)
- **Second Half Status**: ✓ cracked or ✗ pending
- **Second Half Password**: Up to 7 characters (if cracked)
- **Hashlist**: Which hashlist contains this hash

**Example:**
```
User: Administrator
Domain: CORP
First Half: ✓ PASSWORD (cracked)
Second Half: ✗ (pending)
Hashlist: Domain-Controller-LM

Strategic Value: Keyspace reduced from 95^14 to 95^7 combinations
```

#### Strategic Value

**Why Partial Cracks Matter:**

1. **Keyspace Reduction**:
   - Full 14-char LM: ~95^14 combinations (~4.7 × 10^27)
   - One half known: ~95^7 combinations (~6.9 × 10^13)
   - Reduction factor: ~68 trillion times faster

2. **Pattern Recognition**:
   - First half often reveals password pattern (e.g., `WELCOME`)
   - Likely second half: digits/special chars (e.g., `123!`)
   - Inform targeted wordlists for the unknown half

3. **LM-to-NTLM Intelligence**:
   - Partial LM knowledge helps generate NTLM masks
   - See [LM-to-NTLM Masks](#lm-to-ntlm-masks-v121) section

#### Recommended Actions

**For First Half Known:**
- Generate wordlists for second half (common suffixes: `123`, `2024`, `!`)
- Use mask attacks: `?d?d?d?d` (4 digits), `?d?d?d?s` (3 digits + special char)
- Check for keyboard patterns continuing from first half

**For Second Half Known:**
- Generate wordlists for first half (common prefixes: `WELCOME`, `PASSWORD`)
- Use mask attacks based on organizational patterns
- Check for common words + known suffix pattern

**Domain Filtering:**
Partial crack analysis respects domain filtering, allowing targeted analysis of specific organizational units.

### LM-to-NTLM Masks (v1.2.1+)

Leverages cracked LM passwords to generate hashcat masks for attacking stronger NTLM hashes, exploiting the relationship between LM (uppercase, max 14 chars) and NTLM (case-sensitive, case-preserving) password representations.

**Concept:**
When an LM password is cracked, it reveals the uppercase version of the original password (e.g., LM: `PASSWORD123`). The original NTLM password likely uses mixed case (e.g., `Password123`, `PassWord123`, `PASSWORD123`). By analyzing patterns in cracked LM passwords, the system generates targeted masks to crack NTLM efficiently.

#### Key Metrics

**Overview Statistics:**
- **Total LM Cracked**: Count of fully cracked LM passwords
- **Total Masks Generated**: Number of unique mask patterns created
- **Total Estimated Keyspace**: Sum of all mask keyspaces (attempts needed)

**Mask Generation Table:**

For each generated mask, shows:

- **Mask**: Hashcat mask format (e.g., `?u?l?l?l?l?l?l?l?d?d?d`)
- **LM Pattern**: Pattern derived from LM analysis (e.g., `AAAAAADDD`)
- **Count**: Number of LM passwords matching this pattern
- **Percentage**: Percentage of total LM passwords following this pattern
- **Match Percentage**: Estimated NTLM match rate for this mask
- **Estimated Keyspace**: Number of combinations to try
- **Example LM**: Sample LM password showing the pattern

**Example:**
```
Mask: ?u?l?l?l?l?l?l?l?d?d?d
LM Pattern: PASSWORD123
Count: 342 LM passwords
Percentage: 23.4% of LM hashes
Estimated Keyspace: 1,757,600,000 combinations
Example: PASSWORD123 → likely Password123, PassWord123, etc.
```

#### Mask Format

**Hashcat Mask Characters:**
- `?l` = lowercase letter (a-z)
- `?u` = uppercase letter (A-Z)
- `?d` = digit (0-9)
- `?s` = special character (!@#$%^&*)
- `?a` = any character (all of the above)

**Common Patterns:**
```
?u?l?l?l?l?l?l?l?d?d        = "Welcome24" pattern
?u?l?l?l?l?l?l?l?d?d?d?d    = "Password2024" pattern
?u?l?l?l?l?l?l?l?s          = "Password!" pattern
```

#### How Masks Are Generated

1. **Analyze LM Password**: Extract pattern from cracked LM (e.g., `WELCOME2024`)
2. **Identify Components**:
   - First character: uppercase (LM is always uppercase)
   - Next 6 characters: likely lowercase in NTLM
   - Digits: remain digits
   - Special chars: remain special chars (if present)
3. **Generate Mask**: Create hashcat mask representation
4. **Calculate Keyspace**: Estimate attempts needed (26^lowercase × 10^digits)

**Pattern Intelligence:**
- First char usually uppercase (corporate password policies)
- Remaining letters typically lowercase
- Digits and special chars maintain position

#### Using Generated Masks

**Export Masks for Hashcat:**

```bash
# Use top mask from analytics
hashcat -m 1000 -a 3 ntlm_hashes.txt '?u?l?l?l?l?l?l?l?d?d?d'

# Try top 5 masks in sequence
hashcat -m 1000 -a 3 ntlm_hashes.txt '?u?l?l?l?l?l?l?l?d?d?d'
hashcat -m 1000 -a 3 ntlm_hashes.txt '?u?l?l?l?l?l?l?l?d?d?d?d'
hashcat -m 1000 -a 3 ntlm_hashes.txt '?u?l?l?l?l?l?l?l?s'
```

**Strategic Workflow:**

1. **Crack LM hashes first** (much faster)
2. **Generate analytics report** to produce LM-to-NTLM masks
3. **Export top masks** from analytics section
4. **Run mask attacks on NTLM hashes** using generated patterns
5. **Refine based on results** - adjust masks if needed

#### Domain Filtering

When domain filtering is active, masks are generated only from LM passwords in the selected domain, enabling:

- **Domain-Specific Patterns**: Different organizational units may have different password patterns
- **Targeted Attacks**: Use domain-specific masks for maximum efficiency
- **Comparative Analysis**: See which domains have weaker password patterns

#### Performance Considerations

**Keyspace Analysis:**
The system estimates keyspace for each mask to help prioritize:

- **Small Keyspace** (<1 billion): Fast to crack, try first
- **Medium Keyspace** (1B - 100B): Moderate time, worth attempting
- **Large Keyspace** (>100B): Consider refining mask or using wordlists

**Match Percentage:**
Indicates estimated success rate for each mask based on pattern frequency. Higher match percentage = more likely to crack NTLM hashes.

### Recommendations

Automated security recommendations based on analysis:

- **Policy Improvements**: Suggested password policy changes
- **Training Topics**: Areas where user education is needed
- **Technical Controls**: MFA, password managers, monitoring
- **Specific Findings**: Critical issues requiring immediate attention

**Example Recommendations:**
- "Implement minimum 12-character requirement (current avg: 8.3)"
- "Prohibit use of company name in passwords (found in 34% of passwords)"
- "Deploy password manager to reduce reuse (45% reuse rate detected)"

## Using Analytics in Client Reports

### Best Practices for Client Reporting

#### 1. Executive Summary
- Focus on Overall Statistics and Strength Metrics
- Use percentages rather than raw numbers for impact
- Highlight top 3-5 critical findings
- Provide clear, actionable recommendations

#### 2. Technical Details
- Include all relevant analytics sections
- Use visualizations from Length Distribution and Complexity Analysis
- Reference specific patterns and examples (anonymized if needed)
- Document methodology and tools used

#### 3. Comparative Analysis
- Use domain filtering to show differences between business units
- Compare against industry benchmarks
- Track improvements if this is a follow-up assessment
- Show before/after if policies were changed

### Exporting Analytics Data

**Export Options:**
- **PDF Export**: Generate printable client reports (Coming Soon)
- **CSV Export**: Export raw analytics data for further analysis (Coming Soon)
- **Screenshots**: Capture charts and tables for presentations

**Privacy Considerations:**
- Never include actual passwords in client reports
- Anonymize usernames if required by engagement scope
- Use password examples only with explicit permission
- Aggregate sensitive findings to prevent individual identification

### Presentation Tips

**For Executive Audiences:**
- Lead with crack rate percentage and risk level
- Use domain filtering to show business-unit-specific issues
- Focus on business impact and compliance
- Provide clear ROI for recommendations

**For Technical Audiences:**
- Include detailed pattern analysis and mask distributions
- Show specific examples of vulnerable configurations
- Reference technical controls and implementation steps
- Provide timeline and resource estimates for remediation

## Technical Details

### Pre-Calculated Analytics

Analytics reports use pre-calculation to ensure fast performance:

1. **Report Generation**: All analytics calculated when report is created
2. **Domain Analytics**: Separate analytics calculated for each domain
3. **Storage**: Complete analytics stored in database as JSONB
4. **Retrieval**: Instant loading when viewing saved reports

**Performance Characteristics:**
- Generation: 5-15 seconds for 100,000+ hashes
- Viewing: <1 second for any report size
- Domain Filtering**: Client-side switching (instant)

### Domain Extraction Process

Domains are automatically extracted during hashlist upload:

**Supported Hash Formats:**
- **NetNTLMv2 (5600)**: `username::domain:challenge:response`
- **NTLM pwdump (1000)**: `DOMAIN\username:sid:lm:nt:::`
- **Kerberos (18200)**: `$krb5asrep$23$user@domain.com:hash`

**Extraction Details:**
- Domains stored in `hashes.domain` column
- Indexed for fast filtering
- See [Username Extraction Architecture](../reference/architecture/username-extraction.md) for full details

### Database Schema

Analytics reports are stored in the `analytics_reports` table:

```sql
CREATE TABLE analytics_reports (
    id UUID PRIMARY KEY,
    client_id UUID REFERENCES clients(id),
    name VARCHAR(255),
    hashlist_ids INTEGER[],
    analytics_data JSONB,
    total_hashlists INTEGER,
    total_hashes INTEGER,
    total_cracked INTEGER,
    crack_percentage NUMERIC,
    created_at TIMESTAMP,
    created_by UUID REFERENCES users(id)
);
```

**Analytics Data Structure:**
```json
{
  "overview": {...},
  "length_distribution": {...},
  "complexity_analysis": {...},
  "domain_analytics": [
    {
      "domain": "acme.local",
      "overview": {...},
      "length_distribution": {...},
      ...
    }
  ]
}
```

## Troubleshooting

### Report Generation Issues

**Problem**: Report generation fails or times out

**Solutions**:
- Ensure all selected hashlists are in "ready" status
- Try generating with fewer hashlists
- Check backend logs for errors
- Verify database connectivity

**Problem**: Domain tabs don't appear

**Causes**:
- No hashes in the hashlists contain domain information
- Only hash types without domain support (e.g., MD5, SHA1, bcrypt)
- Domains weren't extracted during upload

**Solutions**:
- Verify hash format supports domains (NetNTLMv2, NTLM, Kerberos)
- Check if `domain` column is populated for hashes
- Re-upload hashlist to trigger domain extraction

### Data Accuracy Issues

**Problem**: Analytics don't match expected values

**Checks**:
- Verify correct hashlists are selected
- Check if potfile was imported from external source
- Ensure time range filter is appropriate
- Verify no hashes were manually modified

## Best Practices

### Generating Meaningful Reports

1. **Select Appropriate Scope**: Include only relevant hashlists for the analysis
2. **Use Domain Filtering**: Leverage domain separation for multi-domain environments
3. **Document Context**: Note any custom patterns defined before generation
4. **Regular Cadence**: Generate reports periodically to track improvements
5. **Combine with POT Data**: Cross-reference with raw POT exports for detailed analysis

### Security and Privacy

1. **Access Control**: Restrict report access to authorized personnel
2. **Data Retention**: Delete old reports per data retention policies
3. **Anonymization**: Remove identifying information from client-facing reports
4. **Secure Export**: Encrypt exported analytics data
5. **Audit Trail**: Track who generates and views reports

### Performance Optimization

1. **Hashlist Selection**: Don't include unnecessary hashlists
2. **Archival**: Archive old reports that are no longer needed
3. **Storage Monitoring**: Monitor database growth from analytics data
4. **Cleanup**: Implement retention policies for old analytics reports

## Summary

Analytics Reports provide comprehensive password analysis across 13 metrics with domain-based filtering for multi-domain environments. By pre-calculating analytics during report generation, the system delivers instant results while enabling detailed security assessments for client reporting and organizational security improvement.

For additional analysis capabilities, see:
- [Analyzing Results](analyzing-results.md) - POT file analysis and export
- [Username Extraction](../reference/architecture/username-extraction.md) - Domain extraction details
- [Client Management](../admin-guide/operations/clients.md) - Client configuration
