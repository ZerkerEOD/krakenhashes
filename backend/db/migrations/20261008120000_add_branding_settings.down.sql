DELETE FROM system_settings
WHERE key IN (
    'branding_app_name',
    'branding_page_title',
    'branding_primary_color',
    'branding_secondary_color',
    'branding_logo_file',
    'branding_favicon_file'
);
