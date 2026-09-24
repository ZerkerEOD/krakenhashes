-- GH #90: hashcat's outfile hex-encodes any cracked plain containing the
-- separator (':') or an unprintable byte, e.g. "Pass:word" arrives as
-- "$HEX[506173733a776f7264]". Nothing decoded it on ingest, so those passwords
-- were stored as the literal $HEX[...] text. Ingest now decodes them
-- (hashutils.DecodeHexPlain); this migration applies the same rule to rows
-- stored before that change.
--
-- The rule matches DecodeHexPlain exactly:
--   * only a value that is entirely one uppercase $HEX[<hex>] token;
--   * decoded only when the bytes are valid UTF-8 with no NUL, LF or CR;
--   * otherwise the literal is kept (hashcat reads $HEX[] back natively).
-- convert_from() raises on invalid UTF-8 and on NUL; the EXCEPTION block keeps
-- the literal in that case. decode() raises on odd-length hex, same handling.
--
-- Idempotent: decoded values no longer match the token pattern.

CREATE OR REPLACE FUNCTION kh_decode_hex_plain(plain TEXT) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    raw     BYTEA;
    decoded TEXT;
BEGIN
    IF plain IS NULL OR plain !~ '^\$HEX\[[0-9A-Fa-f]*\]$' THEN
        RETURN plain;
    END IF;
    BEGIN
        raw := decode(substr(plain, 6, length(plain) - 6), 'hex');
        decoded := convert_from(raw, 'UTF8');
    EXCEPTION WHEN OTHERS THEN
        RETURN plain;
    END;
    IF position(E'\n' IN decoded) > 0 OR position(E'\r' IN decoded) > 0 THEN
        RETURN plain;
    END IF;
    RETURN decoded;
END;
$$;

-- Remember each change so loopback sessions can be told about the new value.
CREATE TEMP TABLE hex_plain_changes ON COMMIT DROP AS
SELECT id, password AS old_password, kh_decode_hex_plain(password) AS new_password
FROM hashes
WHERE is_cracked AND password LIKE '$HEX[%';

DELETE FROM hex_plain_changes WHERE new_password = old_password;

UPDATE hashes h
SET password = c.new_password
FROM hex_plain_changes c
WHERE h.id = c.id;

-- A loopback session tracks the plaintexts it has already fed back as
-- md5(password). Record the decoded value too, so the session does not treat a
-- password it already used as a brand-new word.
INSERT INTO loopback_session_plaintexts (session_id, plaintext_md5)
SELECT DISTINCT lsp.session_id, md5(c.new_password)
FROM hex_plain_changes c
JOIN loopback_session_plaintexts lsp ON lsp.plaintext_md5 = md5(c.old_password)
ON CONFLICT DO NOTHING;

-- Staged entries not yet appended to the potfile.
UPDATE potfile_staging
SET password = kh_decode_hex_plain(password)
WHERE NOT processed AND password LIKE '$HEX[%';

DROP TABLE hex_plain_changes;
DROP FUNCTION kh_decode_hex_plain(TEXT);
