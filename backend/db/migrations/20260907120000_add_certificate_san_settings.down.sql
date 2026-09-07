DELETE FROM system_settings
WHERE key IN ('tls_additional_ip_addresses', 'tls_additional_dns_names');
