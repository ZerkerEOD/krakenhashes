-- Migration 000115: Admin Audit Log System (rollback)
-- Drops the audit_log table and its indexes

DROP TABLE IF EXISTS audit_log;
