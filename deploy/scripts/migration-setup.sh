#!/bin/bash

set -e

echo "Setting up database migration user..."

# Create migration user if not exists
echo "Creating migration user: $DB_MIGRATION_USER"
PGPASSWORD="$DB_ADMIN_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_ADMIN_USER" -d "$DB_NAME" -c "
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$DB_MIGRATION_USER') THEN
        CREATE USER \"$DB_MIGRATION_USER\" WITH PASSWORD '$DB_MIGRATION_PASSWORD';
    END IF;
END \$\$;"

# Grant migration user privileges
echo "Granting privileges to migration user..."
PGPASSWORD="$DB_ADMIN_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_ADMIN_USER" -d "$DB_NAME" -c "
GRANT CONNECT ON DATABASE \"$DB_NAME\" TO \"$DB_MIGRATION_USER\";
GRANT USAGE, CREATE ON SCHEMA public TO \"$DB_MIGRATION_USER\";
GRANT CREATE ON DATABASE \"$DB_NAME\" TO \"$DB_MIGRATION_USER\";
-- Grant data-plane permissions WITH GRANT OPTION for external DB support
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO \"$DB_MIGRATION_USER\" WITH GRANT OPTION;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO \"$DB_MIGRATION_USER\" WITH GRANT OPTION;"

echo "Migration user setup completed. Running database migration..."
/usr/local/bin/flightctl-db-migrate

# Tables are created as the migration user; grant the application role the same data-plane access
# as deploy/scripts/setup_database_users.sql so template DBs and CI match production.
DB_APP_USER="${DB_APP_USER:-flightctl_app}"
DB_APP_PASSWORD="${DB_APP_PASSWORD:-adminpass}"
echo "Ensuring application user exists: $DB_APP_USER"
PGPASSWORD="$DB_ADMIN_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_ADMIN_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -c "
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$DB_APP_USER') THEN
        EXECUTE format('CREATE USER %I WITH PASSWORD %L CREATEDB', '$DB_APP_USER', '$DB_APP_PASSWORD');
    END IF;
END \$\$;"

echo "Granting application user (${DB_APP_USER}) access to migrated schema..."
PGPASSWORD="$DB_ADMIN_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_ADMIN_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -c "
GRANT USAGE ON SCHEMA public TO \"$DB_APP_USER\";
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO \"$DB_APP_USER\";
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO \"$DB_APP_USER\";
ALTER DEFAULT PRIVILEGES FOR ROLE \"$DB_MIGRATION_USER\" IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO \"$DB_APP_USER\";
ALTER DEFAULT PRIVILEGES FOR ROLE \"$DB_MIGRATION_USER\" IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO \"$DB_APP_USER\";
"