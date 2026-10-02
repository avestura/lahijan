-- 0050_directory_connections.down: reverse of 0050_directory_connections.up.
-- Drop in reverse dependency order.

DROP TABLE IF EXISTS directory_user_links;
DROP TABLE IF EXISTS directory_group_members;
DROP TABLE IF EXISTS directory_groups;
DROP TABLE IF EXISTS directory_connections;
