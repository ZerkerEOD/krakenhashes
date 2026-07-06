-- Revert migration 000128: restore is_salted to state after migration 000107
-- Strategy: reset all to false, then re-apply original 000107 patterns/IDs

UPDATE hash_types SET is_salted = false WHERE is_salted = true;

-- Re-apply original 000107 pattern-based classification
UPDATE hash_types SET is_salted = true WHERE
    name ILIKE '%crypt%' OR
    name ILIKE '%pbkdf%' OR
    name ILIKE '%scrypt%' OR
    name ILIKE '%netntlm%' OR
    name ILIKE '%kerberos%' OR
    name ILIKE '%veracrypt%' OR
    name ILIKE '%truecrypt%' OR
    name ILIKE '%wpa%' OR
    name ILIKE '%argon%' OR
    name ILIKE '%ecryptfs%' OR
    name ILIKE '%luks%' OR
    name ILIKE '%filevault%' OR
    name ILIKE '%itunes%' OR
    name ILIKE '%keepass%' OR
    name ILIKE '%lastpass%' OR
    name ILIKE '%1password%' OR
    name ILIKE '%bitwarden%' OR
    name ILIKE '%ansible%' OR
    name ILIKE '%bitcoin%' OR
    name ILIKE '%ethereum%' OR
    name ILIKE '%electrum%' OR
    name ILIKE '%cisco%' OR
    name ILIKE '%mssql%' OR
    name ILIKE '%postgresql%' OR
    name ILIKE '%mysql%' OR
    name ILIKE '%oracle%' OR
    name ILIKE '%django%' OR
    name ILIKE '%jwt%' OR
    name ILIKE '%pdf%' OR
    name ILIKE '%ms office%' OR
    name ILIKE '%openoffice%' OR
    name ILIKE '%7-zip%' OR
    name ILIKE '%winzip%' OR
    name ILIKE '%rar%' OR
    name ILIKE '%zip%' OR
    name ILIKE '%gpg%' OR
    name ILIKE '%pgp%';

-- Re-apply original 000107 explicit IDs
UPDATE hash_types SET is_salted = true WHERE id IN (5500, 5600);
UPDATE hash_types SET is_salted = true WHERE id IN (7500, 13100, 18200, 19600, 19700, 19800, 19900);
UPDATE hash_types SET is_salted = true WHERE id IN (500, 1500, 1800, 3200, 7400, 7401, 8900, 12400);
