-- Reverse of 20260928120000_add_network_shares.up.sql.
-- Dropping the table reverts the server to local-only storage. This does
-- NOT move any data back off the share; run the in-app reverse migration
-- (share -> local) before disabling the feature if data lives on the share.
DROP TABLE IF EXISTS network_shares;
