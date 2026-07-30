-- Remove the BloodHound-derived AD privilege staging column.
ALTER TABLE analytics_reports DROP COLUMN bloodhound_context;
